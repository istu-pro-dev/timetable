package auth

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/istu-pro-dev/timetable/backend/internal/store"
	"github.com/istu-pro-dev/timetable/backend/internal/store/storetest"
)

const (
	testAdmin    = "Admin"
	testPassword = "s3cret-password"
)

func newTestService(t *testing.T) (*Service, *store.Store, http.Handler) {
	t.Helper()
	st := storetest.New(t)
	svc := NewService(st, Config{
		Secret:        []byte(strings.Repeat("k", 32)),
		AdminLogin:    testAdmin,
		AdminPassword: testPassword,
	}, slog.New(slog.DiscardHandler))
	if err := svc.Bootstrap(t.Context()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	mux := http.NewServeMux()
	for pattern, h := range svc.Routes() {
		mux.Handle(pattern, h)
	}
	return svc, st, svc.Authenticate(mux)
}

type call struct {
	method, path, body string
	bearer             string
	cookies            []*http.Cookie
	remoteAddr         string
}

func do(t *testing.T, h http.Handler, c call) *httptest.ResponseRecorder {
	t.Helper()
	var body io.Reader
	if c.body != "" {
		body = strings.NewReader(c.body)
	}
	req := httptest.NewRequestWithContext(t.Context(), c.method, c.path, body)
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	}
	for _, ck := range c.cookies {
		req.AddCookie(ck)
	}
	if c.remoteAddr != "" {
		req.RemoteAddr = c.remoteAddr
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func cookie(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func loginBody(login, password string) string {
	b, _ := json.Marshal(map[string]string{"login": login, "password": password})
	return string(b)
}

func TestBootstrapIsIdempotent(t *testing.T) {
	svc, st, _ := newTestService(t)
	if err := svc.Bootstrap(t.Context()); err != nil {
		t.Fatalf("second bootstrap: %v", err)
	}
	svc.cfg.AdminLogin, svc.cfg.AdminPassword = "someone-else", "another-password"
	if err := svc.Bootstrap(t.Context()); err != nil {
		t.Fatalf("bootstrap with other credentials: %v", err)
	}
	n, err := st.CountActiveAdmins(t.Context())
	if err != nil || n != 1 {
		t.Fatalf("admins = %d, %v; want 1", n, err)
	}
	u, err := st.GetUserByLogin(t.Context(), "admin")
	if err != nil || u.Role != RoleAdmin {
		t.Fatalf("bootstrap admin = %+v, %v", u, err)
	}
}

func TestBootstrapWithoutCredentials(t *testing.T) {
	st := storetest.New(t)
	var logs bytes.Buffer
	svc := NewService(st, Config{Secret: []byte(strings.Repeat("k", 32))}, slog.New(slog.NewTextHandler(&logs, nil)))
	if err := svc.Bootstrap(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "no admin account exists") {
		t.Fatalf("missing warning, logs: %s", logs.String())
	}
	svc.cfg.AdminLogin, svc.cfg.AdminPassword = "admin", "short"
	if err := svc.Bootstrap(t.Context()); err == nil {
		t.Fatal("short ADMIN_PASSWORD accepted")
	}
}

func TestLoginMeRefreshLogout(t *testing.T) {
	_, _, h := newTestService(t)

	if rec := do(t, h, call{method: "GET", path: "/api/auth/me"}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("me without session: %d", rec.Code)
	}

	rec := do(t, h, call{method: "POST", path: "/api/auth/login", body: loginBody(" ADMIN ", testPassword)})
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}
	var sess SessionResponse
	if err := json.NewDecoder(rec.Body).Decode(&sess); err != nil {
		t.Fatal(err)
	}
	if sess.User.Login != "admin" || sess.User.Role != RoleAdmin || sess.AccessToken == "" {
		t.Fatalf("session = %+v", sess)
	}
	access, refresh := cookie(rec, AccessCookie), cookie(rec, RefreshCookie)
	if access == nil || refresh == nil || !access.HttpOnly || !refresh.HttpOnly ||
		access.SameSite != http.SameSiteLaxMode || refresh.Path != "/api/auth" {
		t.Fatalf("cookies: access=%+v refresh=%+v", access, refresh)
	}

	for name, c := range map[string]call{
		"cookie": {method: "GET", path: "/api/auth/me", cookies: []*http.Cookie{access}},
		"bearer": {method: "GET", path: "/api/auth/me", bearer: sess.AccessToken},
	} {
		rec := do(t, h, c)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"login":"admin"`) {
			t.Fatalf("me via %s: %d %s", name, rec.Code, rec.Body)
		}
	}

	rec = do(t, h, call{method: "POST", path: "/api/auth/refresh", cookies: []*http.Cookie{refresh}})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh: %d %s", rec.Code, rec.Body)
	}
	refresh2 := cookie(rec, RefreshCookie)
	if refresh2 == nil || refresh2.Value == refresh.Value {
		t.Fatal("refresh token not rotated")
	}
	if rec := do(t, h, call{method: "POST", path: "/api/auth/refresh", cookies: []*http.Cookie{refresh}}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("reused refresh token: %d", rec.Code)
	}

	rec = do(t, h, call{method: "POST", path: "/api/auth/logout", cookies: []*http.Cookie{refresh2}})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout: %d", rec.Code)
	}
	if c := cookie(rec, AccessCookie); c == nil || c.MaxAge >= 0 {
		t.Fatalf("access cookie not cleared: %+v", c)
	}
	if rec := do(t, h, call{method: "POST", path: "/api/auth/refresh", cookies: []*http.Cookie{refresh2}}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("refresh after logout: %d", rec.Code)
	}
}

func TestLoginErrors(t *testing.T) {
	_, st, h := newTestService(t)

	tests := []struct {
		name, body string
		want       int
		code       string
	}{
		{"wrong password", loginBody("admin", "nope"), http.StatusUnauthorized, "invalid_credentials"},
		{"unknown user", loginBody("ghost", "nope"), http.StatusUnauthorized, "invalid_credentials"},
		{"empty", loginBody("", ""), http.StatusBadRequest, "invalid_request"},
		{"unknown field", `{"login":"a","password":"b","x":1}`, http.StatusBadRequest, "invalid_request"},
		{"not json", `nope`, http.StatusBadRequest, "invalid_request"},
	}
	for _, tc := range tests {
		rec := do(t, h, call{method: "POST", path: "/api/auth/login", body: tc.body, remoteAddr: "10.0.0.1:1234"})
		if rec.Code != tc.want || !strings.Contains(rec.Body.String(), `"code":"`+tc.code+`"`) {
			t.Errorf("%s: %d %s", tc.name, rec.Code, rec.Body)
		}
	}

	if _, err := st.Pool().Exec(t.Context(), `UPDATE users SET disabled = true WHERE login = 'admin'`); err != nil {
		t.Fatal(err)
	}
	rec := do(t, h, call{method: "POST", path: "/api/auth/login", body: loginBody("admin", testPassword)})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("disabled account: %d %s", rec.Code, rec.Body)
	}
}

func TestLoginRateLimit(t *testing.T) {
	_, _, h := newTestService(t)
	for i := range loginFailThreshold {
		rec := do(t, h, call{method: "POST", path: "/api/auth/login", body: loginBody("admin", "wrong"), remoteAddr: "10.0.0.2:1"})
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d", i, rec.Code)
		}
	}
	// Locked now, even with the right password and from another address.
	rec := do(t, h, call{method: "POST", path: "/api/auth/login", body: loginBody("admin", testPassword), remoteAddr: "10.0.0.3:1"})
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("locked login: %d %s", rec.Code, rec.Body)
	}
}

func TestClientIP(t *testing.T) {
	s := &Service{}
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	r.RemoteAddr = "192.0.2.1:5555"
	r.Header.Set("X-Forwarded-For", "1.1.1.1, 203.0.113.9")
	if ip := s.clientIP(r); ip != "192.0.2.1" {
		t.Errorf("untrusted proxy: %s", ip)
	}
	s.cfg.TrustProxy = true
	if ip := s.clientIP(r); ip != "203.0.113.9" {
		t.Errorf("trusted proxy: %s", ip)
	}
}
