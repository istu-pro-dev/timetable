// Command ttx converts timetable files to the ttx/1 exchange format and reviews them.
//
//	ttx convert <file>        print ttx JSON Lines
//	ttx check [-json] <file>  print the review report
//
// <file> is a Тандем .xls/.xlsx timetable or a ttx file.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/istu-pro-dev/timetable/backend/internal/ingest"
	"github.com/istu-pro-dev/timetable/backend/internal/ingest/ttx"
	"github.com/istu-pro-dev/timetable/backend/internal/review"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "ttx:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ttx convert <file> | ttx check [-json] <file>")
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the report as JSON")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("%s: expected one file", args[0])
	}
	name := fs.Arg(0)
	data, err := os.ReadFile(name)
	if err != nil {
		return err
	}
	batch, err := ingest.Load(name, data)
	if err != nil {
		return err
	}
	switch args[0] {
	case "convert":
		return ttx.Write(out, batch)
	case "check":
		rep, err := review.Analyze(batch)
		if err != nil {
			return err
		}
		if *asJSON {
			enc := json.NewEncoder(out)
			enc.SetIndent("", "  ")
			return enc.Encode(rep)
		}
		printReport(out, rep)
		return nil
	}
	return fmt.Errorf("unknown command %q", args[0])
}

func printReport(w io.Writer, r *review.Report) {
	st := r.Stats
	fmt.Fprintf(w, "%s\nзанятий %d, групп %d, преподавателей %d, аудиторий %d, дисциплин %d\n\n",
		r.Source.File, st.Lessons, st.Groups, st.Teachers, st.Rooms, st.Disciplines)

	fmt.Fprintf(w, "Накладки: %d\n", len(r.Violations))
	for _, v := range r.Violations {
		fmt.Fprintf(w, "  [%s] %s\n", v.Rule, v.Message)
	}

	fmt.Fprintf(w, "\nМягкие критерии (итого %.0f):\n", r.SoftTotal)
	for _, c := range r.Soft {
		if !c.Available {
			fmt.Fprintf(w, "  %-34s нет данных\n", c.Title)
			continue
		}
		fmt.Fprintf(w, "  %-34s %5d × %g = %g\n", c.Title, c.Value, c.Weight, c.Weighted)
	}

	worst := slices.Clone(r.Groups)
	slices.SortFunc(worst, func(a, b review.EntityScore) int { return (b.Gaps + b.Transitions) - (a.Gaps + a.Transitions) })
	fmt.Fprintln(w, "\nГруппы с наибольшим числом окон и переходов:")
	for _, g := range worst[:min(10, len(worst))] {
		fmt.Fprintf(w, "  %-14s окон %2d, переходов %2d, максимум пар в день %d\n", g.Name, g.Gaps, g.Transitions, g.MaxPerDay)
	}

	if len(r.Issues) > 0 {
		fmt.Fprintf(w, "\nКачество данных (%d):\n", len(r.Issues))
		for _, is := range r.Issues {
			fmt.Fprintf(w, "  %s: %s\n", is.Code, is.Message)
		}
	}
	fmt.Fprintln(w, "\nОграничения проверки:\n  "+strings.Join(r.Notes, "\n  "))
}
