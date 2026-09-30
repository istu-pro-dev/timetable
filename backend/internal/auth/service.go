package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/istu-pro-dev/timetable/backend/internal/httpx"
	"github.com/istu-pro-dev/timetable/backend/internal/store"
	"github.com/istu-pro-dev/timetable/backend/internal/store/db"
)

// Input limits.
const (
	maxLoginLen       = 64
	maxPasswordLen    = 256
	minAdminPassword  = 8
	loginBodyMaxBytes = 4 << 10
)

// Login rate limiting: per login 5 free failures, per client IP 20, then exponential backoff
// from 1 s up to 15 min; a streak is forgotten after an hour without failures.
const (
	loginFailThreshold = 5
	ipFailThreshold    = 20
	backoffBase        = time.Second
	backoffMax         = 15 * time.Minute
	failureMemory      = time.Hour
)

// Service implements local login, refresh sessions and the auth HTTP endpoints.
type Service struct {
	store     *store.Store // nil when the server runs without a database
	cfg       Config
	tokens    *Tokens
	byLogin   *Limiter
	byIP      *Limiter
	logger    *slog.Logger
	now       func() time.Time
	randToken func() (string, error)
}

// NewService creates the auth service. st may be nil: the endpoints then answer 503.
func NewService(st *store.Store, cfg Config, logger *slog.Logger) *Service {
	if cfg.AccessTTL <= 0 {
		cfg.AccessTTL = DefaultAccessTTL
	}
	if cfg.RefreshTTL <= 0 {
		cfg.RefreshTTL = DefaultRefreshTTL
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{
		store:     st,
		cfg:       cfg,
		tokens:    NewTokens(cfg.Secret, cfg.AccessTTL),
		byLogin:   NewLimiter(loginFailThreshold, backoffBase, backoffMax, failureMemory),
		byIP:      NewLimiter(ipFailThreshold, backoffBase, backoffMax, failureMemory),
		logger:    logger,
		now:       time.Now,
		randToken: randomToken,
	}
}

// Tokens returns the access-token issuer (for tests and non-HTTP clients).
func (s *Service) Tokens() *Tokens { return s.tokens }

// Routes returns the auth endpoints keyed by ServeMux pattern.
func (s *Service) Routes() map[string]http.Handler {
	return map[string]http.Handler{
		"POST /api/auth/login":   http.HandlerFunc(s.handleLogin),
		"POST /api/auth/refresh": http.HandlerFunc(s.handleRefresh),
		"POST /api/auth/logout":  http.HandlerFunc(s.handleLogout),
		"GET /api/auth/me":       Require()(http.HandlerFunc(s.handleMe)),
	}
}

// NormalizeLogin trims and lower-cases a login.
func NormalizeLogin(login string) string { return strings.ToLower(strings.TrimSpace(login)) }

// Bootstrap creates the first admin from Config.AdminLogin/AdminPassword when no active admin
// exists. It is idempotent: with an admin in place it does nothing.
func (s *Service) Bootstrap(ctx context.Context) error {
	if s.store == nil {
		return nil
	}
	n, err := s.store.CountActiveAdmins(ctx)
	if err != nil {
		return fmt.Errorf("count admins: %w", err)
	}
	if n > 0 {
		return nil
	}
	login := NormalizeLogin(s.cfg.AdminLogin)
	if login == "" || s.cfg.AdminPassword == "" {
		s.logger.Warn("no admin account exists; set ADMIN_LOGIN and ADMIN_PASSWORD to create one on startup")
		return nil
	}
	if len(login) > maxLoginLen {
		return fmt.Errorf("ADMIN_LOGIN is longer than %d characters", maxLoginLen)
	}
	if len(s.cfg.AdminPassword) < minAdminPassword || len(s.cfg.AdminPassword) > maxPasswordLen {
		return fmt.Errorf("ADMIN_PASSWORD must be %d to %d characters long", minAdminPassword, maxPasswordLen)
	}
	hash, err := HashPassword(s.cfg.AdminPassword)
	if err != nil {
		return err
	}
	_, err = s.store.CreateUser(ctx, db.CreateUserParams{
		Login: login, PasswordHash: hash, Role: RoleAdmin, DisplayName: "Administrator",
	})
	if err != nil {
		if store.IsUniqueViolation(err) {
			return fmt.Errorf("bootstrap admin: login %q is taken by an account that is not an active admin", login)
		}
		return fmt.Errorf("bootstrap admin: %w", err)
	}
	s.logger.Info("bootstrap admin created", "login", login)
	return nil
}

// UserResponse is the public view of an account.
type UserResponse struct {
	ID          int64       `json:"id"`
	Login       string      `json:"login"`
	Role        db.UserRole `json:"role"`
	DisplayName string      `json:"display_name"`
	TeacherID   *int64      `json:"teacher_id"`
	GroupID     *int64      `json:"group_id"`
	CreatedAt   time.Time   `json:"created_at"`
}

func userResponse(u db.User) UserResponse {
	return UserResponse{
		ID: u.ID, Login: u.Login, Role: u.Role, DisplayName: u.DisplayName,
		TeacherID: int8Ptr(u.TeacherID), GroupID: int8Ptr(u.GroupID), CreatedAt: u.CreatedAt.Time,
	}
}

func int8Ptr(v pgtype.Int8) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}

// SessionResponse is returned by login and refresh. The access token is also set as an
// HttpOnly cookie; it is in the body for API clients that use the Authorization header.
type SessionResponse struct {
	User        UserResponse `json:"user"`
	AccessToken string       `json:"access_token"`
	ExpiresAt   time.Time    `json:"expires_at"`
}

type loginRequest struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

func (s *Service) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeNoDB(w)
		return
	}
	var req loginRequest
	if err := httpx.DecodeJSON(w, r, &req, loginBodyMaxBytes); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	login := NormalizeLogin(req.Login)
	if login == "" || req.Password == "" || len(login) > maxLoginLen || len(req.Password) > maxPasswordLen {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_request",
			fmt.Sprintf("login (1–%d chars) and password (1–%d chars) are required", maxLoginLen, maxPasswordLen))
		return
	}

	ipKey := "ip:" + s.clientIP(r)
	loginKey := "login:" + login
	if d := max(s.byLogin.RetryAfter(loginKey), s.byIP.RetryAfter(ipKey)); d > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int((d+time.Second-1)/time.Second)))
		httpx.WriteError(w, http.StatusTooManyRequests, "too_many_attempts", "too many failed login attempts, try again later")
		return
	}

	ctx := r.Context()
	user, err := s.store.GetUserByLogin(ctx, login)
	hash := user.PasswordHash
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		hash = dummyHash
	case err != nil:
		s.internalError(w, "load user", err)
		return
	}
	ok, verr := VerifyPassword(hash, req.Password)
	if err != nil || verr != nil || !ok {
		if verr != nil {
			s.logger.Error("verify password", "login", login, "err", verr)
		}
		s.byLogin.Fail(loginKey)
		s.byIP.Fail(ipKey)
		httpx.WriteError(w, http.StatusUnauthorized, "invalid_credentials", "invalid login or password")
		return
	}
	if user.Disabled {
		httpx.WriteError(w, http.StatusForbidden, "account_disabled", "the account is disabled")
		return
	}
	s.byLogin.Reset(loginKey)

	if _, err := s.store.DeleteExpiredSessions(ctx); err != nil {
		s.logger.Warn("delete expired sessions", "err", err)
	}
	resp, refresh, err := s.startSession(ctx, s.store.Queries, user)
	if err != nil {
		s.internalError(w, "start session", err)
		return
	}
	s.setCookies(w, resp.AccessToken, refresh)
	s.logger.Info("login", "user_id", user.ID, "login", user.Login)
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (s *Service) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeNoDB(w)
		return
	}
	c, err := r.Cookie(RefreshCookie)
	if err != nil || c.Value == "" {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthorized", "no refresh session")
		return
	}
	ctx := r.Context()
	var (
		resp    SessionResponse
		refresh string
	)
	errInvalid := errors.New("invalid session")
	err = s.store.InTx(ctx, func(q *db.Queries, _ pgx.Tx) error {
		sess, err := q.GetSessionByTokenHash(ctx, hashToken(c.Value))
		if errors.Is(err, pgx.ErrNoRows) {
			return errInvalid
		}
		if err != nil {
			return err
		}
		// Rotation: the old refresh token is single-use. A concurrent refresh with the same
		// token deletes nothing and fails.
		n, err := q.DeleteSession(ctx, sess.ID)
		if err != nil {
			return err
		}
		if n == 0 || !sess.ExpiresAt.Time.After(s.now()) {
			return errInvalid
		}
		user, err := q.GetUser(ctx, sess.UserID)
		if err != nil {
			return err
		}
		if user.Disabled {
			return errInvalid
		}
		resp, refresh, err = s.startSession(ctx, q, user)
		return err
	})
	switch {
	case errors.Is(err, errInvalid):
		s.clearCookies(w)
		httpx.WriteError(w, http.StatusUnauthorized, "unauthorized", "refresh session is invalid or expired")
	case err != nil:
		s.internalError(w, "refresh session", err)
	default:
		s.setCookies(w, resp.AccessToken, refresh)
		httpx.WriteJSON(w, http.StatusOK, resp)
	}
}

func (s *Service) handleLogout(w http.ResponseWriter, r *http.Request) {
	if s.store != nil {
		ctx := r.Context()
		var sessionID int64
		if c, err := r.Cookie(RefreshCookie); err == nil && c.Value != "" {
			if sess, err := s.store.GetSessionByTokenHash(ctx, hashToken(c.Value)); err == nil {
				sessionID = sess.ID
			}
		}
		if p, ok := FromContext(ctx); ok && sessionID == 0 {
			sessionID = p.SessionID
		}
		if sessionID != 0 {
			if _, err := s.store.DeleteSession(ctx, sessionID); err != nil {
				s.internalError(w, "delete session", err)
				return
			}
		}
	}
	s.clearCookies(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleMe(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeNoDB(w)
		return
	}
	p, _ := FromContext(r.Context())
	user, err := s.store.GetUser(r.Context(), p.UserID)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && user.Disabled {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthorized", "the account no longer exists or is disabled")
		return
	}
	if err != nil {
		s.internalError(w, "load user", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, userResponse(user))
}

// startSession stores a new refresh session for user and issues an access token. It returns
// the response body and the opaque refresh token; the caller sets the cookies once the session
// is committed.
func (s *Service) startSession(ctx context.Context, q *db.Queries, user db.User) (SessionResponse, string, error) {
	refresh, err := s.randToken()
	if err != nil {
		return SessionResponse{}, "", err
	}
	refreshExp := s.now().Add(s.cfg.RefreshTTL)
	sess, err := q.CreateSession(ctx, db.CreateSessionParams{
		UserID:    user.ID,
		TokenHash: hashToken(refresh),
		ExpiresAt: pgtype.Timestamptz{Time: refreshExp, Valid: true},
	})
	if err != nil {
		return SessionResponse{}, "", fmt.Errorf("create session: %w", err)
	}
	access, accessExp, err := s.tokens.Issue(Principal{
		UserID: user.ID, Login: user.Login, Role: user.Role, SessionID: sess.ID,
	})
	if err != nil {
		return SessionResponse{}, "", err
	}
	return SessionResponse{User: userResponse(user), AccessToken: access, ExpiresAt: accessExp}, refresh, nil
}

func (s *Service) setCookies(w http.ResponseWriter, access, refresh string) {
	http.SetCookie(w, s.cookie(AccessCookie, access, "/", int(s.cfg.AccessTTL/time.Second)))
	http.SetCookie(w, s.cookie(RefreshCookie, refresh, "/api/auth", int(s.cfg.RefreshTTL/time.Second)))
}

func (s *Service) cookie(name, value, path string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     path,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	}
}

func (s *Service) clearCookies(w http.ResponseWriter) {
	http.SetCookie(w, s.cookie(AccessCookie, "", "/", -1))
	http.SetCookie(w, s.cookie(RefreshCookie, "", "/api/auth", -1))
}

// clientIP returns the address used for per-IP rate limiting.
func (s *Service) clientIP(r *http.Request) string {
	if s.cfg.TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			hops := strings.Split(xff, ",")
			if ip := strings.TrimSpace(hops[len(hops)-1]); ip != "" {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Service) internalError(w http.ResponseWriter, what string, err error) {
	s.logger.Error(what, "err", err)
	httpx.WriteError(w, http.StatusInternalServerError, "internal", "internal server error")
}

func writeNoDB(w http.ResponseWriter) {
	httpx.WriteError(w, http.StatusServiceUnavailable, "no_database", "the server runs without a database")
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
