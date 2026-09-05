package auth

import (
	"net"
	"net/http"
)

// IsLocalRequest reports whether r originated from this machine
// itself (loopback), the same check RequireLocalhost enforces as a
// hard requirement elsewhere in this package. Used where local
// origin needs to be read as a signal rather than enforced as a
// requirement — e.g. pairing deriving device_type from whether the
// caller is the shell itself (Stage 17 Wave 4).
func IsLocalRequest(r *http.Request) bool {
	ip := net.ParseIP(ClientIP(r))
	return ip != nil && ip.IsLoopback()
}
