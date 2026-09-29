package domain

import (
	"slices"
	"testing"
)

func TestMemberOverlaps(t *testing.T) {
	const g1, g2 GroupID = 1, 2
	tests := []struct {
		name string
		a, b Member
		want bool
	}{
		{"same whole group", WholeGroup(g1), WholeGroup(g1), true},
		{"different groups", WholeGroup(g1), WholeGroup(g2), false},
		{"whole group vs its subgroup", WholeGroup(g1), Subgroup(g1, "english", 1), true},
		{"subgroup of another group", Subgroup(g1, "english", 1), Subgroup(g2, "english", 1), false},
		{"same subgroup", Subgroup(g1, "english", 2), Subgroup(g1, "english", 2), true},
		{"parallel subgroups", Subgroup(g1, "english", 1), Subgroup(g1, "english", 2), false},
		{"cross divisions", Subgroup(g1, "english", 1), Subgroup(g1, "pe", 2), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.Overlaps(tt.b); got != tt.want {
				t.Fatalf("a.Overlaps(b) = %v, want %v", got, tt.want)
			}
			if got := tt.b.Overlaps(tt.a); got != tt.want {
				t.Fatalf("b.Overlaps(a) = %v, want %v (symmetry)", got, tt.want)
			}
		})
	}
}

func TestMemberIsWhole(t *testing.T) {
	if !WholeGroup(1).IsWhole() {
		t.Fatal("WholeGroup should be whole")
	}
	if Subgroup(1, "english", 1).IsWhole() {
		t.Fatal("Subgroup should not be whole")
	}
}

func TestAudienceOverlaps(t *testing.T) {
	stream := Audience{WholeGroup(1), WholeGroup(2), WholeGroup(3)}
	tests := []struct {
		name string
		a, b Audience
		want bool
	}{
		{"stream vs member group", stream, Audience{WholeGroup(2)}, true},
		{"stream vs member subgroup", stream, Audience{Subgroup(3, "lab", 1)}, true},
		{"stream vs outside group", stream, Audience{WholeGroup(4)}, false},
		{"disjoint streams", Audience{WholeGroup(1), WholeGroup(2)}, Audience{WholeGroup(3), WholeGroup(4)}, false},
		{"parallel subgroups", Audience{Subgroup(1, "lab", 1)}, Audience{Subgroup(1, "lab", 2)}, false},
		{"empty audience", Audience{}, stream, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.Overlaps(tt.b); got != tt.want {
				t.Fatalf("Overlaps = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAudienceGroups(t *testing.T) {
	a := Audience{Subgroup(2, "lab", 1), WholeGroup(1), Subgroup(2, "lab", 2)}
	if got := a.Groups(); !slices.Equal(got, []GroupID{2, 1}) {
		t.Fatalf("Groups() = %v", got)
	}
}
