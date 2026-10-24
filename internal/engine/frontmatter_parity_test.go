package engine_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/rules"
	frontmatterfixtures "github.com/frithuz/mdchk/internal/testutil/frontmatter"
)

// The same byte/settings ledger drives the filesystem CLI tests. These paths
// deliberately acquire no files: future project/standalone MCP adapters can
// reuse these expectations without pretending a transport has been implemented.
func TestFrontmatterOperationParityInMemory(t *testing.T) {
	rows, err := frontmatterfixtures.Load()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := engine.NewCatalogRegistry(rules.Registrations())
	if err != nil {
		t.Fatal(err)
	}
	settings := engine.Settings{}
	for _, metadata := range engine.Catalog() {
		enabled := metadata.ID == "MD047" || metadata.ID == "MD094"
		settings[metadata.ID] = engine.RuleSetting{Enabled: &enabled}
	}
	selected, err := registry.SelectRules(settings, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := fix.NewValidator(registry, rules.CorrectionCheckers())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		source := parser.NewSource([]byte(row.Source))
		for _, flavor := range []string{"commonmark", "gfm"} {
			for _, project := range []string{"", "fixture-project"} {
				t.Run(fmt.Sprintf("%s/%s/project=%t", row.ID, flavor, project != ""), func(t *testing.T) {
					opaque := parser.FrontmatterOptions{}
					if row.Frontmatter != nil {
						opaque, err = parser.NewFrontmatterOptions(row.Frontmatter.Enabled, row.Frontmatter.Formats)
						if err != nil {
							t.Fatal(err)
						}
					}
					plan, err := selected.Plan(engine.DocumentContext{Flavor: flavor, Project: project, Frontmatter: opaque}, nil)
					if err != nil {
						t.Fatal(err)
					}
					in, err := plan.NewSourceInput("doc.md", source)
					if err != nil {
						t.Fatal(err)
					}
					out, trace, err := plan.Inspect(context.Background(), in, engine.RunOptions{}, engine.InspectionOptions{})
					if err != nil {
						t.Fatal(err)
					}
					if out.Complete != row.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != row.Diagnostics {
						t.Fatalf("original result complete=%v diagnostics=%+v errors=%+v", out.Complete, out.Diagnostics, out.Errors)
					}
					assertLedgerHeader(t, source, trace.Parsed(), row)
					for _, diagnostic := range out.Diagnostics {
						if diagnostic.Rule != "MD047" || diagnostic.FixAvailable != row.Fix || diagnostic.ByteRange != (result.ByteRange{Start: source.Len(), End: source.Len()}) || diagnostic.Range.Start.Line != row.DiagnosticPosition[0] || diagnostic.Range.Start.Column != row.DiagnosticPosition[1] {
							t.Fatalf("original diagnostic %+v", diagnostic)
						}
					}
					if string(in.Source().Bytes()) != row.Source || trace.Parsed().Source != source {
						t.Fatal("in-memory analysis replaced its original snapshot")
					}
					if !row.Complete {
						located := false
						for _, raw := range out.Notes {
							var note result.Note
							if err := json.Unmarshal(raw, &note); err != nil {
								t.Fatal(err)
							}
							located = located || note.Code == "unterminated-frontmatter" && note.ByteRange != nil && *note.ByteRange == (result.ByteRange{Start: 0, End: source.Len()})
						}
						if !located || trace.Directives().Complete() {
							t.Fatal("unterminated metadata lost explicit incomplete authority")
						}
					}
					budget, err := parser.NewOperationBudget(parser.DefaultLimits())
					if err != nil {
						t.Fatal(err)
					}
					validated, err := validator.Validate(context.Background(), []fix.Target{{Path: "doc.md", Source: source, Plan: plan, Produce: rules.Corrections}}, fix.ValidationOptions{Budget: budget})
					if err != nil {
						t.Fatal(err)
					}
					view, ok := validated.File("doc.md")
					if !ok || validated.Complete() != row.Complete || string(view.Original.Bytes()) != row.Source || string(view.Candidate.Bytes()) != row.Candidate || string(source.Bytes()) != row.Source {
						t.Fatalf("candidate mismatch complete=%v file=%+v", validated.Complete(), view)
					}
					candidate, err := plan.NewSourceInput("doc.md", view.Candidate)
					if err != nil {
						t.Fatal(err)
					}
					saved, savedTrace, err := plan.InspectSaved(context.Background(), candidate, engine.RunOptions{})
					if err != nil {
						t.Fatal(err)
					}
					if saved.Complete != row.Complete || len(saved.Errors) != 0 {
						t.Fatal("saved in-memory analysis lost selection", saved)
					}
					assertLedgerHeader(t, view.Candidate, savedTrace.Parsed(), row)
					wantSaved := row.Diagnostics
					if row.Fix {
						wantSaved = 0
					}
					if len(saved.Diagnostics) != wantSaved {
						t.Fatal("saved findings lost their expected directive/fix state", saved.Diagnostics)
					}
					secondBudget, err := parser.NewOperationBudget(parser.DefaultLimits())
					if err != nil {
						t.Fatal(err)
					}
					second, err := validator.Validate(context.Background(), []fix.Target{{Path: "doc.md", Source: view.Candidate, Plan: plan, Produce: rules.Corrections}}, fix.ValidationOptions{Budget: secondBudget})
					if err != nil {
						t.Fatal(err)
					}
					again, ok := second.File("doc.md")
					if !ok || second.Complete() != row.Complete || string(again.Candidate.Bytes()) != row.Candidate || again.ChangingRounds != 0 {
						t.Fatal("in-memory candidate is not convergent", again)
					}
				})
			}
		}
	}
}

func assertLedgerHeader(t *testing.T, source parser.Source, parsed parser.ParseResult, row frontmatterfixtures.Case) {
	t.Helper()
	if parsed.Source != source || parsed.Document == nil || parsed.Document.Source() != source {
		t.Fatal("parse lost original source ownership")
	}
	if parsed.Document.Root().Span().Start() != 0 || parsed.Document.Root().Span().End() != source.Len() {
		t.Fatal("body parse lost the complete original root envelope")
	}
	if row.Complete && (parsed.Status != parser.Complete || parsed.AST != parser.Available || len(parsed.Uncertainty) != 0) {
		t.Fatal("complete fixture lost parser authority", parsed)
	}
	if !row.Complete && (parsed.Status != parser.Incomplete || parsed.AST != parser.Partial || len(parsed.Uncertainty) != 1 || parsed.Uncertainty[0].Reason != parser.UnterminatedFrontmatter) {
		t.Fatal("unterminated fixture gained complete authority", parsed)
	}
	header, present := parsed.Document.Frontmatter()
	if present != (row.Header != nil) {
		t.Fatalf("header presence %v want %v", present, row.Header != nil)
	}
	if !present {
		return
	}
	want := row.Header
	for _, child := range parsed.Document.Root().Children() {
		if child.Span().Start() < want.BodyOffset {
			t.Fatal("header entered body AST", child)
		}
	}
	for _, definition := range parsed.Document.References().Definitions {
		if definition.Properties().Envelope.Start() < want.BodyOffset {
			t.Fatal("header entered reference inventory", definition)
		}
	}
	if header.Format() != want.Format || header.BodyOffset() != want.BodyOffset {
		t.Fatal("header selection/boundary changed", header)
	}
	spans := []struct {
		got  parser.Span
		want *[2]int
	}{{header.Span(), &want.Region}, {header.Opener(), &want.Opener}, {header.Content(), &want.Content}, {header.Closer(), want.Closer}}
	for _, span := range spans {
		if span.want == nil {
			if span.got.Present() {
				t.Fatal("invented closer")
			}
			continue
		}
		if !span.got.Present() || span.got.Start() != span.want[0] || span.got.End() != span.want[1] {
			t.Fatalf("header span [%d,%d) want %v", span.got.Start(), span.got.End(), *span.want)
		}
		actual, err := source.Text(span.got)
		if err != nil || actual != row.Source[span.want[0]:span.want[1]] {
			t.Fatal("opaque original bytes changed", actual, err)
		}
	}
}
