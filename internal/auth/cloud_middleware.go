package auth

import (
	"context"
	"net/http"
	"strings"

	"a6core/internal/cloud"
	"a6core/internal/state"
)

const cloudUserContextKey contextKey = "a6core_cloud_user"

func CloudUserFromContext(ctx context.Context) (state.CloudUser, bool) {
	u, ok := ctx.Value(cloudUserContextKey).(state.CloudUser)
	return u, ok
}

// RequireCloudSession gates cloud storage endpoints to a valid,
// non-revoked login session — completely separate from RequireDevice.
// A request can be from a perfectly legitimate paired A6 device and
// still be rejected here if it has no (or an expired/logged-out)
// cloud session, matching the deliberate separation between "this
// phone is trusted to control A6" and "this phone is logged into a
// personal cloud account."
func RequireCloudSession(cloudStore *cloud.Store, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(authHeader, prefix) {
			writeAuthError(w, http.StatusUnauthorized, "missing_token", "Authorization: Bearer <session_token> header required")
			return
		}
		token := strings.TrimPrefix(authHeader, prefix)

		user, err := cloudStore.ValidateSession(token)
		if err != nil {
			writeAuthError(w, http.StatusUnauthorized, "invalid_session", "session invalid, expired, or logged out")
			return
		}

		ctx := context.WithValue(r.Context(), cloudUserContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
