package review

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/istu-pro-dev/timetable/backend/internal/engine"
	"github.com/istu-pro-dev/timetable/backend/internal/engine/soft"
	"github.com/istu-pro-dev/timetable/backend/internal/ingest/ttx"
	"github.com/istu-pro-dev/timetable/backend/internal/solver"
	"github.com/istu-pro-dev/timetable/backend/internal/solver/stageb"
)

// ImproveOptions configures Improve.
type ImproveOptions struct {
	// ChangePenalty is the cost of moving one lesson, in weighted penalty points; it decides
	// how many changes are worth a given gain. Zero means 30.
	ChangePenalty float64
	// Budget bounds the search; zero means 10 seconds.
	Budget  time.Duration
	Seed    uint64
	Weights *soft.Weights
}

// Improvement is the result of Improve: the lessons to move and what it gains.
type Improvement struct {
	Moves      []Relocation `json:"moves"`
	Before     []Criterion  `json:"before"`
	After      []Criterion  `json:"after"`
	SoftBefore float64      `json:"soft_before"`
	SoftAfter  float64      `json:"soft_after"`
	HardBefore int          `json:"hard_before"`
	HardAfter  int          `json:"hard_after"`
	Steps      int64        `json:"steps"`
	Duration   string       `json:"duration"`
	Notes      []string     `json:"notes"`
}

// Placement is where a lesson takes place.
type Placement struct {
	Day    string `json:"day"`
	Period int    `json:"period"`
	Parity string `json:"parity"`
	Room   string `json:"room,omitempty"`
}

// Relocation moves one lesson.
type Relocation struct {
	Lesson     int       `json:"lesson_id"`
	Discipline string    `json:"discipline"`
	Kind       string    `json:"lesson_type"`
	Audience   string    `json:"audience"`
	Teachers   []string  `json:"teachers"`
	From       Placement `json:"from"`
	To         Placement `json:"to"`
	Ref        *ttx.Ref  `json:"ref,omitempty"`
}

// Improve runs solver stage B on the batch as a draft: every lesson may move, but each moved
// lesson costs ChangePenalty, so the result is a short list of changes worth making. Hard
// conflicts of the draft are removed first where possible.
func Improve(ctx context.Context, b *ttx.Batch, opt ImproveOptions) (*Improvement, error) {
	if opt.ChangePenalty == 0 {
		opt.ChangePenalty = 30
	}
	if opt.Budget == 0 {
		opt.Budget = 10 * time.Second
	}
	m, p, s, pol, err := load(b)
	if err != nil {
		return nil, err
	}
	w := soft.DefaultWeights()
	if opt.Weights != nil {
		w = *opt.Weights
	}
	before := s.Assignments()
	o := stageb.New(solver.NewGraph(p), s, stageb.Options{
		Weights: &w, Policy: pol, Budget: opt.Budget, Seed: opt.Seed, ChangePenalty: opt.ChangePenalty,
	})
	st := o.Run(ctx)

	imp := &Improvement{
		HardBefore: st.HardBefore, HardAfter: st.HardAfter, Steps: st.Steps, Duration: st.Duration.Round(time.Millisecond).String(),
		SoftBefore: st.Initial.Soft.Total(w), SoftAfter: st.Final.Soft.Total(w),
		Before: criteria(st.Initial.Soft, w, m.capacityOK), After: criteria(st.Final.Soft, w, m.capacityOK),
		Moves: []Relocation{},
		Notes: []string{
			"Занятость преподавателей и аудиторий за пределами файла неизвестна: каждый перенос нужно согласовать.",
			"Занятия без аудитории (онлайн, «-») не переносятся.",
		},
	}
	byID := map[int]*ttx.Lesson{}
	for i := range b.Lessons {
		byID[b.Lessons[i].ID] = &b.Lessons[i]
	}
	for l := range p.Lessons {
		x, y := before[l], s.Assignment(int32(l))
		if x.Slot == y.Slot && x.Room == y.Room {
			continue
		}
		les := byID[int(p.Lessons[l].ID)]
		var groups []string
		for _, mem := range les.Audience {
			groups = append(groups, memberLabel(mem))
		}
		r := Relocation{
			Lesson: les.ID, Discipline: les.Discipline, Kind: les.LessonType, Audience: strings.Join(groups, ", "),
			Teachers: les.Teachers, From: m.placement(p, x), To: m.placement(p, y),
		}
		if len(les.Refs) > 0 {
			r.Ref = &les.Refs[0]
		}
		imp.Moves = append(imp.Moves, r)
	}
	slices.SortFunc(imp.Moves, func(a, b Relocation) int { return strings.Compare(a.Audience, b.Audience) })
	return imp, nil
}

func memberLabel(m ttx.Member) string {
	if m.Subgroup == nil {
		return m.Group
	}
	return m.Group + " п/г " + strconv.Itoa(*m.Subgroup)
}

func (m *model) placement(p *engine.Problem, a engine.Assignment) Placement {
	pl := Placement{Day: ttx.Days[a.Slot.Day], Period: int(a.Slot.Period), Parity: a.Slot.Parity.String()}
	if a.Room != engine.None {
		pl.Room = m.roomName[p.Rooms[a.Room].ID]
	}
	return pl
}

func criteria(b soft.Breakdown, w soft.Weights, capacityOK bool) []Criterion {
	unavailable := map[soft.Criterion]bool{soft.Preferences: true, soft.CapacityWaste: !capacityOK, soft.RoomCount: true}
	var out []Criterion
	for c := soft.Criterion(0); c < soft.NumCriteria; c++ {
		cr := Criterion{Key: c.Key(), Title: criterionTitles[c], Value: b[c], Weight: w[c], Available: !unavailable[c]}
		if cr.Available {
			cr.Weighted = w[c] * float64(b[c])
		}
		out = append(out, cr)
	}
	return out
}
