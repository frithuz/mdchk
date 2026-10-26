package config

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

func TestSlidePolicyDirectFirstMatchOwnershipAndStops(t *testing.T) {
	patterns := []string{"slides/", "slides/**", "**"}
	origin := Origin{Path: "selected.yaml", PropertyPath: "documents.slides", Position: &parser.Position{Line: 3, Column: 2}}
	b, _ := parser.NewOperationBudget(parser.DefaultLimits())
	policy, err := NewSlidePolicy(context.Background(), b, patterns, origin)
	if err != nil {
		t.Fatal(err)
	}
	patterns[1] = "other/**"
	origin.Position.Line = 99
	got, err := policy.Match(context.Background(), b, "slides/a.md", true)
	if err != nil || !got.Known || !got.Slides || got.Pattern != "slides/**" || got.PatternIndex != 1 || got.OriginPosition.Line != 3 {
		t.Fatal(got, err)
	}
	got.Pattern = "changed"
	got.OriginPosition.Line = 99
	again, err := policy.Match(context.Background(), b, "slides/a.md", true)
	if err != nil || again.Pattern != "slides/**" || again.OriginPosition.Line != 3 {
		t.Fatal(again, err)
	}
	outside, err := policy.Match(context.Background(), b, "../bad\x00", false)
	if err != nil || !outside.Known || outside.Slides {
		t.Fatal(outside, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if v, err := policy.Match(ctx, b, "slides/a.md", true); !errors.Is(err, context.Canceled) || v.Known {
		t.Fatal(v, err)
	}
	for _, kind := range []string{"work", "nodes"} {
		q, _ := parser.NewOperationBudget(parser.DefaultLimits())
		if kind == "work" {
			_ = q.ChargeWork(q.Limits().WorkUnits)
		} else {
			_ = q.ChargeNodes(q.Limits().TotalNodes)
		}
		for range 2 {
			v, e := policy.Match(context.Background(), q, "slides/a.md", true)
			var limit *parser.LimitExceeded
			if !errors.As(e, &limit) || v.Known {
				t.Fatal(kind, v, e)
			}
		}
	}
	q, _ := parser.NewOperationBudget(parser.DefaultLimits())
	if _, err := NewSlidePolicy(context.Background(), q, []string{"**", "["}, Origin{}); err == nil {
		t.Fatal("invalid later pattern accepted")
	}
	limits := parser.DefaultLimits()
	limits.TotalNodes = 1
	limits.DocumentNodes = 1
	q, _ = parser.NewOperationBudget(limits)
	if _, err := NewSlidePolicy(context.Background(), q, []string{"**"}, Origin{}); err == nil {
		t.Fatal("unreserved policy construction")
	}
}

func TestSlidePlanPathsAndCandidatePolicyAreImmutable(t *testing.T) {
	var seen []engine.SlideContext
	r, err := engine.NewRegistry(engine.Catalog(), []engine.Registration{{ID: "MD090", Check: func(_ context.Context, c engine.RuleContext) engine.RuleOutcome {
		seen = append(seen, c.Context().Slides)
		return engine.RuleOutcome{}
	}}})
	if err != nil {
		t.Fatal(err)
	}
	body := "version: 1\nflavor: commonmark\ndirectives: {enabled: false}\ndocuments:\n  slides:\n    - nowhere/**\n    - slides/**\nrules: {MD090: {enabled: true}}\n"
	d, err := DecodeYAML("selected.yaml", []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	v, err := ValidateDefinition(d, r, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := v.Admit(Capabilities{CommonMark: true, SlideAnnotation: true}, Permissions{})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	cwd := filepath.Join(root, "working")
	base := filepath.Join(root, "policy")
	plan, err := resolved.ExecutionPlan().WithDocumentPaths(cwd, base)
	if err != nil {
		t.Fatal(err)
	}
	resolved.Settings.Slides[1] = "**"
	*resolved.Slides = Patterns{}
	o := resolved.Origins["documents.slides[1]"]
	if o.Position != nil {
		o.Position.Line = 99
	}
	paths := []string{"../policy/slides/a.md", "../policy/SLIDES/a.md", "../slides/a.md"}
	for _, name := range paths {
		in, e := plan.NewInput(name, []byte("***\n# H"))
		if e != nil {
			t.Fatal(e)
		}
		out, e := plan.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
		if e != nil || len(out.Errors) != 0 {
			t.Fatal(out, e)
		}
		candidate, e := plan.CandidatePlan().NewSourceInput(name, parser.NewSource([]byte("***\n# H\n")))
		if e != nil {
			t.Fatal(e)
		}
		_, trace, e := plan.CandidatePlan().InspectCandidate(context.Background(), candidate, engine.RunOptions{})
		if e != nil {
			t.Fatal(e)
		}
		dc, known := trace.DocumentContext()
		if !known {
			t.Fatal("candidate context unknown")
		}
		if dc.Slides != seen[len(seen)-1] {
			t.Fatal("candidate policy lost", dc, seen)
		}
	}
	if len(seen) != 6 || !seen[0].Slides || !seen[1].Slides || seen[2].Slides || seen[3].Slides || seen[4].Slides || seen[5].Slides {
		t.Fatal(seen)
	}
	if seen[0].PatternIndex != 1 || seen[0].Pattern != "slides/**" || seen[0].OriginPosition != (parser.Position{Line: 7, Column: 7}) || seen[0].OriginProperty != "documents.slides[1]" {
		t.Fatal(seen[0])
	}
	b, _ := parser.NewOperationBudget(parser.DefaultLimits())
	provider := slideProvider{policy: plan.slides, workingDirectory: cwd, base: base}
	if got, e := provider.Slides(context.Background(), b, "../policy/a/../slides/a.md"); e == nil || got.Known {
		t.Fatal("malformed identity cleaned into annotation", got, e)
	}
}

func TestSlidePolicyLongNamesShareOriginalWork(t *testing.T) {
	b, _ := parser.NewOperationBudget(parser.DefaultLimits())
	p, e := NewSlidePolicy(context.Background(), b, []string{"**/*.md"}, Origin{})
	if e != nil {
		t.Fatal(e)
	}
	limits := parser.DefaultLimits()
	limits.WorkUnits = 128
	q, _ := parser.NewOperationBudget(limits)
	got, e := p.Match(context.Background(), q, strings.Repeat("a", 5000)+".md", true)
	var limit *parser.LimitExceeded
	if !errors.As(e, &limit) || got.Known {
		t.Fatal(got, e)
	}
}

type slideStopContext struct {
	context.Context
	calls, stop int
}

func (c *slideStopContext) Err() error {
	c.calls++
	if c.stop > 0 && c.calls >= c.stop {
		return context.Canceled
	}
	return c.Context.Err()
}
func TestSlidePolicyFinalWithholdingIncludesOrdinaryPaths(t *testing.T) {
	for _, tc := range []struct {
		patterns []string
		name     string
		inBase   bool
	}{{[]string{"**"}, "a.md", true}, {[]string{"slides/**"}, "a.md", true}, {[]string{}, "a.md", true}, {[]string{"**"}, "../outside", false}} {
		b, _ := parser.NewOperationBudget(parser.DefaultLimits())
		p, e := NewSlidePolicy(context.Background(), b, tc.patterns, Origin{})
		if e != nil {
			t.Fatal(e)
		}
		count := &slideStopContext{Context: context.Background()}
		if got, e := p.Match(count, b, tc.name, tc.inBase); e != nil || !got.Known {
			t.Fatal(got, e)
		}
		// Outside-base publication has an initial observation and an independent
		// final observation. A removed final check must not move this oracle.
		if !tc.inBase {
			got, e := p.Match(&slideStopContext{Context: context.Background(), stop: 2}, b, tc.name, false)
			if !errors.Is(e, context.Canceled) || got.Known {
				t.Fatal("outside-base final publication escaped caller stop", got, e)
			}
		}
		for _, stop := range []int{1, max(1, count.calls/2), count.calls} {
			got, e := p.Match(&slideStopContext{Context: context.Background(), stop: stop}, b, tc.name, tc.inBase)
			if !errors.Is(e, context.Canceled) || got.Known {
				t.Fatal(tc, stop, got, e)
			}
		}
	}
}

func TestSlidePolicyOriginValidationReservesOriginalWork(t *testing.T) {
	for _, field := range []string{"path", "property"} {
		for _, suffix := range []string{"", "\x00", "\xff"} {
			t.Run(field+"/"+strings.ReplaceAll(suffix, "\x00", "NUL"), func(t *testing.T) {
				origin := Origin{}
				value := strings.Repeat("a", 1<<19) + suffix
				if field == "path" {
					origin.Path = value
				} else {
					origin.PropertyPath = value
				}
				limits := parser.DefaultLimits()
				limits.WorkUnits = 64
				budget, err := parser.NewOperationBudget(limits)
				if err != nil {
					t.Fatal(err)
				}
				policy, err := NewSlidePolicy(context.Background(), budget, []string{"**"}, origin)
				var limit *parser.LimitExceeded
				if policy != nil || !errors.As(err, &limit) || limit.Name != "work-units" {
					t.Fatalf("origin inspected before original work reservation: policy=%v err=%v remaining64=%v", policy, err, budget.ChargeWork(64))
				}
				if err := budget.ChargeWork(0); !errors.As(err, &limit) || limit.Name != "work-units" {
					t.Fatal("original reservation was not sticky", err)
				}
			})
		}
	}
}

func TestSlidePolicyOriginContractAndSingleReservation(t *testing.T) {
	for _, origin := range []Origin{
		{Path: "bad\x00"}, {Path: "bad\xff"}, {PropertyPath: "bad\x00"}, {PropertyPath: "bad\xff"},
		{Position: &parser.Position{Line: 0, Column: 1}}, {Position: &parser.Position{Line: 1, Column: 0}},
	} {
		budget, err := parser.NewOperationBudget(parser.DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		policy, err := NewSlidePolicy(context.Background(), budget, []string{"**"}, origin)
		if policy != nil || !errors.Is(err, ErrInvalidSlideOrigin) {
			t.Fatal("malformed origin lost error identity", policy, err)
		}
	}
	origin := Origin{Path: "selected.yaml", PropertyPath: "documents.slides"}
	limits := parser.DefaultLimits()
	budget, err := parser.NewOperationBudget(limits)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewSlidePolicy(context.Background(), budget, []string{"**"}, origin); err != nil {
		t.Fatal(err)
	}
	// A valid constructor retains exactly its original charge, including origins;
	// the integration adapter must not need a second validation reservation.
	cost := int64(8*(2+len(origin.Path)+len(origin.PropertyPath)) + 4 + 1)
	if err := budget.ChargeWork(limits.WorkUnits - cost); err != nil {
		t.Fatal("valid origin charged twice", err)
	}
	var limit *parser.LimitExceeded
	if err := budget.ChargeWork(1); !errors.As(err, &limit) || limit.Name != "work-units" {
		t.Fatal("origin work omitted", err)
	}
	budget, _ = parser.NewOperationBudget(limits)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewSlidePolicy(ctx, budget, []string{"**"}, Origin{Path: "bad\x00"}); !errors.Is(err, context.Canceled) {
		t.Fatal("caller stop lost", err)
	}
	budget, _ = parser.NewOperationBudget(limits)
	if _, err := NewSlidePolicy(context.Background(), budget, []string{"["}, origin); err == nil {
		t.Fatal("pattern error hidden")
	} else {
		var invalid *PatternError
		if !errors.As(err, &invalid) || invalid.Index != 0 {
			t.Fatal("pattern error identity lost", err)
		}
	}
}

func TestSlidePolicyPatternLimitsPrecedeOriginBounds(t *testing.T) {
	for _, patterns := range [][]string{make([]string, MaxPatterns+1), {strings.Repeat("a", MaxPatternBytes+1)}} {
		limits := parser.DefaultLimits()
		limits.DocumentBytes = 128
		budget, err := parser.NewOperationBudget(limits)
		if err != nil {
			t.Fatal(err)
		}
		_, err = NewSlidePolicy(context.Background(), budget, patterns, Origin{Path: strings.Repeat("a", 129)})
		var invalid *PatternError
		if !errors.Is(err, ErrPatternLimit) || len(patterns) == 1 && (!errors.As(err, &invalid) || invalid.Index != 0) {
			t.Fatal("pattern limit precedence lost to origin bound", err)
		}
	}
}
