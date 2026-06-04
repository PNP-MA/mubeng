package server

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/elazarl/goproxy"
	"github.com/henvic/httpretty"
	"github.com/mbndr/logo"
	"h12.io/socks"
	"ktbs.dev/mubeng/common"
)

type goproxyLogFilter struct{}

func (goproxyLogFilter) Printf(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	if strings.Contains(msg, "Error copying to client") ||
		strings.Contains(msg, "Error responding to client") {
		log.Debugf("goproxy: %s", msg)
		return
	}
	log.Infof("goproxy: %s", msg)
}

func connectDial(network, addr string) (net.Conn, error) {
	timeout := handler.Options.Timeout
	pm := handler.Options.ProxyManager

	// Blacklist: try DIRECT first, fall back to proxy pool on any failure
	if handler.isBlacklisted(addr) {
		if handler.Options.Verbose {
			hostname := extractHostname(addr)
			ips, _ := net.LookupHost(hostname)
			log.Infof("DNS %s -> %s", hostname, strings.Join(ips, ", "))
			log.Infof("%s CONNECT %s -> DIRECT (blacklisted)", "proxy", addr)
		}
		conn, err := net.DialTimeout(network, addr, timeout)
		if err == nil {
			return conn, nil
		}
		log.Warnf("DIRECT connection failed for blacklisted %s: %s — falling back to proxy pool", addr, err)
	}

	if handler.Options != nil && pm != nil && pm.Len() > 0 {
		poolSize := pm.Len()

		// E4: Scale max attempts with pool size
		// Small pools (≤5): try all; Medium (≤20): try 5; Large (>20): try 10
		maxAttempts := 3
		switch {
		case poolSize <= 5:
			maxAttempts = poolSize
		case poolSize <= 20:
			maxAttempts = 5
		default:
			maxAttempts = 10
		}

		// E1: Per-proxy dial timeout — CONNECT should respond in seconds, not 30+
		dialTimeout := timeout
		if dialTimeout > 10*time.Second {
			dialTimeout = 10 * time.Second
		}

		attempts := 0
		for i := 0; i < maxAttempts; i++ {
			proxyAddr := pm.RandomProxy()
			if proxyAddr == "" {
				continue
			}

			// Skip proxies being dialed by another goroutine (avoids burning
			// N×dialTimeout on the same dead proxy under concurrent load).
			if _, loaded := dialTracker.LoadOrStore(proxyAddr, struct{}{}); loaded {
				i-- // retry this slot with a different proxy
				continue
			}

			u, err := url.Parse(proxyAddr)
			if err != nil {
				dialTracker.Delete(proxyAddr)
				continue
			}

			var conn net.Conn
			switch u.Scheme {
			case "http", "https":
				conn, err = dialHTTPProxy(u, addr, dialTimeout)
			case "socks4", "socks4a", "socks5":
				conn, err = dialSOCKSProxy(proxyAddr, network, addr, dialTimeout)
			default:
				dialTracker.Delete(proxyAddr)
				continue
			}
			attempts++
			dialTracker.Delete(proxyAddr)
			if err == nil {
				if handler.Options.Verbose {
					log.Infof("%s CONNECT %s -> via %s", "proxy", addr, proxyAddr)
				}
				// Wrap with MonitoredConn to auto-remove bad proxies on transport errors
				return &MonitoredConn{
					Conn:      conn,
					proxyAddr: proxyAddr,
					onRemove: func(p string) {
						if pm != nil {
							pm.RemoveProxy(p)
							log.Warnf("Self-heal: removed bad proxy %s from pool", p)
						}
					},
				}, nil
			}

			// E2: Remove proxy from pool on dial failure to avoid retrying dead proxies
			if pm != nil {
				pm.RemoveProxy(proxyAddr)
				log.Warnf("Removed bad proxy %s (dial error: %v)", proxyAddr, err)
			}
		}

		// E3: Log pool state on DIRECT fallback
		log.Warnf("No working proxy for %s — falling back to DIRECT (tried %d proxies, %d remaining in pool)",
			addr, attempts, pm.Len())
	} else {
		log.Warnf("No working proxy for %s — falling back to DIRECT (pool empty)", addr)
	}

	return net.DialTimeout(network, addr, timeout)
}

func dialHTTPProxy(u *url.URL, target string, timeout time.Duration) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", u.Host, timeout)
	if err != nil {
		return nil, err
	}
	if timeout > 0 {
		conn.SetDeadline(time.Now().Add(timeout))
	}

	req := &http.Request{
		Method: "CONNECT",
		URL:    &url.URL{Opaque: target},
		Host:   target,
		Header: make(http.Header),
	}

	if u.User != nil {
		username := u.User.Username()
		password, _ := u.User.Password()
		req.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(username+":"+password)))
	}

	if err := req.Write(conn); err != nil {
		conn.Close()
		return nil, err
	}

	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		conn.Close()
		return nil, err
	}
	resp.Body.Close()

	if resp.StatusCode != 200 {
		conn.Close()
		return nil, fmt.Errorf("proxy refused CONNECT: %s", resp.Status)
	}

	conn.SetDeadline(time.Time{})
	return conn, nil
}

func dialSOCKSProxy(proxyAddr, network, addr string, timeout time.Duration) (net.Conn, error) {
	type result struct {
		conn net.Conn
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		conn, err := socks.Dial(proxyAddr)(network, addr)
		ch <- result{conn, err}
	}()
	if timeout > 0 {
		select {
		case r := <-ch:
			return r.conn, r.err
		case <-time.After(timeout):
			return nil, fmt.Errorf("SOCKS dial timeout after %v", timeout)
		}
	} else {
		r := <-ch
		return r.conn, r.err
	}
}

// Run proxy server with a user defined listener.
//
// An active log have 2 receivers, especially stdout and into file if opt.Output isn't empty.
// Then close the proxy server if it receives a signal that interrupts the program.
func Run(opt *common.Options) error {
	cli := logo.NewReceiver(os.Stderr, "")
	cli.Color = true
	cli.Level = logo.DEBUG

	file, _ := logo.Open(opt.Output)
	out := logo.NewReceiver(file, "")
	out.Format = "%s: %s"

	dump = &httpretty.Logger{
		RequestHeader:  true,
		ResponseHeader: true,
		Colors:         true,
	}

	handler = &Proxy{}
	handler.Options = opt

	// Load domain blacklist
	if err := handler.loadBlacklist(opt.Blacklist); err != nil {
		return err
	}

	handler.HTTPProxy = goproxy.NewProxyHttpServer()
	handler.HTTPProxy.Logger = goproxyLogFilter{}
	handler.HTTPProxy.OnRequest().DoFunc(handler.onRequest)
	handler.HTTPProxy.OnRequest().HandleConnectFunc(handler.onConnect)
	handler.HTTPProxy.OnResponse().DoFunc(handler.onResponse)
	handler.HTTPProxy.NonproxyHandler = http.HandlerFunc(nonProxy)
	handler.HTTPProxy.ConnectDial = connectDial

	server = &http.Server{
		Addr:    opt.Address,
		Handler: handler.HTTPProxy,
	}

	log = logo.NewLogger(cli, out)

	if opt.Watch {
		watcher, err := opt.ProxyManager.Watch()
		if err != nil {
			return err
		}
		defer watcher.Close()

		go watch(watcher)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	go interrupt(stop)
	log.Infof("[PID: %d] Starting proxy server on %s", os.Getpid(), opt.Address)
	if err := server.ListenAndServe(); err != nil {
		return err
	}

	return nil
}
