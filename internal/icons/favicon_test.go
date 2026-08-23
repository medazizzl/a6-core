package icons

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIsBlockedIP(t *testing.T) {
	cases := []struct {
		ip      string
		blocked bool
	}{
		{"127.0.0.1", true},
		{"::1", true},
		{"10.0.0.1", true},
		{"172.16.5.5", true},
		{"192.168.1.1", true},
		{"169.254.1.1", true},
		{"100.64.0.1", true},
		{"100.127.255.254", true},
		{"0.0.0.0", true},
		{"224.0.0.1", true},
		{"fc00::1", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"93.184.216.34", false},
	}
	for _, c := range cases {
		ip := net.ParseIP(c.ip)
		if ip == nil {
			t.Fatalf("test setup: failed to parse IP %q", c.ip)
		}
		if got := isBlockedIP(ip); got != c.blocked {
			t.Errorf("isBlockedIP(%q) = %v, want %v", c.ip, got, c.blocked)
		}
	}
}

func TestSafeDialContextBlocksLoopback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := safeDialContext(ctx, "tcp", "127.0.0.1:80"); !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("expected ErrBlockedAddress, got %v", err)
	}
}

func TestFaviconClientRejectsRedirectToUnsupportedScheme(t *testing.T) {
	client := newFaviconClient()
	badReq, err := http.NewRequest(http.MethodGet, "file:///etc/passwd", nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	if err := client.CheckRedirect(badReq, nil); err == nil {
		t.Fatal("expected CheckRedirect to reject a file:// scheme redirect")
	}
}

func TestFetchFaviconRejectsUnsupportedScheme(t *testing.T) {
	store := newTestIconStore(t)
	if _, err := store.FetchFavicon("ftp://example.com/site"); !errors.Is(err, ErrFaviconUnavailable) {
		t.Fatalf("expected ErrFaviconUnavailable for unsupported scheme, got %v", err)
	}
}

func TestFetchFaviconRejectsInvalidURL(t *testing.T) {
	store := newTestIconStore(t)
	if _, err := store.FetchFavicon("::not a valid url::"); !errors.Is(err, ErrFaviconUnavailable) {
		t.Fatalf("expected ErrFaviconUnavailable for an invalid url, got %v", err)
	}
}

// The remaining tests exercise the success/failure shape of a real
// fetch. httptest servers listen on 127.0.0.1 — exactly what our own
// guard blocks — so these substitute the test server's own permissive
// client, deliberately isolating "does the save/validation pipeline
// work" from "does the SSRF guard work" (already proven above).

func TestFetchFaviconSuccess(t *testing.T) {
	iconBytes := smallPNG(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/favicon.ico" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(iconBytes)
	}))
	defer ts.Close()

	store := newTestIconStore(t)
	store.faviconClient = ts.Client()

	iconID, err := store.FetchFavicon(ts.URL)
	if err != nil {
		t.Fatalf("FetchFavicon: %v", err)
	}
	if !store.Exists(iconID) {
		t.Fatal("expected fetched favicon to be saved and retrievable")
	}
	got, _, err := store.Get(iconID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got, iconBytes) {
		t.Fatal("saved favicon bytes do not match what the server served")
	}
}

func TestFetchFaviconMissingReturnsUnavailable(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer ts.Close()

	store := newTestIconStore(t)
	store.faviconClient = ts.Client()

	if _, err := store.FetchFavicon(ts.URL); !errors.Is(err, ErrFaviconUnavailable) {
		t.Fatalf("expected ErrFaviconUnavailable, got %v", err)
	}
}

func TestFetchFaviconRejectsOversized(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, MaxIconSize+1024))
	}))
	defer ts.Close()

	store := newTestIconStore(t)
	store.faviconClient = ts.Client()

	if _, err := store.FetchFavicon(ts.URL); !errors.Is(err, ErrFaviconUnavailable) {
		t.Fatalf("expected ErrFaviconUnavailable for an oversized favicon, got %v", err)
	}
}
