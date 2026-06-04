package checker

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"ktbs.dev/mubeng/common"
	"ktbs.dev/mubeng/internal/proxymanager"
)

// forwardProxy creates an HTTP forward proxy handler that forwards
// all requests to the given target URL. Uses DisableKeepAlives so
// connections are closed immediately after each request.
func forwardProxy(target *url.URL) http.Handler {
	tr := &http.Transport{DisableKeepAlives: true}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		outReq, err := http.NewRequest(r.Method, target.String(), r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for k, v := range r.Header {
			outReq.Header[k] = v
		}

		resp, err := tr.RoundTrip(outReq)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		for k, v := range resp.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	})
}

// passthruProxy creates an HTTP forward proxy that forwards to whatever URL
// appears in the incoming request. Unlike forwardProxy which hardcodes a
// single target, this is used when a test needs the proxy to reach multiple
// different endpoints (e.g. fallback tests).
func passthruProxy() http.Handler {
	tr := &http.Transport{DisableKeepAlives: true}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		outReq, err := http.NewRequest(r.Method, r.URL.String(), r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for k, v := range r.Header {
			outReq.Header[k] = v
		}

		resp, err := tr.RoundTrip(outReq)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		for k, v := range resp.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	})
}

// setupCheckTest creates endpoint and proxy test servers for check() tests.
// Returns the proxy server (whose URL is passed as the proxy address to check()),
// and a cleanup function.
func setupCheckTest(t *testing.T, endpointHandler http.HandlerFunc) (*httptest.Server, func()) {
	t.Helper()

	endpointSrv := httptest.NewServer(endpointHandler)

	endpointURL, err := url.Parse(endpointSrv.URL)
	if err != nil {
		endpointSrv.Close()
		t.Fatalf("failed to parse endpoint URL: %v", err)
	}

	proxySrv := httptest.NewServer(forwardProxy(endpointURL))

	// Override the global endpoints
	oldEndpoints := endpoints
	endpoints = []string{endpointSrv.URL}

	cleanup := func() {
		endpoints = oldEndpoints
		proxySrv.Close()
		endpointSrv.Close()
	}

	return proxySrv, cleanup
}

// TestCheckBodyClosed verifies resp.Body is closed after check() succeeds.
// Uses r.Context().Done() which fires when the server detects the client
// has disconnected (which happens after resp.Body.Close() with Connection: close).
func TestCheckBodyClosed(t *testing.T) {
	ctxClosed := make(chan struct{})

	proxySrv, cleanup := setupCheckTest(t, func(w http.ResponseWriter, r *http.Request) {
		go func() {
			<-r.Context().Done()
			close(ctxClosed)
		}()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"cc":"US","country":"United States","ip":"1.2.3.4"}`)
	})
	defer cleanup()

	_, err := check(proxySrv.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("check() returned unexpected error: %v", err)
	}

	select {
	case <-ctxClosed:
		// resp.Body was closed properly (server detected disconnection)
	case <-time.After(3 * time.Second):
		t.Error("resp.Body was not closed after check() returned successfully")
	}
}

// TestCheckBodyClosedOnReadError verifies resp.Body is closed even when
// ReadAll fails mid-body.
func TestCheckBodyClosedOnReadError(t *testing.T) {
	ctxClosed := make(chan struct{})

	proxySrv, cleanup := setupCheckTest(t, func(w http.ResponseWriter, r *http.Request) {
		go func() {
			<-r.Context().Done()
			close(ctxClosed)
		}()
		// Set Content-Length larger than body to cause a read error on the client
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"cc":"US`) // partial body, not enough to satisfy Content-Length
	})
	defer cleanup()

	_, err := check(proxySrv.URL, 5*time.Second)
	if err == nil {
		t.Error("check() should have returned an error on partial body read")
	}

	select {
	case <-ctxClosed:
		// resp.Body was closed despite read error
	case <-time.After(3 * time.Second):
		t.Error("resp.Body was not closed after check() returned with read error")
	}
}

// TestCheckStatusCodeNon200 verifies check() returns an error for non-200 status codes.
func TestCheckStatusCodeNon200(t *testing.T) {
	proxySrv, cleanup := setupCheckTest(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"cc":"US","country":"United States","ip":"1.2.3.4"}`)
	})
	defer cleanup()

	_, err := check(proxySrv.URL, 5*time.Second)
	if err == nil {
		t.Fatal("check() should have returned error for status 500")
	}
	if !strings.Contains(err.Error(), "unexpected status") {
		t.Errorf("check() error should contain 'unexpected status', got: %v", err)
	}
}

// TestCheckBodyClosedOnNon200 verifies resp.Body is closed even on non-200 status.
func TestCheckBodyClosedOnNon200(t *testing.T) {
	ctxClosed := make(chan struct{})

	proxySrv, cleanup := setupCheckTest(t, func(w http.ResponseWriter, r *http.Request) {
		go func() {
			<-r.Context().Done()
			close(ctxClosed)
		}()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"cc":"US","country":"United States","ip":"1.2.3.4"}`)
	})
	defer cleanup()

	_, err := check(proxySrv.URL, 5*time.Second)
	if err == nil {
		t.Fatal("check() should have returned error for status 500")
	}

	select {
	case <-ctxClosed:
		// resp.Body was closed despite non-200 status
	case <-time.After(3 * time.Second):
		t.Error("resp.Body was not closed after check() returned with non-200 status")
	}
}

// TestCheckCCDeadProxy verifies that with --only-cc, a dead proxy still
// shows "DIED" in verbose mode (error is checked before country filter).
func TestCheckCCDeadProxy(t *testing.T) {
	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Create options with --only-cc, verbose, and a dead proxy
	opt := &common.Options{
		ProxyManager: &proxymanager.ProxyManager{
			Proxies: []string{"http://127.0.0.1:1"},
			Length:  1,
		},
		Countries: []string{"US"},
		Verbose:   true,
		Timeout:   100 * time.Millisecond,
	}

	Do(opt)

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	if !strings.Contains(output, "DIED") {
		t.Errorf("Expected output to contain 'DIED' for dead proxy with --only-cc, got:\n%s", output)
	}
}

// TestCheckFallback verifies check() falls back to the next endpoint when
// the first one returns a non-200 status.
func TestCheckFallback(t *testing.T) {
	firstSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer firstSrv.Close()

	secondSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"cc":"GB","country":"United Kingdom","ip":"5.6.7.8"}`)
	}))
	defer secondSrv.Close()

	proxySrv := httptest.NewServer(passthruProxy())
	defer proxySrv.Close()

	oldEndpoints := endpoints
	endpoints = []string{firstSrv.URL, secondSrv.URL}
	defer func() { endpoints = oldEndpoints }()

	result, err := check(proxySrv.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("check() should have succeeded via fallback, got: %v", err)
	}
	if result.CC != "GB" {
		t.Errorf("expected CC 'GB', got %q", result.CC)
	}
	if result.IP != "5.6.7.8" {
		t.Errorf("expected IP '5.6.7.8', got %q", result.IP)
	}
}

// TestCheckAllEndpointsFailed verifies check() returns a meaningful error
// when every endpoint fails.
func TestCheckAllEndpointsFailed(t *testing.T) {
	firstSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer firstSrv.Close()

	secondSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer secondSrv.Close()

	proxySrv := httptest.NewServer(passthruProxy())
	defer proxySrv.Close()

	oldEndpoints := endpoints
	endpoints = []string{firstSrv.URL, secondSrv.URL}
	defer func() { endpoints = oldEndpoints }()

	_, err := check(proxySrv.URL, 5*time.Second)
	if err == nil {
		t.Fatal("check() should have returned an error when all endpoints fail")
	}
	if !strings.Contains(err.Error(), "all endpoints failed") {
		t.Errorf("error should contain 'all endpoints failed', got: %v", err)
	}
}

// TestCheckFallbackWithIpAPIFormat verifies check() normalizes ip-api.com
// JSON format (countryCode, query) correctly.
func TestCheckFallbackWithIpAPIFormat(t *testing.T) {
	firstSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer firstSrv.Close()

	secondSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"country":"Australia","countryCode":"AU","query":"1.2.3.4"}`)
	}))
	defer secondSrv.Close()

	proxySrv := httptest.NewServer(passthruProxy())
	defer proxySrv.Close()

	oldEndpoints := endpoints
	endpoints = []string{firstSrv.URL, secondSrv.URL}
	defer func() { endpoints = oldEndpoints }()

	result, err := check(proxySrv.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("check() should have succeeded via fallback with ip-api format, got: %v", err)
	}
	if result.CC != "AU" {
		t.Errorf("expected CC 'AU' after normalize, got %q", result.CC)
	}
	if result.IP != "1.2.3.4" {
		t.Errorf("expected IP '1.2.3.4' after normalize, got %q", result.IP)
	}
	if result.Country != "Australia" {
		t.Errorf("expected Country 'Australia', got %q", result.Country)
	}
}

// TestCheckSuccess verifies check() returns correct data from a valid proxy.
func TestCheckSuccess(t *testing.T) {
	proxySrv, cleanup := setupCheckTest(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"cc":"US","country":"United States","ip":"1.2.3.4"}`)
	})
	defer cleanup()

	ip, err := check(proxySrv.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("check() returned unexpected error: %v", err)
	}
	if ip.CC != "US" {
		t.Errorf("expected CC 'US', got %q", ip.CC)
	}
	if ip.IP != "1.2.3.4" {
		t.Errorf("expected IP '1.2.3.4', got %q", ip.IP)
	}
}

// TestCheckWithInvalidAddr verifies check() returns error for invalid address.
func TestCheckWithInvalidAddr(t *testing.T) {
	_, err := check("://invalid", time.Second)
	if err == nil {
		t.Error("check() should have returned error for invalid address")
	}
}

// TestCheckWithTimeout verifies check() returns error for unreachable proxy.
func TestCheckWithTimeout(t *testing.T) {
	_, err := check("http://192.0.2.1:1", 10*time.Millisecond)
	if err == nil {
		t.Error("check() should have returned error for unreachable proxy")
	}
}

// TestIsMatchCC tests the country code matching function.
func TestIsMatchCC(t *testing.T) {
	tests := []struct {
		name string
		cc   []string
		code string
		want bool
	}{
		{"empty code", []string{"US"}, "", false},
		{"match single", []string{"US"}, "US", true},
		{"match case insensitive", []string{"us"}, "US", true},
		{"no match", []string{"US"}, "GB", false},
		{"multiple codes", []string{"US", "GB", "AU"}, "AU", true},
		{"trimmed match", []string{" US "}, "US", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isMatchCC(tt.cc, tt.code); got != tt.want {
				t.Errorf("isMatchCC(%v, %q) = %v, want %v", tt.cc, tt.code, got, tt.want)
			}
		})
	}
}

// TestDoVerboseOutput verifies that Do() in verbose mode prints DIED for dead proxies.
func TestDoVerboseOutput(t *testing.T) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	opt := &common.Options{
		ProxyManager: &proxymanager.ProxyManager{
			Proxies: []string{"http://127.0.0.1:1"},
			Length:  1,
		},
		Verbose: true,
		Timeout: 50 * time.Millisecond,
	}

	Do(opt)

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	if !strings.Contains(output, "DIED") {
		t.Errorf("Expected output to contain 'DIED' in verbose mode, got:\n%s", output)
	}
}

// TestDoNoVerboseSilent verifies that Do() without verbose is silent for dead proxies.
func TestDoNoVerboseSilent(t *testing.T) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	opt := &common.Options{
		ProxyManager: &proxymanager.ProxyManager{
			Proxies: []string{"http://127.0.0.1:1"},
			Length:  1,
		},
		Verbose: false,
		Timeout: 50 * time.Millisecond,
	}

	Do(opt)

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	if output != "" {
		t.Errorf("Expected no output without verbose mode for dead proxy, got:\n%s", output)
	}
}
