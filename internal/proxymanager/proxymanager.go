package proxymanager

import (
	"bufio"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"time"

	"ktbs.dev/mubeng/pkg/helper"
	"ktbs.dev/mubeng/pkg/mubeng"
)

// ProxyManager defines the proxy list and current proxy position
type ProxyManager struct {
	mu sync.RWMutex

	CurrentIndex int
	filepath     string
	Length       int
	Proxies      []string
}

// Len returns the current number of proxies in the pool (thread-safe).
func (p *ProxyManager) Len() int {
	if p == nil {
		return 0
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.Length
}

// RemoveProxy removes a proxy address from the pool (thread-safe).
func (p *ProxyManager) RemoveProxy(addr string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, proxy := range p.Proxies {
		if proxy == addr {
			// Swap with last element, then truncate
			p.Proxies[i] = p.Proxies[len(p.Proxies)-1]
			p.Proxies = p.Proxies[:len(p.Proxies)-1]
			p.Length = len(p.Proxies)
			if p.CurrentIndex >= p.Length {
				if p.Length > 0 {
					p.CurrentIndex = p.Length - 1
				} else {
					p.CurrentIndex = -1
				}
			}
			return
		}
	}
}

func init() {
	rand.Seed(time.Now().UnixNano())

	manager = &ProxyManager{CurrentIndex: -1}
}

// New initialize ProxyManager
func New(filename string) (*ProxyManager, error) {
	keys := make(map[string]bool)

	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	pm := &ProxyManager{CurrentIndex: -1}
	pm.filepath = filename

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		proxy := helper.Eval(scanner.Text())
		if _, value := keys[proxy]; !value {
			_, err = mubeng.Transport(placeholder.ReplaceAllString(proxy, ""))
			if err == nil {
				keys[proxy] = true
				pm.Proxies = append(pm.Proxies, proxy)
			}
		}
	}

	pm.Length = len(pm.Proxies)
	if pm.Length < 1 {
		return pm, fmt.Errorf("open %s: has no valid proxy URLs", filename)
	}

	return pm, scanner.Err()
}
