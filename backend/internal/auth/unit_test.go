package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestPasswordHash(t *testing.T) {
	h, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("unexpected hash format %q", h)
	}
	if ok, err := VerifyPassword(h, "correct horse"); err != nil || !ok {
		t.Fatalf("verify correct = %v, %v", ok, err)
	}
	if ok, err := VerifyPassword(h, "wrong horse"); err != nil || ok {
		t.Fatalf("verify wrong = %v, %v", ok, err)
	}
	h2, _ := HashPassword("correct horse")
	if h == h2 {
		t.Fatal("hashes of the same password must differ (salt)")
	}
	for _, bad := range []string{"", "plain", "$argon2i$v=19$m=1,t=1,p=1$AA$AA", "$argon2id$v=19$m=x$AA$AA", "$argon2id$v=19$m=1,t=1,p=1$!!$AA"} {
		if _, err := VerifyPassword(bad, "x"); err == nil {
			t.Errorf("VerifyPassword(%q) accepted a malformed hash", bad)
		}
	}
}

func TestTokens(t *testing.T) {
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	tok := NewTokens([]byte(strings.Repeat("k", 32)), time.Minute)
	tok.now = func() time.Time { return now }

	p := Principal{UserID: 7, Login: "admin", Role: RoleAdmin, SessionID: 3}
	s, exp, err := tok.Issue(p)
	if err != nil {
		t.Fatal(err)
	}
	if !exp.Equal(now.Add(time.Minute)) {
		t.Fatalf("exp = %v", exp)
	}
	got, err := tok.Parse(s)
	if err != nil || got != p {
		t.Fatalf("Parse = %+v, %v; want %+v", got, err, p)
	}

	other := NewTokens([]byte(strings.Repeat("x", 32)), time.Minute)
	if _, err := other.Parse(s); err == nil {
		t.Error("token signed with another secret accepted")
	}

	tok.now = func() time.Time { return now.Add(2 * time.Minute) }
	if _, err := tok.Parse(s); err == nil {
		t.Error("expired token accepted")
	}

	none, err := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
		"sub": "1", "role": "admin", "iss": tokenIssuer, "exp": now.Add(time.Hour).Unix(),
	}).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	tok.now = func() time.Time { return now }
	if _, err := tok.Parse(none); err == nil {
		t.Error("alg=none token accepted")
	}
}

func TestLimiter(t *testing.T) {
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	l := NewLimiter(3, time.Second, 10*time.Second, time.Hour)
	l.now = func() time.Time { return now }

	for range 2 {
		l.Fail("k")
	}
	if d := l.RetryAfter("k"); d != 0 {
		t.Fatalf("locked below threshold: %v", d)
	}
	l.Fail("k") // 3rd failure: 1s
	if d := l.RetryAfter("k"); d != time.Second {
		t.Fatalf("after 3 failures RetryAfter = %v, want 1s", d)
	}
	l.Fail("k") // 2s
	l.Fail("k") // 4s
	if d := l.RetryAfter("k"); d != 4*time.Second {
		t.Fatalf("after 5 failures RetryAfter = %v, want 4s", d)
	}
	for range 10 {
		l.Fail("k")
	}
	if d := l.RetryAfter("k"); d != 10*time.Second {
		t.Fatalf("backoff not capped: %v", d)
	}
	if d := l.RetryAfter("other"); d != 0 {
		t.Fatalf("unrelated key locked: %v", d)
	}

	now = now.Add(11 * time.Second)
	if d := l.RetryAfter("k"); d != 0 {
		t.Fatalf("lock did not expire: %v", d)
	}
	l.Reset("k")
	l.Fail("k")
	if d := l.RetryAfter("k"); d != 0 {
		t.Fatalf("reset did not clear the streak: %v", d)
	}

	for range 3 {
		l.Fail("old")
	}
	now = now.Add(2 * time.Hour)
	l.Fail("old")
	if d := l.RetryAfter("old"); d != 0 {
		t.Fatalf("stale streak not forgotten: %v", d)
	}
}

func TestConfigFromEnv(t *testing.T) {
	env := func(kv map[string]string) func(string) string {
		return func(k string) string { return kv[k] }
	}
	if _, _, err := ConfigFromEnv(env(nil)); err == nil {
		t.Error("missing JWT_SECRET accepted outside dev")
	}
	if _, _, err := ConfigFromEnv(env(map[string]string{"JWT_SECRET": "short"})); err == nil {
		t.Error("short JWT_SECRET accepted")
	}
	cfg, warns, err := ConfigFromEnv(env(map[string]string{"APP_ENV": "dev"}))
	if err != nil || len(warns) != 1 || len(cfg.Secret) < minSecretLen || cfg.CookieSecure {
		t.Errorf("dev config = %+v, %v, %v", cfg, warns, err)
	}
	cfg, warns, err = ConfigFromEnv(env(map[string]string{"JWT_SECRET": strings.Repeat("s", 32), "TRUST_PROXY": "true"}))
	if err != nil || len(warns) != 0 || !cfg.CookieSecure || !cfg.TrustProxy {
		t.Errorf("prod config = %+v, %v, %v", cfg, warns, err)
	}
	if _, _, err := ConfigFromEnv(env(map[string]string{"APP_ENV": "dev", "COOKIE_SECURE": "maybe"})); err == nil {
		t.Error("bad COOKIE_SECURE accepted")
	}
}

func TestRequire(t *testing.T) {
	svc := NewService(nil, Config{Secret: []byte(strings.Repeat("k", 32))}, nil)
	admin, _, _ := svc.Tokens().Issue(Principal{UserID: 1, Login: "a", Role: RoleAdmin})
	student, _, _ := svc.Tokens().Issue(Principal{UserID: 2, Login: "s", Role: RoleStudent})

	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := FromContext(r.Context())
		if p.UserID == 0 {
			t.Error("principal missing in handler")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	adminOnly := svc.Authenticate(Require(RoleAdmin)(ok))
	anyRole := svc.Authenticate(Require()(ok))

	tests := []struct {
		name    string
		h       http.Handler
		header  string
		cookie  string
		want    int
		errCode string
	}{
		{"no token", anyRole, "", "", http.StatusUnauthorized, "unauthorized"},
		{"garbage bearer", anyRole, "Bearer garbage", "", http.StatusUnauthorized, "unauthorized"},
		{"basic scheme", anyRole, "Basic " + admin, "", http.StatusUnauthorized, "unauthorized"},
		{"bearer admin", adminOnly, "Bearer " + admin, "", http.StatusNoContent, ""},
		{"cookie admin", adminOnly, "", admin, http.StatusNoContent, ""},
		{"student on admin route", adminOnly, "Bearer " + student, "", http.StatusForbidden, "forbidden"},
		{"student on any route", anyRole, "", student, http.StatusNoContent, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			if tc.cookie != "" {
				req.AddCookie(&http.Cookie{Name: AccessCookie, Value: tc.cookie})
			}
			rec := httptest.NewRecorder()
			tc.h.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.want, rec.Body)
			}
			if tc.errCode != "" && !strings.Contains(rec.Body.String(), `"code":"`+tc.errCode+`"`) {
				t.Fatalf("body = %s, want error code %s", rec.Body, tc.errCode)
			}
		})
	}
}

func TestEndpointsWithoutDatabase(t *testing.T) {
	svc := NewService(nil, Config{Secret: []byte(strings.Repeat("k", 32))}, nil)
	h := svc.Routes()["POST /api/auth/login"]
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/auth/login",
		strings.NewReader(`{"login":"a","password":"b"}`)))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}
