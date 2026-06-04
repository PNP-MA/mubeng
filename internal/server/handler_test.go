package server

import (
	"encoding/base64"
	"io/ioutil"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/elazarl/goproxy"
	"github.com/mbndr/logo"
	"ktbs.dev/mubeng/common"
	"ktbs.dev/mubeng/internal/proxymanager"
)

// init initializes the package-level logger for tests.
// In production, Run() in server.go does this; in tests we do it here.
func init() {
	cli := logo.NewReceiver(ioutil.Discard, "")
	cli.Level = logo.DEBUG

	out := logo.NewReceiver(ioutil.Discard, "")
	out.Format = "%s: %s"

	log = logo.NewLogger(cli, out)
}

// TestOnRequestNoDataRace calls onRequest concurrently with Sync=false.
// Before the fix, this triggers the Go race detector on rotate/ok globals.
// After the fix, go test -race must pass.
func TestOnRequestNoDataRace(t *testing.T) {
	rotate = ""
	ok = 1

	pm := &proxymanager.ProxyManager{
		Proxies: []string{
			"http://127.0.0.1:1",
			"http://127.0.0.1:2",
			"http://127.0.0.1:3",
		},
		Length:       3,
		CurrentIndex: -1,
	}

	p := &Proxy{
		Options: &common.Options{
			Sync:         false,
			Rotate:       3,
			Method:       "sequent",
			ProxyManager: pm,
			Timeout:      200 * time.Millisecond,
		},
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, err := http.NewRequest("GET", "http://example.com", nil)
			if err != nil {
				return
			}
			ctx := &goproxy.ProxyCtx{}
			p.onRequest(req, ctx)
		}()
	}
	wg.Wait()
}

// TestOnRequestGoroutinePanic verifies that if a panic occurs inside the
// onRequest goroutine, the deferred recover() catches it and sends the error
// to errChan, preventing the select{} from blocking forever.
//
// Without the fix, a goroutine panic silently terminates the goroutine without
// sending to either channel, causing select to block forever.
// With the fix, recover() sends the panic to errChan, and onRequest returns
// a BadGateway response.
//
// Panic trigger: we set req.Header = nil. The goroutine calls proxy.New(req)
// which does req.Header.Set(...). Writing to a nil map panics with
// "assignment to entry in nil map", which is caught by the deferred recover().
func TestOnRequestGoroutinePanic(t *testing.T) {
	rotate = ""
	ok = 1

	pm := &proxymanager.ProxyManager{
		Proxies: []string{
			"http://127.0.0.1:1",
		},
		Length:       1,
		CurrentIndex: -1,
	}

	p := &Proxy{
		Options: &common.Options{
			Sync:         false,
			Rotate:       1,
			Method:       "sequent",
			ProxyManager: pm,
			Timeout:      100 * time.Millisecond,
		},
	}

	// Create a request with nil Header.
	// proxy.New(req) inside the goroutine calls req.Header.Set() which panics
	// on a nil map. The recover() catches it.
	req, err := http.NewRequest("GET", "http://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header = nil

	ctx := &goproxy.ProxyCtx{}

	// This must NOT block forever. The recover() catches the nil map panic
	// from proxy.New(req) and sends it to errChan.
	done := make(chan struct{}, 1)
	var resp *http.Response
	go func() {
		_, resp = p.onRequest(req, ctx)
		done <- struct{}{}
	}()

	select {
	case <-done:
		if resp == nil {
			t.Fatal("expected a response, got nil")
		}
		t.Logf("onRequest returned (status=%d) — recover worked correctly", resp.StatusCode)
	case <-time.After(10 * time.Second):
		t.Fatal("onRequest blocked forever — goroutine panic not recovered by select")
	}
}

// TestOnRequestAuthMissing verifies that onRequest returns 407 when
// auth is configured but the request has no Proxy-Authorization header.
func TestOnRequestAuthMissing(t *testing.T) {
	rotate = ""
	ok = 1

	pm := &proxymanager.ProxyManager{
		Proxies:      []string{"http://127.0.0.1:1"},
		Length:       1,
		CurrentIndex: -1,
	}

	p := &Proxy{
		Options: &common.Options{
			Auth:         "user:pass",
			Sync:         false,
			Rotate:       1,
			Method:       "sequent",
			ProxyManager: pm,
			Timeout:      100 * time.Millisecond,
		},
	}

	req, err := http.NewRequest("GET", "http://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := &goproxy.ProxyCtx{}

	_, resp := p.onRequest(req, ctx)
	if resp == nil {
		t.Fatal("expected 407 response, got nil")
	}
	if resp.StatusCode != http.StatusProxyAuthRequired {
		t.Fatalf("expected status 407, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Proxy-Authenticate") == "" {
		t.Fatal("expected Proxy-Authenticate header in 407 response")
	}
	t.Logf("onRequest returned 407 as expected")
}

// TestOnRequestAuthInvalid verifies that onRequest returns 407 when
// the Proxy-Authorization header contains wrong credentials.
func TestOnRequestAuthInvalid(t *testing.T) {
	rotate = ""
	ok = 1

	pm := &proxymanager.ProxyManager{
		Proxies:      []string{"http://127.0.0.1:1"},
		Length:       1,
		CurrentIndex: -1,
	}

	p := &Proxy{
		Options: &common.Options{
			Auth:         "user:pass",
			Sync:         false,
			Rotate:       1,
			Method:       "sequent",
			ProxyManager: pm,
			Timeout:      100 * time.Millisecond,
		},
	}

	req, err := http.NewRequest("GET", "http://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("wrong:wrong")))
	ctx := &goproxy.ProxyCtx{}

	_, resp := p.onRequest(req, ctx)
	if resp == nil {
		t.Fatal("expected 407 response, got nil")
	}
	if resp.StatusCode != http.StatusProxyAuthRequired {
		t.Fatalf("expected status 407, got %d", resp.StatusCode)
	}
}

// TestOnRequestAuthValid verifies that onRequest proceeds past the auth
// check when valid credentials are supplied. The request will eventually
// fail with BadGateway because the upstream proxy is unreachable, but
// that proves the auth gate was passed.
func TestOnRequestAuthValid(t *testing.T) {
	rotate = ""
	ok = 1

	pm := &proxymanager.ProxyManager{
		Proxies:      []string{"http://0.0.0.0:1"},
		Length:       1,
		CurrentIndex: -1,
	}

	p := &Proxy{
		Options: &common.Options{
			Auth:         "user:pass",
			Sync:         false,
			Rotate:       1,
			Method:       "sequent",
			ProxyManager: pm,
			Timeout:      100 * time.Millisecond,
		},
	}

	req, err := http.NewRequest("GET", "http://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user:pass")))
	ctx := &goproxy.ProxyCtx{}

	_, resp := p.onRequest(req, ctx)
	if resp == nil {
		t.Fatal("expected a response, got nil")
	}
	if resp.StatusCode == http.StatusProxyAuthRequired {
		t.Fatal("auth gate rejected valid credentials")
	}
	t.Logf("onRequest returned status %d — auth passed, request routed", resp.StatusCode)
}

// TestOnRequestAuthDisabled verifies that when no auth is configured,
// requests proceed without checking for a Proxy-Authorization header.
func TestOnRequestAuthDisabled(t *testing.T) {
	rotate = ""
	ok = 1

	pm := &proxymanager.ProxyManager{
		Proxies:      []string{"http://0.0.0.0:1"},
		Length:       1,
		CurrentIndex: -1,
	}

	p := &Proxy{
		Options: &common.Options{
			Auth:         "",
			Sync:         false,
			Rotate:       1,
			Method:       "sequent",
			ProxyManager: pm,
			Timeout:      100 * time.Millisecond,
		},
	}

	req, err := http.NewRequest("GET", "http://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := &goproxy.ProxyCtx{}

	_, resp := p.onRequest(req, ctx)
	if resp == nil {
		t.Fatal("expected a response, got nil")
	}
	if resp.StatusCode == http.StatusProxyAuthRequired {
		t.Fatal("auth check triggered when no auth configured")
	}
	t.Logf("onRequest returned status %d — no auth configured, request routed", resp.StatusCode)
}

// TestOnRequestErrorPath verifies the normal error path when a proxy is
// unreachable — this proves the select receives from errChan and does not
// block on ordinary errors (improves isolation from the panic test).
func TestOnRequestErrorPath(t *testing.T) {
	rotate = ""
	ok = 1

	pm := &proxymanager.ProxyManager{
		Proxies: []string{
			"http://0.0.0.0:1", // unreachable — will cause connection error
		},
		Length:       1,
		CurrentIndex: -1,
	}

	p := &Proxy{
		Options: &common.Options{
			Sync:         false,
			Rotate:       1,
			Method:       "sequent",
			ProxyManager: pm,
			Timeout:      100 * time.Millisecond,
		},
	}

	req, err := http.NewRequest("GET", "http://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := &goproxy.ProxyCtx{}

	_, resp := p.onRequest(req, ctx)
	if resp == nil {
		t.Fatal("expected a response, got nil")
	}
	t.Logf("onRequest returned with status %d — normal error path works", resp.StatusCode)
}
