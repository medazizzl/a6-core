package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsLocalRequestLoopback(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "127.0.0.1:54321"
	if !IsLocalRequest(r) {
		t.Fatal("expected a loopback request to be treated as local")
	}
}

func TestIsLocalRequestRemote(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "192.168.1.50:54321"
	if IsLocalRequest(r) {
		t.Fatal("expected a remote request to not be treated as local")
	}
}
