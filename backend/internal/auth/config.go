package auth

import (
	"errors"
	"fmt"
	"strconv"
	"time"
)

// Default token lifetimes.
const (
	DefaultAccessTTL  = 15 * time.Minute
	DefaultRefreshTTL = 30 * 24 * time.Hour
)

// minSecretLen is the minimum JWT_SECRET length for HS256 (256 bits).
const minSecretLen = 32

// devSecret is used only when APP_ENV=dev and JWT_SECRET is unset.
const devSecret = "dev-only-insecure-jwt-secret-change-me!!"

// Config configures sessions and tokens.
type Config struct {
	// Secret signs the HS256 access tokens.
	Secret []byte
	// AccessTTL is the lifetime of an access token (JWT).
	AccessTTL time.Duration
	// RefreshTTL is the lifetime of a refresh session.
	RefreshTTL time.Duration
	// CookieSecure sets the Secure attribute on session cookies.
	CookieSecure bool
	// TrustProxy takes the client IP for login rate limiting from X-Forwarded-For
	// (the last hop, as set by the reverse proxy) instead of the TCP peer address.
	TrustProxy bool
	// AdminLogin and AdminPassword bootstrap the first admin account.
	AdminLogin    string
	AdminPassword string
	// Dev reports APP_ENV=dev.
	Dev bool
}

// ConfigFromEnv reads the configuration from environment variables:
//
//	APP_ENV         "dev" enables development defaults (insecure JWT secret, non-Secure cookies)
//	JWT_SECRET      HS256 signing key, at least 32 bytes; required unless APP_ENV=dev
//	COOKIE_SECURE   "true"/"false"; defaults to true, false when APP_ENV=dev
//	TRUST_PROXY     "true" to take the client IP from X-Forwarded-For; default false
//	ADMIN_LOGIN     login of the bootstrap admin
//	ADMIN_PASSWORD  password of the bootstrap admin
//
// The returned warnings describe insecure development defaults in effect.
func ConfigFromEnv(getenv func(string) string) (Config, []string, error) {
	cfg := Config{
		AccessTTL:     DefaultAccessTTL,
		RefreshTTL:    DefaultRefreshTTL,
		AdminLogin:    getenv("ADMIN_LOGIN"),
		AdminPassword: getenv("ADMIN_PASSWORD"),
		Dev:           getenv("APP_ENV") == "dev",
	}
	var warnings []string

	switch secret := getenv("JWT_SECRET"); {
	case secret == "" && cfg.Dev:
		cfg.Secret = []byte(devSecret)
		warnings = append(warnings, "JWT_SECRET is not set, using the insecure development secret (APP_ENV=dev)")
	case secret == "":
		return Config{}, nil, errors.New("JWT_SECRET is required (set APP_ENV=dev to use a development secret)")
	case len(secret) < minSecretLen:
		return Config{}, nil, fmt.Errorf("JWT_SECRET must be at least %d bytes", minSecretLen)
	default:
		cfg.Secret = []byte(secret)
	}

	var err error
	if cfg.CookieSecure, err = boolEnv(getenv, "COOKIE_SECURE", !cfg.Dev); err != nil {
		return Config{}, nil, err
	}
	if cfg.TrustProxy, err = boolEnv(getenv, "TRUST_PROXY", false); err != nil {
		return Config{}, nil, err
	}
	return cfg, warnings, nil
}

func boolEnv(getenv func(string) string, name string, def bool) (bool, error) {
	v := getenv(name)
	if v == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s: %w", name, err)
	}
	return b, nil
}
