package tandem

import (
	"errors"
	"os"
	"reflect"
	"slices"
	"testing"

	"github.com/istu-pro-dev/timetable/backend/internal/ingest/sheet"
	"github.com/istu-pro-dev/timetable/backend/internal/ingest/ttx"
)

func convert(t *testing.T, name string) *ttx.Batch {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	wb, err := sheet.Read(data)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Convert(wb, name, data)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func find(t *testing.T, b *ttx.Batch, day string, period int, parity, discipline string) []ttx.Lesson {
	t.Helper()
	var out []ttx.Lesson
	for _, l := range b.Lessons {
		if l.Day == day && l.Period == period && l.Parity == parity && l.Discipline == discipline {
			out = append(out, l)
		}
	}
	return out
}

func audience(l ttx.Lesson) []string {
	var out []string
	for _, m := range l.Audience {
		out = append(out, memberKey(m))
	}
	return out
}

func TestConvertFixture(t *testing.T) {
	b := convert(t, "t1.xls")

	if b.Source.Adapter != Adapter || b.Source.File != "t1.xls" || len(b.Source.SHA256) != 64 {
		t.Errorf("source = %+v", b.Source)
	}
	if len(b.Lessons) != 11 {
		t.Errorf("lessons = %d, want 11", len(b.Lessons))
	}

	t.Run("subgroups by position", func(t *testing.T) {
		db := find(t, b, "mon", 1, "odd", "Базы данных")
		os := find(t, b, "mon", 1, "odd", "Операционные системы")
		if len(db) != 1 || len(os) != 1 {
			t.Fatalf("got %d and %d lessons", len(db), len(os))
		}
		if a := audience(db[0]); !slices.Equal(a, []string{"Б24-782-1/1"}) {
			t.Errorf("БД audience = %v", a)
		}
		if a := audience(os[0]); !slices.Equal(a, []string{"Б24-782-1/2"}) {
			t.Errorf("ОС audience = %v", a)
		}
		if !slices.Contains(db[0].Flags, "subgroup_by_position") {
			t.Errorf("flags = %v", db[0].Flags)
		}
	})

	t.Run("stream lecture in both halves is one every-week lesson", func(t *testing.T) {
		ls := find(t, b, "mon", 2, "every", "Правовые основы")
		if len(ls) != 1 {
			t.Fatalf("got %d lessons", len(ls))
		}
		if a := audience(ls[0]); !slices.Equal(a, []string{"Б24-782-1", "Б24-782-2"}) {
			t.Errorf("audience = %v", a)
		}
		if len(ls[0].Refs) != 4 {
			t.Errorf("refs = %v", ls[0].Refs)
		}
	})

	t.Run("rooms and places", func(t *testing.T) {
		os := find(t, b, "mon", 1, "odd", "Операционные системы")[0]
		if os.Room != "3-603а" {
			t.Errorf("room = %q", os.Room)
		}
		rooms := map[string]ttx.Room{}
		for _, r := range b.Rooms {
			rooms[r.Key] = r
		}
		if r := rooms["3-603а"]; r.Building != "3" || r.Type != "computer" || r.Capacity == nil || *r.Capacity != 28 {
			t.Errorf("3-603а = %+v", r)
		}
		if r := rooms["2-420"]; r.Building != "2" || r.Type != "general" || r.Capacity != nil {
			t.Errorf("2-420 = %+v", r)
		}
		if r := rooms["Интеграл"]; r.Type != "sport" {
			t.Errorf("Интеграл = %+v", r)
		}
		if l := find(t, b, "mon", 4, "odd", "Неклассические логики"); len(l) != 1 || l[0].Place != "online" || l[0].Room != "" {
			t.Errorf("online lesson = %+v", l)
		}
	})

	t.Run("lesson without teacher row", func(t *testing.T) {
		l := find(t, b, "mon", 1, "even", "Кураторский час")
		if len(l) != 1 || l[0].LessonType != "curator" || len(l[0].Teachers) != 0 || l[0].Place != "none" {
			t.Fatalf("curator = %+v", l)
		}
		if !slices.Contains(l[0].Flags, "no_teacher") {
			t.Errorf("flags = %v", l[0].Flags)
		}
	})

	t.Run("typos are fixed and reported", func(t *testing.T) {
		l := find(t, b, "sat", 1, "even", "Производственная практика")
		if len(l) != 1 || l[0].LessonType != "internship" {
			t.Fatalf("internship = %+v", l)
		}
		var codes []string
		for _, is := range b.Issues {
			codes = append(codes, is.Code)
		}
		if !slices.Equal(codes, []string{"typo_week", "typo_day"}) {
			t.Errorf("issues = %+v", b.Issues)
		}
	})

	t.Run("groups are parsed", func(t *testing.T) {
		g := b.Groups[1]
		want := ttx.Group{Key: "Б24-782-2", Level: "bachelor", Intake: 2024, ProgramNo: "782", No: 2, Form: "full_time"}
		if !reflect.DeepEqual(g, want) {
			t.Errorf("group = %+v", g)
		}
	})
}

func TestConvertXLSXSameAsXLS(t *testing.T) {
	a, b := convert(t, "t1.xls"), convert(t, "t1.xlsx")
	if !reflect.DeepEqual(a.Lessons, b.Lessons) {
		t.Error("lessons differ between .xls and .xlsx")
	}
	if !reflect.DeepEqual(a.Rooms, b.Rooms) || !reflect.DeepEqual(a.Groups, b.Groups) {
		t.Error("reference data differs between .xls and .xlsx")
	}
}

func TestDetect(t *testing.T) {
	data, err := os.ReadFile("testdata/t1.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	wb, err := sheet.Read(data)
	if err != nil {
		t.Fatal(err)
	}
	if !Detect(wb) {
		t.Error("fixture not detected")
	}
	if Detect(&sheet.Workbook{}) {
		t.Error("empty workbook detected")
	}
	if _, err := Convert(&sheet.Workbook{}, "x.xls", nil); !errors.Is(err, ErrNotTandem) {
		t.Errorf("err = %v", err)
	}
}

func TestConvertHeaderOnly(t *testing.T) {
	data, err := os.ReadFile("testdata/t1.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	wb, err := sheet.Read(data)
	if err != nil {
		t.Fatal(err)
	}
	s := wb.Sheets[0]
	for p := range s.Cells {
		if p.Row > 1 {
			delete(s.Cells, p)
		}
	}
	if _, err := Convert(wb, "t1.xlsx", data); !errors.Is(err, ErrNoLessons) {
		t.Errorf("err = %v, want ErrNoLessons", err)
	}
}

func TestParseGroup(t *testing.T) {
	cases := map[string]ttx.Group{
		"С25-361-1":   {Key: "С25-361-1", Level: "specialist", Intake: 2025, ProgramNo: "361", No: 1, Form: "full_time"},
		"Б23-831-1зу": {Key: "Б23-831-1зу", Level: "bachelor", Intake: 2023, ProgramNo: "831", No: 1, Form: "part_time_accelerated"},
		"Группа":      {Key: "Группа"},
	}
	for code, want := range cases {
		if got := parseGroup(code); !reflect.DeepEqual(got, want) {
			t.Errorf("parseGroup(%q) = %+v", code, got)
		}
	}
}
