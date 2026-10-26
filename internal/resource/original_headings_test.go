package resource

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	"github.com/frithuz/mdchk/internal/parser"
)

type originalAnchorCounter struct {
	context.Context
	calls, stop int
}

func (c *originalAnchorCounter) Err() error {
	c.calls++
	if c.stop > 0 && c.calls >= c.stop {
		return context.Canceled
	}
	return c.Context.Err()
}

func originalAnchorParse(t *testing.T, text string, options parser.ParseOptions) (parser.ParseResult, *parser.OperationBudget, parser.LineIndex) {
	t.Helper()
	source := parser.NewSource([]byte(text))
	budget, err := parser.NewOperationBudget(parser.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	parsed := parser.ParseWithOptions(context.Background(), source, budget, options)
	if parsed.Status != parser.Complete {
		t.Fatal(parsed)
	}
	return parsed, budget, parser.NewLineIndex(source)
}
func TestOriginalHeadingAnchorsAuthorityAndPolicy(t *testing.T) {
	options := parser.ParseOptions{}
	parsed, budget, index := originalAnchorParse(t, "# A\n# A", options)
	for _, style := range []string{"github", "explicit", "github"} {
		got, availability, err := OriginalHeadingAnchors(context.Background(), parsed, budget, index, options, style)
		if err != nil || availability != Available || len(got) != 2 || got[1].Label.Start() != 6 || got[1].Label.End() != 7 {
			t.Fatal(got, availability, err)
		}
		if style == "github" && (got[0].Base != parser.Some("a") || got[1].Base != parser.Some("a") || got[1].ID != parser.Some("a-1")) {
			t.Fatal(got)
		}
		if style == "explicit" && (got[0].Base.Present || got[1].Base.Present) {
			t.Fatal(got)
		}
	}
	for _, invalidate := range []func(*parser.ParseResult, **parser.OperationBudget, *parser.LineIndex, *parser.ParseOptions, *string){
		func(p *parser.ParseResult, _ **parser.OperationBudget, _ *parser.LineIndex, _ *parser.ParseOptions, _ *string) {
			p.Source = parser.NewSource([]byte("# A\n# A"))
		},
		func(_ *parser.ParseResult, b **parser.OperationBudget, _ *parser.LineIndex, _ *parser.ParseOptions, _ *string) {
			*b, _ = parser.NewOperationBudget(parser.DefaultLimits())
		},
		func(_ *parser.ParseResult, _ **parser.OperationBudget, i *parser.LineIndex, _ *parser.ParseOptions, _ *string) {
			*i = parser.NewLineIndex(parser.NewSource([]byte("# A\n# A")))
		},
		func(_ *parser.ParseResult, _ **parser.OperationBudget, _ *parser.LineIndex, o *parser.ParseOptions, _ *string) {
			o.GFM = true
		},
		func(_ *parser.ParseResult, _ **parser.OperationBudget, _ *parser.LineIndex, o *parser.ParseOptions, _ *string) {
			o.Frontmatter, _ = parser.NewFrontmatterOptions(true, nil)
		},
		func(p *parser.ParseResult, _ **parser.OperationBudget, _ *parser.LineIndex, _ *parser.ParseOptions, _ *string) {
			p.Document, _ = parser.NewDocument(p.Document.Root(), p.Document.References())
			p.Status, p.AST = parser.Complete, parser.Available
			p.Phases = parser.PhaseProgress{Blocks: parser.PhaseComplete, Inlines: parser.PhaseComplete}
		},
		func(_ *parser.ParseResult, _ **parser.OperationBudget, _ *parser.LineIndex, _ *parser.ParseOptions, s *string) {
			*s = "other"
		},
	} {
		p, b, i, o, style := parsed, budget, index, options, "github"
		invalidate(&p, &b, &i, &o, &style)
		if got, availability, err := OriginalHeadingAnchors(context.Background(), p, b, i, o, style); err == nil || got != nil || availability == Available {
			t.Fatal(got, availability, err)
		}
	}
	// Public completion fields cannot turn an unfinished actual document into authority.
	fm, _ := parser.NewFrontmatterOptions(true, nil)
	source := parser.NewSource([]byte("---\n# A"))
	b, _ := parser.NewOperationBudget(parser.DefaultLimits())
	p := parser.ParseWithOptions(context.Background(), source, b, parser.ParseOptions{Frontmatter: fm})
	p.Status, p.AST = parser.Complete, parser.Available
	p.Phases = parser.PhaseProgress{Blocks: parser.PhaseComplete, Inlines: parser.PhaseComplete}
	p.Uncertainty = nil
	if got, availability, err := OriginalHeadingAnchors(context.Background(), p, b, parser.NewLineIndex(source), parser.ParseOptions{Frontmatter: fm}, "github"); err == nil || got != nil || availability == Available {
		t.Fatal(got, availability, err)
	}
}
func TestOriginalHeadingAnchorsUnavailableAndOwnedFacts(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		options := parser.ParseOptions{GFM: gfm}
		p, b, i := originalAnchorParse(t, "# A\n# A\n\n<script id=\"x\">\n</script>", options)
		got, a, err := OriginalHeadingAnchors(context.Background(), p, b, i, options, "github")
		if err != nil {
			t.Fatal(err)
		}
		if gfm {
			if a != Uncertain || got != nil {
				t.Fatal(got, a)
			}
		} else if a != Available || len(got) != 2 {
			t.Fatal(got, a)
		}
	}
	p, b, i := originalAnchorParse(t, "> # A\n> # A", parser.ParseOptions{})
	got, a, err := OriginalHeadingAnchors(context.Background(), p, b, i, parser.ParseOptions{}, "github")
	if err != nil || a != Available || len(got) != 2 || len(got[0].Containers) != 1 {
		t.Fatal(got, a, err)
	}
	got[0].Base.Value = "changed"
	got[0].Containers[0].Kind = parser.DocumentKind
	again, a, err := OriginalHeadingAnchors(context.Background(), p, b, i, parser.ParseOptions{}, "github")
	if err != nil || a != Available || again[0].Base.Value != "a" || again[0].Containers[0].Kind != parser.BlockQuote {
		t.Fatal(again, a, err)
	}
}
func TestOriginalHeadingAnchorsCannotMintServiceAuthority(t *testing.T) {
	f := newAnchorFixture(t, "# A\n# A", "github", parser.ParseOptions{})
	spec, existing := readAnchorFixture(t, f)
	parsed, err := f.service.Parse(context.Background(), f.view, spec.Validated)
	if err != nil {
		t.Fatal(err)
	}
	f.service.mu.Lock()
	beforeAnchors, beforeValidations := len(f.service.anchors), len(f.service.validations)
	f.service.mu.Unlock()
	got, a, err := OriginalHeadingAnchors(context.Background(), parsed, f.op.budget, parser.NewLineIndex(parsed.Source), parser.ParseOptions{}, "github")
	want, e := existing.Headings(context.Background())
	if err != nil || e != nil || a != Available || !reflect.DeepEqual(got, want) {
		t.Fatal(got, want, a, err, e)
	}
	f.service.mu.Lock()
	afterAnchors, afterValidations := len(f.service.anchors), len(f.service.validations)
	f.service.mu.Unlock()
	if beforeAnchors != afterAnchors || beforeValidations != afterValidations {
		t.Fatal("local projection altered service admission/cache")
	}
}
func TestOriginalHeadingAnchorsFinalWithholdingAndStickyLimits(t *testing.T) {
	p, b, i := originalAnchorParse(t, "# A\n# A", parser.ParseOptions{})
	counter := &originalAnchorCounter{Context: context.Background()}
	if _, a, e := OriginalHeadingAnchors(counter, p, b, i, parser.ParseOptions{}, "github"); e != nil || a != Available {
		t.Fatal(a, e)
	}
	for _, stop := range []int{1, counter.calls / 2, counter.calls} {
		got, a, e := OriginalHeadingAnchors(&originalAnchorCounter{Context: context.Background(), stop: stop}, p, b, i, parser.ParseOptions{}, "github")
		if !errors.Is(e, context.Canceled) || got != nil || a != Interrupted {
			t.Fatal(stop, got, a, e)
		}
	}
	synctest.Test(t, func(t *testing.T) {
		for _, kind := range []string{"work", "nodes", "elapsed"} {
			limits := parser.DefaultLimits()
			limits.OperationTime = 20 * time.Millisecond
			b, _ := parser.NewOperationBudget(limits)
			source := parser.NewSource([]byte("# A\n# A"))
			p := parser.ParseWithBudget(context.Background(), source, b)
			switch kind {
			case "work":
				_ = b.ChargeWork(limits.WorkUnits)
			case "nodes":
				_ = b.ChargeNodes(limits.TotalNodes)
			case "elapsed":
				time.Sleep(25 * time.Millisecond)
			}
			for range 2 {
				got, a, e := OriginalHeadingAnchors(context.Background(), p, b, parser.NewLineIndex(source), parser.ParseOptions{}, "github")
				var limit *parser.LimitExceeded
				if !errors.As(e, &limit) || got != nil || a != Interrupted {
					t.Fatal(kind, got, a, e)
				}
			}
		}
	})
}
