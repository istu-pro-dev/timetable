// Package api exposes the REST/WebSocket HTTP interface.
package api

import (
	"crypto/rand"
	"log/slog"
	"maps"
	"net/http"
	"slices"

	apispec "github.com/istu-pro-dev/timetable/backend/api"
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
	s.handle("GET /api/openapi.yaml", http.HandlerFunc(handleOpenAPI))
	authRoutes := d.Auth.Routes()
	for _, pattern := range slices.Sorted(maps.Keys(authRoutes)) {
		s.handle(pattern, authRoutes[pattern])
	}
	s.registerReference()
	return s
}

// registerReference registers the reference-data CRUD endpoints. Reads need any authenticated
// user, changes need the admin role; every change is audited.
func (s *server) registerReference() {
	read := func(pattern string, h http.HandlerFunc) { s.handle(pattern, auth.Require()(s.withStore(h))) }
	write := func(pattern string, h http.HandlerFunc) {
		s.handle(pattern, auth.Require(auth.RoleAdmin)(s.withStore(h)))
	}

	read("GET /api/buildings", s.listBuildings)
	write("POST /api/buildings", s.createBuilding)
	read("GET /api/buildings/{id}", s.getBuilding)
	write("PUT /api/buildings/{id}", s.updateBuilding)
	write("DELETE /api/buildings/{id}", s.deleteBuilding)

	read("GET /api/room-types", s.listRoomTypes)
	write("POST /api/room-types", s.createRoomType)
	read("GET /api/room-types/{code}", s.getRoomType)
	write("PUT /api/room-types/{code}", s.updateRoomType)
	write("DELETE /api/room-types/{code}", s.deleteRoomType)

	read("GET /api/rooms", s.listRooms)
	write("POST /api/rooms", s.createRoom)
	read("GET /api/rooms/{id}", s.getRoom)
	write("PUT /api/rooms/{id}", s.updateRoom)
	write("DELETE /api/rooms/{id}", s.deleteRoom)
	read("GET /api/rooms/{id}/availability", s.getRoomAvailability)
	write("PUT /api/rooms/{id}/availability", s.putRoomAvailability)

	read("GET /api/groups", s.listGroups)
	write("POST /api/groups", s.createGroup)
	read("GET /api/groups/{id}", s.getGroup)
	write("PUT /api/groups/{id}", s.updateGroup)
	write("DELETE /api/groups/{id}", s.deleteGroup)
	read("GET /api/groups/{id}/subgroups", s.listSubgroups)
	write("POST /api/groups/{id}/subgroups", s.createSubgroup)
	read("GET /api/groups/{id}/subgroups/{subgroup_id}", s.getSubgroup)
	write("PUT /api/groups/{id}/subgroups/{subgroup_id}", s.updateSubgroup)
	write("DELETE /api/groups/{id}/subgroups/{subgroup_id}", s.deleteSubgroup)

	read("GET /api/teachers", s.listTeachers)
	write("POST /api/teachers", s.createTeacher)
	read("GET /api/teachers/{id}", s.getTeacher)
	write("PUT /api/teachers/{id}", s.updateTeacher)
	write("DELETE /api/teachers/{id}", s.deleteTeacher)
	read("GET /api/teachers/{id}/availability", s.getTeacherAvailability)
	write("PUT /api/teachers/{id}/availability", s.putTeacherAvailability)

	read("GET /api/disciplines", s.listDisciplines)
	write("POST /api/disciplines", s.createDiscipline)
	read("GET /api/disciplines/{id}", s.getDiscipline)
	write("PUT /api/disciplines/{id}", s.updateDiscipline)
	write("DELETE /api/disciplines/{id}", s.deleteDiscipline)

	read("GET /api/time-grid", s.getTimeGrid)
	write("PUT /api/time-grid", s.putTimeGrid)
	read("GET /api/periods", s.listPeriods)
	read("GET /api/periods/{number}", s.getPeriod)
	write("PUT /api/periods/{number}", s.putPeriod)
	write("DELETE /api/periods/{number}", s.deletePeriod)

	read("GET /api/curriculum-items", s.listCurriculumItems)
	write("POST /api/curriculum-items", s.createCurriculumItem)
	read("GET /api/curriculum-items/{id}", s.getCurriculumItem)
	write("PUT /api/curriculum-items/{id}", s.updateCurriculumItem)
	write("DELETE /api/curriculum-items/{id}", s.deleteCurriculumItem)
}

func handleOpenAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	_, _ = w.Write(apispec.OpenAPI)
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
