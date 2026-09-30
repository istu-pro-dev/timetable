package auth

import (
	"net/http"
	"slices"
	"strings"

	"github.com/istu-pro-dev/timetable/backend/internal/httpx"
	"github.com/istu-pro-dev/timetable/backend/internal/store/db"
)

// Cookie names.
const (
	AccessCookie  = "tt_access"
	RefreshCookie = "tt_refresh"
)

// Authenticate reads the access token from the Authorization: Bearer header or, failing that,
// from the access cookie. A valid token puts its Principal into the request context; a missing
// or invalid token leaves the request anonymous (Require then answers 401).
func (s *Service) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token := accessToken(r); token != "" {
			if p, err := s.tokens.Parse(token); err == nil {
				r = r.WithContext(WithPrincipal(r.Context(), p))
			}
		}
		next.ServeHTTP(w, r)
	})
}

func accessToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		scheme, token, ok := strings.Cut(h, " ")
		if ok && strings.EqualFold(scheme, "Bearer") {
			return strings.TrimSpace(token)
		}
		return ""
	}
	if c, err := r.Cookie(AccessCookie); err == nil {
		return c.Value
	}
	return ""
}

// Require allows the request only for an authenticated principal whose role is one of roles
// (any role when roles is empty). It answers 401 without a valid session and 403 for a wrong
// role. It relies on Authenticate running earlier in the chain.
func Require(roles ...db.UserRole) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := FromContext(r.Context())
			if !ok {
				w.Header().Set("WWW-Authenticate", `Bearer realm="timetable"`)
				httpx.WriteError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
				return
			}
			if len(roles) > 0 && !slices.Contains(roles, p.Role) {
				httpx.WriteError(w, http.StatusForbidden, "forbidden", "insufficient role for this operation")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
