package auth

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/istu-pro-dev/timetable/backend/internal/store/db"
)

const tokenIssuer = "timetable"

// claims is the JWT payload of an access token.
type claims struct {
	Login     string      `json:"login"`
	Role      db.UserRole `json:"role"`
	SessionID int64       `json:"sid,omitempty"`
	jwt.RegisteredClaims
}

// Tokens signs and verifies HS256 access tokens.
type Tokens struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

// NewTokens creates a token issuer.
func NewTokens(secret []byte, ttl time.Duration) *Tokens {
	return &Tokens{secret: secret, ttl: ttl, now: time.Now}
}

// Issue signs an access token for p and returns it with its expiry time.
func (t *Tokens) Issue(p Principal) (string, time.Time, error) {
	now := t.now()
	exp := now.Add(t.ttl)
	c := claims{
		Login:     p.Login,
		Role:      p.Role,
		SessionID: p.SessionID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    tokenIssuer,
			Subject:   strconv.FormatInt(p.UserID, 10),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(t.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign token: %w", err)
	}
	return s, exp, nil
}

// Parse verifies an access token and returns its principal.
func (t *Tokens) Parse(token string) (Principal, error) {
	var c claims
	_, err := jwt.ParseWithClaims(token, &c, func(*jwt.Token) (any, error) { return t.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(tokenIssuer),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(t.now),
	)
	if err != nil {
		return Principal{}, err
	}
	id, err := strconv.ParseInt(c.Subject, 10, 64)
	if err != nil || id <= 0 {
		return Principal{}, errors.New("token: bad subject")
	}
	if !c.Role.Valid() {
		return Principal{}, errors.New("token: bad role")
	}
	return Principal{UserID: id, Login: c.Login, Role: c.Role, SessionID: c.SessionID}, nil
}
