package domain

import (
	"errors"
	"slices"
	"testing"
)

func TestDayRoundTrip(t *testing.T) {
	for d := Monday; d <= Sunday; d++ {
		got, err := ParseDay(d.String())
		if err != nil || got != d {
			t.Fatalf("ParseDay(%q) = %v, %v; want %v", d.String(), got, err, d)
		}
	}
	if _, err := ParseDay("XX"); err == nil {
		t.Fatal("ParseDay(XX) should fail")
	}
	if got := Day(9).String(); got != "Day(9)" {
		t.Fatalf("Day(9).String() = %q", got)
	}
}

func TestParityRoundTrip(t *testing.T) {
	for p := EveryWeek; p <= EvenWeek; p++ {
		got, err := ParseParity(p.String())
		if err != nil || got != p {
			t.Fatalf("ParseParity(%q) = %v, %v; want %v", p.String(), got, err, p)
		}
	}
	if _, err := ParseParity("weekly"); err == nil {
		t.Fatal("ParseParity(weekly) should fail")
	}
	if got := Parity(7).String(); got != "Parity(7)" {
		t.Fatalf("Parity(7).String() = %q", got)
	}
}

func TestParityOverlaps(t *testing.T) {
	tests := []struct {
		p, q Parity
		want bool
	}{
		{EveryWeek, EveryWeek, true},
		{EveryWeek, OddWeek, true},
		{EveryWeek, EvenWeek, true},
		{OddWeek, OddWeek, true},
		{EvenWeek, EvenWeek, true},
		{OddWeek, EvenWeek, false},
	}
	for _, tt := range tests {
		if got := tt.p.Overlaps(tt.q); got != tt.want {
			t.Errorf("%v.Overlaps(%v) = %v, want %v", tt.p, tt.q, got, tt.want)
		}
		if got := tt.q.Overlaps(tt.p); got != tt.want {
			t.Errorf("%v.Overlaps(%v) = %v, want %v (symmetry)", tt.q, tt.p, got, tt.want)
		}
	}
}

func TestSlotOverlaps(t *testing.T) {
	base := Slot{Day: Tuesday, Period: 3, Parity: OddWeek}
	tests := []struct {
		name  string
		other Slot
		want  bool
	}{
		{"same", base, true},
		{"every week covers odd", Slot{Tuesday, 3, EveryWeek}, true},
		{"odd vs even", Slot{Tuesday, 3, EvenWeek}, false},
		{"other period", Slot{Tuesday, 4, OddWeek}, false},
		{"other day", Slot{Wednesday, 3, OddWeek}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := base.Overlaps(tt.other); got != tt.want {
				t.Fatalf("Overlaps = %v, want %v", got, tt.want)
			}
		})
	}
	if got := base.String(); got != "TU-3/odd" {
		t.Fatalf("String() = %q", got)
	}
}

func TestGridValidate(t *testing.T) {
	tests := []struct {
		grid Grid
		ok   bool
	}{
		{Grid{Days: 6, PeriodsPerDay: 7}, true},
		{Grid{Days: 7, PeriodsPerDay: 1}, true},
		{Grid{Days: 0, PeriodsPerDay: 7}, false},
		{Grid{Days: 8, PeriodsPerDay: 7}, false},
		{Grid{Days: 6, PeriodsPerDay: 0}, false},
	}
	for _, tt := range tests {
		err := tt.grid.Validate()
		if tt.ok && err != nil {
			t.Errorf("%+v: unexpected error %v", tt.grid, err)
		}
		if !tt.ok && !errors.Is(err, ErrInvalidGrid) {
			t.Errorf("%+v: error = %v, want ErrInvalidGrid", tt.grid, err)
		}
	}
}

func TestGridContains(t *testing.T) {
	g := Grid{Days: 6, PeriodsPerDay: 7}
	tests := []struct {
		slot Slot
		want bool
	}{
		{Slot{Monday, 1, EveryWeek}, true},
		{Slot{Saturday, 7, EvenWeek}, true},
		{Slot{Sunday, 1, EveryWeek}, false},
		{Slot{Monday, 0, EveryWeek}, false},
		{Slot{Monday, 8, EveryWeek}, false},
		{Slot{Monday, 1, Parity(3)}, false},
	}
	for _, tt := range tests {
		if got := g.Contains(tt.slot); got != tt.want {
			t.Errorf("Contains(%v) = %v, want %v", tt.slot, got, tt.want)
		}
	}
}

func TestGridCells(t *testing.T) {
	g := Grid{Days: 6, PeriodsPerDay: 7}
	if g.CellCount() != 84 || g.TimeCount() != 42 {
		t.Fatalf("CellCount/TimeCount = %d/%d", g.CellCount(), g.TimeCount())
	}

	s := Slot{Tuesday, 3, EveryWeek}
	if got := g.TimeIndex(s); got != 9 {
		t.Fatalf("TimeIndex = %d, want 9", got)
	}
	if got := g.Cells(s); !slices.Equal(got, []int{18, 19}) {
		t.Fatalf("Cells(every) = %v", got)
	}
	s.Parity = OddWeek
	if got := g.Cells(s); !slices.Equal(got, []int{18}) {
		t.Fatalf("Cells(odd) = %v", got)
	}
	s.Parity = EvenWeek
	if got := g.Cells(s); !slices.Equal(got, []int{19}) {
		t.Fatalf("Cells(even) = %v", got)
	}
}

// Two in-grid slots overlap exactly when their cell sets intersect.
func TestGridCellsMatchOverlaps(t *testing.T) {
	g := Grid{Days: 2, PeriodsPerDay: 3}
	slots := g.Slots(EveryWeek, OddWeek, EvenWeek)
	for _, a := range slots {
		for _, b := range slots {
			shared := false
			for _, c := range g.Cells(a) {
				if slices.Contains(g.Cells(b), c) {
					shared = true
				}
			}
			if shared != a.Overlaps(b) {
				t.Fatalf("%v vs %v: shared cells = %v, Overlaps = %v", a, b, shared, a.Overlaps(b))
			}
		}
	}
}

func TestGridSlots(t *testing.T) {
	g := Grid{Days: 2, PeriodsPerDay: 2}
	got := g.Slots()
	want := []Slot{
		{Monday, 1, EveryWeek}, {Monday, 2, EveryWeek},
		{Tuesday, 1, EveryWeek}, {Tuesday, 2, EveryWeek},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Slots() = %v", got)
	}
	if n := len(g.Slots(OddWeek, EvenWeek)); n != 8 {
		t.Fatalf("len(Slots(odd, even)) = %d, want 8", n)
	}
	for _, s := range g.Slots(EveryWeek, OddWeek, EvenWeek) {
		if !g.Contains(s) {
			t.Fatalf("Slots produced out-of-grid slot %v", s)
		}
	}
}
