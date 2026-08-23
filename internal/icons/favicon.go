package icons

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

const (
	faviconFetchTimeout = 3 * time.Second
	faviconPath         = "/favicon.ico"
)

var (
	ErrBlockedAddress     = errors.New("icons: refused to fetch from a private, loopback, or internal address")
	ErrFaviconUnavailable = errors.New("icons: favicon unavailable")
)

var extraBlockedRanges = mustParseCIDRs(
	"100.64.0.0/10",
)

func mustParseCIDRs(cidrs ...string) []*net.IPNet {
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic(fmt.Sprintf("icons: invalid hardcoded CIDR %q: %v", c, err))
		}
		nets = append(nets, n)
	}
	return nets
}

func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	for _, n := range extraBlockedRanges {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("icons: invalid address %q: %w", addr, err)
	}
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("icons: resolving %q: %w", host, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("icons: no addresses found for %q", host)
	}
	for _, ip := range ips {
		if isBlockedIP(ip) {
			return nil, ErrBlockedAddress
		}
	}
	dialer := &net.Dialer{}
	return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
}

func newFaviconClient() *http.Client {
	return &http.Client{
		Timeout: faviconFetchTimeout,
		Transport: &http.Transport{
			DialContext: safeDialContext,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("icons: refusing redirect to unsupported scheme %q", req.URL.Scheme)
			}
			if len(via) >= 5 {
				return fmt.Errorf("icons: too many redirects")
			}
			return nil
		},
	}
}

// FetchFavicon attempts to download {origin}/favicon.ico, convert it
// if needed, validate it through the same pipeline as a manual
// upload (Save), and return its new icon_id. Any failure is a SOFT
// failure — the caller falls back to the embedded default icon
// rather than treating this as fatal to shortcut creation.
func (s *Store) FetchFavicon(shortcutURL string) (iconID string, err error) {
	parsed, err := url.Parse(shortcutURL)
	if err != nil {
		return "", fmt.Errorf("%w: parsing shortcut url: %v", ErrFaviconUnavailable, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("%w: unsupported scheme %q", ErrFaviconUnavailable, parsed.Scheme)
	}

	faviconURL := parsed.Scheme + "://" + parsed.Host + faviconPath

	ctx, cancel := context.WithTimeout(context.Background(), faviconFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, faviconURL, nil)
	if err != nil {
		return "", fmt.Errorf("%w: building request: %v", ErrFaviconUnavailable, err)
	}

	resp, err := s.faviconClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrFaviconUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: unexpected status %d", ErrFaviconUnavailable, resp.StatusCode)
	}

	limited := io.LimitReader(resp.Body, MaxIconSize+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return "", fmt.Errorf("%w: reading response: %v", ErrFaviconUnavailable, err)
	}
	if len(data) > MaxIconSize {
		return "", fmt.Errorf("%w: exceeds maximum size", ErrFaviconUnavailable)
	}

	// NEW: favicon.ico is very commonly a classic .ico file, which
	// Save() has never accepted (it only stores PNG/JPEG, per the
	// frozen spec). Convert it here, at the fetch boundary, rather
	// than loosening what Save() accepts for everyone — explicit
	// manual uploads should stay exactly as strict as before.
	sniffLen := len(data)
	if sniffLen > 512 {
		sniffLen = 512
	}
	if http.DetectContentType(data[:sniffLen]) == "image/x-icon" {
		converted, convErr := decodeICOToPNG(data)
		if convErr != nil {
			return "", fmt.Errorf("%w: could not read ico format: %v", ErrFaviconUnavailable, convErr)
		}
		data = converted
	}

	iconID, err = s.Save(data)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrFaviconUnavailable, err)
	}
	return iconID, nil
}

// SetFaviconClientForTest overrides the favicon HTTP client. Exists
// only so tests in other packages can point favicon fetches at a
// local test server instead of the real, SSRF-guarded production
// client — production code should never call this.
func (s *Store) SetFaviconClientForTest(client *http.Client) {
	s.faviconClient = client
}
