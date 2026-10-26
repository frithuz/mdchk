package integration

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/resource"
)

func slideFixture(t *testing.T, operationContext context.Context, patterns []string) (*Scope, Document, *resource.Operation) {
	t.Helper()
	source := parser.NewSource([]byte("---\nformat: revealjs\n---\n雪\r\n# Talk\n"))
	root, err := resource.NewRoot(resource.SuppliedRoot)
	if err != nil {
		t.Fatal(err)
	}
	budget, err := parser.NewOperationBudget(parser.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	settings, err := resource.NewSettings(parser.ParseOptions{}, "index.md", "github", "slides-test")
	if err != nil {
		t.Fatal(err)
	}
	operation, err := resource.NewOperation(operationContext, budget, root, settings, []resource.Target{{ID: "talk.md", Source: source}})
	if err != nil {
		t.Fatal(err)
	}
	view, err := operation.NewView(resource.Original, nil)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := NewScope(context.Background(), operation, view, Settings{Slides: SlideSettings{Patterns: patterns, Origin: config.Origin{Path: "project/.mdchk.yaml", PropertyPath: "documents.slides", Position: &parser.Position{Line: 4, Column: 3}}}})
	if err != nil {
		t.Fatal(err)
	}
	document, err := scope.Document(context.Background(), "talk.md")
	if err != nil {
		t.Fatal(err)
	}
	return scope, document, operation
}

func TestSlidesExplicitDirectSelectedAnnotation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		patterns []string
		inBase   bool
		matched  string
	}{
		{"talk.md", nil, true, ""},
		{"slides/talk.md", []string{"slides/**", "**"}, true, "slides/**"},
		{"deck.slides.md", []string{"**/*.slides.md"}, true, "**/*.slides.md"},
		{"talk.md", []string{"**"}, true, "**"},
		{"SLIDES/talk.md", []string{"slides/**"}, true, ""},
		{"slides/talk.md", []string{"slides/"}, true, ""},
		{"../outside\x00", []string{"**"}, false, ""},
	} {
		t.Run(tc.name+tc.matched, func(t *testing.T) {
			scope, document, operation := slideFixture(t, context.Background(), tc.patterns)
			before := operation.Targets()
			out, err := scope.Slides(context.Background(), document, tc.name, tc.inBase)
			if err != nil || out.Status() != Available || out.Document().Source() != document.Source() || !out.Document().View().Same(document.View()) {
				t.Fatalf("wrong selected annotation: %#v %v", out, err)
			}
			pattern, matched := out.MatchedPattern()
			if pattern != tc.matched || matched != (tc.matched != "") {
				t.Fatalf("pattern=%q matched=%v want %q", pattern, matched, tc.matched)
			}
			if scope.mkdocs.Enabled || !reflect.DeepEqual(before, operation.Targets()) || operation.Settings().AnchorStyle() != "github" || operation.Settings().ParseOptions().GFM {
				t.Fatal("annotation changed independent settings or selection")
			}
		})
	}
}

func TestSlidesOwnedPolicyAndOrigins(t *testing.T) {
	scope, document, operation := slideFixture(t, context.Background(), nil)
	patterns := []string{"slides/**", "**"}
	origin := config.Origin{Path: "config/.mdchk.yaml", PropertyPath: "documents.slides", Position: &parser.Position{Line: 9, Column: 2}}
	second, err := NewScope(context.Background(), operation, document.View(), Settings{Slides: SlideSettings{Patterns: patterns, Origin: origin}})
	if err != nil {
		t.Fatal(err)
	}
	secondDocument, err := second.Document(context.Background(), document.ID())
	if err != nil {
		t.Fatal(err)
	}
	patterns[0] = "never/**"
	origin.Position.Line = 99
	out, err := second.Slides(context.Background(), secondDocument, "slides/talk.md", true)
	if err != nil {
		t.Fatal(err)
	}
	if pattern, _ := out.MatchedPattern(); pattern != "slides/**" || out.Origin().Position.Line != 9 {
		t.Fatal("caller mutation changed owned pattern/provenance", out)
	}
	returned := out.Origin()
	returned.Position.Line = 1
	if out.Origin().Position.Line != 9 || out.Origin().Path != "config/.mdchk.yaml" || out.Origin().PropertyPath != "documents.slides" {
		t.Fatal("returned origin aliases retained provenance")
	}
	if _, err := scope.Slides(context.Background(), secondDocument, "slides/talk.md", true); !errors.Is(err, ErrContract) {
		t.Fatal("foreign scope document accepted", err)
	}
	forged := secondDocument
	forged.source = parser.NewSource(secondDocument.Source().Bytes())
	if _, err := second.Slides(context.Background(), forged, "slides/talk.md", true); !errors.Is(err, ErrContract) {
		t.Fatal("equal-byte foreign source accepted", err)
	}
}

func TestSlidesViewsAndUnavailableSaved(t *testing.T) {
	_, original, operation := slideFixture(t, context.Background(), []string{"**"})
	for _, phase := range []resource.Phase{resource.Candidate, resource.Saved} {
		replacement := parser.NewSource([]byte("# Changed\r\n"))
		view, err := operation.NewView(phase, []resource.Replacement{{TargetID: original.ID(), Source: replacement}})
		if err != nil {
			t.Fatal(err)
		}
		scope, err := NewScope(context.Background(), operation, view, Settings{Slides: SlideSettings{Patterns: []string{"**"}}})
		if err != nil {
			t.Fatal(err)
		}
		document, err := scope.Document(context.Background(), original.ID())
		if err != nil {
			t.Fatal(err)
		}
		out, err := scope.Slides(context.Background(), document, "talk.md", true)
		if err != nil || out.Status() != Available || out.Document().Source() != replacement || out.Document().View().Phase() != phase {
			t.Fatal("view/source identity lost", out, err)
		}
		if _, err := scope.Slides(context.Background(), original, "talk.md", true); !errors.Is(err, ErrContract) {
			t.Fatal("stale original accepted", err)
		}
	}
	view, err := operation.NewView(resource.Saved, []resource.Replacement{{TargetID: original.ID(), Unavailable: true}})
	if err != nil {
		t.Fatal(err)
	}
	scope, err := NewScope(context.Background(), operation, view, Settings{Slides: SlideSettings{Patterns: []string{"**"}}})
	if err != nil {
		t.Fatal(err)
	}
	document, err := scope.Document(context.Background(), original.ID())
	if err != nil {
		t.Fatal(err)
	}
	out, err := scope.Slides(context.Background(), document, "talk.md", true)
	if err != nil || out.Status() != Unavailable || out.Document().Source() != (parser.Source{}) {
		t.Fatal("unavailable Saved borrowed original", out, err)
	}
	if _, matched := out.MatchedPattern(); matched {
		t.Fatal("unavailable Saved gained annotation")
	}
}

func TestSlidesStopsAndWholePolicyValidation(t *testing.T) {
	scope, document, operation := slideFixture(t, context.Background(), []string{"**"})
	_, err := NewScope(context.Background(), operation, document.View(), Settings{Slides: SlideSettings{Patterns: []string{"**", "["}}})
	var invalid *config.PatternError
	if !errors.As(err, &invalid) || invalid.Index != 1 {
		t.Fatal("invalid later pattern hidden by first match", err)
	}
	if _, err := scope.Slides(nil, document, "a.md", true); !errors.Is(err, ErrContract) { //nolint:staticcheck // Exercise the rejected nil-context contract.
		t.Fatal("nil context accepted", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = operation.Budget().ChargeWork(operation.Budget().Limits().WorkUnits)
	out, err := scope.Slides(ctx, document, "a.md", true)
	var limit *parser.LimitExceeded
	if out.Status() != Unavailable || !errors.Is(err, context.Canceled) || !errors.As(err, &limit) || limit.Name != "work-units" {
		t.Fatal("actual caller/quota stops lost or became ordinary", out, err)
	}
	opContext, stopOperation := context.WithCancel(context.Background())
	scope, document, _ = slideFixture(t, opContext, []string{"**"})
	stopOperation()
	out, err = scope.Slides(context.Background(), document, "a.md", true)
	if out.Status() != Unavailable || !errors.Is(err, context.Canceled) {
		t.Fatal("operation stop lost", out, err)
	}
}

func TestSlideScopeOriginValidationReservesOriginalWork(t *testing.T) {
	for _, field := range []string{"path", "property"} {
		for _, suffix := range []string{"", "\x00", "\xff"} {
			t.Run(field+"/"+strings.ReplaceAll(suffix, "\x00", "NUL"), func(t *testing.T) {
				origin := config.Origin{}
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
				root, err := resource.NewRoot(resource.SuppliedRoot)
				if err != nil {
					t.Fatal(err)
				}
				settings, err := resource.NewSettings(parser.ParseOptions{}, "index.md", "github", "slide-origin-test")
				if err != nil {
					t.Fatal(err)
				}
				operation, err := resource.NewOperation(context.Background(), budget, root, settings, nil)
				if err != nil {
					t.Fatal(err)
				}
				view, err := operation.NewView(resource.Original, nil)
				if err != nil {
					t.Fatal(err)
				}
				scope, err := NewScope(context.Background(), operation, view, Settings{Slides: SlideSettings{Patterns: []string{"**"}, Origin: origin}})
				var limit *parser.LimitExceeded
				if scope != nil || !errors.As(err, &limit) || limit.Name != "work-units" {
					t.Fatalf("public NewScope inspected origin before original work reservation: scope=%v err=%v remaining60=%v", scope, err, budget.ChargeWork(60))
				}
				if err := budget.ChargeWork(0); !errors.As(err, &limit) || limit.Name != "work-units" {
					t.Fatal("original reservation was not sticky", err)
				}
			})
		}
	}
}

func TestSlideScopeMalformedOriginRetainsContract(t *testing.T) {
	_, document, operation := slideFixture(t, context.Background(), nil)
	for _, origin := range []config.Origin{
		{Path: "bad\x00"}, {Path: "bad\xff"}, {PropertyPath: "bad\x00"}, {PropertyPath: "bad\xff"},
		{Position: &parser.Position{Line: 0, Column: 1}}, {Position: &parser.Position{Line: 1, Column: 0}},
	} {
		scope, err := NewScope(context.Background(), operation, document.View(), Settings{Slides: SlideSettings{Patterns: []string{"**"}, Origin: origin}})
		if scope != nil || !errors.Is(err, ErrContract) {
			t.Fatal("malformed selected origin lost scoped contract identity", scope, err)
		}
	}
}

func TestSlideScopePatternLimitsPrecedeOriginBounds(t *testing.T) {
	for _, patterns := range [][]string{make([]string, config.MaxPatterns+1), {strings.Repeat("a", config.MaxPatternBytes+1)}} {
		limits := parser.DefaultLimits()
		limits.DocumentBytes = 128
		budget, err := parser.NewOperationBudget(limits)
		if err != nil {
			t.Fatal(err)
		}
		root, err := resource.NewRoot(resource.SuppliedRoot)
		if err != nil {
			t.Fatal(err)
		}
		settings, err := resource.NewSettings(parser.ParseOptions{}, "index.md", "github", "slide-pattern-test")
		if err != nil {
			t.Fatal(err)
		}
		operation, err := resource.NewOperation(context.Background(), budget, root, settings, nil)
		if err != nil {
			t.Fatal(err)
		}
		view, err := operation.NewView(resource.Original, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = NewScope(context.Background(), operation, view, Settings{Slides: SlideSettings{Patterns: patterns, Origin: config.Origin{Path: strings.Repeat("a", 129)}}})
		var invalid *config.PatternError
		if !errors.Is(err, config.ErrPatternLimit) || len(patterns) == 1 && (!errors.As(err, &invalid) || invalid.Index != 0) {
			t.Fatal("scoped pattern limit precedence lost to origin bound", err)
		}
	}
}
