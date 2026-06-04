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

	// Blacklist check: dial directly for blacklisted hosts
	if handler.isBlacklisted(addr) {
		if handler.Options.Verbose {
			hostname := extractHostname(addr)
			ips, _ := net.LookupHost(hostname)
			log.Infof("DNS %s -> %s", hostname, strings.Join(ips, ", "))
			log.Infof("%s CONNECT %s -> DIRECT (blacklisted)", "proxy", addr)
		}
		return net.DialTimeout(network, addr, timeout)
	}

	if handler.Options == nil || handler.Options.ProxyManager == nil || handler.Options.ProxyManager.Length == 0 {
		return nil, fmt.Errorf("no upstream proxies available")
	}

	maxAttempts := handler.Options.ProxyManager.Length
	if maxAttempts > 3 {
		maxAttempts = 3
	}

	var lastErr error
	for i := 0; i < maxAttempts; i++ {
		proxyAddr := handler.Options.ProxyManager.RandomProxy()

		u, err := url.Parse(proxyAddr)
		if err != nil {
			lastErr = err
			continue
		}

		var conn net.Conn
		switch u.Scheme {
		case "http", "https":
			conn, err = dialHTTPProxy(u, addr, timeout)
		case "socks4", "socks4a", "socks5":
			conn, err = dialSOCKSProxy(proxyAddr, network, addr, timeout)
		default:
			continue
		}
		if err == nil {
			if handler.Options.Verbose {
				log.Infof("%s CONNECT %s -> via %s", "proxy", addr, proxyAddr)
			}
			return conn, nil
		}
		lastErr = err
	}

	return nil, fmt.Errorf("all upstream proxies failed: %w", lastErr)
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

func dialSOCKSProxy(proxyAddr, network, addr string, _ time.Duration) (net.Conn, error) {
	return socks.Dial(proxyAddr)(network, addr)
}

// Run proxy server with a user defined listener.
//
// An active log have 2 receivers, especially stdout and into file if opt.Output isn't empty.
// Then close the proxy server if it receives a signal that interrupts the program.
func Run(opt *common.Options) {
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
		log.Fatal(err)
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
			log.Fatal(err)
		}
		defer watcher.Close()

		go watch(watcher)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	go interrupt(stop)

	log.Infof("[PID: %d] Starting proxy server on %s", os.Getpid(), opt.Address)
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
