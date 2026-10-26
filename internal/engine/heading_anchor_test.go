package engine

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/resource"
)

func TestRuleContextHeadingAnchorsActualAuthority(t *testing.T) {
	c := headingContext(t, "# A\n# A", parser.DefaultLimits(), context.Background())
	got, a, err := c.HeadingAnchors(context.Background())
	if err != nil || a != resource.Available || len(got) != 2 || got[1].Label.Start() != 6 {
		t.Fatal(got, a, err)
	}
	for _, invalidate := range []func(*RuleContext){
		func(c *RuleContext) { c.syntaxReliable = false }, func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }, func(c *RuleContext) { c.budget = nil },
		func(c *RuleContext) { c.index = parser.NewLineIndex(parser.NewSource([]byte("# A\n# A"))) },
		func(c *RuleContext) { c.budget, _ = parser.NewOperationBudget(parser.DefaultLimits()) },
		func(c *RuleContext) { c.input, _ = NewInput("foreign.md", []byte("# A\n# A"), nil, DocumentContext{}) },
		func(c *RuleContext) { c.document, _ = parser.NewDocument(c.document.Root(), c.document.References()) },
		func(c *RuleContext) {
			c.parsed.Document, _ = parser.NewDocument(c.document.Root(), c.document.References())
		},
		func(c *RuleContext) { c.input.documentContext.Flavor = "gfm" },
		func(c *RuleContext) { c.input.documentContext.Frontmatter = parser.FrontmatterOptions{} },
	} {
		bad := c
		invalidate(&bad)
		if got, a, err := bad.HeadingAnchors(context.Background()); err == nil || got != nil || a == resource.Available {
			t.Fatal(got, a, err)
		}
	}
	//nolint:staticcheck // Explicitly reject absent caller authority.
	if _, _, err := c.HeadingAnchors(nil); err == nil {
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
			if got, a, err := q.HeadingAnchors(caller); !errors.Is(err, reason) || got != nil || a != resource.Interrupted {
				t.Fatal(got, a, err)
			}
		}
	}
}
func TestRuleContextHeadingAnchorsPolicySnapshotsAndConstructors(t *testing.T) {
	r, err := NewCatalogRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := r.SelectRules(nil, ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	for _, style := range []string{"", "github", "explicit"} {
		dc := DocumentContext{Flavor: "commonmark", AnchorStyle: style}
		plan, err := selection.Plan(dc, nil)
		if err != nil {
			t.Fatal(err)
		}
		dc.AnchorStyle = "changed"
		want := style
		if want == "" {
			want = "github"
		}
		if plan.Context().AnchorStyle != want {
			t.Fatal(plan.Context())
		}
		in, err := plan.NewInput("a.md", []byte("# A\n# A"))
		if err != nil || in.documentContext.AnchorStyle != want {
			t.Fatal(in, err)
		}
		copy := in.documentContext
		copy.AnchorStyle = "changed"
		if in.documentContext.AnchorStyle != want {
			t.Fatal("mutable context")
		}
		sourceIn, err := plan.NewSourceInput("a.md", in.Source())
		if err != nil || sourceIn.documentContext.AnchorStyle != want || sourceIn.Source() != in.Source() {
			t.Fatal(sourceIn, err)
		}
	}
	if _, err := NewInput("a.md", nil, nil, DocumentContext{AnchorStyle: "bad"}); err == nil {
		t.Fatal("unknown input anchor style")
	}
	if _, err := selection.Plan(DocumentContext{Flavor: "commonmark", AnchorStyle: "bad"}, nil); err == nil {
		t.Fatal("unknown plan anchor style")
	}
}
func TestRuleContextHeadingAnchorsOriginalControlsAndFinalWithholding(t *testing.T) {
	c := headingContext(t, "# A\n# A", parser.DefaultLimits(), context.Background())
	count := &terminalCounter{Context: context.Background()}
	if _, _, err := c.HeadingAnchors(count); err != nil {
		t.Fatal(err)
	}
	for _, stop := range []int{1, count.calls / 2, count.calls} {
		if got, a, err := c.HeadingAnchors(&terminalCounter{Context: context.Background(), stop: stop}); !errors.Is(err, context.Canceled) || got != nil || a != resource.Interrupted {
			t.Fatal(stop, got, a, err)
		}
	}
	synctest.Test(t, func(t *testing.T) {
		for _, kind := range []string{"work", "nodes", "elapsed"} {
			limits := parser.DefaultLimits()
			limits.OperationTime = 20 * time.Millisecond
			q := headingContext(t, "# A\n# A", limits, context.Background())
			switch kind {
			case "work":
				_ = q.budget.ChargeWork(limits.WorkUnits)
			case "nodes":
				_ = q.budget.ChargeNodes(limits.TotalNodes)
			case "elapsed":
				time.Sleep(25 * time.Millisecond)
			}
			for range 2 {
				got, a, err := q.HeadingAnchors(context.Background())
				var limit *parser.LimitExceeded
				if !errors.As(err, &limit) || got != nil || a != resource.Interrupted {
					t.Fatal(kind, got, a, err)
				}
			}
		}
	})
}
