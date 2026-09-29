package domain

import (
	"errors"
	"fmt"
)

// Day is a day of the teaching week, Monday = 0.
type Day uint8

// Days of the week.
const (
	Monday Day = iota
	Tuesday
	Wednesday
	Thursday
	Friday
	Saturday
	Sunday
)

var dayCodes = [...]string{"MO", "TU", "WE", "TH", "FR", "SA", "SU"}

// String returns the two-letter code used in APIs and .rasp files ("MO".."SU").
func (d Day) String() string {
	if int(d) < len(dayCodes) {
		return dayCodes[d]
	}
	return fmt.Sprintf("Day(%d)", d)
}

// ParseDay parses a two-letter day code.
func ParseDay(s string) (Day, error) {
	for i, c := range dayCodes {
		if c == s {
			return Day(i), nil
		}
	}
	return 0, fmt.Errorf("unknown day %q", s)
}

// Parity says in which weeks of a two-week cycle a lesson takes place
// (числитель / знаменатель).
type Parity uint8

// Week parities.
const (
	// EveryWeek lessons take place every week.
	EveryWeek Parity = iota
	// OddWeek lessons take place in odd weeks only (числитель).
	OddWeek
	// EvenWeek lessons take place in even weeks only (знаменатель).
	EvenWeek
)

var parityCodes = [...]string{"every", "odd", "even"}

func (p Parity) String() string {
	if int(p) < len(parityCodes) {
		return parityCodes[p]
	}
	return fmt.Sprintf("Parity(%d)", p)
}

// ParseParity parses "every", "odd" or "even".
func ParseParity(s string) (Parity, error) {
	for i, c := range parityCodes {
		if c == s {
			return Parity(i), nil
		}
	}
	return 0, fmt.Errorf("unknown parity %q", s)
}

// Overlaps reports whether lessons with parities p and q can meet in the same week.
func (p Parity) Overlaps(q Parity) bool {
	return p == EveryWeek || q == EveryWeek || p == q
}

// Slot is a position in the weekly timetable: day × period (1-based pair number) × parity.
type Slot struct {
	Day    Day
	Period uint8
	Parity Parity
}

func (s Slot) String() string {
	return fmt.Sprintf("%s-%d/%s", s.Day, s.Period, s.Parity)
}

// Overlaps reports whether two slots occupy the same time in at least one week.
func (s Slot) Overlaps(o Slot) bool {
	return s.Day == o.Day && s.Period == o.Period && s.Parity.Overlaps(o.Parity)
}

// Grid describes the shape of the teaching week.
//
// Occupancy is tracked per cell: a (day, period, week) triple where week is 0 (odd)
// or 1 (even). An every-week slot covers two cells, an odd/even slot covers one.
// With a flat cell index conflict checks become plain array lookups.
type Grid struct {
	Days          uint8 // teaching days per week, starting from Monday
	PeriodsPerDay uint8 // pairs per day, periods are numbered 1..PeriodsPerDay
}

// Grid validation errors.
var (
	ErrInvalidGrid = errors.New("invalid grid")
	ErrOutOfGrid   = errors.New("slot is outside the grid")
)

// Validate checks that the grid has a sane shape.
func (g Grid) Validate() error {
	if g.Days == 0 || g.Days > 7 {
		return fmt.Errorf("%w: days must be in 1..7, got %d", ErrInvalidGrid, g.Days)
	}
	if g.PeriodsPerDay == 0 {
		return fmt.Errorf("%w: periods per day must be positive", ErrInvalidGrid)
	}
	return nil
}

// Contains reports whether the slot fits into the grid.
func (g Grid) Contains(s Slot) bool {
	return uint8(s.Day) < g.Days && s.Period >= 1 && s.Period <= g.PeriodsPerDay && s.Parity <= EvenWeek
}

// CellCount is the number of occupancy cells in the grid.
func (g Grid) CellCount() int {
	return int(g.Days) * int(g.PeriodsPerDay) * 2
}

// TimeCount is the number of (day, period) positions in the grid, ignoring parity.
func (g Grid) TimeCount() int {
	return int(g.Days) * int(g.PeriodsPerDay)
}

// TimeIndex returns the flat index of the slot's (day, period) position in 0..TimeCount-1.
func (g Grid) TimeIndex(s Slot) int {
	return int(s.Day)*int(g.PeriodsPerDay) + int(s.Period) - 1
}

// Cells returns the occupancy cells covered by the slot: one for odd/even slots,
// two for every-week slots. The slot must be inside the grid.
func (g Grid) Cells(s Slot) []int {
	base := g.TimeIndex(s) * 2
	switch s.Parity {
	case OddWeek:
		return []int{base}
	case EvenWeek:
		return []int{base + 1}
	default:
		return []int{base, base + 1}
	}
}

// Slots enumerates every slot of the grid for the given parities, ordered by day,
// period and then parity in the order given.
func (g Grid) Slots(parities ...Parity) []Slot {
	if len(parities) == 0 {
		parities = []Parity{EveryWeek}
	}
	out := make([]Slot, 0, g.TimeCount()*len(parities))
	for d := range g.Days {
		for p := uint8(1); p <= g.PeriodsPerDay; p++ {
			for _, par := range parities {
				out = append(out, Slot{Day: Day(d), Period: p, Parity: par})
			}
		}
	}
	return out
}
