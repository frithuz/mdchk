package rules

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func md089Settings() engine.Settings {
	s := settings()
	off, on := false, true
	s["MD047"] = engine.RuleSetting{Enabled: &off}
	s["MD089"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{}}
	return s
}
func TestMD089AnalysisFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/md089.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source, Flavor  string
		Recognize, Incomplete bool
		SHA256                string
		SizeBytes             int
		Maximum               int
		Formats               []string

		Expected []struct {
			Range   []int
			Source  string
			Message string
		}
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	r := registry(t, Registrations())
	for _, f := range fixtures {
		for _, flavor := range []string{"commonmark", "gfm"} {
			if f.Flavor != "" && f.Flavor != flavor {
				continue
			}
			t.Run(f.Name+"/"+flavor, func(t *testing.T) {
				formats := f.Formats
				if len(formats) == 0 {
					formats = nil
				}
				fm, e := parser.NewFrontmatterOptions(f.Recognize, formats)
				if e != nil {
					t.Fatal(e)
				}
				dc := engine.DocumentContext{Flavor: flavor, Frontmatter: fm}
				setting := md089Settings()
				on := true
				setting["MD089"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{}}
				out := analyzeHeadings(t, r, f.Source, setting, dc, engine.RunOptions{})
				again := analyzeHeadings(t, r, f.Source, setting, dc, engine.RunOptions{})
				if !reflect.DeepEqual(out, again) || out.Complete == f.Incomplete || len(out.Errors) != 0 || len(out.Interruptions) != 0 || len(out.Diagnostics) != len(f.Expected) {
					t.Fatal("outcome", out.Complete, out.Diagnostics, out.Errors, out.Interruptions, "expected", f.Expected)
				}
				snapshot := out.Files[0].InputSnapshot
				if snapshot == nil || snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(f.Source))) || snapshot.SizeBytes != f.SizeBytes || snapshot.SHA256 != f.SHA256 {
					t.Fatal(out.Files)
				}
				uncertain := false
				for _, coverage := range out.Coverage {
					uncertain = uncertain || coverage.Reason == result.ParserUncertain
				}
				if uncertain != f.Incomplete || (!f.Incomplete && len(out.Coverage) != 0) {
					t.Fatal(out.Coverage)
				}
				for i, w := range f.Expected {
					d := out.Diagnostics[i]
					got := []int{d.ByteRange.Start, d.ByteRange.End, d.Range.Start.Line, d.Range.Start.Column, d.Range.End.Line, d.Range.End.Column}
					if !reflect.DeepEqual(got, w.Range) || f.Source[d.ByteRange.Start:d.ByteRange.End] != w.Source || d.Rule != "MD089" || d.FixAvailable || d.Message != w.Message {
						t.Fatal(got, d, w)
					}
				}
			})
		}
	}
}

func TestMD089CatalogStrictSettingsAndDefaults(t *testing.T) {
	r := registry(t, Registrations())
	enabled := 0
	for _, m := range r.Metadata() {
		if m.DefaultEnabled {
			enabled++
		}
		if m.ID == "MD089" && (m.DefaultEnabled || !m.RequiresSyntax || m.UsesFrontmatter || m.Fix != engine.PlannedFix || len(m.Options) != 0) {
			t.Fatal(m)
		}
	}
	if len(r.Metadata()) != 87 || enabled != 59 || !r.Available("MD089") {
		t.Fatal("catalog")
	}
	off := false
	for _, value := range []any{0, -1, 1.5, true, "2", nil} {
		if _, e := r.ResolveSettings(engine.Settings{"MD089": {Enabled: &off, Options: engine.Options{"unknown": value}}}, engine.ValidationProviders{}); e == nil {
			t.Fatal(value)
		}
	}
	if _, e := r.ResolveSettings(engine.Settings{"MD089": {Enabled: &off, Options: engine.Options{"unknown": true}}}, engine.ValidationProviders{}); e == nil {
		t.Fatal("unknown")
	}
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, explicit := range []bool{false, true} {
			s := md089Settings()
			if explicit {
				s["MD089"] = engine.RuleSetting{}
			} else {
				delete(s, "MD089")
			}
			out := analyzeHeadings(t, r, "中A", s, engine.DocumentContext{Flavor: flavor}, engine.RunOptions{})
			if !out.Complete || len(out.Diagnostics) != 0 {
				t.Fatal(out)
			}
		}
	}
}
