package icons

import _ "embed"

// defaultIconBytes is a small placeholder image, compiled directly
// into the binary via go:embed — used whenever a shortcut has no
// explicit icon_id and the favicon fetch (favicon.go) either wasn't
// attempted or failed. This is genuinely placeholder art; the real
// visual design belongs to the TV shell work, not this stage.
//
//go:embed assets/default.png
var defaultIconBytes []byte

// DefaultIconContentType is fixed, since defaultIconBytes is a
// compiled-in constant, not something requiring runtime sniffing
// the way uploaded or fetched icons are (icons.go, favicon.go).
const DefaultIconContentType = "image/png"

// Default returns the embedded fallback icon's raw bytes and content
// type, ready to serve directly — this never touches disk and can
// never fail, unlike Get, which depends on a real stored file.
func Default() (data []byte, contentType string) {
	return defaultIconBytes, DefaultIconContentType
}
