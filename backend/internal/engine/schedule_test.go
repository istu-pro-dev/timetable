package engine

import (
	"errors"
	"math/rand/v2"
	"testing"

	"github.com/istu-pro-dev/timetable/backend/internal/domain"
)

func TestPlaceAndLoads(t *testing.T) {
	p := mustProblem(t, smallInput())
	s := NewSchedule(p)
	g := p.Grid
	tue2 := slot(domain.Tuesday, 2)
	odd, even := g.Cells(tue2)[0], g.Cells(tue2)[1]

	if err := s.Place(0, tue2, 0); err != nil { // lecture, stream G1+G2, hall
		t.Fatal(err)
	}
	if s.TeacherLoad(0, odd) != 1 || s.TeacherLoad(0, even) != 1 || s.RoomLoad(0, odd) != 1 {
		t.Fatal("lecture occupancy not recorded")
	}
	if s.GroupLoad(0, odd) != 1 || s.GroupLoad(1, even) != 1 {
		t.Fatal("stream occupancy not recorded for both groups")
	}

	// Subgroup lab (english/1) in odd week of the same position: G1 is busy with the lecture.
	if err := s.Place(2, slotP(domain.Tuesday, 2, domain.OddWeek), 2); err != nil {
		t.Fatal(err)
	}
	u1 := p.Lessons[2].Units[0]
	u2 := p.Lessons[3].Units[0]
	if got := s.UnitLoad(u1, odd); got != 2 {
		t.Fatalf("UnitLoad(english/1, odd) = %d, want 2 (lecture + lab)", got)
	}
	if got := s.UnitLoad(u1, even); got != 1 {
		t.Fatalf("UnitLoad(english/1, even) = %d, want 1 (lecture)", got)
	}
	// english/2 sees the whole-group lecture but not the parallel english/1 lab.
	if got := s.UnitLoad(u2, odd); got != 1 {
		t.Fatalf("UnitLoad(english/2, odd) = %d, want 1", got)
	}
	// The whole group sees everything.
	if got := s.UnitLoad(p.Lessons[1].Units[0], odd); got != 2 {
		t.Fatalf("UnitLoad(G1, odd) = %d, want 2", got)
	}

	// Moving the lecture away frees the cells.
	if err := s.Place(0, slot(domain.Wednesday, 1), 0); err != nil {
		t.Fatal(err)
	}
	if s.TeacherLoad(0, odd) != 0 || s.GroupLoad(1, odd) != 0 || s.RoomLoad(0, odd) != 0 {
		t.Fatal("old cells not released after move")
	}
	if s.UnitLoad(u1, odd) != 1 {
		t.Fatalf("UnitLoad(english/1, odd) after move = %d, want 1", s.UnitLoad(u1, odd))
	}

	s.Unplace(2)
	s.Unplace(2) // no-op
	if s.UnitLoad(u1, odd) != 0 || s.Assignment(2).Placed || s.Assignment(2).Room != None {
		t.Fatal("unplace did not clear the lab")
	}
}

func TestParallelSubgroupsDoNotCollide(t *testing.T) {
	p := mustProblem(t, smallInput())
	s := NewSchedule(p)
	c := p.Grid.Cells(slotP(domain.Monday, 3, domain.EvenWeek))[0]

	mustPlace(t, s, 2, slotP(domain.Monday, 3, domain.EvenWeek), None) // english/1
	mustPlace(t, s, 3, slot(domain.Monday, 3), None)                   // english/2
	if s.UnitLoad(p.Lessons[2].Units[0], c) != 1 || s.UnitLoad(p.Lessons[3].Units[0], c) != 1 {
		t.Fatal("parallel subgroups of one division must not see each other")
	}
	if s.GroupLoad(0, c) != 2 {
		t.Fatalf("GroupLoad = %d, want 2", s.GroupLoad(0, c))
	}
}

func TestCrossDivisionSubgroupsCollide(t *testing.T) {
	in := smallInput()
	in.Lessons = append(in.Lessons, LessonInput{
		ID: 5, Item: 5, Discipline: 1, Kind: "lab", Teacher: 2, RoomType: "lab",
		Audience: domain.Audience{domain.Subgroup(10, "pe", 1)},
	})
	p := mustProblem(t, in)
	s := NewSchedule(p)
	mustPlace(t, s, 3, slot(domain.Monday, 3), None) // english/2
	mustPlace(t, s, 4, slot(domain.Monday, 3), None) // pe/1
	c := p.Grid.Cells(slot(domain.Monday, 3))[0]
	if s.UnitLoad(p.Lessons[4].Units[0], c) != 2 || s.UnitLoad(p.Lessons[3].Units[0], c) != 2 {
		t.Fatal("subgroups of different divisions share students")
	}
}

func TestPlaceValidation(t *testing.T) {
	p := mustProblem(t, smallInput())
	s := NewSchedule(p)
	tests := []struct {
		name   string
		lesson int32
		slot   domain.Slot
		room   int32
		want   error
	}{
		{"outside grid", 0, slot(domain.Sunday, 1), None, ErrSlotOutOfGrid},
		{"weekly lesson in odd week", 0, slotP(domain.Monday, 1, domain.OddWeek), None, ErrParityMismatch},
		{"biweekly lesson every week", 2, slot(domain.Monday, 1), None, ErrParityMismatch},
		{"unknown room", 0, slot(domain.Monday, 1), 7, ErrUnknownRoom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := s.Place(tt.lesson, tt.slot, tt.room); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
	if s.Assignment(0).Placed {
		t.Fatal("rejected placement must not change the schedule")
	}
}

func TestPinnedSurvivesMoves(t *testing.T) {
	p := mustProblem(t, smallInput())
	s := NewSchedule(p)
	s.SetPinned(1, true)
	mustPlace(t, s, 1, slot(domain.Monday, 1), 1)
	s.Unplace(1)
	mustPlace(t, s, 1, slot(domain.Monday, 2), 1)
	if !s.Assignment(1).Pinned {
		t.Fatal("pinned flag lost")
	}
}

func TestCloneIsIndependent(t *testing.T) {
	p := mustProblem(t, smallInput())
	s := NewSchedule(p)
	mustPlace(t, s, 0, slot(domain.Monday, 2), 0)
	c := s.Clone()
	mustPlace(t, c, 0, slot(domain.Tuesday, 2), 0)
	cell := p.Grid.Cells(slot(domain.Monday, 2))[0]
	if s.TeacherLoad(0, cell) != 1 || c.TeacherLoad(0, cell) != 0 {
		t.Fatal("clone shares occupancy with original")
	}
	if s.Assignment(0).Slot.Day != domain.Monday {
		t.Fatal("clone shares assignments with original")
	}

	s.CopyFrom(c)
	if s.Assignment(0).Slot.Day != domain.Tuesday || s.TeacherLoad(0, cell) != 0 {
		t.Fatal("CopyFrom did not copy state")
	}
	if got := s.Assignments(); len(got) != 4 || got[0].Slot.Day != domain.Tuesday {
		t.Fatalf("Assignments() = %+v", got)
	}
}

func TestCopyFromDifferentProblemPanics(t *testing.T) {
	a := NewSchedule(mustProblem(t, smallInput()))
	b := NewSchedule(mustProblem(t, smallInput()))
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	a.CopyFrom(b)
}

// Occupancy maintained incrementally over random moves equals occupancy rebuilt from scratch.
func TestIncrementalOccupancyMatchesRebuild(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	p := mustProblem(t, randomInput(r, 300))
	s := NewSchedule(p)
	for range 5000 {
		l := int32(r.IntN(len(p.Lessons)))
		if r.IntN(8) == 0 {
			s.Unplace(l)
			continue
		}
		mustPlace(t, s, l, randomSlot(r, p, l), randomRoom(r, p, l))
	}

	fresh := NewSchedule(p)
	for l, a := range s.Assignments() {
		if a.Placed {
			mustPlace(t, fresh, int32(l), a.Slot, a.Room)
		}
	}
	for name, pair := range map[string][2][]uint16{
		"teacher": {s.teacherOcc, fresh.teacherOcc},
		"room":    {s.roomOcc, fresh.roomOcc},
		"unit":    {s.unitOcc, fresh.unitOcc},
		"whole":   {s.wholeOcc, fresh.wholeOcc},
		"sub":     {s.subOcc, fresh.subOcc},
		"div":     {s.divOcc, fresh.divOcc},
	} {
		for i := range pair[0] {
			if pair[0][i] != pair[1][i] {
				t.Fatalf("%s occupancy differs at %d: %d vs %d", name, i, pair[0][i], pair[1][i])
			}
		}
	}
}

func mustPlace(t testing.TB, s *Schedule, l int32, sl domain.Slot, room int32) {
	t.Helper()
	if err := s.Place(l, sl, room); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkClone5k(b *testing.B) {
	r := rand.New(rand.NewPCG(3, 4))
	p := mustProblem(b, randomInput(r, 5000))
	s := NewSchedule(p)
	for l := range p.Lessons {
		mustPlace(b, s, int32(l), randomSlot(r, p, int32(l)), randomRoom(r, p, int32(l)))
	}
	b.ResetTimer()
	for b.Loop() {
		_ = s.Clone()
	}
}

func BenchmarkCopyFrom5k(b *testing.B) {
	r := rand.New(rand.NewPCG(3, 4))
	p := mustProblem(b, randomInput(r, 5000))
	s := NewSchedule(p)
	dst := NewSchedule(p)
	b.ResetTimer()
	for b.Loop() {
		dst.CopyFrom(s)
	}
}
