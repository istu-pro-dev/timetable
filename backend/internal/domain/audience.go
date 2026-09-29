package domain

import "slices"

// ID types mirror bigint identity keys of the reference tables in the database.
type (
	// BuildingID identifies a building (корпус).
	BuildingID int64
	// RoomID identifies a room.
	RoomID int64
	// GroupID identifies a student group.
	GroupID int64
	// TeacherID identifies a teacher.
	TeacherID int64
	// DisciplineID identifies a discipline.
	DisciplineID int64
	// LessonID identifies a lesson — one schedulable occurrence from the curriculum.
	LessonID int64
)

// Member is one part of a lesson's audience: a whole group or one subgroup of it.
//
// A group may be split in several independent ways — divisions — e.g. "english"
// (by language level) and "pe" (by gender). Subgroups of the same division are
// disjoint sets of students and may study in parallel; subgroups of different
// divisions share students.
type Member struct {
	Group    GroupID
	Division string // empty for the whole group
	Part     uint8  // 1-based subgroup number within Division; 0 for the whole group
}

// WholeGroup returns the member that stands for all students of the group.
func WholeGroup(g GroupID) Member {
	return Member{Group: g}
}

// Subgroup returns the member for part `part` of the group's division.
func Subgroup(g GroupID, division string, part uint8) Member {
	return Member{Group: g, Division: division, Part: part}
}

// IsWhole reports whether the member is a whole group.
func (m Member) IsWhole() bool {
	return m.Division == ""
}

// Overlaps reports whether two members may contain the same student.
func (m Member) Overlaps(o Member) bool {
	if m.Group != o.Group {
		return false
	}
	if m.IsWhole() || o.IsWhole() {
		return true
	}
	if m.Division != o.Division {
		// Different divisions of one group cut across each other.
		return true
	}
	return m.Part == o.Part
}

// Audience is the set of students attending a lesson: a single group, a subgroup,
// or a stream (several groups at a joint lecture).
type Audience []Member

// Overlaps reports whether two audiences share at least one student, i.e. the
// lessons cannot take place at the same time (H2).
func (a Audience) Overlaps(b Audience) bool {
	return slices.ContainsFunc(a, func(m Member) bool {
		return slices.ContainsFunc(b, m.Overlaps)
	})
}

// Groups returns the distinct groups the audience touches, in first-seen order.
func (a Audience) Groups() []GroupID {
	out := make([]GroupID, 0, len(a))
	seen := make(map[GroupID]struct{}, len(a))
	for _, m := range a {
		if _, ok := seen[m.Group]; ok {
			continue
		}
		seen[m.Group] = struct{}{}
		out = append(out, m.Group)
	}
	return out
}
