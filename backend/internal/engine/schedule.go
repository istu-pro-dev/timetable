package engine

import (
	"errors"
	"fmt"
	"slices"

	"github.com/istu-pro-dev/timetable/backend/internal/domain"
)

// Assignment is the placement of one lesson.
type Assignment struct {
	Placed bool
	Slot   domain.Slot
	Room   int32 // None if no room is assigned yet
	Pinned bool
}

// Schedule is a mutable assignment of lessons to slots and rooms together with occupancy
// indexes for O(1) conflict lookups. It does not enforce any constraint: it can represent
// infeasible states so that checkers can report what is wrong with them.
//
// A Schedule is not safe for concurrent use; solver workers each own a Clone.
type Schedule struct {
	P   *Problem
	asg []Assignment

	cells int // cells per resource row

	// Occupancy counters: row-major [resource][cell].
	teacherOcc []uint16
	roomOcc    []uint16
	unitOcc    []uint16 // per audience unit (whole group or subgroup)
	wholeOcc   []uint16 // per group: lessons attended by the whole group
	subOcc     []uint16 // per group: lessons attended by any of its subgroups
	divOcc     []uint16 // per division: lessons attended by any of its subgroups
}

// NewSchedule returns an empty schedule for the problem.
func NewSchedule(p *Problem) *Schedule {
	c := p.Grid.CellCount()
	s := &Schedule{
		P:          p,
		asg:        make([]Assignment, len(p.Lessons)),
		cells:      c,
		teacherOcc: make([]uint16, len(p.Teachers)*c),
		roomOcc:    make([]uint16, len(p.Rooms)*c),
		unitOcc:    make([]uint16, len(p.Units)*c),
		wholeOcc:   make([]uint16, len(p.Groups)*c),
		subOcc:     make([]uint16, len(p.Groups)*c),
		divOcc:     make([]uint16, p.Divisions*c),
	}
	for i := range s.asg {
		s.asg[i].Room = None
	}
	return s
}

// Clone returns an independent deep copy sharing only the immutable Problem.
func (s *Schedule) Clone() *Schedule {
	return &Schedule{
		P:          s.P,
		asg:        slices.Clone(s.asg),
		cells:      s.cells,
		teacherOcc: slices.Clone(s.teacherOcc),
		roomOcc:    slices.Clone(s.roomOcc),
		unitOcc:    slices.Clone(s.unitOcc),
		wholeOcc:   slices.Clone(s.wholeOcc),
		subOcc:     slices.Clone(s.subOcc),
		divOcc:     slices.Clone(s.divOcc),
	}
}

// CopyFrom overwrites s with the state of src, reusing s's memory. Both must share a Problem.
func (s *Schedule) CopyFrom(src *Schedule) {
	if s.P != src.P {
		panic("engine: CopyFrom between schedules of different problems")
	}
	copy(s.asg, src.asg)
	copy(s.teacherOcc, src.teacherOcc)
	copy(s.roomOcc, src.roomOcc)
	copy(s.unitOcc, src.unitOcc)
	copy(s.wholeOcc, src.wholeOcc)
	copy(s.subOcc, src.subOcc)
	copy(s.divOcc, src.divOcc)
}

// Assignment returns the placement of lesson l.
func (s *Schedule) Assignment(l int32) Assignment { return s.asg[l] }

// Assignments returns a copy of all placements, indexed by lesson.
func (s *Schedule) Assignments() []Assignment { return slices.Clone(s.asg) }

// Placement errors.
var (
	ErrSlotOutOfGrid  = errors.New("slot is outside the grid")
	ErrParityMismatch = errors.New("slot parity does not match the lesson")
	ErrUnknownRoom    = errors.New("unknown room")
	ErrPinned         = errors.New("lesson is pinned")
)

// Place puts lesson l into the slot and room (None for no room), replacing any previous
// placement. It validates only structural properties: the slot is in the grid, its parity
// matches the lesson and the room exists. Conflicts are allowed and counted.
func (s *Schedule) Place(l int32, slot domain.Slot, room int32) error {
	if err := s.CanPlace(l, slot, room); err != nil {
		return err
	}
	s.Unplace(l)
	s.asg[l] = Assignment{Placed: true, Slot: slot, Room: room, Pinned: s.asg[l].Pinned}
	s.apply(l, +1)
	return nil
}

// CanPlace reports whether Place would accept the arguments.
func (s *Schedule) CanPlace(l int32, slot domain.Slot, room int32) error {
	if !s.P.Grid.Contains(slot) {
		return fmt.Errorf("%w: %v", ErrSlotOutOfGrid, slot)
	}
	if s.P.Lessons[l].Biweekly != (slot.Parity != domain.EveryWeek) {
		return fmt.Errorf("%w: lesson %d, slot %v", ErrParityMismatch, s.P.Lessons[l].ID, slot)
	}
	if room != None && (room < 0 || int(room) >= len(s.P.Rooms)) {
		return fmt.Errorf("%w: %d", ErrUnknownRoom, room)
	}
	return nil
}

// Unplace removes lesson l from the schedule. It is a no-op for unplaced lessons.
func (s *Schedule) Unplace(l int32) {
	if !s.asg[l].Placed {
		return
	}
	s.apply(l, -1)
	pinned := s.asg[l].Pinned
	s.asg[l] = Assignment{Room: None, Pinned: pinned}
}

// SetPinned marks lesson l as fixed for the solver (arch §10.3).
func (s *Schedule) SetPinned(l int32, pinned bool) { s.asg[l].Pinned = pinned }

func (s *Schedule) apply(l int32, delta int) {
	a := s.asg[l]
	les := &s.P.Lessons[l]
	for _, c := range s.P.Grid.Cells(a.Slot) {
		bump(s.teacherOcc, int(les.Teacher)*s.cells+c, delta)
		if a.Room != None {
			bump(s.roomOcc, int(a.Room)*s.cells+c, delta)
		}
		for _, u := range les.Units {
			unit := &s.P.Units[u]
			bump(s.unitOcc, int(u)*s.cells+c, delta)
			if unit.Division == None {
				bump(s.wholeOcc, int(unit.Group)*s.cells+c, delta)
			} else {
				bump(s.subOcc, int(unit.Group)*s.cells+c, delta)
				bump(s.divOcc, int(unit.Division)*s.cells+c, delta)
			}
		}
	}
}

func bump(occ []uint16, i, delta int) {
	occ[i] = uint16(int(occ[i]) + delta)
}

// TeacherLoad returns how many placed lessons of teacher t occupy the cell.
func (s *Schedule) TeacherLoad(t int32, cell int) int {
	return int(s.teacherOcc[int(t)*s.cells+cell])
}

// RoomLoad returns how many placed lessons occupy room r in the cell.
func (s *Schedule) RoomLoad(r int32, cell int) int {
	return int(s.roomOcc[int(r)*s.cells+cell])
}

// UnitLoad returns how many placed lessons that share at least one student with unit u
// occupy the cell. For a whole group that is every lesson of the group or any of its
// subgroups; for a subgroup it is whole-group lessons, lessons of subgroups from other
// divisions and lessons of the same subgroup.
func (s *Schedule) UnitLoad(u int32, cell int) int {
	unit := &s.P.Units[u]
	g := int(unit.Group)*s.cells + cell
	if unit.Division == None {
		return int(s.wholeOcc[g]) + int(s.subOcc[g])
	}
	d := int(unit.Division)*s.cells + cell
	otherDivisions := int(s.subOcc[g]) - int(s.divOcc[d])
	return int(s.wholeOcc[g]) + otherDivisions + int(s.unitOcc[int(u)*s.cells+cell])
}

// GroupLoad returns how many placed lessons involve group g (whole or any subgroup) in the cell.
func (s *Schedule) GroupLoad(g int32, cell int) int {
	i := int(g)*s.cells + cell
	return int(s.wholeOcc[i]) + int(s.subOcc[i])
}
