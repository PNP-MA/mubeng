package downloader

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/projectdiscovery/gologger"
	"ktbs.dev/mubeng/common"
	"ktbs.dev/mubeng/pkg/mubeng"
)

// Do fetches proxy lists from all URLs listed in the URL file,
// deduplicates them, and writes the result to the output file.
func Do(opt *common.Options) error {
	inFile := opt.File
	if inFile == "" {
		inFile = "url_proxy.txt"
	}

	outFile := opt.Output
	if outFile == "" {
		outFile = "proxies.txt"
	}

	gologger.Info().Msgf("Reading proxy source URLs from %s", inFile)

	urls, err := readURLs(inFile)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", inFile, err)
	}

	if len(urls) == 0 {
		return fmt.Errorf("no URLs found in %s", inFile)
	}

	gologger.Info().Msgf("Found %d proxy source URLs", len(urls))

	timeout := opt.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	client := &http.Client{
		Timeout: timeout,
	}

	seen := make(map[string]bool)
	var allProxies []string

	for _, uri := range urls {
		gologger.Info().Msgf("Fetching %s", uri)

		proxies, err := fetchProxies(client, uri)
		if err != nil {
			gologger.Warning().Msgf("Failed to fetch %s: %s", uri, err)
			continue
		}

		for _, p := range proxies {
			normalized := normalizeProxy(p)
			if normalized == "" {
				continue
			}

			// Validate with mubeng transport
			if _, err := mubeng.Transport(normalized); err != nil {
				continue
			}

			if !seen[normalized] {
				seen[normalized] = true
				allProxies = append(allProxies, normalized)
			}
		}

		gologger.Info().Msgf("Fetched %d proxies from %s (total unique: %d)", len(proxies), uri, len(allProxies))
	}

	if len(allProxies) == 0 {
		return fmt.Errorf("no valid proxies found from any source")
	}

	outPath, err := filepath.Abs(outFile)
	if err != nil {
		return err
	}

	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", outPath, err)
	}
	defer f.Close()

	for _, p := range allProxies {
		fmt.Fprintln(f, p)
	}

	gologger.Info().Msgf("Written %d proxies to %s", len(allProxies), outPath)
	return nil
}

// readURLs reads a file containing one URL per line.
// Lines beginning with # are treated as comments and skipped.
func readURLs(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var urls []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		urls = append(urls, line)
	}

	return urls, scanner.Err()
}

// fetchProxies downloads from a URI and returns raw proxy lines.
func fetchProxies(client *http.Client, uri string) ([]string, error) {
	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; mubeng-downloader/1.0)")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var proxies []string
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		proxies = append(proxies, line)
	}

	return proxies, nil
}

// normalizeProxy converts various proxy formats to the canonical
// protocol://ip:port format expected by mubeng.
func normalizeProxy(raw string) string {
	// Strip any trailing garbage
	raw = strings.TrimSpace(raw)

	// Already has a scheme
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return ""
		}
		if u.Host == "" {
			return ""
		}
		// Ensure port is present
		parts := strings.Split(u.Host, ":")
		if len(parts) < 2 {
			return ""
		}
		return raw
	}

	// ip:port format — prepend http://
	if strings.Contains(raw, ":") {
		parts := strings.Split(raw, ":")
		if len(parts) == 2 {
			return "http://" + raw
		}
		// user:pass@ip:port
		if len(parts) >= 3 {
			return "http://" + raw
		}
	}

	return ""
}
