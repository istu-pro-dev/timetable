// Package ttx is the exchange format "ttx/1" (docs/istu-data-format.md §4): JSON Lines, one
// record per line, the "kind" field tells the record type. Every import adapter produces a
// Batch; analysis and loading only ever see this format, never the source files.
package ttx

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
)

// Format is the value of Source.Format.
const Format = "ttx/1"

// Days are the day codes used by lessons, Monday first.
var Days = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}

// Source describes where a batch comes from.
type Source struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Format  string `json:"format"`
	Adapter string `json:"adapter"`
	File    string `json:"file,omitempty"`
	SHA256  string `json:"sha256,omitempty"`
	Title   string `json:"title,omitempty"`
}

// Period is one entry of the bell schedule.
type Period struct {
	Kind  string `json:"kind"`
	N     int    `json:"n"`
	Start string `json:"start"`
	End   string `json:"end"`
}

// Group is a student group; Key is its code (Б24-782-2).
type Group struct {
	Kind      string `json:"kind"`
	Key       string `json:"key"`
	Level     string `json:"level,omitempty"`
	Intake    int    `json:"intake,omitempty"`
	ProgramNo string `json:"program_no,omitempty"`
	No        int    `json:"no,omitempty"`
	Form      string `json:"form,omitempty"`
	Size      *int   `json:"size"`
}

// Teacher is keyed by "Фамилия И.О.".
type Teacher struct {
	Kind     string `json:"kind"`
	Key      string `json:"key"`
	FullName string `json:"full_name,omitempty"`
}

// Room is a physical room; Key is its code (3-605).
type Room struct {
	Kind     string `json:"kind"`
	Key      string `json:"key"`
	Building string `json:"building"`
	Type     string `json:"type"`
	Title    string `json:"title"`
	Capacity *int   `json:"capacity"`
}

// Place is a non-room location: online, none.
type Place struct {
	Kind    string `json:"kind"`
	Key     string `json:"key"`
	Title   string `json:"title"`
	Virtual bool   `json:"virtual"`
}

// Ref points at the source cell a record was read from.
type Ref struct {
	Sheet string `json:"sheet"`
	Row   int    `json:"row"` // 1-based, as shown by spreadsheet programs
	Col   string `json:"col"`
}

func (r Ref) String() string { return fmt.Sprintf("лист %s, %s%d", r.Sheet, r.Col, r.Row) }

// Member is one part of a lesson's audience; Subgroup is nil for the whole group.
type Member struct {
	Group    string `json:"group"`
	Subgroup *int   `json:"subgroup"`
}

// Lesson is one observed lesson: one audience in one slot.
type Lesson struct {
	Kind       string            `json:"kind"`
	ID         int               `json:"id"`
	Refs       []Ref             `json:"refs"`
	Audience   []Member          `json:"audience"`
	Day        string            `json:"day"`
	Period     int               `json:"period"`
	Parity     string            `json:"parity"` // every, odd (числитель), even (знаменатель)
	LessonType string            `json:"lesson_type"`
	Discipline string            `json:"discipline"`
	Teachers   []string          `json:"teachers"`
	Room       string            `json:"room,omitempty"`
	Place      string            `json:"place,omitempty"`
	Raw        map[string]string `json:"raw,omitempty"`
	Note       string            `json:"note,omitempty"`
	Flags      []string          `json:"flags,omitempty"`
}

// Issue is a data-quality finding of the adapter, tied to a source cell.
type Issue struct {
	Kind    string `json:"kind"`
	Code    string `json:"code"`
	Ref     *Ref   `json:"ref,omitempty"`
	Message string `json:"message"`
}

// Batch is the content of one ttx file.
type Batch struct {
	Source   Source
	Periods  []Period
	Groups   []Group
	Teachers []Teacher
	Rooms    []Room
	Places   []Place
	Lessons  []Lesson
	Issues   []Issue
}

// Write encodes the batch as JSON Lines, in a stable order.
func Write(w io.Writer, b *Batch) error {
	bw := bufio.NewWriter(w)
	enc := json.NewEncoder(bw)
	enc.SetEscapeHTML(false)
	put := func(v any) error { return enc.Encode(v) }
	src := b.Source
	src.Kind, src.Format = "source", Format
	if err := put(src); err != nil {
		return err
	}
	for _, x := range b.Periods {
		x.Kind = "period"
		if err := put(x); err != nil {
			return err
		}
	}
	for _, x := range b.Groups {
		x.Kind = "group"
		if err := put(x); err != nil {
			return err
		}
	}
	for _, x := range b.Teachers {
		x.Kind = "teacher"
		if err := put(x); err != nil {
			return err
		}
	}
	for _, x := range b.Rooms {
		x.Kind = "room"
		if err := put(x); err != nil {
			return err
		}
	}
	for _, x := range b.Places {
		x.Kind = "place"
		if err := put(x); err != nil {
			return err
		}
	}
	for _, x := range b.Lessons {
		x.Kind = "lesson"
		if x.Teachers == nil {
			x.Teachers = []string{}
		}
		if err := put(x); err != nil {
			return err
		}
	}
	for _, x := range b.Issues {
		x.Kind = "issue"
		if err := put(x); err != nil {
			return err
		}
	}
	return bw.Flush()
}

// ErrFormat wraps every decoding error.
var ErrFormat = errors.New("invalid ttx data")

// maxLine bounds one JSON line.
const maxLine = 1 << 20

// Read decodes JSON Lines into a batch. Unknown kinds are skipped so that newer producers
// stay readable; references between records are checked by Validate.
func Read(r io.Reader) (*Batch, error) {
	b := &Batch{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), maxLine)
	line := 0
	for sc.Scan() {
		line++
		data := sc.Bytes()
		if len(trimSpace(data)) == 0 {
			continue
		}
		var head struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(data, &head); err != nil {
			return nil, fmt.Errorf("%w: line %d: %w", ErrFormat, line, err)
		}
		var err error
		switch head.Kind {
		case "source":
			err = json.Unmarshal(data, &b.Source)
		case "period":
			err = appendDecoded(data, &b.Periods)
		case "group":
			err = appendDecoded(data, &b.Groups)
		case "teacher":
			err = appendDecoded(data, &b.Teachers)
		case "room":
			err = appendDecoded(data, &b.Rooms)
		case "place":
			err = appendDecoded(data, &b.Places)
		case "lesson":
			err = appendDecoded(data, &b.Lessons)
		case "issue":
			err = appendDecoded(data, &b.Issues)
		}
		if err != nil {
			return nil, fmt.Errorf("%w: line %d: %w", ErrFormat, line, err)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFormat, err)
	}
	if err := b.Validate(); err != nil {
		return nil, err
	}
	return b, nil
}

func appendDecoded[T any](data []byte, list *[]T) error {
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*list = append(*list, v)
	return nil
}

func trimSpace(b []byte) []byte {
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\t' || b[0] == '\r') {
		b = b[1:]
	}
	return b
}

// Validate checks the references between records. Groups, teachers and rooms that lessons
// mention but the batch does not declare are added with only a key: a producer may send
// lessons alone.
func (b *Batch) Validate() error {
	periods := map[int]bool{}
	for _, p := range b.Periods {
		periods[p.N] = true
	}
	groups := map[string]bool{}
	for _, g := range b.Groups {
		groups[g.Key] = true
	}
	teachers := map[string]bool{}
	for _, t := range b.Teachers {
		teachers[t.Key] = true
	}
	rooms := map[string]bool{}
	for _, r := range b.Rooms {
		rooms[r.Key] = true
	}
	places := map[string]bool{}
	for _, p := range b.Places {
		places[p.Key] = true
	}
	ids := map[int]bool{}
	for i := range b.Lessons {
		l := &b.Lessons[i]
		at := fmt.Sprintf("lesson %d", l.ID)
		if ids[l.ID] {
			return fmt.Errorf("%w: duplicate %s", ErrFormat, at)
		}
		ids[l.ID] = true
		if !slices.Contains(Days, l.Day) {
			return fmt.Errorf("%w: %s: unknown day %q", ErrFormat, at, l.Day)
		}
		if l.Period < 1 || len(periods) > 0 && !periods[l.Period] {
			return fmt.Errorf("%w: %s: unknown period %d", ErrFormat, at, l.Period)
		}
		if l.Parity == "" {
			l.Parity = "every"
		}
		if l.Parity != "every" && l.Parity != "odd" && l.Parity != "even" {
			return fmt.Errorf("%w: %s: unknown parity %q", ErrFormat, at, l.Parity)
		}
		if l.Teachers == nil {
			l.Teachers = []string{}
		}
		if len(l.Audience) == 0 {
			return fmt.Errorf("%w: %s: empty audience", ErrFormat, at)
		}
		for _, m := range l.Audience {
			if m.Subgroup != nil && (*m.Subgroup < 1 || *m.Subgroup > 255) {
				return fmt.Errorf("%w: %s: subgroup out of range", ErrFormat, at)
			}
			if !groups[m.Group] {
				groups[m.Group] = true
				b.Groups = append(b.Groups, Group{Key: m.Group})
			}
		}
		for _, t := range l.Teachers {
			if !teachers[t] {
				teachers[t] = true
				b.Teachers = append(b.Teachers, Teacher{Key: t})
			}
		}
		if l.Room != "" && l.Place != "" {
			return fmt.Errorf("%w: %s: both room and place", ErrFormat, at)
		}
		if l.Room != "" && !rooms[l.Room] {
			rooms[l.Room] = true
			b.Rooms = append(b.Rooms, Room{Key: l.Room, Title: l.Room})
		}
		if l.Place != "" && !places[l.Place] {
			places[l.Place] = true
			b.Places = append(b.Places, Place{Key: l.Place, Title: l.Place, Virtual: true})
		}
	}
	return nil
}
