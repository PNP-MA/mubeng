package server

import (
	"context"
	"net"
	"net/url"
	"sync/atomic"
	"time"

	"ktbs.dev/mubeng/internal/proxymanager"
)

const cleanTarget = "checkip.amazonaws.com:443"

// PoolCleaner periodically tests proxies in the pool and removes dead ones
// before request goroutines waste time dialing them. Runs on a configurable
// interval with limited concurrency.
type PoolCleaner struct {
	pm          *proxymanager.ProxyManager
	timeout     time.Duration
	target      string
	interval    time.Duration
	concurrency int
}

// NewPoolCleaner creates a background pool cleaner.
// Zero values for timeout, interval, and concurrency are replaced with
// sensible defaults (5s, 60s, 10).
func NewPoolCleaner(pm *proxymanager.ProxyManager, timeout, interval time.Duration, concurrency int) *PoolCleaner {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	if interval <= 0 {
		interval = 60 * time.Second
	}
	if concurrency <= 0 {
		concurrency = 10
	}
	return &PoolCleaner{
		pm:          pm,
		timeout:     timeout,
		target:      cleanTarget,
		interval:    interval,
		concurrency: concurrency,
	}
}

// Run starts the background cleaner loop. Blocks until ctx is cancelled.
// The first clean cycle runs immediately; subsequent cycles run every interval.
func (pc *PoolCleaner) Run(ctx context.Context) {
	ticker := time.NewTicker(pc.interval)
	defer ticker.Stop()

	pc.clean()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pc.clean()
		}
	}
}

func (pc *PoolCleaner) clean() {
	proxies := pc.pm.All()
	if len(proxies) == 0 {
		return
	}

	log.Infof("Pool cleaner: checking %d proxies...", len(proxies))

	sem := make(chan struct{}, pc.concurrency)
	var removed int32

	for _, addr := range proxies {
		// Skip proxies currently being dialed by a request goroutine.
		if _, loaded := dialTracker.LoadOrStore(addr, struct{}{}); loaded {
			continue
		}

		sem <- struct{}{}
		go func(proxyAddr string) {
			defer func() { <-sem }()
			defer dialTracker.Delete(proxyAddr)

			if !pc.test(proxyAddr) {
				pc.pm.RemoveProxy(proxyAddr)
				atomic.AddInt32(&removed, 1)
				log.Warnf("Pool cleaner: removed dead proxy %s", proxyAddr)
			}
		}(addr)
	}

	// Drain semaphore (wait for all goroutines to finish).
	for i := 0; i < cap(sem); i++ {
		sem <- struct{}{}
	}

	remaining := pc.pm.Len()
	r := atomic.LoadInt32(&removed)
	log.Infof("Pool cleaner: done — %d proxies remaining (%d removed)", remaining, r)
}

func (pc *PoolCleaner) test(proxyAddr string) bool {
	u, err := url.Parse(proxyAddr)
	if err != nil {
		return false
	}

	var conn net.Conn
	switch u.Scheme {
	case "http", "https":
		conn, err = dialHTTPProxy(u, pc.target, pc.timeout)
	case "socks4", "socks4a", "socks5":
		conn, err = dialSOCKSProxy(proxyAddr, "tcp", pc.target, pc.timeout)
	default:
		return false
	}
	if conn != nil {
		conn.Close()
	}
	return err == nil
}
