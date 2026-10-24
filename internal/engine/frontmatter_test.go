package engine_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

func TestFrontmatterOrdinaryAndExplicitMetadataObservations(t *testing.T) {
	text := "\xef\xbb\xbf---\r\n<!-- mdchk:disable MD001 -->\r\nbad: [\r\n---\r\n雪body"
	body := strings.Index(text, "雪body")
	for _, active := range []bool{true, false} {
		t.Run(map[bool]string{true: "active", false: "observational"}[active], func(t *testing.T) {
			ordinary := metadata("MD001")
			ordinary.DefaultEnabled = active
			aware := metadata("MD003")
			aware.UsesFrontmatter = true
			reg := registry(t, []engine.Metadata{ordinary, aware}, engine.Registration{ID: "MD001", Check: func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
				if _, present := c.Frontmatter(); present {
					t.Fatal("ordinary callback received explicit metadata access")
				}
				inside, _ := c.Source().Span(8, 9)
				crossing, _ := c.Source().Span(8, body+3)
				visible, _ := c.Source().Span(body, body+3)
				return engine.RuleOutcome{Findings: []engine.Finding{{Message: "header", Span: inside}, {Message: "crossing", Span: crossing}, {Message: "body", Span: visible}}}
			}}, engine.Registration{ID: "MD003", Check: func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
				header, present := c.Frontmatter()
				if !present || header.Format() != "yaml" || header.BodyOffset() != body {
					t.Fatal("explicit opaque metadata unavailable", header, present)
				}
				return engine.RuleOutcome{Findings: []engine.Finding{{Message: "explicit metadata", Span: header.Content()}}}
			}})
			selected, err := reg.SelectRules(nil, engine.ValidationProviders{})
			if err != nil {
				t.Fatal(err)
			}
			opaque, err := parser.NewFrontmatterOptions(true, nil)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := selected.Plan(engine.DocumentContext{Flavor: "commonmark", Frontmatter: opaque}, nil)
			if err != nil {
				t.Fatal(err)
			}
			in, err := plan.NewInput("doc.md", []byte(text))
			if err != nil {
				t.Fatal(err)
			}
			out, trace, err := plan.Inspect(context.Background(), in, engine.RunOptions{}, engine.InspectionOptions{ObserveInactive: true})
			if err != nil || !out.Complete || len(out.Errors) != 0 || len(out.Coverage) != 0 || len(trace.Directives().Candidates()) != 0 {
				t.Fatal(err, out)
			}
			want := 1
			if active {
				want++
			}
			if len(out.Diagnostics) != want {
				t.Fatal("opaque findings escaped", out.Diagnostics)
			}
			for _, finding := range out.Diagnostics {
				if finding.Rule == "MD001" && (finding.Message != "body" || finding.ByteRange.Start != body || finding.Range.Start.Line != 5 || finding.Range.Start.Column != 1 || finding.Range.End.Column != 2) {
					t.Fatal(finding)
				}
			}
			if !active {
				observations := trace.Suppressed()
				if len(observations) != 1 || observations[0].Diagnostic.Message != "body" {
					t.Fatal(observations)
				}
			}
			for _, evaluation := range trace.Evaluations() {
				if evaluation.Rule != "MD001" {
					continue
				}
				coverage := evaluation.ActiveCoverage
				if !active {
					coverage = evaluation.ObservationalCoverage
				}
				if len(coverage) != 1 || coverage[0].Start() != body || coverage[0].End() != len(text) {
					t.Fatal("ordinary coverage included header", coverage)
				}
			}
		})
	}
}

func TestFrontmatterUnterminatedSharedAnalysis(t *testing.T) {
	calls := 0
	reg := registry(t, []engine.Metadata{metadata("MD001")}, engine.Registration{ID: "MD001", Check: func(context.Context, engine.RuleContext) engine.RuleOutcome { calls++; return engine.RuleOutcome{} }})
	options, err := parser.NewFrontmatterOptions(true, []string{"toml"})
	if err != nil {
		t.Fatal(err)
	}
	text := "+++\r\n<!-- mdchk:disable MD001 -->\r\n雪"
	in, err := engine.NewInput("doc.md", []byte(text), nil, engine.DocumentContext{Flavor: "gfm", Frontmatter: options})
	if err != nil {
		t.Fatal(err)
	}
	out := lint(t, reg, in)
	if calls != 0 || out.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != 0 || len(out.Coverage) == 0 || len(out.Interruptions) != 0 {
		t.Fatal("unterminated header lost uncertainty", calls, out)
	}
	found := false
	for _, raw := range out.Notes {
		var note result.Note
		if err := json.Unmarshal(raw, &note); err != nil {
			t.Fatal(err)
		}
		if note.Code == "unterminated-frontmatter" {
			found = note.ByteRange != nil && note.ByteRange.Start == 0 && note.ByteRange.End == len(text) && note.Range != nil && note.Range.Start.Line == 1 && note.Range.End.Line == 3
		}
	}
	if !found {
		t.Fatal("missing located uncertainty", out.Notes)
	}
}

func TestFrontmatterDisabledAndUnselectedRemainOrdinary(t *testing.T) {
	for _, formats := range [][]string{nil, {"toml"}} {
		enabled := formats != nil
		options, err := parser.NewFrontmatterOptions(enabled, formats)
		if err != nil {
			t.Fatal(err)
		}
		reg := registry(t, []engine.Metadata{metadata("MD001")}, engine.Registration{ID: "MD001", Check: finding(4, 5)})
		in, err := engine.NewInput("doc.md", []byte("---\nx: y\n---\n"), nil, engine.DocumentContext{Flavor: "commonmark", Frontmatter: options})
		if err != nil {
			t.Fatal(err)
		}
		out := lint(t, reg, in)
		if !out.Complete || len(out.Diagnostics) != 1 {
			t.Fatal("ordinary interpretation changed", out)
		}
	}
}
