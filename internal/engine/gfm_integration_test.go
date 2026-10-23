package engine

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// The callback below is a consumer fixture, not a production rule implementation.
func TestGFMEngineConsumers(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n"} {
		t.Run(strings.ReplaceAll(ending, "\r", "CR"), func(t *testing.T) {
			text := strings.ReplaceAll("> - [X]\té ~~ok~~ www.a.b\n\n| a | b |\n| :- | -: |\n| one |\n| two | `code` | <!-- mdchk:disable MD001 --> |\n\nactive\n<!-- mdchk:enable MD001 -->\nvisible\n\n<textarea>\n<!-- mdchk:disable MD001 -->\n</textarea>\n\n`<!-- mdchk:disable MD001 -->`\nend", "\n", ending)
			calls := 0
			r, err := NewRegistry([]Metadata{{ID: "MD001", Name: "GFM source consumer fixture", DefaultEnabled: true, RequiresSyntax: true, Fix: NoFix}}, []Registration{{ID: "MD001", Check: func(_ context.Context, c RuleContext) RuleOutcome {
				calls++
				if c.Document() == nil {
					t.Fatal("missing genuine document")
				}
				counts := map[parser.Kind]int{}
				bare, filtered, tasks := 0, 0, 0
				parser.Walk(c.Document().Root(), func(n *parser.Node) bool {
					counts[n.Kind()]++
					p := n.Properties()
					if p.Task != nil {
						tasks++
					}
					if p.Link != nil && p.Link.Origin == "bare-gfm" {
						bare++
					}
					if p.HTML != nil {
						filtered += len(p.HTML.FilteredTags)
					}
					return true
				})
				if c.Context().Flavor == "gfm" && (counts[parser.Table] != 1 || counts[parser.Strike] != 1 || bare != 1 || filtered != 2 || tasks != 2) {
					t.Fatalf("actual GFM facts: %v bare=%d filtered=%d tasks=%d", counts, bare, filtered, tasks)
				}
				if c.Context().Flavor == "commonmark" && (counts[parser.Table] != 0 || counts[parser.Strike] != 0 || bare != 0 || filtered != 0 || tasks != 0) {
					t.Fatal("GFM leaked into core")
				}
				out := RuleOutcome{}
				for _, token := range []string{"[X]", "~~", "www.a.b", "é", "active", "visible", "end"} {
					at := strings.Index(text, token)
					if token == "end" {
						at = strings.LastIndex(text, token)
					}
					span, _ := c.Source().Span(at, at+len(token))
					out.Findings = append(out.Findings, Finding{Span: span, Message: token})
				}
				return out
			}}})
			if err != nil {
				t.Fatal(err)
			}
			sel, err := r.SelectRules(nil, ValidationProviders{})
			if err != nil {
				t.Fatal(err)
			}
			for _, flavor := range []string{"gfm", "commonmark"} {
				p, err := sel.Plan(DocumentContext{Flavor: flavor}, nil)
				if err != nil {
					t.Fatal(err)
				}
				source := parser.NewSource([]byte(text))
				in, err := p.NewSourceInput("x.md", source)
				if err != nil {
					t.Fatal(err)
				}
				budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
				out, trace, err := p.Inspect(context.Background(), in, RunOptions{Budget: budget}, InspectionOptions{})
				if err != nil {
					t.Fatal(err)
				}
				parsed := trace.Parsed()
				if !out.Complete || parsed.Status != parser.Complete || parsed.AST != parser.Available || parsed.Phases.Blocks != parser.PhaseComplete || parsed.Phases.Inlines != parser.PhaseComplete || parsed.Document.References().Availability != parser.Available || len(parsed.Uncertainty) != 0 || parsed.Source != source {
					t.Fatalf("incomplete authority: %+v", out)
				}
				for _, consume := range []func(context.Context, *parser.OperationBudget) error{
					func(ctx context.Context, b *parser.OperationBudget) error {
						_, e := parsed.Document.Comments(ctx, b)
						return e
					},
					func(ctx context.Context, b *parser.OperationBudget) error {
						_, e := parsed.Document.LiteralSpans(ctx, b)
						return e
					},
					func(ctx context.Context, b *parser.OperationBudget) error {
						_, e := parsed.Document.ProtectedNodes(ctx, b)
						return e
					},
				} {
					if err := consume(context.Background(), budget); err != nil {
						t.Fatal(err)
					}
					foreign, _ := parser.NewOperationBudget(parser.DefaultLimits())
					if err := consume(context.Background(), foreign); err == nil {
						t.Fatal("foreign operation granted authority")
					}
				}
				if len(out.Diagnostics) != 6 || len(out.SuppressedDiagnostics) != 1 || !trace.Directives().Complete() || len(trace.Directives().Candidates()) != 2 {
					t.Fatalf("diagnostics/directives: %+v candidates=%+v", out, trace.Directives().Candidates())
				}
				var hidden result.SuppressedDiagnostic
				if err := json.Unmarshal(out.SuppressedDiagnostics[0], &hidden); err != nil {
					t.Fatal(err)
				}
				if hidden.Diagnostic.Message != "active" || hidden.Diagnostic.Rule != "MD001" || hidden.Diagnostic.Range != (result.Range{Start: parser.Position{Line: 8, Column: 1}, End: parser.Position{Line: 8, Column: 7}}) || hidden.SuppressedBy.Kind != "directive" || hidden.SuppressedBy.DirectiveID != "" {
					t.Fatal("wrong suppression owner", hidden)
				}
				owner, err := trace.Directives().StateAt("MD001", strings.Index(text, "active"))
				if err != nil || owner.Enabled || owner.Owner != 0 {
					t.Fatal("wrong shared directive interval owner", owner, err)
				}
				candidates := trace.Directives().Candidates()
				if candidates[0].Raw != "<!-- mdchk:disable MD001 -->" || candidates[1].Raw != "<!-- mdchk:enable MD001 -->" {
					t.Fatal("opaque spelling granted directive authority", candidates)
				}
				// Public scalar columns after a quote, task marker, tab and UTF-8 text.
				expected := map[string]result.Range{"[X]": {Start: parser.Position{Line: 1, Column: 5}, End: parser.Position{Line: 1, Column: 8}}, "é": {Start: parser.Position{Line: 1, Column: 9}, End: parser.Position{Line: 1, Column: 10}}, "~~": {Start: parser.Position{Line: 1, Column: 11}, End: parser.Position{Line: 1, Column: 13}}, "www.a.b": {Start: parser.Position{Line: 1, Column: 18}, End: parser.Position{Line: 1, Column: 25}}, "visible": {Start: parser.Position{Line: 10, Column: 1}, End: parser.Position{Line: 10, Column: 8}}, "end": {Start: parser.Position{Line: 17, Column: 1}, End: parser.Position{Line: 17, Column: 4}}}
				for _, d := range out.Diagnostics {
					want, ok := expected[d.Message]
					if !ok || d.Range != want || d.Rule != "MD001" || d.Snapshot.SizeBytes != len(text) || d.FixAvailable {
						t.Fatalf("diagnostic %+v want %+v", d, want)
					}
					if text[d.ByteRange.Start:d.ByteRange.End] != d.Message {
						t.Fatal("diagnostic source changed")
					}
				}
				lint, err := p.Lint(context.Background(), []Input{in}, RunOptions{})
				if err != nil || !reflect.DeepEqual(out.Diagnostics, lint.Diagnostics) || !reflect.DeepEqual(out.SuppressedDiagnostics, lint.SuppressedDiagnostics) {
					t.Fatal("lint/inspect mismatch", err)
				}
				if string(source.Bytes()) != text {
					t.Fatal("lint changed source")
				}
			}
			if calls != 4 {
				t.Fatal("callbacks", calls)
			}
		})
	}
}

func TestGFMEngineIncompleteAuthority(t *testing.T) {
	calls := 0
	r, err := NewRegistry([]Metadata{{ID: "MD001", Name: "fixture", DefaultEnabled: true, RequiresSyntax: true, Fix: NoFix}}, []Registration{{ID: "MD001", Check: func(context.Context, RuleContext) RuleOutcome { calls++; return RuleOutcome{} }}})
	if err != nil {
		t.Fatal(err)
	}
	sel, _ := r.SelectRules(nil, ValidationProviders{})
	p, _ := sel.Plan(DocumentContext{Flavor: "gfm"}, nil)
	for _, test := range []string{"work", "depth", "cancel", "deadline", "utf8"} {
		t.Run(test, func(t *testing.T) {
			text := "| ~~a~~ |\n| - |\n"
			ctx := context.Background()
			limits := parser.DefaultLimits()
			switch test {
			case "work":
				limits.WorkUnits = 100
			case "depth":
				limits.SyntaxDepth = 2
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "deadline":
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, time.Unix(1, 0))
				defer cancel()
			case "utf8":
				text += "\xff"
			}
			b, _ := parser.NewOperationBudget(limits)
			in, _ := p.NewInput("x.md", []byte(text))
			out, trace, err := p.Inspect(ctx, in, RunOptions{Budget: b}, InspectionOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if out.Complete || calls != 0 || len(out.Diagnostics) != 0 {
				t.Fatal("incomplete parse authorized callback", out)
			}
			if test == "utf8" {
				if len(out.Errors) != 1 || out.Errors[0].Code != "invalid-utf8" {
					t.Fatal(out)
				}
			} else if len(out.Interruptions) != 1 {
				t.Fatal(out)
			}
			if test == "depth" {
				limit := trace.Parsed().Limit
				if limit == nil || limit.Name != "syntax-depth" || limit.Allowed != 2 || limit.Observed != 3 {
					t.Fatal(limit)
				}
			}
		})
	}
}
