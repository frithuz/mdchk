package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/frithuz/mdchk/internal/parser"
)

func headingContext(t *testing.T, text string, limits parser.Limits, operation context.Context) RuleContext {
	t.Helper()
	budget, err := parser.NewOperationBudget(limits)
	if err != nil {
		t.Fatal(err)
	}
	fm, _ := parser.NewFrontmatterOptions(true, nil)
	in, err := NewInput("context.md", []byte(text), nil, DocumentContext{Frontmatter: fm})
	if err != nil {
		t.Fatal(err)
	}
	parsed := parser.ParseWithOptions(operation, in.source, budget, in.documentContext.ParseOptions())
	if parsed.Status != parser.Complete {
		t.Fatal(parsed)
	}
	return RuleContext{input: in, document: parsed.Document, parsed: parsed, budget: budget, operation: operation, syntaxReliable: true, options: Options{"frontmatter_title": true}}
}

func TestRuleContextHeadingAndTitleOriginalAuthority(t *testing.T) {
	c := headingContext(t, "---\ntitle: Guide\n---\n# Root\n### Third\n", parser.DefaultLimits(), context.Background())
	title, err := c.FrontmatterTitle(context.Background())
	if err != nil || !title.Known || !title.Present || title.Text != "Guide" || title.Location.SourceToken() != c.Source().Token() {
		t.Fatal(title, err)
	}
	facts, err := c.Headings(context.Background())
	if err != nil || len(facts) != 2 || facts[1].Level != 3 || facts[1].Envelope.SourceToken() != c.Source().Token() || facts[1].OpeningSeparator.SourceToken() != c.Source().Token() || !facts[1].HasRawContent {
		t.Fatal(facts, err)
	}
	if _, present := c.Frontmatter(); present {
		t.Fatal("semantic title widened opaque header observation")
	}
	// Returned records can be changed without mutating the document inventory.
	facts[1].Level = 1
	again, err := c.Headings(context.Background())
	if err != nil || again[1].Level != 3 {
		t.Fatal(again, err)
	}
	c.options["frontmatter_title"] = false
	if _, err := c.FrontmatterTitle(context.Background()); err == nil {
		t.Fatal("title decode without opt-in")
	}
	c.syntaxReliable = false
	if _, err := c.Headings(context.Background()); err == nil {
		t.Fatal("unreliable heading inventory")
	}
}

func TestRuleContextTitleOriginalDocumentAllowance(t *testing.T) {
	source := "---\ntitle: Guide\n---\n### Third\n"
	// Discover the minimum allowance admitting this original parse, without using
	// private counters or deriving the semantic reservation from its implementation.
	var minimum int64
	for n := int64(1); n < 500; n++ {
		limits := parser.DefaultLimits()
		limits.DocumentNodes = n
		b, _ := parser.NewOperationBudget(limits)
		fm, _ := parser.NewFrontmatterOptions(true, nil)
		p := parser.ParseWithOptions(context.Background(), parser.NewSource([]byte(source)), b, parser.ParseOptions{Frontmatter: fm})
		if p.Status == parser.Complete {
			minimum = n
			break
		}
	}
	if minimum == 0 {
		t.Fatal("no complete original parse")
	}
	for _, extra := range []int64{0, 10000} {
		limits := parser.DefaultLimits()
		limits.DocumentNodes = minimum + extra
		c := headingContext(t, source, limits, context.Background())
		title, err := c.FrontmatterTitle(context.Background())
		if extra == 0 {
			var limit *parser.LimitExceeded
			if !errors.As(err, &limit) || limit.Name != "document-nodes" || title.Known {
				t.Fatal("reset original document allowance", title, err)
			}
			// The original local stop remains sticky; another document is independent.
			_, again := c.FrontmatterTitle(context.Background())
			var repeat *parser.LimitExceeded
			if !errors.As(again, &repeat) || *repeat != *limit {
				t.Fatal(limit, repeat, again)
			}
			next := parser.ParseWithOptions(context.Background(), parser.NewSource([]byte(source)), c.budget, c.Context().ParseOptions())
			if next.Status != parser.Complete {
				t.Fatal("local title stop poisoned next document", next)
			}
		} else if err != nil || !title.Present || !title.Known {
			t.Fatal("sufficient original allowance failed", title, err)
		}
	}
}

func TestRuleContextCompoundControls(t *testing.T) {
	for _, access := range []string{"headings", "title", "opening hashes", "closing hashes"} {
		operation, cancel := context.WithCancel(context.Background())
		c := headingContext(t, "---\ntitle: Guide\n---\n# Root #\n### Third #\n", parser.DefaultLimits(), operation)
		// A caller deadline, original operation cancellation and original cumulative
		// quota must all survive the added bridge as canonical causes.
		caller, stop := context.WithDeadline(context.Background(), time.Unix(0, 0))
		defer stop()
		cancel()
		_ = c.budget.ChargeWork(c.budget.Limits().WorkUnits)
		var err error
		if access == "headings" {
			_, err = c.Headings(caller)
		} else if access == "opening hashes" {
			_, err = c.OpeningHashCandidates(caller)
		} else if access == "closing hashes" {
			_, err = c.ClosingHashCandidates(caller)
		} else {
			_, err = c.FrontmatterTitle(caller)
		}
		var limit *parser.LimitExceeded
		if !errors.Is(err, context.Canceled) || !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &limit) || limit.Name != "work-units" {
			t.Fatal(access, err)
		}
	}
}

func TestRuleContextHeadingsOriginalAllowance(t *testing.T) {
	const source = "#  <!-- raw -->  #\n###  a\x00b  #\n#\t#\n"
	for _, kind := range []string{"nodes", "work"} {
		// Find the exact parser admission boundary from public outcomes. Projection
		// must consume its residual allowance rather than start a fresh parse budget.
		low, high := int64(1), int64(10000)
		for low < high {
			n := (low + high) / 2
			limits := parser.DefaultLimits()
			if kind == "nodes" {
				limits.TotalNodes = n
				limits.DocumentNodes = n
			} else {
				limits.WorkUnits = n
			}
			b, _ := parser.NewOperationBudget(limits)
			fm, _ := parser.NewFrontmatterOptions(true, nil)
			p := parser.ParseWithOptions(context.Background(), parser.NewSource([]byte(source)), b, parser.ParseOptions{Frontmatter: fm})
			if p.Status == parser.Complete {
				high = n
			} else {
				low = n + 1
			}
		}
		for _, extra := range []int64{0, 10000} {
			limits := parser.DefaultLimits()
			if kind == "nodes" {
				limits.TotalNodes = low + extra
				limits.DocumentNodes = low + extra
			} else {
				limits.WorkUnits = low + extra
			}
			c := headingContext(t, source, limits, context.Background())
			facts, err := c.Headings(context.Background())
			if extra == 0 {
				var limit *parser.LimitExceeded
				expected := "total-nodes"
				if kind == "work" {
					expected = "work-units"
				}
				if !errors.As(err, &limit) || limit.Name != expected || facts != nil {
					t.Fatal("new projection allowance", facts, err)
				}
			} else if err != nil || len(facts) != 3 {
				t.Fatal("sufficient original allowance failed", facts, err)
			}
		}
	}
}

func TestRuleContextOriginalElapsedAllowance(t *testing.T) {
	limits := parser.DefaultLimits()
	limits.OperationTime = time.Second
	c := headingContext(t, "---\ntitle: Guide\n---\n# Root #\n### Third #\n", limits, context.Background())
	// The allowance begins at the original parse, rather than at an accessor.
	time.Sleep(limits.OperationTime + time.Millisecond)
	facts, headingErr := c.Headings(context.Background())
	title, titleErr := c.FrontmatterTitle(context.Background())
	points, pointErr := c.OpeningHashCandidates(context.Background())
	closing, closingErr := c.ClosingHashCandidates(context.Background())
	for _, err := range []error{headingErr, titleErr, pointErr, closingErr} {
		var limit *parser.LimitExceeded
		if !errors.As(err, &limit) || limit.Name != "operation-time" {
			t.Fatal(err)
		}
	}
	if facts != nil || points != nil || closing != nil || title.Known {
		t.Fatal("elapsed stop published complete facts", facts, title)
	}
}

func TestRuleContextOpeningHashesOriginalAllowance(t *testing.T) {
	const source = "##Root\n###Third\n"
	for _, kind := range []string{"nodes", "work"} {
		// Find the exact parser admission boundary from public outcomes. Projection
		// must consume its residual allowance rather than start a fresh parse budget.
		low, high := int64(1), int64(10000)
		for low < high {
			n := (low + high) / 2
			limits := parser.DefaultLimits()
			if kind == "nodes" {
				limits.TotalNodes = n
				limits.DocumentNodes = n
			} else {
				limits.WorkUnits = n
			}
			b, _ := parser.NewOperationBudget(limits)
			fm, _ := parser.NewFrontmatterOptions(true, nil)
			p := parser.ParseWithOptions(context.Background(), parser.NewSource([]byte(source)), b, parser.ParseOptions{Frontmatter: fm})
			if p.Status == parser.Complete {
				high = n
			} else {
				low = n + 1
			}
		}
		for _, extra := range []int64{0, 10000} {
			limits := parser.DefaultLimits()
			if kind == "nodes" {
				limits.TotalNodes = low + extra
				limits.DocumentNodes = low + extra
			} else {
				limits.WorkUnits = low + extra
			}
			c := headingContext(t, source, limits, context.Background())
			facts, err := c.OpeningHashCandidates(context.Background())
			if extra == 0 {
				var limit *parser.LimitExceeded
				expected := "total-nodes"
				if kind == "work" {
					expected = "work-units"
				}
				if !errors.As(err, &limit) || limit.Name != expected || facts != nil {
					t.Fatal("new projection allowance", facts, err)
				}
			} else if err != nil || len(facts) != 2 {
				t.Fatal("sufficient original allowance failed", facts, err)
			}
		}
	}
}

func TestRuleContextOpeningHashesAuthority(t *testing.T) {
	c := headingContext(t, "##bad", parser.DefaultLimits(), context.Background())
	points, err := c.OpeningHashCandidates(context.Background())
	if err != nil || len(points) != 1 || points[0].Start() != 2 || points[0].SourceToken() != c.Source().Token() {
		t.Fatal(points, err)
	}
	for _, invalidate := range []func(*RuleContext){func(c *RuleContext) { c.syntaxReliable = false }, func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }} {
		copy := c
		invalidate(&copy)
		if partial, err := copy.OpeningHashCandidates(context.Background()); err == nil || partial != nil {
			t.Fatal(partial, err)
		}
	}
}

func TestRuleContextClosingHashesOriginalAllowance(t *testing.T) {
	const source = "## Root##\n### Third##\n"
	for _, kind := range []string{"nodes", "work"} {
		// Find the exact parser admission boundary from public outcomes. Projection
		// must consume its residual allowance rather than start a fresh parse budget.
		low, high := int64(1), int64(10000)
		for low < high {
			n := (low + high) / 2
			limits := parser.DefaultLimits()
			if kind == "nodes" {
				limits.TotalNodes = n
				limits.DocumentNodes = n
			} else {
				limits.WorkUnits = n
			}
			b, _ := parser.NewOperationBudget(limits)
			fm, _ := parser.NewFrontmatterOptions(true, nil)
			p := parser.ParseWithOptions(context.Background(), parser.NewSource([]byte(source)), b, parser.ParseOptions{Frontmatter: fm})
			if p.Status == parser.Complete {
				high = n
			} else {
				low = n + 1
			}
		}
		for _, extra := range []int64{0, 10000} {
			limits := parser.DefaultLimits()
			if kind == "nodes" {
				limits.TotalNodes = low + extra
				limits.DocumentNodes = low + extra
			} else {
				limits.WorkUnits = low + extra
			}
			c := headingContext(t, source, limits, context.Background())
			facts, err := c.ClosingHashCandidates(context.Background())
			if extra == 0 {
				var limit *parser.LimitExceeded
				expected := "total-nodes"
				if kind == "work" {
					expected = "work-units"
				}
				if !errors.As(err, &limit) || limit.Name != expected || facts != nil {
					t.Fatal("new projection allowance", facts, err)
				}
			} else if err != nil || len(facts) != 2 {
				t.Fatal("sufficient original allowance failed", facts, err)
			}
		}
	}
}

func TestRuleContextClosingHashesAuthority(t *testing.T) {
	c := headingContext(t, "## bad##", parser.DefaultLimits(), context.Background())
	points, err := c.ClosingHashCandidates(context.Background())
	if err != nil || len(points) != 1 || points[0].Start() != 6 || points[0].SourceToken() != c.Source().Token() {
		t.Fatal(points, err)
	}
	for _, invalidate := range []func(*RuleContext){func(c *RuleContext) { c.syntaxReliable = false }, func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }} {
		copy := c
		invalidate(&copy)
		if partial, err := copy.ClosingHashCandidates(context.Background()); err == nil || partial != nil {
			t.Fatal(partial, err)
		}
	}
}

func TestRuleContextClosedGapAuthority(t *testing.T) {
	c := headingContext(t, "#  a\x00b  #\n#\t#", parser.DefaultLimits(), context.Background())
	facts, err := c.Headings(context.Background())
	if err != nil || len(facts) != 2 || facts[0].ClosedClosingGap.SourceToken() != c.Source().Token() || facts[1].ClosedOpeningGap.SourceToken() != c.Source().Token() {
		t.Fatal(facts, err)
	}
	for _, invalidate := range []func(*RuleContext){func(c *RuleContext) { c.syntaxReliable = false }, func(c *RuleContext) { c.operation = nil }, func(c *RuleContext) { c.document = nil }} {
		copy := c
		invalidate(&copy)
		if got, err := copy.Headings(context.Background()); err == nil || got != nil {
			t.Fatal("invalid context granted gaps", got, err)
		}
	}
}
