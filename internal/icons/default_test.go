package icons

import (
	"net/http"
	"testing"
)

func TestDefaultIconIsValidImage(t *testing.T) {
	data, contentType := Default()

	if len(data) == 0 {
		t.Fatal("expected embedded default icon to be non-empty")
	}
	if contentType != "image/png" {
		t.Fatalf("expected content type image/png, got %q", contentType)
	}

	// Confirm the embedded bytes actually sniff as a real PNG, the
	// same check every uploaded/fetched icon has to pass — the
	// embedded default shouldn't get a free pass on the property
	// that matters most.
	sniffLen := len(data)
	if sniffLen > 512 {
		sniffLen = 512
	}
	sniffed := http.DetectContentType(data[:sniffLen])
	if sniffed != "image/png" {
		t.Fatalf("embedded default icon does not sniff as image/png, got %q", sniffed)
	}
}

func TestDefaultIconWithinSizeCap(t *testing.T) {
	data, _ := Default()
	if len(data) > MaxIconSize {
		t.Fatalf("embedded default icon (%d bytes) exceeds MaxIconSize (%d bytes)", len(data), MaxIconSize)
	}
}
