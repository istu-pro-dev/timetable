package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/istu-pro-dev/timetable/backend/internal/store"
	"github.com/istu-pro-dev/timetable/backend/internal/store/db"
)

// maxAvailabilityEntries bounds one availability set (7 days × 16 periods × 3 parities).
const maxAvailabilityEntries = 7 * 16 * 3

// availabilityEntry is an exception for one slot; a slot without an entry is available.
type availabilityEntry struct {
	Day    int16           `json:"day"`
	Period int16           `json:"period"`
	Parity db.Parity       `json:"parity"`
	Status db.Availability `json:"status"`
}

// availability is the full set of exceptions of a teacher or a room.
type availability struct {
	Entries []availabilityEntry `json:"entries"`
}

func (in *availability) validate() error {
	var v validator
	if in.Entries == nil {
		v.addf("entries", "is required (use [] to clear)")
	}
	if len(in.Entries) > maxAvailabilityEntries {
		v.addf("entries", "must have at most %d items", maxAvailabilityEntries)
	}
	type key struct {
		day, period int16
		parity      db.Parity
	}
	seen := make(map[key]bool, len(in.Entries))
	for i := range in.Entries {
		e := &in.Entries[i]
		field := fmt.Sprintf("entries[%d]", i)
		if e.Parity == "" {
			e.Parity = db.ParityEvery
		}
		v.between(field+".day", int64(e.Day), 0, 6)
		v.between(field+".period", int64(e.Period), 1, 16)
		if !e.Parity.Valid() {
			v.addf(field+".parity", "must be one of every, odd, even")
		}
		if !e.Status.Valid() {
			v.addf(field+".status", "must be one of unavailable, undesired, preferred")
		}
		k := key{e.Day, e.Period, e.Parity}
		if seen[k] {
			v.addf(field, "duplicates another entry for the same day, period and parity")
		}
		seen[k] = true
	}
	return v.err()
}

func (s *server) getTeacherAvailability(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.writeError(w, err, opRead)
		return
	}
	s.read(w, r, func(ctx context.Context, q *db.Queries) (any, error) {
		return teacherAvailability(ctx, q, id)
	})
}

func teacherAvailability(ctx context.Context, q *db.Queries, id int64) (availability, error) {
	if _, err := q.GetTeacher(ctx, id); err != nil {
		return availability{}, orNotFound(err, "teacher")
	}
	rows, err := q.ListTeacherAvailabilityOf(ctx, id)
	return availability{Entries: mapSlice(rows, func(a db.TeacherAvailability) availabilityEntry {
		return availabilityEntry{Day: a.Day, Period: a.Period, Parity: a.Parity, Status: a.Status}
	})}, err
}

func (s *server) putTeacherAvailability(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.writeError(w, err, opWrite)
		return
	}
	var in availability
	if !s.decode(w, r, &in) {
		return
	}
	s.mutate(w, r, mutation{entity: "teacher_availability", entityID: idString(id), op: opWrite, status: http.StatusOK,
		run: func(ctx context.Context, q *db.Queries, c *store.Change) (any, error) {
			before, err := teacherAvailability(ctx, q, id)
			if err != nil {
				return nil, err
			}
			if err := q.ClearTeacherAvailability(ctx, id); err != nil {
				return nil, err
			}
			for _, e := range in.Entries {
				if err := q.SetTeacherAvailability(ctx, db.SetTeacherAvailabilityParams{
					TeacherID: id, Day: e.Day, Period: e.Period, Parity: e.Parity, Status: e.Status,
				}); err != nil {
					return nil, err
				}
			}
			after, err := teacherAvailability(ctx, q, id)
			c.Before, c.After = before, after
			return after, err
		}})
}

func (s *server) getRoomAvailability(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.writeError(w, err, opRead)
		return
	}
	s.read(w, r, func(ctx context.Context, q *db.Queries) (any, error) {
		return roomAvailability(ctx, q, id)
	})
}

func roomAvailability(ctx context.Context, q *db.Queries, id int64) (availability, error) {
	if _, err := q.GetRoom(ctx, id); err != nil {
		return availability{}, orNotFound(err, "room")
	}
	rows, err := q.ListRoomAvailabilityOf(ctx, id)
	return availability{Entries: mapSlice(rows, func(a db.RoomAvailability) availabilityEntry {
		return availabilityEntry{Day: a.Day, Period: a.Period, Parity: a.Parity, Status: a.Status}
	})}, err
}

func (s *server) putRoomAvailability(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.writeError(w, err, opWrite)
		return
	}
	var in availability
	if !s.decode(w, r, &in) {
		return
	}
	s.mutate(w, r, mutation{entity: "room_availability", entityID: idString(id), op: opWrite, status: http.StatusOK,
		run: func(ctx context.Context, q *db.Queries, c *store.Change) (any, error) {
			before, err := roomAvailability(ctx, q, id)
			if err != nil {
				return nil, err
			}
			if err := q.ClearRoomAvailability(ctx, id); err != nil {
				return nil, err
			}
			for _, e := range in.Entries {
				if err := q.SetRoomAvailability(ctx, db.SetRoomAvailabilityParams{
					RoomID: id, Day: e.Day, Period: e.Period, Parity: e.Parity, Status: e.Status,
				}); err != nil {
					return nil, err
				}
			}
			after, err := roomAvailability(ctx, q, id)
			c.Before, c.After = before, after
			return after, err
		}})
}
