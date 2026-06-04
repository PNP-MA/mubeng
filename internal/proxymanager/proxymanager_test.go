package proxymanager

import (
	"io/ioutil"
	"os"
	"sync"
	"testing"
)

func TestNextProxyEmpty(t *testing.T) {
	p := &ProxyManager{
		Proxies:      []string{},
		CurrentIndex: -1,
		Length:       0,
	}

	got := p.NextProxy()
	if got != "" {
		t.Errorf("NextProxy() on empty pool = %q, want empty string", got)
	}
}

func TestRandomProxyEmpty(t *testing.T) {
	p := &ProxyManager{
		Proxies:      []string{},
		CurrentIndex: -1,
		Length:       0,
	}

	got := p.RandomProxy()
	if got != "" {
		t.Errorf("RandomProxy() on empty pool = %q, want empty string", got)
	}
}

func TestRemoveProxyClampsCurrentIndex(t *testing.T) {
	p := &ProxyManager{
		Proxies:      []string{"a", "b", "c"},
		CurrentIndex: -1,
		Length:       3,
	}

	// Advance CurrentIndex to 2 (third element)
	p.NextProxy() // index 0
	p.NextProxy() // index 1
	p.NextProxy() // index 2

	if p.CurrentIndex != 2 {
		t.Fatalf("expected CurrentIndex=2, got %d", p.CurrentIndex)
	}

	// Remove the last element (swap-remove index 2)
	p.RemoveProxy("c")

	// CurrentIndex should be clamped to Length-1 = 1
	if p.CurrentIndex != 1 {
		t.Errorf("after removing last element, CurrentIndex=%d, want 1", p.CurrentIndex)
	}
}

func TestRemoveProxyClampsToZero(t *testing.T) {
	p := &ProxyManager{
		Proxies:      []string{"a"},
		CurrentIndex: 0,
		Length:       1,
	}

	// Remove the only element
	p.RemoveProxy("a")

	// CurrentIndex should be -1 (empty pool)
	if p.CurrentIndex != -1 {
		t.Errorf("after removing only element, CurrentIndex=%d, want -1", p.CurrentIndex)
	}

	// NextProxy should return empty string
	got := p.NextProxy()
	if got != "" {
		t.Errorf("NextProxy() after draining pool = %q, want empty string", got)
	}
}

func TestReloadUpdatesProxies(t *testing.T) {
	content := "http://127.0.0.1:8080\nhttp://127.0.0.2:8080\nhttp://127.0.0.3:8080\n"
	tmpFile, err := ioutil.TempFile("", "proxymanager_test_*.txt")
	if err != nil {
		t.Fatal(err)
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		t.Fatal(err)
	}
	tmpFile.Close()
	defer os.Remove(tmpPath)

	pm, err := New(tmpPath)
	if err != nil {
		t.Fatal(err)
	}

	if pm.Length != 3 {
		t.Fatalf("expected Length=3, got %d", pm.Length)
	}

	newContent := "http://127.0.0.4:8080\nhttp://127.0.0.5:8080\nhttp://127.0.0.6:8080\n"
	if err := ioutil.WriteFile(tmpPath, []byte(newContent), 0644); err != nil {
		t.Fatal(err)
	}

	if err := pm.Reload(); err != nil {
		t.Fatal(err)
	}

	if pm.Length != 3 {
		t.Fatalf("after reload expected Length=3, got %d", pm.Length)
	}

	for _, proxy := range pm.Proxies {
		if proxy == "http://127.0.0.1:8080" || proxy == "http://127.0.0.2:8080" || proxy == "http://127.0.0.3:8080" {
			t.Errorf("Reload() did not update proxies: still contains %q", proxy)
		}
	}

	found := make(map[string]bool)
	for _, proxy := range pm.Proxies {
		found[proxy] = true
	}
	for _, expected := range []string{"http://127.0.0.4:8080", "http://127.0.0.5:8080", "http://127.0.0.6:8080"} {
		if !found[expected] {
			t.Errorf("Reload() missing expected proxy %q", expected)
		}
	}
}

func TestNewReturnsFreshManager(t *testing.T) {
	content1 := "http://127.0.0.1:8080\nhttp://127.0.0.2:8080\n"
	tmpFile1, err := ioutil.TempFile("", "proxymanager_test_*.txt")
	if err != nil {
		t.Fatal(err)
	}
	tmpPath1 := tmpFile1.Name()

	if _, err := tmpFile1.WriteString(content1); err != nil {
		tmpFile1.Close()
		os.Remove(tmpPath1)
		t.Fatal(err)
	}
	tmpFile1.Close()
	defer os.Remove(tmpPath1)

	content2 := "http://127.0.0.3:8080\n"
	tmpFile2, err := ioutil.TempFile("", "proxymanager_test_*.txt")
	if err != nil {
		t.Fatal(err)
	}
	tmpPath2 := tmpFile2.Name()

	if _, err := tmpFile2.WriteString(content2); err != nil {
		tmpFile2.Close()
		os.Remove(tmpPath2)
		t.Fatal(err)
	}
	tmpFile2.Close()
	defer os.Remove(tmpPath2)

	pm1, err := New(tmpPath1)
	if err != nil {
		t.Fatal(err)
	}
	pm2, err := New(tmpPath2)
	if err != nil {
		t.Fatal(err)
	}

	if pm1 == pm2 {
		t.Error("New() returned the same instance for two calls")
	}

	if pm1.Length != 2 {
		t.Errorf("pm1.Length = %d, want 2", pm1.Length)
	}
	if pm2.Length != 1 {
		t.Errorf("pm2.Length = %d, want 1", pm2.Length)
	}

	if len(pm1.Proxies) != 2 {
		t.Errorf("len(pm1.Proxies) = %d, want 2", len(pm1.Proxies))
	}
	if len(pm2.Proxies) != 1 {
		t.Errorf("len(pm2.Proxies) = %d, want 1", len(pm2.Proxies))
	}

	if pm1.Proxies[0] == "" {
		t.Error("pm1.Proxies[0] is empty, expected proxy URL")
	}
	if pm2.Proxies[0] == "" {
		t.Error("pm2.Proxies[0] is empty, expected proxy URL")
	}

	if pm2.Length != 1 {
		t.Errorf("pm2.Length changed after creation = %d, want 1", pm2.Length)
	}
}

func TestReloadConcurrent(t *testing.T) {
	content := "http://127.0.0.1:8080\nhttp://127.0.0.2:8080\nhttp://127.0.0.3:8080\n"
	tmpFile, err := ioutil.TempFile("", "proxymanager_test_*.txt")
	if err != nil {
		t.Fatal(err)
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		t.Fatal(err)
	}
	tmpFile.Close()
	defer os.Remove(tmpPath)

	pm, err := New(tmpPath)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := pm.Reload(); err != nil {
				t.Errorf("Reload() error: %v", err)
			}
		}()
	}

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = pm.NextProxy()
		}()
	}

	wg.Wait()
}
