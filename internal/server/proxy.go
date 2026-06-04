package server

import (
	"bufio"
	"net"
	"os"
	"strings"

	"github.com/elazarl/goproxy"
	"github.com/projectdiscovery/gologger"
	"ktbs.dev/mubeng/common"
)

// Proxy as ServeMux in proxy server handler.
type Proxy struct {
	HTTPProxy *goproxy.ProxyHttpServer
	Options   *common.Options
	Blacklist []string
}

// loadBlacklist reads the blacklist file and populates the Blacklist slice.
// Each non-empty, non-comment line is treated as a domain.
// Lines starting with "." match all subdomains (e.g. ".example.com").
func (p *Proxy) loadBlacklist(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			gologger.Info().Msgf("Blacklist file %s not found, all domains will go through proxy", path)
			return nil
		}
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Normalise: remove protocol prefix if present
		domain := strings.TrimPrefix(line, "http://")
		domain = strings.TrimPrefix(domain, "https://")
		domain = strings.TrimPrefix(domain, "*")
		domain = strings.TrimSpace(domain)

		if domain != "" {
			p.Blacklist = append(p.Blacklist, domain)
		}
	}
	return scanner.Err()
}

// isBlacklisted checks whether the given host (which may include port)
// matches any entry in the blacklist.
func (p *Proxy) isBlacklisted(host string) bool {
	if len(p.Blacklist) == 0 {
		return false
	}

	// Strip port to get hostname
	hostname := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		hostname = h
	}

	for _, entry := range p.Blacklist {
		// Wildcard prefix — ".example.com" matches "sub.example.com"
		if strings.HasPrefix(entry, ".") {
			if strings.HasSuffix(hostname, entry) || hostname == entry[1:] {
				return true
			}
		} else if strings.EqualFold(hostname, entry) {
			return true
		}
	}
	return false
}

// logVerbose logs detailed request information when verbose mode is enabled.
// It performs a DNS lookup and logs the resolved IPs.
func logVerbose(reqAddr, method, urlStr string) {
	log.Infof("%s %s %s", reqAddr, method, urlStr)
}

// logDNS performs a DNS lookup and logs the results.
func logDNS(hostname string) {
	ips, err := net.LookupHost(hostname)
	if err != nil {
		log.Debugf("DNS %s: %s", hostname, err)
		return
	}
	log.Infof("DNS %s -> %s", hostname, strings.Join(ips, ", "))
}

// extractHostname strips the port from a host string.
func extractHostname(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}
