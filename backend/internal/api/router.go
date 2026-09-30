// Package api exposes the REST/WebSocket HTTP interface.
package api

import (
	"crypto/rand"
	"log/slog"
	"maps"
	"net/http"
	"slices"

	"github.com/istu-pro-dev/timetable/backend/internal/auth"
	"github.com/istu-pro-dev/timetable/backend/internal/httpx"
	"github.com/istu-pro-dev/timetable/backend/internal/store"
)

// Deps are the router's dependencies.
type Deps struct {
	// Store is nil when the server runs without a database; data endpoints then answer 503.
	Store *store.Store
	// Auth handles sessions. When nil, a service with a random secret and no store is used.
	Auth   *auth.Service
	Logger *slog.Logger
}

type server struct {
	store  *store.Store
	auth   *auth.Service
	logger *slog.Logger
	mux    *http.ServeMux
	routes []string
}

// NewRouter builds the HTTP handler with all API routes.
func NewRouter(d Deps) http.Handler {
	return newServer(d).handler()
}

func newServer(d Deps) *server {
	if d.Logger == nil {
		d.Logger = slog.New(slog.DiscardHandler)
	}
	if d.Auth == nil {
		d.Auth = auth.NewService(nil, auth.Config{Secret: []byte(rand.Text() + rand.Text())}, d.Logger)
	}
	s := &server{store: d.Store, auth: d.Auth, logger: d.Logger, mux: http.NewServeMux()}

	s.handle("GET /api/healthz", http.HandlerFunc(handleHealthz))
	authRoutes := d.Auth.Routes()
	for _, pattern := range slices.Sorted(maps.Keys(authRoutes)) {
		s.handle(pattern, authRoutes[pattern])
	}
	return s
}

// handle registers a route and remembers its pattern (the OpenAPI test checks every pattern
// against the spec).
func (s *server) handle(pattern string, h http.Handler) {
	s.routes = append(s.routes, pattern)
	s.mux.Handle(pattern, h)
}

func (s *server) handler() http.Handler {
	return s.auth.Authenticate(s.mux)
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
