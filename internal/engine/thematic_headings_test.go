package engine

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestRuleContextThematicHeadingsActualAuthority(t *testing.T) {
	c := thematicContext(t, "***\n# H", parser.DefaultLimits(), context.Background())
	got, err := c.ThematicBreaksBeforeHeadings(context.Background())
	if err != nil || len(got) != 1 || got[0].Start() != 0 {
		t.Fatal(got, err)
	}
	for _, invalidate := range []func(*RuleContext){
		func(c *RuleContext) { c.syntaxReliable = false },
		func(c *RuleContext) { c.parsed.Source = parser.NewSource([]byte("***\n# H")) },
		func(c *RuleContext) { c.parsed.Status = parser.Incomplete },
		func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }, func(c *RuleContext) { c.budget = nil },
		func(c *RuleContext) { c.index = parser.NewLineIndex(parser.NewSource([]byte("***\n# H"))) },
		func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) },
		func(c *RuleContext) { c.input, _ = NewInput("foreign.md", []byte("***\n# H"), nil, DocumentContext{}) },
		func(c *RuleContext) { c.document, _ = parser.NewDocument(c.document.Root(), c.document.References()) },
		func(c *RuleContext) {
			c.parsed.Document, _ = parser.NewDocument(c.document.Root(), c.document.References())
		},
		func(c *RuleContext) { c.input.documentContext.Flavor = "gfm" },
		func(c *RuleContext) { c.input.documentContext.Frontmatter = parser.FrontmatterOptions{} },
	} {
		bad := c
		invalidate(&bad)
		if got, err := bad.ThematicBreaksBeforeHeadings(context.Background()); err == nil || got != nil {
			t.Fatal(got, err)
		}
	}
	//nolint:staticcheck // Explicitly reject absent caller authority.
	if _, err := c.ThematicBreaksBeforeHeadings(nil); err == nil {
		t.Fatal("nil context")
	}
	for _, reason := range []error{context.Canceled, context.DeadlineExceeded} {
		for _, origin := range []bool{false, true} {
			q := c
			caller := context.Background()
			if origin {
				q.operation = &mdTerminalStopped{Context: q.operation, reason: reason}
			} else {
				caller = &mdTerminalStopped{Context: caller, reason: reason}
			}
			if got, err := q.ThematicBreaksBeforeHeadings(caller); !errors.Is(err, reason) || got != nil {
				t.Fatal(got, err)
			}
		}
	}
}
func TestRuleContextThematicHeadingsOriginalControlsAndFinalWithholding(t *testing.T) {
	c := thematicContext(t, "***\n# H", parser.DefaultLimits(), context.Background())
	count := &terminalCounter{Context: context.Background()}
	if _, err := c.ThematicBreaksBeforeHeadings(count); err != nil {
		t.Fatal(err)
	}
	for _, stop := range []int{1, count.calls / 2, count.calls} {
		if got, err := c.ThematicBreaksBeforeHeadings(&terminalCounter{Context: context.Background(), stop: stop}); !errors.Is(err, context.Canceled) || got != nil {
			t.Fatal(stop, got, err)
		}
	}
	synctest.Test(t, func(t *testing.T) {
		for _, kind := range []string{"work", "nodes", "elapsed"} {
			limits := parser.DefaultLimits()
			limits.OperationTime = 20 * time.Millisecond
			q := thematicContext(t, "***\n# H", limits, context.Background())
			switch kind {
			case "work":
				_ = q.budget.ChargeWork(limits.WorkUnits)
			case "nodes":
				_ = q.budget.ChargeNodes(limits.TotalNodes)
			case "elapsed":
				time.Sleep(25 * time.Millisecond)
			}
			for range 2 {
				got, err := q.ThematicBreaksBeforeHeadings(context.Background())
				var limit *parser.LimitExceeded
				if !errors.As(err, &limit) || got != nil {
					t.Fatal(kind, got, err)
				}
			}
		}
	})
}

func thematicContext(t *testing.T, text string, limits parser.Limits, operation context.Context) RuleContext {
	c := headingContext(t, text, limits, operation)
	c.options = Options{"allow_slides": true}
	return c
}
func TestSlideContextRejectsUnknownAndInconsistentPayload(t *testing.T) {
	for _, slides := range []SlideContext{{Pattern: "**"}, {Slides: true, Pattern: "**"}, {Known: true, Pattern: "**"}, {Known: true, Slides: true}, {Known: true, Slides: true, Pattern: "**", PatternIndex: -1}, {Known: true, Slides: true, Pattern: "**", OriginPosition: parser.Position{Line: 1}}} {
		if _, err := NewInput("a.md", nil, nil, DocumentContext{Slides: slides}); err == nil {
			t.Fatal(slides)
		}
	}
	in, err := NewInput("a.md", nil, nil, DocumentContext{})
	if err != nil || !in.documentContext.Slides.Known || in.documentContext.Slides.Slides {
		t.Fatal(in, err)
	}
}

type failingSlideProvider struct {
	calls int
	err   error
}

func (p *failingSlideProvider) Slides(context.Context, *parser.OperationBudget, string) (SlideContext, error) {
	p.calls++
	return SlideContext{}, p.err
}
func TestSlideAnnotationCannotMaskMandatoryEncoding(t *testing.T) {
	r, err := NewRegistry(Catalog(), nil)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := r.SelectRules(nil, ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := selected.Plan(DocumentContext{Flavor: "commonmark"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	provider := &failingSlideProvider{err: context.Canceled}
	plan = plan.WithSlideProvider(provider)
	in, err := plan.NewInput("invalid.md", []byte{0xff})
	if err != nil {
		t.Fatal(err)
	}
	out, err := plan.Lint(context.Background(), []Input{in}, RunOptions{})
	if err != nil || len(out.Errors) != 1 || out.Errors[0].Code != "invalid-utf8" || provider.calls != 0 {
		t.Fatal(out, err, provider.calls)
	}
	in, err = plan.NewInput("valid.md", []byte("***\n# H"))
	if err != nil {
		t.Fatal(err)
	}
	out, err = plan.Lint(context.Background(), []Input{in}, RunOptions{})
	if err != nil || out.Complete || len(out.Interruptions) != 1 || provider.calls != 1 {
		t.Fatal(out, err, provider.calls)
	}
}

func TestSlideUnknownProviderCannotBecomeOrdinaryOrExempt(t *testing.T) {
	r, err := NewRegistry(Catalog(), nil)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := r.SelectRules(nil, ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := selected.Plan(DocumentContext{Flavor: "commonmark"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	provider := &failingSlideProvider{}
	plan = plan.WithSlideProvider(provider)
	in, err := plan.NewInput("valid.md", []byte("***\n# H"))
	if err != nil {
		t.Fatal(err)
	}
	out, trace, err := plan.Inspect(context.Background(), in, RunOptions{}, InspectionOptions{})
	if err != nil || out.Complete || len(out.Errors) != 1 || out.Errors[0].Code != "document-context" || len(out.Interruptions) != 0 || provider.calls != 1 {
		t.Fatal(out, err, provider.calls)
	}
	if dc, known := trace.DocumentContext(); known || dc.Slides.Known || dc.Slides.Slides {
		t.Fatal("unknown annotation became ordinary/exempt", dc, known)
	}
	for _, e := range trace.Evaluations() {
		if e.Rule == "MD090" && len(e.Reasons) == 1 && e.Reasons[0] == "inapplicable" {
			t.Fatal(e)
		}
	}
}
