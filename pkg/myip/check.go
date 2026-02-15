package myip

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jetkvm/kvm/pkg/nmlite/link"
)

func (ps *PublicIPState) request(ctx context.Context, url string, family int) ([]byte, error) {
	client := ps.httpClient(family)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %w", err)
	}

	return body, err
}

// checkCloudflare uses cdn-cgi/trace to get the public IP address
func (ps *PublicIPState) checkCloudflare(ctx context.Context, family int) (*PublicIP, error) {
	u, err := url.JoinPath(ps.cloudflareEndpoint, "/cdn-cgi/trace")
	if err != nil {
		return nil, fmt.Errorf("error joining path: %w", err)
	}

	body, err := ps.request(ctx, u, family)
	if err != nil {
		return nil, err
	}

	values := make(map[string]string)
	for line := range strings.SplitSeq(string(body), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[key] = value
	}

	ps.lastUpdated = time.Now()
	if ts, ok := values["ts"]; ok {
		if ts, err := strconv.ParseFloat(ts, 64); err == nil {
			ps.lastUpdated = time.Unix(int64(ts), 0)
		}
	}

	ipStr, ok := values["ip"]
	if !ok {
		return nil, fmt.Errorf("no IP address found")
	}

	ip := net.ParseIP(ipStr)
	if ip == nil {
		return nil, fmt.Errorf("invalid IP address: %s", ipStr)
	}

	return &PublicIP{
		IPAddress:   ip,
		LastUpdated: ps.lastUpdated,
	}, nil
}

// checkAPI uses the API endpoint to get the public IP address
func (ps *PublicIPState) checkAPI(_ context.Context, _ int) (*PublicIP, error) {
	return nil, fmt.Errorf("not implemented")
}

// checkIPs checks both IPv4 and IPv6 public IP addresses in parallel
// and updates the IPAddresses slice with the results
func (ps *PublicIPState) checkIPs(ctx context.Context, checkIPv4, checkIPv6 bool) error {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var ips []PublicIP
	var errors []error

	checkFamily := func(family int, familyName string) {
		wg.Add(1)
		go func(f int, name string) {
			defer wg.Done()

			ip, err := ps.checkIPForFamily(ctx, f)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errors = append(errors, fmt.Errorf("%s check failed: %w", name, err))
				return
			}
			if ip != nil {
				ips = append(ips, *ip)
			}
		}(family, familyName)
	}

	if checkIPv4 {
		checkFamily(link.AfInet, "IPv4")
	}

	if checkIPv6 {
		checkFamily(link.AfInet6, "IPv6")
	}

	wg.Wait()

	if len(ips) > 0 {
		ps.mu.Lock()
		defer ps.mu.Unlock()

		ps.addresses = ips
		ps.lastUpdated = time.Now()
	}

	if len(errors) > 0 && len(ips) == 0 {
		return errors[0]
	}

	return nil
}

func (ps *PublicIPState) checkIPForFamily(ctx context.Context, family int) (*PublicIP, error) {
	if ps.apiEndpoint != "" {
		ip, err := ps.checkAPI(ctx, family)
		if err == nil && ip != nil {
			return ip, nil
		}
	}

	if ps.cloudflareEndpoint != "" {
		ip, err := ps.checkCloudflare(ctx, family)
		if err == nil && ip != nil {
			return ip, nil
		}
	}

	return nil, fmt.Errorf("all IP check methods failed for family %d", family)
}
