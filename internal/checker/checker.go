package checker

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/logrusorgru/aurora"
	"ktbs.dev/mubeng/common"
	"ktbs.dev/mubeng/pkg/helper"
	"ktbs.dev/mubeng/pkg/mubeng"
)

// Do checks proxy from list.
//
// Displays proxies that have died if verbose mode is enabled,
// or save live proxies into user defined files.
func Do(opt *common.Options) {
	// Limit concurrent checks to prevent hitting OS thread limits
	maxConcurrent := opt.Concurrent
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	sem := make(chan struct{}, maxConcurrent)

	for _, proxy := range opt.ProxyManager.Proxies {
		wg.Add(1)
		sem <- struct{}{}

		go func(address string) {
			defer wg.Done()
			defer func() { <-sem }()

			addr, err := check(address, opt.Timeout)
			if err != nil {
				if opt.Verbose {
					fmt.Printf("[%s] %s\n", aurora.Red("DIED"), address)
				}
				return
			}

			if len(opt.Countries) > 0 && !isMatchCC(opt.Countries, addr.CC) {
				return
			}

			fmt.Printf("[%s] [%s] [%s] %s\n", aurora.Green("LIVE"), aurora.Magenta(addr.CC), aurora.Cyan(addr.IP), address)

			if opt.Output != "" {
				fmt.Fprintf(opt.Result, "%s\n", address)
			}
		}(helper.EvalFunc(proxy))
	}

	wg.Wait()
}

func isMatchCC(cc []string, code string) bool {
	if code == "" {
		return false
	}

	for _, c := range cc {
		if code == strings.ToUpper(strings.TrimSpace(c)) {
			return true
		}
	}

	return false
}

func check(address string, timeout time.Duration) (myIP, error) {
	var lastErr error
	for _, ep := range endpoints {
		result, err := tryCheck(address, ep, timeout)
		if err != nil {
			lastErr = err
			continue
		}
		result.normalize()
		if result.IP != "" && result.CC != "" {
			return result, nil
		}
		lastErr = fmt.Errorf("incomplete response from %s", ep)
	}
	return myip, fmt.Errorf("all endpoints failed: %w", lastErr)
}

func tryCheck(address, endpoint string, timeout time.Duration) (myIP, error) {
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return myip, err
	}

	tr, err := mubeng.Transport(address)
	if err != nil {
		return myip, err
	}

	// Verify TLS certificates during check to detect MITM proxies.
	// Transport() globally sets InsecureSkipVerify: true, which lets
	// MITM proxies with expired/self-signed certs pass the check.
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: false}

	proxy := &mubeng.Proxy{
		Address:   address,
		Transport: tr,
	}

	cl, req := proxy.New(req)
	cl.Timeout = timeout
	req.Header.Add("Connection", "close")

	resp, err := cl.Do(req)
	if err != nil {
		return myip, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return myip, fmt.Errorf("unexpected status: %d from %s", resp.StatusCode, endpoint)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return myip, err
	}

	var result myIP
	err = json.Unmarshal(body, &result)
	if err != nil {
		return myip, err
	}

	return result, nil
}
