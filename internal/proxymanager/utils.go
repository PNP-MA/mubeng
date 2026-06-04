package proxymanager

import (
	"math/rand"

	"github.com/fsnotify/fsnotify"
)

// NextProxy will navigate the next proxy to use
func (p *ProxyManager) NextProxy() string {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.Proxies) == 0 {
		return ""
	}

	p.CurrentIndex++
	if p.CurrentIndex > len(p.Proxies)-1 {
		p.CurrentIndex = 0
	}

	return p.Proxies[p.CurrentIndex]
}

// RandomProxy will choose a proxy randomly from the list
func (p *ProxyManager) RandomProxy() string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.Proxies) == 0 {
		return ""
	}

	return p.Proxies[rand.Intn(len(p.Proxies))]
}

// Watch proxy file from events
func (p *ProxyManager) Watch() (*fsnotify.Watcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return watcher, err
	}

	if err := watcher.Add(p.filepath); err != nil {
		return watcher, err
	}

	return watcher, nil
}

// Reload proxy pool
func (p *ProxyManager) Reload() error {
	p.mu.Lock()
	filepath := p.filepath
	p.mu.Unlock()

	pm, err := New(filepath)
	if err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.Proxies = pm.Proxies
	p.Length = pm.Length
	p.filepath = pm.filepath
	if p.CurrentIndex >= p.Length {
		if p.Length > 0 {
			p.CurrentIndex = p.Length - 1
		} else {
			p.CurrentIndex = -1
		}
	}

	return nil
}
