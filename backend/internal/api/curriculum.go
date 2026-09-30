package api

import (
	"context"
	"fmt"
	"net/http"
	"slices"

	"github.com/istu-pro-dev/timetable/backend/internal/store"
	"github.com/istu-pro-dev/timetable/backend/internal/store/db"
)

// Limits of a curriculum item.
const (
	maxLessonsPerWeek = 20
	maxAudience       = 50
)

type audienceMember struct {
	GroupID  int64  `json:"group_id"`
	Division string `json:"division"`
	Part     int16  `json:"part"`
}

type lesson struct {
	ID       int64 `json:"id"`
	Seq      int16 `json:"seq"`
	Biweekly bool  `json:"biweekly"`
}

type curriculumItem struct {
	ID            int64            `json:"id"`
	DisciplineID  int64            `json:"discipline_id"`
	Kind          db.LessonKind    `json:"kind"`
	TeacherID     int64            `json:"teacher_id"`
	RoomType      string           `json:"room_type"`
	WeeklyCount   int16            `json:"weekly_count"`
	BiweeklyCount int16            `json:"biweekly_count"`
	Audience      []audienceMember `json:"audience"`
	Lessons       []lesson         `json:"lessons"`
}

type curriculumInput struct {
	DisciplineID  int64            `json:"discipline_id"`
	Kind          db.LessonKind    `json:"kind"`
	TeacherID     int64            `json:"teacher_id"`
	RoomType      string           `json:"room_type"`
	WeeklyCount   int16            `json:"weekly_count"`
	BiweeklyCount int16            `json:"biweekly_count"`
	Audience      []audienceMember `json:"audience"`
}

func (in *curriculumInput) validate() error {
	var v validator
	v.positive("discipline_id", in.DisciplineID)
	if !in.Kind.Valid() {
		v.addf("kind", "must be one of lecture, seminar, lab")
	}
	v.positive("teacher_id", in.TeacherID)
	v.code("room_type", in.RoomType)
	v.between("weekly_count", int64(in.WeeklyCount), 0, maxLessonsPerWeek)
	v.between("biweekly_count", int64(in.BiweeklyCount), 0, maxLessonsPerWeek)
	if in.WeeklyCount+in.BiweeklyCount <= 0 {
		v.addf("weekly_count", "weekly_count + biweekly_count must be positive")
	}
	if len(in.Audience) == 0 || len(in.Audience) > maxAudience {
		v.addf("audience", "must have 1 to %d members", maxAudience)
	}
	seen := make(map[audienceMember]bool, len(in.Audience))
	for i := range in.Audience {
		m := &in.Audience[i]
		field := fmt.Sprintf("audience[%d]", i)
		v.positive(field+".group_id", m.GroupID)
		v.text(field+".division", &m.Division, 0, maxDivisionLen)
		switch {
		case m.Division == "" && m.Part != 0:
			v.addf(field+".part", "must be 0 for a whole group (empty division)")
		case m.Division != "" && (m.Part < 1 || m.Part > maxPart):
			v.addf(field+".part", "must be between 1 and %d for a subgroup", maxPart)
		}
		if seen[*m] {
			v.addf(field, "duplicates another audience member")
		}
		seen[*m] = true
	}
	return v.err()
}

func toCurriculumItem(it db.CurriculumItem, aud []db.CurriculumAudience, les []db.Lesson) curriculumItem {
	return curriculumItem{
		ID: it.ID, DisciplineID: it.DisciplineID, Kind: it.Kind, TeacherID: it.TeacherID, RoomType: it.RoomType,
		WeeklyCount: it.WeeklyCount, BiweeklyCount: it.BiweeklyCount,
		Audience: mapSlice(aud, func(a db.CurriculumAudience) audienceMember {
			return audienceMember{GroupID: a.GroupID, Division: a.Division, Part: a.Part}
		}),
		Lessons: mapSlice(les, func(l db.Lesson) lesson { return lesson{ID: l.ID, Seq: l.Seq, Biweekly: l.Biweekly} }),
	}
}

func loadCurriculumItem(ctx context.Context, q *db.Queries, id int64) (curriculumItem, error) {
	it, err := q.GetCurriculumItem(ctx, id)
	if err != nil {
		return curriculumItem{}, orNotFound(err, "curriculum item")
	}
	aud, err := q.ListCurriculumAudienceOf(ctx, id)
	if err != nil {
		return curriculumItem{}, err
	}
	les, err := q.ListLessonsOf(ctx, id)
	if err != nil {
		return curriculumItem{}, err
	}
	return toCurriculumItem(it, aud, les), nil
}

func (s *server) listCurriculumItems(w http.ResponseWriter, r *http.Request) {
	var p db.ListCurriculumItemsFilteredParams
	var err error
	if p.TeacherID, err = queryID(r, "teacher_id"); err == nil {
		if p.DisciplineID, err = queryID(r, "discipline_id"); err == nil {
			p.GroupID, err = queryID(r, "group_id")
		}
	}
	if err != nil {
		s.writeError(w, err, opRead)
		return
	}
	s.read(w, r, func(ctx context.Context, q *db.Queries) (any, error) {
		items, err := q.ListCurriculumItemsFiltered(ctx, p)
		if err != nil {
			return nil, err
		}
		audience, err := q.ListCurriculumAudience(ctx)
		if err != nil {
			return nil, err
		}
		lessons, err := q.ListLessons(ctx)
		if err != nil {
			return nil, err
		}
		audBy := make(map[int64][]db.CurriculumAudience)
		for _, a := range audience {
			audBy[a.CurriculumItemID] = append(audBy[a.CurriculumItemID], a)
		}
		lesBy := make(map[int64][]db.Lesson)
		for _, l := range lessons {
			lesBy[l.CurriculumItemID] = append(lesBy[l.CurriculumItemID], l)
		}
		out := make([]curriculumItem, len(items))
		for i, it := range items {
			out[i] = toCurriculumItem(it, audBy[it.ID], lesBy[it.ID])
		}
		return out, nil
	})
}

func (s *server) getCurriculumItem(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.writeError(w, err, opRead)
		return
	}
	s.read(w, r, func(ctx context.Context, q *db.Queries) (any, error) {
		return loadCurriculumItem(ctx, q, id)
	})
}

func (s *server) createCurriculumItem(w http.ResponseWriter, r *http.Request) {
	var in curriculumInput
	if !s.decode(w, r, &in) {
		return
	}
	s.mutate(w, r, mutation{entity: "curriculum_item", op: opWrite, status: http.StatusCreated,
		run: func(ctx context.Context, q *db.Queries, c *store.Change) (any, error) {
			it, err := q.CreateCurriculumItem(ctx, db.CreateCurriculumItemParams{
				DisciplineID: in.DisciplineID, Kind: in.Kind, TeacherID: in.TeacherID, RoomType: in.RoomType,
				WeeklyCount: in.WeeklyCount, BiweeklyCount: in.BiweeklyCount,
			})
			if err != nil {
				return nil, err
			}
			if err := setAudience(ctx, q, it.ID, in.Audience); err != nil {
				return nil, err
			}
			if err := generateLessons(ctx, q, it); err != nil {
				return nil, err
			}
			out, err := loadCurriculumItem(ctx, q, it.ID)
			c.EntityID, c.After = idString(it.ID), out
			return out, err
		}})
}

// updateCurriculumItem replaces the item and its audience. When weekly_count or
// biweekly_count change, the item's lessons are regenerated: the old lessons are deleted and
// their schedule assignments go with them (ON DELETE CASCADE), so the item has to be placed
// again in every schedule.
func (s *server) updateCurriculumItem(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.writeError(w, err, opWrite)
		return
	}
	var in curriculumInput
	if !s.decode(w, r, &in) {
		return
	}
	s.mutate(w, r, mutation{entity: "curriculum_item", entityID: idString(id), op: opWrite, status: http.StatusOK,
		run: func(ctx context.Context, q *db.Queries, c *store.Change) (any, error) {
			before, err := loadCurriculumItem(ctx, q, id)
			if err != nil {
				return nil, err
			}
			it, err := q.UpdateCurriculumItem(ctx, db.UpdateCurriculumItemParams{
				ID: id, DisciplineID: in.DisciplineID, Kind: in.Kind, TeacherID: in.TeacherID, RoomType: in.RoomType,
				WeeklyCount: in.WeeklyCount, BiweeklyCount: in.BiweeklyCount,
			})
			if err != nil {
				return nil, err
			}
			if !sameAudience(before.Audience, in.Audience) {
				if err := q.ClearCurriculumAudience(ctx, id); err != nil {
					return nil, err
				}
				if err := setAudience(ctx, q, id, in.Audience); err != nil {
					return nil, err
				}
			}
			if before.WeeklyCount != in.WeeklyCount || before.BiweeklyCount != in.BiweeklyCount {
				if err := q.DeleteLessonsOfItem(ctx, id); err != nil {
					return nil, err
				}
				if err := generateLessons(ctx, q, it); err != nil {
					return nil, err
				}
			}
			out, err := loadCurriculumItem(ctx, q, id)
			c.Before, c.After = before, out
			return out, err
		}})
}

// deleteCurriculumItem removes the item with its audience, lessons and their assignments.
func (s *server) deleteCurriculumItem(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.writeError(w, err, opDelete)
		return
	}
	s.mutate(w, r, mutation{entity: "curriculum_item", entityID: idString(id), op: opDelete, status: http.StatusNoContent,
		run: func(ctx context.Context, q *db.Queries, c *store.Change) (any, error) {
			before, err := loadCurriculumItem(ctx, q, id)
			if err != nil {
				return nil, err
			}
			c.Before = before
			_, err = q.DeleteCurriculumItem(ctx, id)
			return nil, err
		}})
}

func setAudience(ctx context.Context, q *db.Queries, itemID int64, members []audienceMember) error {
	for _, m := range members {
		if err := q.AddCurriculumAudience(ctx, db.AddCurriculumAudienceParams{
			CurriculumItemID: itemID, GroupID: m.GroupID, Division: m.Division, Part: m.Part,
		}); err != nil {
			return err
		}
	}
	return nil
}

// generateLessons creates weekly_count weekly lessons and then biweekly_count biweekly ones,
// numbered seq 1..N.
func generateLessons(ctx context.Context, q *db.Queries, it db.CurriculumItem) error {
	for i := range it.WeeklyCount + it.BiweeklyCount {
		if _, err := q.CreateLesson(ctx, db.CreateLessonParams{
			CurriculumItemID: it.ID, Seq: i + 1, Biweekly: i >= it.WeeklyCount,
		}); err != nil {
			return err
		}
	}
	return nil
}

func sameAudience(a, b []audienceMember) bool {
	key := func(m audienceMember) string { return fmt.Sprintf("%d/%s/%d", m.GroupID, m.Division, m.Part) }
	ka, kb := mapSlice(a, key), mapSlice(b, key)
	slices.Sort(ka)
	slices.Sort(kb)
	return slices.Equal(ka, kb)
}
