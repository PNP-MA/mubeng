package downloader

import (
	"testing"
)

func TestNormalizeProxyIPv6WithScheme(t *testing.T) {
	input := "http://[::1]:8080"
	got := normalizeProxy(input)
	if got != input {
		t.Errorf("normalizeProxy(%q) = %q, want %q", input, got, input)
	}
}

func TestNormalizeProxyIPv6NoScheme(t *testing.T) {
	input := "[::1]:8080"
	want := "http://[::1]:8080"
	got := normalizeProxy(input)
	if got != want {
		t.Errorf("normalizeProxy(%q) = %q, want %q", input, got, want)
	}
}

func TestNormalizeProxyIPv6WithAuth(t *testing.T) {
	input := "socks5://user:pass@[::1]:1080"
	got := normalizeProxy(input)
	if got != input {
		t.Errorf("normalizeProxy(%q) = %q, want %q", input, got, input)
	}
}

func TestNormalizeProxyIPv4(t *testing.T) {
	input := "http://1.2.3.4:8080"
	got := normalizeProxy(input)
	if got != input {
		t.Errorf("normalizeProxy(%q) = %q, want %q", input, got, input)
	}
}

func TestNormalizeProxyIPv4Bare(t *testing.T) {
	input := "1.2.3.4:8080"
	want := "http://1.2.3.4:8080"
	got := normalizeProxy(input)
	if got != want {
		t.Errorf("normalizeProxy(%q) = %q, want %q", input, got, want)
	}
}

func TestNormalizeProxyAuth(t *testing.T) {
	input := "user:pass@1.2.3.4:8080"
	want := "http://user:pass@1.2.3.4:8080"
	got := normalizeProxy(input)
	if got != want {
		t.Errorf("normalizeProxy(%q) = %q, want %q", input, got, want)
	}
}

func TestNormalizeProxyIPv6DifferentPort(t *testing.T) {
	input := "[fe80::1%25eth0]:3128"
	want := "http://[fe80::1%25eth0]:3128"
	got := normalizeProxy(input)
	if got != want {
		t.Errorf("normalizeProxy(%q) = %q, want %q", input, got, want)
	}
}

func TestNormalizeProxyEmpty(t *testing.T) {
	got := normalizeProxy("")
	if got != "" {
		t.Errorf("normalizeProxy(\"\") = %q, want \"\"", got)
	}
}

func TestNormalizeProxyNoPort(t *testing.T) {
	got := normalizeProxy("http://1.2.3.4")
	if got != "" {
		t.Errorf("normalizeProxy(\"http://1.2.3.4\") = %q, want \"\"", got)
	}
}
