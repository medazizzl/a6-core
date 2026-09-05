package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"a6core/internal/state"
)

func TestDeviceTypeForRequestLoopbackIsShell(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/pair", nil)
	r.RemoteAddr = "127.0.0.1:54321"
	if got := deviceTypeForRequest(r); got != state.DeviceTypeShell {
		t.Fatalf("expected %q for a loopback request, got %q", state.DeviceTypeShell, got)
	}
}

func TestDeviceTypeForRequestRemoteIsPhone(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/pair", nil)
	r.RemoteAddr = "192.168.1.50:54321"
	if got := deviceTypeForRequest(r); got != state.DeviceTypePhone {
		t.Fatalf("expected %q for a remote request, got %q", state.DeviceTypePhone, got)
	}
}
