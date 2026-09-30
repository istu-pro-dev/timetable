package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/istu-pro-dev/timetable/backend/internal/auth"
	"github.com/istu-pro-dev/timetable/backend/internal/store"
	"github.com/istu-pro-dev/timetable/backend/internal/store/db"
	"github.com/istu-pro-dev/timetable/backend/internal/store/storetest"
)

// testAPI is a router on a fresh database with an admin and a student token.
type testAPI struct {
	t       *testing.T
	st      *store.Store
	h       http.Handler
	admin   string
	adminID int64
	student string
}

func newTestAPI(t *testing.T) *testAPI {
	t.Helper()
	st := storetest.New(t)
	svc := auth.NewService(st, auth.Config{Secret: []byte(strings.Repeat("k", 32))}, nil)
	a := &testAPI{t: t, st: st, h: NewRouter(Deps{Store: st, Auth: svc})}
	for _, u := range []struct {
		login string
		role  db.UserRole
		token *string
	}{{"admin", auth.RoleAdmin, &a.admin}, {"student", auth.RoleStudent, &a.student}} {
		row, err := st.CreateUser(t.Context(), db.CreateUserParams{Login: u.login, PasswordHash: "x", Role: u.role})
		if err != nil {
			t.Fatal(err)
		}
		tok, _, err := svc.Tokens().Issue(auth.Principal{UserID: row.ID, Login: row.Login, Role: row.Role})
		if err != nil {
			t.Fatal(err)
		}
		*u.token = tok
		if u.role == auth.RoleAdmin {
			a.adminID = row.ID
		}
	}
	return a
}

type response struct {
	code int
	body []byte
}

// errCode returns error.code of an error response.
func (r response) errCode() string {
	var e struct {
		Error struct{ Code string } `json:"error"`
	}
	_ = json.Unmarshal(r.body, &e)
	return e.Error.Code
}

func (r response) decode(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.body, v); err != nil {
		t.Fatalf("decode %s: %v", r.body, err)
	}
}

func (a *testAPI) call(token, method, path string, body any) response {
	a.t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			a.t.Fatal(err)
		}
		rd = bytes.NewReader(raw)
	}
	req := httptest.NewRequestWithContext(a.t.Context(), method, path, rd)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	a.h.ServeHTTP(rec, req)
	return response{code: rec.Code, body: rec.Body.Bytes()}
}

func (a *testAPI) do(method, path string, body any) response {
	a.t.Helper()
	return a.call(a.admin, method, path, body)
}

// must performs an admin request and fails unless it returns want.
func (a *testAPI) must(want int, method, path string, body any) response {
	a.t.Helper()
	r := a.do(method, path, body)
	if r.code != want {
		a.t.Fatalf("%s %s: status %d, want %d: %s", method, path, r.code, want, r.body)
	}
	return r
}

// expectErr performs an admin request and checks the error status and code.
func (a *testAPI) expectErr(status int, code, method, path string, body any) {
	a.t.Helper()
	r := a.do(method, path, body)
	if r.code != status || r.errCode() != code {
		a.t.Errorf("%s %s: got %d %q, want %d %q: %s", method, path, r.code, r.errCode(), status, code, r.body)
	}
}

func (a *testAPI) create(path string, body any) int64 {
	a.t.Helper()
	var out struct{ ID int64 }
	a.must(http.StatusCreated, http.MethodPost, path, body).decode(a.t, &out)
	return out.ID
}

func (a *testAPI) auditCount(entity string) int {
	a.t.Helper()
	var n int
	err := a.st.Pool().QueryRow(a.t.Context(),
		`SELECT count(*) FROM audit_log WHERE entity = $1 AND actor_type = 'human' AND actor_id = $2`,
		entity, fmt.Sprint(a.adminID)).Scan(&n)
	if err != nil {
		a.t.Fatal(err)
	}
	return n
}

func TestCRUDAuthorization(t *testing.T) {
	a := newTestAPI(t)
	if r := a.call("", http.MethodGet, "/api/buildings", nil); r.code != http.StatusUnauthorized || r.errCode() != "unauthorized" {
		t.Errorf("anonymous read: %d %s", r.code, r.body)
	}
	if r := a.call(a.student, http.MethodGet, "/api/buildings", nil); r.code != http.StatusOK || string(r.body) != "[]\n" {
		t.Errorf("student read: %d %s", r.code, r.body)
	}
	if r := a.call(a.student, http.MethodPost, "/api/buildings", map[string]string{"name": "A"}); r.code != http.StatusForbidden || r.errCode() != "forbidden" {
		t.Errorf("student write: %d %s", r.code, r.body)
	}
	if n := a.auditCount("building"); n != 0 {
		t.Errorf("audit rows after rejected writes: %d", n)
	}
}

func TestBuildings(t *testing.T) {
	a := newTestAPI(t)
	id := a.create("/api/buildings", map[string]string{"name": "  Main ", "address": "Lenina 1"})

	var b building
	a.must(http.StatusOK, http.MethodGet, fmt.Sprintf("/api/buildings/%d", id), nil).decode(t, &b)
	if b != (building{ID: id, Name: "Main", Address: "Lenina 1"}) {
		t.Fatalf("building = %+v", b)
	}
	a.must(http.StatusOK, http.MethodPut, fmt.Sprintf("/api/buildings/%d", id), map[string]string{"name": "Main 2"}).decode(t, &b)
	if b.Name != "Main 2" || b.Address != "" {
		t.Fatalf("updated = %+v", b)
	}
	var list []building
	a.must(http.StatusOK, http.MethodGet, "/api/buildings", nil).decode(t, &list)
	if len(list) != 1 {
		t.Fatalf("list = %+v", list)
	}

	a.expectErr(http.StatusConflict, "already_exists", http.MethodPost, "/api/buildings", map[string]string{"name": "Main 2"})
	a.expectErr(http.StatusUnprocessableEntity, "validation_failed", http.MethodPost, "/api/buildings", map[string]string{"name": " "})
	a.expectErr(http.StatusUnprocessableEntity, "validation_failed", http.MethodPost, "/api/buildings", map[string]string{"name": strings.Repeat("я", maxNameLen+1)})
	a.expectErr(http.StatusBadRequest, "invalid_request", http.MethodPost, "/api/buildings", `{"name":"B","floors":3}`)
	a.expectErr(http.StatusBadRequest, "invalid_request", http.MethodPost, "/api/buildings", `{"name":`)
	a.expectErr(http.StatusBadRequest, "invalid_request", http.MethodPost, "/api/buildings", `{"name":1}`)
	a.expectErr(http.StatusBadRequest, "invalid_request", http.MethodGet, "/api/buildings/abc", nil)
	a.expectErr(http.StatusNotFound, "not_found", http.MethodGet, "/api/buildings/999", nil)
	a.expectErr(http.StatusNotFound, "not_found", http.MethodPut, "/api/buildings/999", map[string]string{"name": "X"})
	a.expectErr(http.StatusNotFound, "not_found", http.MethodDelete, "/api/buildings/999", nil)

	a.create("/api/room-types", map[string]string{"code": "lecture", "name": "Lecture hall"})
	roomID := a.create("/api/rooms", map[string]any{"building_id": id, "name": "101", "room_type": "lecture", "capacity": 100})
	a.expectErr(http.StatusConflict, "in_use", http.MethodDelete, fmt.Sprintf("/api/buildings/%d", id), nil)
	a.must(http.StatusNoContent, http.MethodDelete, fmt.Sprintf("/api/rooms/%d", roomID), nil)
	a.must(http.StatusNoContent, http.MethodDelete, fmt.Sprintf("/api/buildings/%d", id), nil)
	a.expectErr(http.StatusNotFound, "not_found", http.MethodGet, fmt.Sprintf("/api/buildings/%d", id), nil)

	// create + update + delete are audited; failed changes are not.
	if n := a.auditCount("building"); n != 3 {
		t.Errorf("building audit rows = %d, want 3", n)
	}
	var before, after []byte
	err := a.st.Pool().QueryRow(t.Context(),
		`SELECT before, after FROM audit_log WHERE entity = 'building' AND entity_id = $1 ORDER BY id LIMIT 1 OFFSET 1`,
		fmt.Sprint(id)).Scan(&before, &after)
	if err != nil || !strings.Contains(string(before), `"Main"`) || !strings.Contains(string(after), `"Main 2"`) {
		t.Errorf("update audit before=%s after=%s err=%v", before, after, err)
	}
}

func TestRoomTypesRoomsAvailability(t *testing.T) {
	a := newTestAPI(t)
	bID := a.create("/api/buildings", map[string]string{"name": "B"})

	a.must(http.StatusCreated, http.MethodPost, "/api/room-types", map[string]string{"code": "lab", "name": "Lab"})
	a.expectErr(http.StatusConflict, "already_exists", http.MethodPost, "/api/room-types", map[string]string{"code": "lab", "name": "Lab"})
	a.expectErr(http.StatusUnprocessableEntity, "validation_failed", http.MethodPost, "/api/room-types", map[string]string{"code": "Bad Code", "name": "x"})
	var rt roomType
	a.must(http.StatusOK, http.MethodPut, "/api/room-types/lab", map[string]string{"name": "Computer lab"}).decode(t, &rt)
	if rt != (roomType{Code: "lab", Name: "Computer lab"}) {
		t.Fatalf("room type = %+v", rt)
	}
	a.expectErr(http.StatusNotFound, "not_found", http.MethodGet, "/api/room-types/nope", nil)

	a.expectErr(http.StatusUnprocessableEntity, "invalid_reference", http.MethodPost, "/api/rooms",
		map[string]any{"building_id": 999, "name": "1", "room_type": "lab", "capacity": 10})
	a.expectErr(http.StatusUnprocessableEntity, "invalid_reference", http.MethodPost, "/api/rooms",
		map[string]any{"building_id": bID, "name": "1", "room_type": "gym", "capacity": 10})
	a.expectErr(http.StatusUnprocessableEntity, "validation_failed", http.MethodPost, "/api/rooms",
		map[string]any{"building_id": bID, "name": "1", "room_type": "lab", "capacity": 0})
	roomID := a.create("/api/rooms", map[string]any{"building_id": bID, "name": "1", "room_type": "lab", "capacity": 10})
	a.expectErr(http.StatusConflict, "already_exists", http.MethodPost, "/api/rooms",
		map[string]any{"building_id": bID, "name": "1", "room_type": "lab", "capacity": 20})
	var rm room
	a.must(http.StatusOK, http.MethodPut, fmt.Sprintf("/api/rooms/%d", roomID),
		map[string]any{"building_id": bID, "name": "1a", "room_type": "lab", "capacity": 12}).decode(t, &rm)
	if rm.Name != "1a" || rm.Capacity != 12 {
		t.Fatalf("room = %+v", rm)
	}
	var rooms []room
	a.must(http.StatusOK, http.MethodGet, fmt.Sprintf("/api/rooms?building_id=%d&room_type=lab", bID), nil).decode(t, &rooms)
	if len(rooms) != 1 {
		t.Fatalf("filtered rooms = %+v", rooms)
	}
	a.must(http.StatusOK, http.MethodGet, "/api/rooms?room_type=lecture", nil).decode(t, &rooms)
	if len(rooms) != 0 {
		t.Fatalf("filtered rooms = %+v", rooms)
	}
	a.expectErr(http.StatusBadRequest, "invalid_request", http.MethodGet, "/api/rooms?building_id=x", nil)
	a.expectErr(http.StatusConflict, "in_use", http.MethodDelete, "/api/room-types/lab", nil)

	path := fmt.Sprintf("/api/rooms/%d/availability", roomID)
	var av availability
	a.must(http.StatusOK, http.MethodGet, path, nil).decode(t, &av)
	if av.Entries == nil || len(av.Entries) != 0 {
		t.Fatalf("empty availability = %+v", av)
	}
	a.must(http.StatusOK, http.MethodPut, path, map[string]any{"entries": []map[string]any{
		{"day": 0, "period": 1, "status": "unavailable"},
		{"day": 2, "period": 3, "parity": "odd", "status": "preferred"},
	}}).decode(t, &av)
	if len(av.Entries) != 2 || av.Entries[0].Parity != db.ParityEvery {
		t.Fatalf("availability = %+v", av)
	}
	a.expectErr(http.StatusUnprocessableEntity, "validation_failed", http.MethodPut, path, map[string]any{"entries": []map[string]any{
		{"day": 0, "period": 1, "status": "unavailable"}, {"day": 0, "period": 1, "status": "preferred"},
	}})
	a.expectErr(http.StatusUnprocessableEntity, "validation_failed", http.MethodPut, path, map[string]any{"entries": []map[string]any{
		{"day": 7, "period": 0, "status": "maybe"},
	}})
	a.expectErr(http.StatusUnprocessableEntity, "validation_failed", http.MethodPut, path, `{}`)
	a.expectErr(http.StatusNotFound, "not_found", http.MethodGet, "/api/rooms/999/availability", nil)
	a.must(http.StatusOK, http.MethodPut, path, map[string]any{"entries": []any{}})
	a.must(http.StatusOK, http.MethodGet, path, nil).decode(t, &av)
	if len(av.Entries) != 0 {
		t.Fatalf("cleared availability = %+v", av)
	}
	if n := a.auditCount("room_availability"); n != 2 {
		t.Errorf("room_availability audit rows = %d, want 2", n)
	}
}

func TestGroupsAndSubgroups(t *testing.T) {
	a := newTestAPI(t)
	gID := a.create("/api/groups", map[string]any{"name": "IVT-21", "size": 25})
	var g group
	a.must(http.StatusOK, http.MethodGet, fmt.Sprintf("/api/groups/%d", gID), nil).decode(t, &g)
	if g.Course != 1 || g.Size != 25 {
		t.Fatalf("group = %+v", g)
	}
	a.must(http.StatusOK, http.MethodPut, fmt.Sprintf("/api/groups/%d", gID), map[string]any{"name": "IVT-21", "course": 2, "size": 24})
	a.expectErr(http.StatusUnprocessableEntity, "validation_failed", http.MethodPost, "/api/groups", map[string]any{"name": "X", "course": 7, "size": -1})
	a.expectErr(http.StatusConflict, "already_exists", http.MethodPost, "/api/groups", map[string]any{"name": "IVT-21", "size": 1})

	sub := fmt.Sprintf("/api/groups/%d/subgroups", gID)
	s1 := a.create(sub, map[string]any{"division": "lab", "part": 1, "size": 12})
	a.create(sub, map[string]any{"division": "lab", "part": 2, "size": 12})
	a.expectErr(http.StatusConflict, "already_exists", http.MethodPost, sub, map[string]any{"division": "lab", "part": 1, "size": 1})
	a.expectErr(http.StatusUnprocessableEntity, "validation_failed", http.MethodPost, sub, map[string]any{"division": "", "part": 0, "size": 1})
	a.expectErr(http.StatusNotFound, "not_found", http.MethodPost, "/api/groups/999/subgroups", map[string]any{"division": "lab", "part": 1, "size": 1})
	a.expectErr(http.StatusNotFound, "not_found", http.MethodGet, "/api/groups/999/subgroups", nil)

	var subs []subgroup
	a.must(http.StatusOK, http.MethodGet, sub, nil).decode(t, &subs)
	if len(subs) != 2 || subs[0].Part != 1 {
		t.Fatalf("subgroups = %+v", subs)
	}
	var s subgroup
	a.must(http.StatusOK, http.MethodPut, fmt.Sprintf("%s/%d", sub, s1), map[string]any{"division": "lab", "part": 1, "size": 13}).decode(t, &s)
	if s.Size != 13 || s.GroupID != gID {
		t.Fatalf("subgroup = %+v", s)
	}

	other := a.create("/api/groups", map[string]any{"name": "IVT-22", "size": 20})
	a.expectErr(http.StatusNotFound, "not_found", http.MethodGet, fmt.Sprintf("/api/groups/%d/subgroups/%d", other, s1), nil)
	a.expectErr(http.StatusNotFound, "not_found", http.MethodDelete, fmt.Sprintf("/api/groups/%d/subgroups/%d", other, s1), nil)
	a.must(http.StatusNoContent, http.MethodDelete, fmt.Sprintf("%s/%d", sub, s1), nil)

	// Deleting a group deletes its subgroups.
	a.must(http.StatusNoContent, http.MethodDelete, fmt.Sprintf("/api/groups/%d", gID), nil)
	var n int
	if err := a.st.Pool().QueryRow(t.Context(), `SELECT count(*) FROM subgroups`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("subgroups left: %d %v", n, err)
	}
	if got := a.auditCount("subgroup"); got != 4 {
		t.Errorf("subgroup audit rows = %d, want 4", got)
	}
}

func TestTeachersAndDisciplines(t *testing.T) {
	a := newTestAPI(t)
	tID := a.create("/api/teachers", map[string]string{"full_name": "Ivanov Ivan", "short_name": "Ivanov I."})
	var tc teacher
	a.must(http.StatusOK, http.MethodPut, fmt.Sprintf("/api/teachers/%d", tID),
		map[string]string{"full_name": "Ivanov Ivan I.", "short_name": "Ivanov I.I."}).decode(t, &tc)
	if tc.FullName != "Ivanov Ivan I." {
		t.Fatalf("teacher = %+v", tc)
	}
	a.expectErr(http.StatusUnprocessableEntity, "validation_failed", http.MethodPost, "/api/teachers", map[string]string{"full_name": "X"})
	var teachers []teacher
	a.must(http.StatusOK, http.MethodGet, "/api/teachers", nil).decode(t, &teachers)
	if len(teachers) != 1 {
		t.Fatalf("teachers = %+v", teachers)
	}

	path := fmt.Sprintf("/api/teachers/%d/availability", tID)
	a.must(http.StatusOK, http.MethodPut, path, map[string]any{"entries": []map[string]any{{"day": 5, "period": 1, "status": "undesired"}}})
	var av availability
	a.must(http.StatusOK, http.MethodGet, path, nil).decode(t, &av)
	if len(av.Entries) != 1 || av.Entries[0].Status != db.AvailabilityUndesired {
		t.Fatalf("availability = %+v", av)
	}
	a.expectErr(http.StatusNotFound, "not_found", http.MethodPut, "/api/teachers/999/availability", map[string]any{"entries": []any{}})

	dID := a.create("/api/disciplines", map[string]string{"name": "Math"})
	a.expectErr(http.StatusConflict, "already_exists", http.MethodPost, "/api/disciplines", map[string]string{"name": "Math"})
	var d discipline
	a.must(http.StatusOK, http.MethodPut, fmt.Sprintf("/api/disciplines/%d", dID), map[string]string{"name": "Algebra"}).decode(t, &d)
	a.must(http.StatusOK, http.MethodGet, fmt.Sprintf("/api/disciplines/%d", dID), nil).decode(t, &d)
	if d.Name != "Algebra" {
		t.Fatalf("discipline = %+v", d)
	}
	a.must(http.StatusNoContent, http.MethodDelete, fmt.Sprintf("/api/disciplines/%d", dID), nil)
	a.must(http.StatusNoContent, http.MethodDelete, fmt.Sprintf("/api/teachers/%d", tID), nil)
	a.expectErr(http.StatusNotFound, "not_found", http.MethodGet, fmt.Sprintf("/api/teachers/%d", tID), nil)
}

func TestTimeGridAndPeriods(t *testing.T) {
	a := newTestAPI(t)
	a.expectErr(http.StatusNotFound, "not_found", http.MethodGet, "/api/time-grid", nil)
	a.must(http.StatusOK, http.MethodPut, "/api/time-grid", map[string]int{"days": 6, "periods_per_day": 7})
	var g timeGrid
	a.must(http.StatusOK, http.MethodGet, "/api/time-grid", nil).decode(t, &g)
	if g != (timeGrid{Days: 6, PeriodsPerDay: 7}) {
		t.Fatalf("grid = %+v", g)
	}
	a.expectErr(http.StatusUnprocessableEntity, "validation_failed", http.MethodPut, "/api/time-grid", map[string]int{"days": 8, "periods_per_day": 0})

	var p period
	a.must(http.StatusOK, http.MethodPut, "/api/periods/1", map[string]string{"starts_at": "08:30", "ends_at": "10:00"}).decode(t, &p)
	if p != (period{Number: 1, StartsAt: "08:30", EndsAt: "10:00"}) {
		t.Fatalf("period = %+v", p)
	}
	a.must(http.StatusOK, http.MethodPut, "/api/periods/1", map[string]string{"starts_at": "08:15", "ends_at": "09:45:30"}).decode(t, &p)
	if p.EndsAt != "09:45:30" {
		t.Fatalf("period = %+v", p)
	}
	a.expectErr(http.StatusUnprocessableEntity, "validation_failed", http.MethodPut, "/api/periods/2", map[string]string{"starts_at": "10:00", "ends_at": "09:00"})
	a.expectErr(http.StatusUnprocessableEntity, "validation_failed", http.MethodPut, "/api/periods/2", map[string]string{"starts_at": "25:00", "ends_at": "x"})
	a.expectErr(http.StatusBadRequest, "invalid_request", http.MethodPut, "/api/periods/17", map[string]string{"starts_at": "08:00", "ends_at": "09:00"})
	var ps []period
	a.must(http.StatusOK, http.MethodGet, "/api/periods", nil).decode(t, &ps)
	if len(ps) != 1 {
		t.Fatalf("periods = %+v", ps)
	}
	a.must(http.StatusNoContent, http.MethodDelete, "/api/periods/1", nil)
	a.expectErr(http.StatusNotFound, "not_found", http.MethodGet, "/api/periods/1", nil)
	if n := a.auditCount("period"); n != 3 {
		t.Errorf("period audit rows = %d, want 3", n)
	}
}

func TestCurriculumItems(t *testing.T) {
	a := newTestAPI(t)
	a.create("/api/room-types", map[string]string{"code": "lecture", "name": "Lecture hall"})
	tID := a.create("/api/teachers", map[string]string{"full_name": "Petrov P.", "short_name": "Petrov"})
	dID := a.create("/api/disciplines", map[string]string{"name": "Physics"})
	g1 := a.create("/api/groups", map[string]any{"name": "G1", "size": 20})
	g2 := a.create("/api/groups", map[string]any{"name": "G2", "size": 20})

	in := map[string]any{
		"discipline_id": dID, "kind": "lecture", "teacher_id": tID, "room_type": "lecture",
		"weekly_count": 2, "biweekly_count": 1,
		"audience": []map[string]any{{"group_id": g1}, {"group_id": g2}},
	}
	var it curriculumItem
	a.must(http.StatusCreated, http.MethodPost, "/api/curriculum-items", in).decode(t, &it)
	if len(it.Audience) != 2 || len(it.Lessons) != 3 {
		t.Fatalf("item = %+v", it)
	}
	for i, l := range it.Lessons {
		if l.Seq != int16(i+1) || l.Biweekly != (i == 2) {
			t.Fatalf("lesson %d = %+v", i, l)
		}
	}
	path := fmt.Sprintf("/api/curriculum-items/%d", it.ID)

	// Filters.
	var items []curriculumItem
	a.must(http.StatusOK, http.MethodGet, fmt.Sprintf("/api/curriculum-items?group_id=%d&teacher_id=%d", g2, tID), nil).decode(t, &items)
	if len(items) != 1 || len(items[0].Lessons) != 3 {
		t.Fatalf("filtered items = %+v", items)
	}
	a.must(http.StatusOK, http.MethodGet, "/api/curriculum-items?discipline_id=999", nil).decode(t, &items)
	if len(items) != 0 {
		t.Fatalf("filtered items = %+v", items)
	}

	// Place the first lesson in a schedule.
	sched, err := a.st.CreateSchedule(t.Context(), "draft")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.st.UpsertAssignment(t.Context(), db.UpsertAssignmentParams{
		ScheduleID: sched.ID, LessonID: it.Lessons[0].ID, Day: 0, Period: 1, Parity: db.ParityEvery,
	}); err != nil {
		t.Fatal(err)
	}
	countAssignments := func() int {
		var n int
		if err := a.st.Pool().QueryRow(t.Context(), `SELECT count(*) FROM assignments`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// Same counts, new audience: lessons (and assignments) are kept.
	in["audience"] = []map[string]any{{"group_id": g1}}
	var upd curriculumItem
	a.must(http.StatusOK, http.MethodPut, path, in).decode(t, &upd)
	if len(upd.Audience) != 1 || upd.Lessons[0].ID != it.Lessons[0].ID || countAssignments() != 1 {
		t.Fatalf("update without count change: %+v, assignments %d", upd, countAssignments())
	}

	// New counts: lessons are regenerated and their assignments dropped.
	in["weekly_count"], in["biweekly_count"] = 1, 2
	a.must(http.StatusOK, http.MethodPut, path, in).decode(t, &upd)
	if len(upd.Lessons) != 3 || upd.Lessons[0].ID == it.Lessons[0].ID || upd.Lessons[0].Biweekly || !upd.Lessons[1].Biweekly {
		t.Fatalf("regenerated lessons = %+v", upd.Lessons)
	}
	if n := countAssignments(); n != 0 {
		t.Fatalf("assignments after regeneration = %d", n)
	}

	bad := func(mut func(m map[string]any)) map[string]any {
		m := map[string]any{}
		for k, v := range in {
			m[k] = v
		}
		mut(m)
		return m
	}
	a.expectErr(http.StatusUnprocessableEntity, "invalid_reference", http.MethodPost, "/api/curriculum-items",
		bad(func(m map[string]any) { m["teacher_id"] = 999 }))
	a.expectErr(http.StatusUnprocessableEntity, "invalid_reference", http.MethodPost, "/api/curriculum-items",
		bad(func(m map[string]any) { m["audience"] = []map[string]any{{"group_id": 999}} }))
	a.expectErr(http.StatusUnprocessableEntity, "validation_failed", http.MethodPost, "/api/curriculum-items",
		bad(func(m map[string]any) { m["weekly_count"], m["biweekly_count"] = 0, 0 }))
	a.expectErr(http.StatusUnprocessableEntity, "validation_failed", http.MethodPost, "/api/curriculum-items",
		bad(func(m map[string]any) { m["kind"] = "exam" }))
	a.expectErr(http.StatusUnprocessableEntity, "validation_failed", http.MethodPost, "/api/curriculum-items",
		bad(func(m map[string]any) { m["audience"] = []any{} }))
	a.expectErr(http.StatusUnprocessableEntity, "validation_failed", http.MethodPost, "/api/curriculum-items",
		bad(func(m map[string]any) {
			m["audience"] = []map[string]any{{"group_id": g1, "division": "lab"}, {"group_id": g1, "part": 1}}
		}))
	a.expectErr(http.StatusUnprocessableEntity, "validation_failed", http.MethodPost, "/api/curriculum-items",
		bad(func(m map[string]any) { m["audience"] = []map[string]any{{"group_id": g1}, {"group_id": g1}} }))
	a.expectErr(http.StatusNotFound, "not_found", http.MethodPut, "/api/curriculum-items/999", in)
	var lessons int
	if err := a.st.Pool().QueryRow(t.Context(), `SELECT count(*) FROM lessons`).Scan(&lessons); err != nil || lessons != 3 {
		t.Fatalf("lessons after failed creates = %d %v", lessons, err)
	}

	// Referenced teacher, group and discipline cannot be deleted.
	a.expectErr(http.StatusConflict, "in_use", http.MethodDelete, fmt.Sprintf("/api/teachers/%d", tID), nil)
	a.expectErr(http.StatusConflict, "in_use", http.MethodDelete, fmt.Sprintf("/api/groups/%d", g1), nil)
	a.expectErr(http.StatusConflict, "in_use", http.MethodDelete, fmt.Sprintf("/api/disciplines/%d", dID), nil)

	a.must(http.StatusNoContent, http.MethodDelete, path, nil)
	a.expectErr(http.StatusNotFound, "not_found", http.MethodGet, path, nil)
	if err := a.st.Pool().QueryRow(t.Context(), `SELECT count(*) FROM lessons`).Scan(&lessons); err != nil || lessons != 0 {
		t.Fatalf("lessons after delete = %d %v", lessons, err)
	}
	a.must(http.StatusNoContent, http.MethodDelete, fmt.Sprintf("/api/teachers/%d", tID), nil)
	if n := a.auditCount("curriculum_item"); n != 4 {
		t.Errorf("curriculum_item audit rows = %d, want 4", n)
	}
}

func TestCRUDWithoutDatabase(t *testing.T) {
	svc := auth.NewService(nil, auth.Config{Secret: []byte(strings.Repeat("k", 32))}, nil)
	tok, _, _ := svc.Tokens().Issue(auth.Principal{UserID: 1, Role: auth.RoleAdmin})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/buildings", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	NewRouter(Deps{Auth: svc}).ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}
