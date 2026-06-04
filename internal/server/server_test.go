package server

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"ktbs.dev/mubeng/common"
)

func TestDialSOCKSProxyTimeout(t *testing.T) {
	proxyAddr := "socks5://192.0.2.1:1"
	network := "tcp"
	addr := "example.com:80"
	timeout := 1 * time.Millisecond

	start := time.Now()
	_, err := dialSOCKSProxy(proxyAddr, network, addr, timeout)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected timeout error, got nil")
	}
	if elapsed > 5*time.Second {
		t.Error("dialSOCKSProxy with 1ms timeout took too long, may have hung")
	}
}

func TestRunReturnsError(t *testing.T) {
	dir := t.TempDir()
	badPath := filepath.Join(dir, "blacklist.txt")
	if err := os.WriteFile(badPath, []byte("test"), 0); err != nil {
		t.Fatal(err)
	}

	opt := &common.Options{
		Address:   ":0",
		Blacklist: badPath,
	}

	err := Run(opt)
	if err == nil {
		t.Error("Run() should return an error with an unreadable blacklist path")
	}
}
