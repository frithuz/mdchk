package inspect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// metadataOptions is the Acquire configuration shared by the fixtures below.
func metadataOptions(t *testing.T, text, flavor, recognition, md072 string) Options {
	t.Helper()
	root, options := fixture(t)
	options.Capabilities.GFM = true
	options.Capabilities.FrontmatterYAML = true
	options.Capabilities.FrontmatterTOML = true
	r, err := engine.NewCatalogRegistry([]engine.Registration{{ID: "MD072", Check: func(context.Context, engine.RuleContext) engine.RuleOutcome { return engine.RuleOutcome{} }}})
	if err != nil {
		t.Fatal(err)
	}
	options.Registry = r
	var definition strings.Builder
	fmt.Fprintf(&definition, "version: 1\nflavor: %s\nfrontmatter: %s\nrules:\n", flavor, recognition)
	for _, metadata := range engine.Catalog() {
		if metadata.ID != "MD072" {
			fmt.Fprintf(&definition, "  %s: {enabled: false}\n", metadata.ID)
		}
	}
	fmt.Fprintf(&definition, "  MD072: %s\n", md072)
	write(t, filepath.Join(root, config.Filename), []byte(definition.String()))
	write(t, filepath.Join(root, "x.md"), []byte(text))
	return options
}

const metadataRuleOff = "{enabled: false}"

func metadataRecord(t *testing.T, flavor, text, md072 string) (*Snapshot, Analysis, MetadataRecord) {
	t.Helper()
	s := acquire(t, "x.md", metadataOptions(t, text, flavor, "{enabled: true}", md072))
	a := analyze(t, s, false)
	if a.Projection.Metadata.Availability != "available" || a.Projection.Metadata.Data == nil {
		t.Fatalf("metadata section %+v", a.Projection.Metadata)
	}
	validateProduced(t, a)
	return s, a, *a.Projection.Metadata.Data
}

func metadataFlavors(t *testing.T, run func(*testing.T, string)) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) { run(t, flavor) })
	}
}

func TestMetadataStates(t *testing.T) {
	metadataFlavors(t, func(t *testing.T, flavor string) {
		t.Run("disabled", func(t *testing.T) {
			s := acquire(t, "x.md", metadataOptions(t, "---\nz: 1\n---\n", flavor, "{enabled: false}", metadataRuleOff))
			a := analyze(t, s, false)
			validateProduced(t, a)
			if !reflect.DeepEqual(*a.Projection.Metadata.Data, MetadataRecord{State: "disabled"}) {
				t.Fatalf("%+v", a.Projection.Metadata.Data)
			}
		})
		for _, tc := range []struct{ name, text string }{{"absent", "# Body\n"}, {"unselected", "+++\nz = 1\n+++\n"}} {
			t.Run(tc.name, func(t *testing.T) {
				recognition := "{enabled: true}"
				if tc.name == "unselected" {
					recognition = "{enabled: true, formats: [yaml]}"
				}
				s := acquire(t, "x.md", metadataOptions(t, tc.text, flavor, recognition, metadataRuleOff))
				a := analyze(t, s, false)
				validateProduced(t, a)
				if !reflect.DeepEqual(*a.Projection.Metadata.Data, MetadataRecord{State: "absent"}) {
					t.Fatalf("%+v", a.Projection.Metadata.Data)
				}
				raw, _ := json.Marshal(a.Projection.Metadata.Data)
				if string(raw) != `{"state":"absent"}` {
					t.Fatal(string(raw))
				}
			})
		}
		t.Run("unterminated", func(t *testing.T) {
			text := "---\nz: 1\na: 2\n"
			_, a, m := metadataRecord(t, flavor, text, metadataRuleOff)
			if m.State != "unterminated" || m.Format != "yaml" || *m.ByteRange != (result.ByteRange{Start: 0, End: len(text)}) || *m.ContextOnly || m.Problems != nil || m.Title != nil || m.Ordering != nil {
				t.Fatalf("%+v", m)
			}
			notes := *a.Projection.ParsingNotes.Data
			if len(notes) != 1 || notes[0].Code != "unterminated-frontmatter" || *notes[0].ByteRange != *m.ByteRange {
				t.Fatal(notes)
			}
		})
		t.Run("empty-header", func(t *testing.T) {
			_, _, m := metadataRecord(t, flavor, "---\n---\n", metadataRuleOff)
			if m.State != "complete" || len(*m.Problems) != 0 || m.Title.Status != "missing" {
				t.Fatalf("%+v", m)
			}
			raw, _ := json.Marshal(m)
			if !strings.Contains(string(raw), `"problems":[]`) {
				t.Fatal(string(raw))
			}
		})
	})
}

func TestMetadataCoordinatesAcrossFormatsAndEndings(t *testing.T) {
	metadataFlavors(t, func(t *testing.T, flavor string) {
		for _, tc := range []struct {
			name, text, format  string
			start, line, column int
			end                 int
		}{
			{"yaml-lf", "---\nz: 1\na: 2\n---\n# Body\n", "yaml", 9, 3, 1, 18},
			{"yaml-crlf", "---\r\nz: 1\r\na: 2\r\n---\r\n# Body\r\n", "yaml", 11, 3, 1, 22},
			{"yaml-bom", "\ufeff---\nz: 1\na: 2\n---\n# Body\n", "yaml", 12, 3, 1, 21},
			{"toml-lf", "+++\nz = 1\na = 2\n+++\n", "toml", 10, 3, 1, 20},
			{"toml-crlf", "+++\r\nz = 1\r\na = 2\r\n+++\r\n", "toml", 12, 3, 1, 24},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, a, m := metadataRecord(t, flavor, tc.text, metadataRuleOff)
				if m.State != "complete" || m.Format != tc.format || m.ByteRange.End != tc.end {
					t.Fatalf("%+v", m)
				}
				if header := a.Projection.AST.Data.Frontmatter; header.Format != m.Format || header.ByteRange != *m.ByteRange || header.Range != *m.Range {
					t.Fatalf("differs from opaque record: %+v %+v", header, m)
				}
				if m.Ordering.Kind != "observation" || m.Ordering.Recursive || !m.Ordering.CaseSensitive || len(m.Ordering.Mappings) != 1 {
					t.Fatalf("%+v", m.Ordering)
				}
				mapping := m.Ordering.Mappings[0]
				if !mapping.Violation || len(mapping.OffendingKeys) != 1 {
					t.Fatalf("%+v", mapping)
				}
				key := mapping.OffendingKeys[0]
				if key.Key != "a" || *key.ByteRange != (result.ByteRange{Start: tc.start, End: tc.start + 1}) || key.Range.Start.Line != tc.line || key.Range.Start.Column != tc.column {
					t.Fatalf("%+v", key)
				}
				if len(a.Result.Diagnostics) != 0 || !a.Result.Complete || rule(t, a, "MD072").EffectiveState == "active" {
					t.Fatalf("observation changed outcome: %+v", a.Result)
				}
			})
		}
	})
}

func TestMetadataProblemsAndTitles(t *testing.T) {
	merge := "---\nbase: &b {y: 1, x: 2}\nz: 1\nm:\n  <<: *b\na: 2\n---\n"
	metadataFlavors(t, func(t *testing.T, flavor string) {
		for _, tc := range []struct {
			name, text, code string
			start, end       int
		}{
			{"custom-tag", "---\nz: !foo 1\na: 2\n---\n", "unsupported", 7, 11},
			{"duplicate-key", "---\nz: 1\nz: 2\na: 3\n---\n", "duplicate-key", 9, 10},
			{"malformed", "---\na: [\n---\n", "malformed", 4, 9},
			{"merge-key-collapses", merge, "unsupported", 36, 38},
			{"cycle", "---\na: &a\n  b: *a\n---\n", "unsupported", 15, 17},
			{"non-mapping-root", "---\n- a\n---\n", "unsupported", 4, 7},
			{"toml-malformed", "+++\na = [\n+++\n", "malformed", 9, 10},
			{"toml-duplicate-key", "+++\nz = 1\nz = 2\n+++\n", "duplicate-key", 10, 11},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, a, m := metadataRecord(t, flavor, tc.text, metadataRuleOff)
				if m.State != "incomplete" || len(*m.Problems) != 1 {
					t.Fatalf("%+v %+v", m, m.Problems)
				}
				p := (*m.Problems)[0]
				if p.Code != tc.code || p.ByteRange != (result.ByteRange{Start: tc.start, End: tc.end}) {
					t.Fatalf("%+v", p)
				}
				if !a.Result.Complete {
					t.Fatal("an incomplete record alone made the envelope incomplete")
				}
			})
		}
		for _, tc := range []struct{ name, text, status, want, state string }{
			{"found-beside-problem", "---\ntitle: ok\nz: !foo 1\n---\n", "found", "ok", "incomplete"},
			{"found-untrimmed", "---\ntitle: \"  ok \"\n---\n", "found", "  ok ", "complete"},
			{"toml-found", "+++\ntitle = \"ok\"\n+++\n", "found", "ok", "complete"},
			{"non-string", "---\ntitle: 1\n---\n", "missing", "", "complete"},
			{"blank", "---\ntitle: \" \"\n---\n", "missing", "", "complete"},
			{"duplicate", "---\ntitle: a\ntitle: b\n---\n", "unknown", "", "incomplete"},
			{"undecodable", "---\na: [\n---\n", "unknown", "", "incomplete"},
		} {
			t.Run("title-"+tc.name, func(t *testing.T) {
				_, _, m := metadataRecord(t, flavor, tc.text, metadataRuleOff)
				if m.State != tc.state || m.Title.Status != tc.status || m.Title.Text != tc.want || (m.Title.ByteRange != nil) != (tc.status == "found") || (m.Title.Range != nil) != (tc.status == "found") {
					t.Fatalf("%+v %+v", m, m.Title)
				}
			})
		}
	})
}

func TestMetadataTitleLocation(t *testing.T) {
	_, _, m := metadataRecord(t, "commonmark", "---\ntitle: ok\n---\n", metadataRuleOff)
	if *m.Title.ByteRange != (result.ByteRange{Start: 11, End: 13}) || m.Title.Range.Start.Line != 2 || m.Title.Range.Start.Column != 8 {
		t.Fatalf("%+v", m.Title)
	}
}

func TestMetadataOrderingOptions(t *testing.T) {
	nested := "---\na:\n  z: 1\n  b: 2\nb:\n  - z: 1\n    b: 2\nc: [z, b]\n---\n"
	violations := func(m MetadataRecord) (out []string) {
		for _, mapping := range m.Ordering.Mappings {
			if mapping.Violation {
				for _, key := range mapping.OffendingKeys {
					out = append(out, key.Key)
				}
			}
		}
		return out
	}
	metadataFlavors(t, func(t *testing.T, flavor string) {
		for _, enabled := range []string{"true", "false"} {
			t.Run("enabled-"+enabled, func(t *testing.T) {
				rule := func(options string) string { return "{enabled: " + enabled + options + "}" }
				_, a, m := metadataRecord(t, flavor, nested, rule(", recursive: true"))
				if !m.Ordering.Recursive || !reflect.DeepEqual(violations(m), []string{"b", "b"}) || len(m.Ordering.Mappings) != 3 {
					t.Fatalf("%+v", m.Ordering)
				}
				if m.Ordering.Mappings[0].Violation {
					t.Fatalf("root %+v", m.Ordering.Mappings[0])
				}
				if len(a.Result.Diagnostics) != 0 {
					t.Fatal("the stub rule produced no diagnostics, the record must not either", a.Result.Diagnostics)
				}
				_, _, m = metadataRecord(t, flavor, nested, rule(""))
				if m.Ordering.Recursive || len(m.Ordering.Mappings) != 1 || m.Ordering.Mappings[0].Violation {
					t.Fatalf("%+v", m.Ordering)
				}
				_, _, m = metadataRecord(t, flavor, "---\na: 1\nA: 2\n---\n", rule(""))
				if !m.Ordering.CaseSensitive || !reflect.DeepEqual(violations(m), []string{"A"}) {
					t.Fatalf("%+v", m.Ordering)
				}
				_, _, m = metadataRecord(t, flavor, "---\na: 1\nA: 2\n---\n", rule(", case_sensitive: false"))
				if m.Ordering.CaseSensitive || m.Ordering.Mappings[0].Violation {
					t.Fatalf("%+v", m.Ordering)
				}
				_, _, m = metadataRecord(t, flavor, "---\nB: 1\na: 2\n---\n", rule(", case_sensitive: false"))
				if !reflect.DeepEqual(violations(m), []string{"a"}) {
					t.Fatalf("%+v", m.Ordering)
				}
			})
		}
	})
}

func TestMetadataAbsentSpansAndAliases(t *testing.T) {
	metadataFlavors(t, func(t *testing.T, flavor string) {
		t.Run("synthesized-toml-parent", func(t *testing.T) {
			_, _, m := metadataRecord(t, flavor, "+++\n[z.x]\nk = 1\n[a]\nk = 2\n+++\n", "{enabled: false, recursive: true}")
			if m.State != "complete" {
				t.Fatalf("%+v", m)
			}
			absent, keyless := false, false
			for _, mapping := range m.Ordering.Mappings {
				if (mapping.Range == nil) != (mapping.ByteRange == nil) {
					t.Fatalf("%+v", mapping)
				}
				absent = absent || mapping.ByteRange == nil
				for _, key := range mapping.OffendingKeys {
					if (key.Range == nil) != (key.ByteRange == nil) {
						t.Fatalf("%+v", key)
					}
					keyless = keyless || key.ByteRange == nil
				}
			}
			if !absent || !keyless {
				t.Fatalf("expected an absent mapping and key span: %+v", m.Ordering)
			}
			raw, _ := json.Marshal(m.Ordering)
			if strings.Contains(string(raw), "null") {
				t.Fatal(string(raw))
			}
		})
		t.Run("alias-mapping-lists-no-key", func(t *testing.T) {
			_, _, m := metadataRecord(t, flavor, "---\na: &x\n  z: 1\n  b: 2\nc: *x\n---\n", "{enabled: false, recursive: true}")
			alias := false
			for _, mapping := range m.Ordering.Mappings {
				alias = alias || mapping.Violation && len(mapping.OffendingKeys) == 0
			}
			if !alias {
				t.Fatalf("%+v", m.Ordering)
			}
		})
	})
}

func TestMetadataSelectionKeepsWholeRecord(t *testing.T) {
	text := "---\ntitle: ok\nz: 1\na: 2\n---\n# Body\n"
	metadataFlavors(t, func(t *testing.T, flavor string) {
		s, full, m := metadataRecord(t, flavor, text, metadataRuleOff)
		for _, tc := range []struct {
			lines       string
			contextOnly bool
		}{{"6", true}, {"2", false}} {
			view, err := Filter(context.Background(), s, full, tc.lines)
			if err != nil {
				t.Fatal(err)
			}
			validateProduced(t, view)
			got := *view.Projection.Metadata.Data
			if *got.ContextOnly != tc.contextOnly || !got.ExtendsRegion {
				t.Fatalf("lines %s: %+v", tc.lines, got)
			}
			if header := view.Projection.AST.Data.Frontmatter; header.ContextOnly != *got.ContextOnly || header.ExtendsRegion != got.ExtendsRegion {
				t.Fatalf("differs from opaque record: %+v %+v", header, got)
			}
			got.ContextOnly, got.ExtendsRegion = m.ContextOnly, m.ExtendsRegion
			if !reflect.DeepEqual(got, m) {
				t.Fatalf("lines %s changed the record: %+v", tc.lines, got)
			}
		}
		whole, err := Filter(context.Background(), s, full, "")
		if err != nil || !reflect.DeepEqual(*whole.Projection.Metadata.Data, m) {
			t.Fatalf("whole-file filter changed the record: %v", err)
		}
	})
}

// The record's selection flags cannot depend on the AST section being present.
func TestMetadataSelectionWithoutAST(t *testing.T) {
	s, full, _ := metadataRecord(t, "commonmark", "---\nz: 1\n---\n# Body\n", metadataRuleOff)
	var wire map[string]map[string]json.RawMessage
	if err := json.Unmarshal(full.wire, &wire); err != nil {
		t.Fatal(err)
	}
	wire["Projection"]["ast"] = json.RawMessage(`{"availability":"unavailable","reasons":["context-unavailable"]}`)
	withoutAST, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	full.wire = withoutAST
	view, err := Filter(context.Background(), s, full, "4")
	if err != nil {
		t.Fatal(err)
	}
	if m := view.Projection.Metadata.Data; view.Projection.AST.Data != nil || m == nil || !*m.ContextOnly || !m.ExtendsRegion {
		t.Fatalf("%+v", m)
	}
}

func TestMetadataUnavailableWithoutDocument(t *testing.T) {
	for _, tc := range []struct{ definition, reason string }{{"version: [", "configuration-invalid"}, {"version: 1\nflavor: gfm\n", "capability-unavailable"}} {
		root, o := fixture(t)
		write(t, filepath.Join(root, "x.md"), []byte("---\nz: 1\n---\n"))
		write(t, filepath.Join(root, config.Filename), []byte(tc.definition))
		a := analyze(t, acquire(t, "x.md", o), false)
		section := a.Projection.Metadata
		if section.Availability != "unavailable" || section.Data != nil || !reflect.DeepEqual(section.Reasons, a.Projection.AST.Reasons) || section.Reasons[0] != tc.reason {
			t.Fatalf("%q: %+v ast %+v", tc.definition, section, a.Projection.AST.Reasons)
		}
		validateProduced(t, a)
	}
	root, o := fixture(t)
	write(t, filepath.Join(root, "x.md"), []byte("---\nz: \xff\n---\n"))
	write(t, filepath.Join(root, config.Filename), []byte("version: 1\nflavor: commonmark\n"))
	a := analyze(t, acquire(t, "x.md", o), false)
	if section := a.Projection.Metadata; section.Availability != "unavailable" || section.Reasons[0] != "decoding-failed" {
		t.Fatalf("%+v", section)
	}
	validateProduced(t, a)
}

// metadataBoundary finds the smallest allowance that publishes the record and
// returns the analysis one unit below it, where the stop lands inside the record.
func metadataBoundary(t *testing.T, low, high int64, analyzeAt func(int64) (Analysis, error)) (Analysis, error) {
	t.Helper()
	for low+1 < high {
		middle := low + (high-low)/2
		if a, _ := analyzeAt(middle); a.Projection.Metadata.Availability == "available" {
			high = middle
		} else {
			low = middle
		}
	}
	return analyzeAt(low)
}

func TestMetadataBudgetStopKeepsCompletedSections(t *testing.T) {
	text := "---\ntitle: ok\nz: 1\na: 2\n---\n# Body\n"
	metadataFlavors(t, func(t *testing.T, flavor string) {
		for _, name := range []string{"work-units", "total-nodes"} {
			t.Run(name, func(t *testing.T) {
				options := metadataOptions(t, text, flavor, "{enabled: true}", metadataRuleOff)
				defaults := parser.DefaultLimits()
				high := defaults.WorkUnits
				if name == "total-nodes" {
					high = defaults.TotalNodes
				}
				a, err := metadataBoundary(t, 0, high, func(limit int64) (Analysis, error) {
					limits := parser.DefaultLimits()
					if name == "work-units" {
						limits.WorkUnits = limit
					} else {
						limits.TotalNodes = limit
						limits.DocumentNodes = min(limits.DocumentNodes, limit)
					}
					budget, err := parser.NewOperationBudget(limits)
					if err != nil {
						return Analysis{}, nil
					}
					options.Budget = budget
					s, err := Acquire(context.Background(), "x.md", options)
					if err != nil {
						return Analysis{}, nil
					}
					return Analyze(context.Background(), s, AnalyzeOptions{})
				})
				var limit *parser.LimitExceeded
				if !errors.As(err, &limit) || limit.Name != name {
					t.Fatalf("%v", err)
				}
				p := a.Projection
				if p.Metadata.Availability != "unavailable" || p.Metadata.Data != nil || len(p.Metadata.CauseIDs) == 0 || p.Metadata.Reasons[0] != "limit-exceeded" {
					t.Fatalf("%+v", p.Metadata)
				}
				if p.AST.Availability != "available" || p.Rules.Availability != "available" || p.Directives.Availability != "available" || a.Result.Status != "incomplete" {
					t.Fatalf("a completed section was disturbed: %+v %+v", p.AST.Availability, a.Result.Status)
				}
				validateProduced(t, a)
			})
		}
	})
}

// cancelAfter reports cancellation once Err has been consulted a fixed number
// of times, so the search below can land the stop on one builder checkpoint.
type cancelAfter struct {
	context.Context
	remaining int64
}

func (c *cancelAfter) Err() error {
	if c.remaining <= 0 {
		return context.Canceled
	}
	c.remaining--
	return nil
}

func TestMetadataCancellationKeepsCompletedSections(t *testing.T) {
	options := metadataOptions(t, "---\ntitle: ok\nz: 1\na: 2\n---\n# Body\n", "commonmark", "{enabled: true}", metadataRuleOff)
	a, err := metadataBoundary(t, 0, 1<<20, func(limit int64) (Analysis, error) {
		s, err := Acquire(context.Background(), "x.md", options)
		if err != nil {
			t.Fatal(err)
		}
		return Analyze(&cancelAfter{context.Background(), limit}, s, AnalyzeOptions{})
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	p := a.Projection
	if p.Metadata.Availability != "unavailable" || p.Metadata.Reasons[0] != "cancelled" || len(p.Metadata.CauseIDs) == 0 || p.AST.Availability != "available" || p.Rules.Availability != "available" {
		t.Fatalf("%+v %s %s", p.Metadata, p.AST.Availability, p.Rules.Availability)
	}
	validateProduced(t, a)
}

func TestMetadataEarlierInterruptionLeavesRecordUnavailable(t *testing.T) {
	s, _ := projectionFixture(t, "source\n", "", nil)
	_ = s.budget.ChargeWork(s.budget.Limits().WorkUnits)
	a, err := Analyze(context.Background(), s, AnalyzeOptions{})
	if err == nil || a.Projection.Metadata.Availability != "unavailable" || len(a.Projection.Metadata.CauseIDs) == 0 {
		t.Fatalf("%v %+v", err, a.Projection.Metadata)
	}
	validateProduced(t, a)
}

// metadataTightDocument parses text under the smallest work allowance that
// admits the parse plus spare units, leaving exactly spare for the builder.
func metadataTightDocument(t *testing.T, text string, spare int64) (*parser.Document, *parser.OperationBudget) {
	t.Helper()
	options, err := parser.NewFrontmatterOptions(true, nil)
	if err != nil {
		t.Fatal(err)
	}
	parse := func(work int64) (parser.ParseResult, *parser.OperationBudget) {
		limits := parser.DefaultLimits()
		limits.WorkUnits = work
		budget, err := parser.NewOperationBudget(limits)
		if err != nil {
			t.Fatal(err)
		}
		return parser.ParseWithOptions(context.Background(), parser.NewSource([]byte(text)), budget, parser.ParseOptions{Frontmatter: options}), budget
	}
	low, high := int64(0), parser.DefaultLimits().WorkUnits
	for low+1 < high {
		middle := low + (high-low)/2
		if parsed, _ := parse(middle); parsed.Limit == nil && parsed.Status == parser.Complete {
			high = middle
		} else {
			low = middle
		}
	}
	parsed, budget := parse(high + spare)
	if parsed.Document == nil {
		t.Fatal("no document")
	}
	return parsed.Document, budget
}

func TestMetadataDisabledAndAbsentNeverDecode(t *testing.T) {
	rule := engine.ResolvedRule{Options: engine.Options{"recursive": false, "case_sensitive": true}}
	// Disabled recognition binds nothing: a nil document would fail Bind.
	limits := parser.DefaultLimits()
	limits.WorkUnits = 16
	budget, err := parser.NewOperationBudget(limits)
	if err != nil {
		t.Fatal(err)
	}
	b := builder{ctx: context.Background(), budget: budget}
	if m, err := b.metadata(nil, false, rule); err != nil || m.State != "disabled" {
		t.Fatal(m, err)
	}

	// 17 units pay the record's own 16 and Bind's 1. Decode charges more even for
	// an absent header, so succeeding here shows it was not called.
	document, budget := metadataTightDocument(t, "# Body\n", 17)
	b = builder{ctx: context.Background(), budget: budget}
	if m, err := b.metadata(document, true, rule); err != nil || m.State != "absent" {
		t.Fatal(m, err)
	}

	// The same allowance cannot pay for a header that does reach the decoder.
	text := "---\nz: 1\n---\n"
	document, budget = metadataTightDocument(t, text, 17)
	source := parser.NewSource([]byte(text))
	b = builder{ctx: context.Background(), budget: budget, source: source, index: parser.NewLineIndex(source)}
	var limit *parser.LimitExceeded
	if _, err := b.metadata(document, true, rule); !errors.As(err, &limit) {
		t.Fatal(err)
	}
}

// go-toml reports a parse position that can fall inside a multibyte scalar. The
// record must still build, at the scalar boundaries around that position.
func TestMetadataProblemInsideAScalarIsAligned(t *testing.T) {
	for _, text := range []string{"\ufeff+++\n中A = [\n+++\n中B\n", "+++\n中A = [\n+++\n", "+++\na = \"中中中\" x\n+++\n", "+++\n中中中 x\n+++\n"} {
		_, _, m := metadataRecord(t, "commonmark", text, metadataRuleOff)
		if m.State != "incomplete" || len(*m.Problems) != 1 {
			t.Fatalf("%q %+v", text, m)
		}
		span := (*m.Problems)[0].ByteRange
		if !utf8.ValidString(text[span.Start:span.End]) || span.Start < m.ByteRange.Start || span.End > m.ByteRange.End {
			t.Fatalf("%q %+v", text, span)
		}
	}
}
