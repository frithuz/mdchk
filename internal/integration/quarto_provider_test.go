package integration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/resource"
)

type quartoFixture struct {
	scope      *Scope
	doc        Document
	service    *resource.Service
	operation  *resource.Operation
	view       resource.View
	nativeName string
}

func newQuartoFixture(t *testing.T, ctx context.Context, raw string, options parser.ParseOptions, enabled bool, key string, profiles ...parser.Limits) quartoFixture {
	t.Helper()
	root, _ := resource.NewRoot(resource.SuppliedRoot)
	limits := parser.DefaultLimits()
	if len(profiles) != 0 {
		limits = profiles[0]
	}
	budget, err := parser.NewOperationBudget(limits)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := resource.NewSettings(options, "index.md", "github", "quarto-provider-test")
	if err != nil {
		t.Fatal(err)
	}
	source := parser.NewSource([]byte(raw))
	var resourceKey resource.Key
	if key != "" && key != "@metadata" {
		resourceKey, err = resource.NewKey(key)
		if err != nil {
			t.Fatal(err)
		}
	}
	target := resource.Target{ID: "selected.qmd", Source: source, ResourceKey: resourceKey}
	var native *resource.FilesystemRoot
	var proof resource.FilesystemTarget
	nativeName := ""
	if key == "" || key == "@metadata" {
		dir, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		outside, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.Join(outside, "outside.qmd")
		if key == "@metadata" {
			name = filepath.Join(dir, ".git", "selected.qmd")
			if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
				t.Fatal(err)
			}
		}
		nativeName = name
		if err := os.WriteFile(name, source.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		native, err = resource.OpenFilesystemRoot(ctx, budget, dir, resource.DefaultFilesystemLimits())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := native.Close(); err != nil {
				t.Error(err)
			}
		})
		proof, err = native.BindTarget(name, "selected.qmd", source)
		if err != nil {
			t.Fatal(err)
		}
		target, root = proof.Target(), native.Root()
		if target.ResourceKey != (resource.Key{}) {
			t.Fatal("outside source gained a key")
		}
	}
	op, err := resource.NewOperation(ctx, budget, root, policy, []resource.Target{target})
	if err != nil {
		t.Fatal(err)
	}
	var seeds []resource.SuppliedFile
	if key != "" && key != "@metadata" {
		seeds = []resource.SuppliedFile{{Key: resourceKey, Source: source}}
	}
	var service *resource.Service
	if native != nil {
		reader, readErr := native.Reader(op, proof)
		if readErr != nil {
			t.Fatal(readErr)
		}
		service, err = resource.NewFilesystemService(reader, resource.DefaultServiceLimits())
	} else {
		input, decodeErr := resource.DecodeSupplied(ctx, budget, resource.DefaultInputLimits(), nil, seeds)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		reader, readErr := resource.NewSuppliedReader(ctx, op, input, resource.DefaultInputLimits())
		if readErr != nil {
			t.Fatal(readErr)
		}
		service, err = resource.NewSuppliedService(reader, resource.DefaultServiceLimits())
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Error(err)
		}
	})
	view, err := op.NewView(resource.Original, nil)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := NewScope(ctx, op, view, Settings{QuartoEnabled: enabled})
	if err != nil {
		t.Fatal(err)
	}
	scope, err = scope.WithQuarto(ctx, service)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := scope.Document(ctx, "selected.qmd")
	if err != nil {
		t.Fatal(err)
	}
	return quartoFixture{scope, doc, service, op, view, nativeName}
}

// This fixture is selected by the actual three-OS native matrix, alongside
// resource confinement tests. Pure policy fixtures remain outside that filter.
func TestFilesystemQuartoSelectedNoKeyViewsAndAnchorIndependence(t *testing.T) {
	for _, test := range []struct{ name, key string }{{"outside", ""}, {"metadata", "@metadata"}} {
		t.Run(test.name, func(t *testing.T) {
			raw := "<svg><title><span id=x>foreign</span></title></svg>\n\n```{r original}\n```\n"
			f := newQuartoFixture(t, context.Background(), raw, parser.ParseOptions{GFM: true}, true, test.key)
			if err := os.Remove(f.nativeName); err != nil {
				t.Fatal(err)
			} // selected snapshot is the sole content authority
			type readyView struct {
				view   resource.View
				scope  *Scope
				doc    Document
				source parser.Source
			}
			var ready []readyView
			for _, phase := range []resource.Phase{resource.Original, resource.Candidate, resource.Saved} {
				want := "original"
				source := f.doc.Source()
				var replacements []resource.Replacement
				if phase != resource.Original {
					want = string(phase)
					source = parser.NewSource([]byte("<svg><title><span id=x>foreign</span></title></svg>\n\n```{r " + want + "}\n```\n"))
					replacements = []resource.Replacement{{TargetID: "selected.qmd", Source: source}}
				}
				view, err := f.operation.NewView(phase, replacements)
				if err != nil {
					t.Fatal(err)
				}
				scope, err := NewScope(context.Background(), f.operation, view, Settings{QuartoEnabled: true})
				if err != nil {
					t.Fatal(err)
				}
				scope, err = scope.WithQuarto(context.Background(), f.service)
				if err != nil {
					t.Fatal(err)
				}
				doc, err := scope.Document(context.Background(), "selected.qmd")
				if err != nil {
					t.Fatal(err)
				}
				anchor, err := f.service.TargetAnchors(context.Background(), view, "selected.qmd")
				if err != nil || anchor.Availability() != resource.Uncertain {
					t.Fatal(anchor.Availability(), err)
				}
				out, err := scope.Quarto(context.Background(), doc)
				if err != nil || !out.Complete() {
					t.Fatal(out.Status(), err)
				}
				cell, ok := out.KnownCell(0)
				if !ok || out.KnownCellCount() != 1 || cell.Labels().Effective().Text() != want || cell.Document().Source().Token() != source.Token() {
					t.Fatal("native selected source changed", out, cell, want)
				}
				ready = append(ready, readyView{view, scope, doc, source})
				if f.operation.Targets()[0].ResourceKey != (resource.Key{}) || doc.View().Root() != f.operation.Root() {
					t.Fatal("NoKey Quarto widened authority")
				}
			}
			saved, err := f.operation.NewView(resource.Saved, []resource.Replacement{{TargetID: "selected.qmd", Unavailable: true}})
			if err != nil {
				t.Fatal(err)
			}
			scope, _ := NewScope(context.Background(), f.operation, saved, Settings{QuartoEnabled: true})
			scope, _ = scope.WithQuarto(context.Background(), f.service)
			doc, _ := scope.Document(context.Background(), "selected.qmd")
			out, err := scope.Quarto(context.Background(), doc)
			if err != nil || out.Complete() || out.KnownCellCount() != 0 {
				t.Fatal("native saved fallback", out, err)
			}
			// Native selected snapshots remain ready after actual stops; a new cell
			// query still cannot assemble from that retained completed parse.
			caller, cancel := context.WithCancel(context.Background())
			cancel()
			for stage := 0; stage < 3; stage++ {
				var want *parser.LimitExceeded
				if stage == 1 {
					if err := f.service.Close(); err != nil {
						t.Fatal(err)
					}
				}
				if stage == 2 {
					if err := f.operation.Budget().ChargeWork(f.operation.Budget().Limits().WorkUnits + 1); !errors.As(err, &want) {
						t.Fatal(err)
					}
				}
				for _, r := range ready {
					got, err := f.service.TargetParse(caller, r.view, "selected.qmd")
					parsed, ok := got.Parsed()
					if !errors.Is(err, context.Canceled) || got.Availability() != resource.Available || got.Source().Token() != r.source.Token() || !ok || parsed.Status != parser.Complete {
						t.Fatal("native ready parse lost after stop", stage, got, err)
					}
					var actual *parser.LimitExceeded
					if want != nil && (!errors.As(err, &actual) || *actual != *want) {
						t.Fatal("native first quota lost", err)
					}
					stopped, err := r.scope.Quarto(caller, r.doc)
					if _, known := stopped.Parsed(); known {
						t.Fatal("entry-stopped query claimed it acquired a parse")
					}
					if !errors.Is(err, context.Canceled) || stopped.Status() != Unavailable || stopped.Complete() || stopped.KnownCellCount() != 0 {
						t.Fatal("stopped native query assembled cells", stopped, err)
					}
				}
			}
		})
	}
}

func providerCells(t *testing.T, f quartoFixture) (QuartoContext, []Cell) {
	t.Helper()
	out, err := f.scope.Quarto(context.Background(), f.doc)
	if err != nil {
		t.Fatal(err)
	}
	cells, err := out.Cells(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return out, cells
}

func TestQuartoProviderActualSyntaxAndRawHeaderPolicy(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, frontmatter := range []bool{false, true} {
			fm, err := parser.NewFrontmatterOptions(frontmatter, []string{"yaml"})
			if err != nil {
				t.Fatal(err)
			}
			options := parser.ParseOptions{GFM: gfm, Frontmatter: fm}
			for _, tc := range []struct {
				name, raw string
				count     int
				status    Status
				value     string
			}{
				{"python", "```{python}\n#| label: py\nprint(1)\n```\n", 1, Available, "py"},
				{"julia-tilde", "~~~~{julia}\n#| label: julia\n~~~~\n", 1, Available, "julia"},
				{"r-header", "```{r, label = 'old label', eval=FALSE}\n```\n", 1, Available, "old label"},
				{"r-position", "```{r header, echo=FALSE}\n```\n", 1, Available, "header"},
				{"missing", "```{r}\nprint(1)\n```\n", 1, Available, ""},
				{"eof", "```{r last}\nprint(1)", 1, Available, "last"},
				{"ordinary", "```python\n#| label: no\n```\n", 0, Available, ""},
				{"escaped-example", "```{{python}}\n#| label: no\n```\n", 0, Available, ""},
				{"entity-braces", "~~~&#123;python&#125;\n#| label: no\n~~~\n", 0, Available, ""},
				{"escaped-braces", "~~~\\{python\\}\n#| label: no\n~~~\n", 0, Available, ""},
				{"enclosing-example", "````markdown\n```{r}\n#| label: no\n```\n````\n", 0, Available, ""},
				{"indented-example", "    ```{r}\n    #| label: no\n    ```\n", 0, Available, ""},
				{"html-example", "<pre>\n```{r}\n#| label: no\n```\n</pre>\n", 0, Available, ""},
				{"unsupported", "```{ruby}\n#| label: value\n```\n", 1, Unavailable, "value"},
			} {
				t.Run(tc.name+"/"+map[bool]string{false: "cm", true: "gfm"}[gfm]+"/"+map[bool]string{false: "no-fm", true: "fm"}[frontmatter], func(t *testing.T) {
					f := newQuartoFixture(t, context.Background(), tc.raw, options, true, "selected.qmd")
					out, cells := providerCells(t, f)
					if out.Status() != tc.status || len(cells) != tc.count {
						t.Fatal(out.Status(), len(cells), tc)
					}
					if len(cells) > 0 && cells[0].Labels().Effective().Text() != tc.value {
						t.Fatal(cells[0].Labels(), tc.value)
					}
					if string(f.doc.Source().Bytes()) != tc.raw {
						t.Fatal("cell source changed")
					}
				})
			}
		}
	}
	// Disabled recognition performs no hidden parse/provider work, even for
	// malformed options and a closed parse service.
	f := newQuartoFixture(t, context.Background(), "```{r}\n#| label: [broken\n```\n", parser.ParseOptions{}, false, "selected.qmd")
	if err := f.service.Close(); err != nil {
		t.Fatal(err)
	}
	out, cells := providerCells(t, f)
	if out.Status() != Inactive || len(cells) != 0 {
		t.Fatal(out, cells)
	}
}

func TestQuartoProviderGoldenContainerCRLFLabelSpans(t *testing.T) {
	raw := "雪\r\n\r\n> - ```{r label='old label'}\r\n>   #| label: ''\r\n>   print(1)\r\n>   #| label: later\r\n>   ```\r\n\r\n~~~{python}\r\n#| label: \"雪\\u2003図\"\r\n~~~\r\n"
	f := newQuartoFixture(t, context.Background(), raw, parser.ParseOptions{GFM: true}, true, "selected.qmd")
	out, cells := providerCells(t, f)
	if !out.Complete() || len(cells) != 2 {
		t.Fatal(out.Status(), cells)
	}
	first := cells[0]
	if first.Language() != "r" || first.Labels().Effective().Kind() != LabelString || first.Labels().Effective().Text() != "" {
		t.Fatal(first.Labels())
	}
	declared, err := first.DeclaredLabels(context.Background())
	if err != nil || len(declared) != 2 {
		t.Fatal(declared, err)
	}
	for i, spelling := range []string{"'old label'", "''"} {
		start := strings.Index(raw, spelling)
		span := declared[i].Location().Span()
		if span.Start() != start || span.End() != start+len(spelling) {
			t.Fatalf("declared%d [%d,%d) want [%d,%d)", i, span.Start(), span.End(), start, start+len(spelling))
		}
	}
	body, err := first.Content(context.Background())
	if err != nil || body.Semantic.Value != "#| label: ''\nprint(1)\n#| label: later\n" {
		t.Fatal(body, err)
	}
	containers, _ := first.Containers(context.Background())
	if len(containers) != 3 || containers[0].Kind != parser.BlockQuote || containers[1].Kind != parser.List || containers[2].Kind != parser.ListItem {
		t.Fatal(containers)
	}
	if cells[1].Labels().Effective().Text() != "雪\u2003図" {
		t.Fatal(cells[1].Labels())
	}
	span := cells[1].Labels().Effective().Location().Span()
	spelling := "\"雪\\u2003図\""
	start := strings.Index(raw, spelling)
	if span.Start() != start || span.End() != start+len(spelling) {
		t.Fatal(span.Start(), span.End(), start)
	}
	declared[0] = Label{}
	body.Segments[0].Semantic = parser.Some("changed")
	again, _ := first.DeclaredLabels(context.Background())
	againBody, _ := first.Content(context.Background())
	if again[0].Text() != "old label" || againBody.Semantic.Value != "#| label: ''\nprint(1)\n#| label: later\n" || string(f.doc.Source().Bytes()) != raw {
		t.Fatal("mutable provider output")
	}
}

func TestQuartoProviderPartialLabelsAliasesAndIndependentAnchors(t *testing.T) {
	raw := "<svg><title><span id=x>foreign</span></title></svg>\n\n```{r label='header space'}\n#| label: 'first space'\n#| label: second\n```\n\n```{python}\n#| base: &x 'okay'\n#| label: *x\n```\n"
	f := newQuartoFixture(t, context.Background(), raw, parser.ParseOptions{}, true, "")
	anchors, err := f.service.TargetAnchors(context.Background(), f.view, "selected.qmd")
	if err != nil || anchors.Availability() != resource.Uncertain {
		t.Fatal(anchors.Availability(), err)
	}
	out, cells := providerCells(t, f)
	if out.Status() != Unavailable || len(cells) != 2 || cells[0].Status() != Unavailable || cells[1].Status() != Available || cells[1].Labels().Effective().Text() != "okay" {
		t.Fatal(out, cells)
	}
	declared, _ := cells[0].DeclaredLabels(context.Background())
	if len(declared) != 3 || declared[0].Text() != "header space" || declared[1].Text() != "first space" || declared[2].Text() != "second" {
		t.Fatal(declared)
	}
	problems, err := out.Problems(context.Background())
	if err != nil || len(problems) == 0 {
		t.Fatal(problems, err)
	}
	parsed, present := out.Parsed()
	if !present || parsed.Availability() != resource.Available || parsed.Source().Token() != f.doc.Source().Token() {
		t.Fatal(parsed, present)
	}
	for _, ending := range []string{"\n", "\r\n", "\r"} {
		text := strings.ReplaceAll("```{r}\n#| label: |\n#|   雪\n#|   図\n```\n", "\n", ending)
		g := newQuartoFixture(t, context.Background(), text, parser.ParseOptions{}, true, "selected.qmd")
		got, known := providerCells(t, g)
		if !got.Complete() || len(known) != 1 || known[0].Labels().Effective().Text() != "雪\n図\n" {
			t.Fatal(got.Status(), known)
		}
	}
}

func TestQuartoProviderDecodedInputsAndUnknownContexts(t *testing.T) {
	for _, test := range []struct {
		name, body   string
		kind         LabelKind
		value        string
		status       Status
		declarations int
	}{
		{"blank", "#| label: ''\n", LabelString, "", Available, 2},
		{"whitespace", "#| label: ' \u2003 '\n", LabelString, " \u2003 ", Available, 2},
		{"quoted-number", "#| label: '001'\n", LabelString, "001", Available, 2},
		{"number", "#| label: 1\n", LabelNonString, "", Unavailable, 1},
		{"null", "#| label: null\n", LabelNonString, "", Unavailable, 1},
		{"bool", "#| label: false\n", LabelNonString, "", Unavailable, 1},
		{"literal-entity", "#| label: '&amp;'\n", LabelString, "&amp;", Available, 2},
		{"unicode-replacement", "#| label: '�'\n", LabelString, "�", Available, 2},
		{"raw-nul", "#| label: 'a\x00b'\n", LabelUnknown, "", Unavailable, 1},
		{"folded", "#| label: >-\n#|   first\n#|   second\n", LabelString, "first second", Available, 2},
		{"unrelated-duplicate", "#| x: 1\n#| x: 2\n#| label: okay\n", LabelUnknown, "", Unavailable, 2},
		{"tag", "#| label: !execute nope\n", LabelUnknown, "", Unavailable, 1},
		{"cycle", "#| x: &x [*x]\n#| label: okay\n", LabelUnknown, "", Unavailable, 2},
		{"late", "print(1)\n#| label: late\n", LabelString, "header space", Available, 1},
		{"blank-boundary", "\n#| label: late\n", LabelString, "header space", Available, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newQuartoFixture(t, context.Background(), "```{r label='header space'}\n"+test.body+"```\n", parser.ParseOptions{}, true, "selected.qmd")
			out, cells := providerCells(t, f)
			if out.Status() != test.status || len(cells) != 1 || cells[0].Labels().Effective().Kind() != test.kind || cells[0].Labels().Effective().Text() != test.value {
				t.Fatal(out.Status(), cells, test)
			}
			declared, err := cells[0].DeclaredLabels(context.Background())
			if err != nil || len(declared) != test.declarations {
				t.Fatal(declared, err, test)
			}
			if test.status == Unavailable {
				problems, err := out.Problems(context.Background())
				if err != nil || len(problems) == 0 {
					t.Fatal("unlocated unavailable interpretation", problems, err)
				}
			}
		})
	}
	text := "```{r A}\n```\n\n```{python}\n#| label: a\n```\n\n```{julia}\n#| label: é\n```\n\n```{python}\n#| label: é\n```\n\n```{r A}\n```\n"
	f := newQuartoFixture(t, context.Background(), text, parser.ParseOptions{}, true, "selected.qmd")
	out, cells := providerCells(t, f)
	if !out.Complete() || len(cells) != 5 {
		t.Fatal(out, cells)
	}
	for i, want := range []string{"A", "a", "é", "é", "A"} {
		if cells[i].Labels().Effective().Text() != want {
			t.Fatal("uniqueness input changed", i, cells[i].Labels(), want)
		}
	}
}

func TestQuartoProviderFrontmatterAndRealProjectionLimits(t *testing.T) {
	raw := "---\n```{r hidden}\n```\n---\n\n```{python}\n#| label: visible\n```\n"
	fm, _ := parser.NewFrontmatterOptions(true, []string{"yaml"})
	f := newQuartoFixture(t, context.Background(), raw, parser.ParseOptions{Frontmatter: fm}, true, "selected.qmd")
	out, cells := providerCells(t, f)
	if !out.Complete() || len(cells) != 1 || cells[0].Labels().Effective().Text() != "visible" {
		t.Fatal("frontmatter code became a cell", out, cells)
	}
	limits := parser.DefaultLimits()
	limits.SyntaxDepth = 4
	g := newQuartoFixture(t, context.Background(), "```{r}\n#| x: [[[[[1]]]]]\n#| label: value\n```\n", parser.ParseOptions{}, true, "selected.qmd", limits)
	got, err := g.scope.Quarto(context.Background(), g.doc)
	var limit *parser.LimitExceeded
	if !errors.As(err, &limit) || limit.Name != "structured-input-depth" || got.Complete() || got.KnownCellCount() != 0 {
		t.Fatal("option depth became clean/malformed", got, err)
	}
	if err := f.operation.Budget().ChargeWork(f.operation.Budget().Limits().WorkUnits + 1); err == nil {
		t.Fatal("quota not exhausted")
	}
	if copy, err := out.Cells(context.Background()); !errors.As(err, &limit) || len(copy) != 0 || !out.Complete() || out.KnownCellCount() != 1 {
		t.Fatal("later inspection rewrote retained completion", out, copy, err)
	}
	if known, ok := out.KnownCell(0); !ok || known.Labels().Effective().Text() != "visible" {
		t.Fatal("known fact lost", known, ok)
	}
}

func FuzzQuartoProviderBoundedOwnership(f *testing.F) {
	for _, seed := range []string{"```{r label='a b'}\n#| label: ''\n```\n", "> ```{python}\r\n> #| base: &x '雪'\r\n> #| label: *x\r\n> ```\r\n", "````markdown\n```{r}\n```\n````\n", "```{r}\n#| x: &x [*x]\n```\n"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 4096 || !utf8.ValidString(raw) {
			return
		}
		limits := parser.DefaultLimits()
		limits.WorkUnits = 500000
		limits.SyntaxDepth = 32
		fixture := newQuartoFixture(t, context.Background(), raw, parser.ParseOptions{GFM: true}, true, "selected.qmd", limits)
		out, err := fixture.scope.Quarto(context.Background(), fixture.doc)
		if err != nil && out.Complete() {
			t.Fatal("failed query complete", err)
		}
		for i := 0; i < out.KnownCellCount(); i++ {
			cell, ok := out.KnownCell(i)
			if !ok || cell.Document().Source().Token() != fixture.doc.Source().Token() {
				t.Fatal("foreign cell")
			}
			span := cell.Envelope().Span()
			if !span.Present() || span.SourceToken() != fixture.doc.Source().Token() || span.Start() < 0 || span.End() > len(raw) {
				t.Fatal("invalid cell span", span)
			}
			if out.Complete() && (cell.Status() != Available || cell.Labels().Status() != Available || !cell.Labels().DeclarationsComplete()) {
				t.Fatal("unavailable fact in complete collection")
			}
		}
		if string(fixture.doc.Source().Bytes()) != raw {
			t.Fatal("source mutation")
		}
	})
}

func TestQuartoProviderViewsBindingAndImmutableParseAuthority(t *testing.T) {
	f := newQuartoFixture(t, context.Background(), "```{r original}\n```\n", parser.ParseOptions{}, true, "")
	policyOnly, _ := NewScope(context.Background(), f.operation, f.view, Settings{QuartoEnabled: true})
	oldDocument, _ := policyOnly.Document(context.Background(), "selected.qmd")
	if _, err := f.scope.Quarto(context.Background(), oldDocument); !errors.Is(err, ErrContract) {
		t.Fatal("old scope borrowed binding", err)
	}
	foreign := newQuartoFixture(t, context.Background(), "```{r original}\n```\n", parser.ParseOptions{}, true, "")
	if _, err := f.scope.WithQuarto(context.Background(), foreign.service); !errors.Is(err, ErrContract) {
		t.Fatal("foreign service", err)
	}
	if _, err := f.scope.WithQuarto(context.Background(), nil); !errors.Is(err, ErrContract) {
		t.Fatal("nil service", err)
	}
	for _, phase := range []resource.Phase{resource.Original, resource.Candidate, resource.Saved} {
		source := f.doc.Source()
		var replacements []resource.Replacement
		want := "original"
		if phase != resource.Original {
			want = string(phase)
			source = parser.NewSource([]byte("```{r " + want + "}\n```\n"))
			replacements = []resource.Replacement{{TargetID: "selected.qmd", Source: source}}
		}
		view, err := f.operation.NewView(phase, replacements)
		if err != nil {
			t.Fatal(err)
		}
		scope, err := NewScope(context.Background(), f.operation, view, Settings{QuartoEnabled: true})
		if err != nil {
			t.Fatal(err)
		}
		scope, err = scope.WithQuarto(context.Background(), f.service)
		if err != nil {
			t.Fatal(err)
		}
		doc, err := scope.Document(context.Background(), "selected.qmd")
		if err != nil {
			t.Fatal(err)
		}
		out, err := scope.Quarto(context.Background(), doc)
		if err != nil {
			t.Fatal(err)
		}
		cells, _ := out.Cells(context.Background())
		if len(cells) != 1 || cells[0].Labels().Effective().Text() != want || cells[0].Document().Source().Token() != source.Token() {
			t.Fatal(out, cells, want)
		}
		if _, err := scope.Quarto(context.Background(), f.doc); !errors.Is(err, ErrContract) {
			t.Fatal("stale view/scope accepted", err)
		}
	}
	saved, err := f.operation.NewView(resource.Saved, []resource.Replacement{{TargetID: "selected.qmd", Unavailable: true}})
	if err != nil {
		t.Fatal(err)
	}
	scope, _ := NewScope(context.Background(), f.operation, saved, Settings{QuartoEnabled: true})
	scope, _ = scope.WithQuarto(context.Background(), f.service)
	doc, _ := scope.Document(context.Background(), "selected.qmd")
	out, err := scope.Quarto(context.Background(), doc)
	if err != nil || out.Status() != Unavailable || out.KnownCellCount() != 0 {
		t.Fatal("unavailable saved fallback", out, err)
	}
}

type closeAssemblyContext struct {
	context.Context
	calls  int
	closed bool
	close  func()
}

func (c *closeAssemblyContext) Err() error {
	c.calls++
	if !c.closed {
		// Trigger the real Close only once the actual bounded option adapter
		// is running, rather than depending on an arbitrary checkpoint count.
		var callers [32]uintptr
		frames := runtime.CallersFrames(callers[:runtime.Callers(2, callers[:])])
		for {
			frame, more := frames.Next()
			if strings.HasSuffix(frame.Function, ".decodeQuartoOptions") {
				c.closed = true
				c.close()
				break
			}
			if !more {
				break
			}
		}
	}
	return c.Context.Err()
}

func TestQuartoProviderCloseAndRealCombinedStops(t *testing.T) {
	f := newQuartoFixture(t, context.Background(), "```{r ready}\n#| label: value\n```\n", parser.ParseOptions{}, true, "selected.qmd")
	warm, _ := providerCells(t, f)
	parsed, _ := warm.Parsed()
	ctx := &closeAssemblyContext{Context: context.Background(), close: func() {
		if err := f.service.Close(); err != nil {
			t.Fatal(err)
		}
	}}
	out, err := f.scope.Quarto(ctx, f.doc)
	if !ctx.closed || !errors.Is(err, context.Canceled) || out.Status() != Unavailable {
		t.Fatal("close became complete", out.Status(), err, ctx.calls)
	}
	if current, known := out.Parsed(); !known || current.Availability() != resource.Available {
		t.Fatal("assembly lost already acquired parse", current, known)
	}
	if _, ok := parsed.Parsed(); !ok {
		t.Fatal("close erased acquired parse")
	}
	out, err = f.scope.Quarto(context.Background(), f.doc)
	if !errors.Is(err, context.Canceled) || out.Status() != Unavailable {
		t.Fatal("closed service reused", out, err)
	}
	synctest.Test(t, func(t *testing.T) {
		opctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		g := newQuartoFixture(t, opctx, "```{r ready}\n```\n", parser.ParseOptions{}, true, "selected.qmd")
		_, _ = providerCells(t, g)
		quota := g.operation.Budget().ChargeWork(g.operation.Budget().Limits().WorkUnits + 1)
		var want *parser.LimitExceeded
		if !errors.As(quota, &want) {
			t.Fatal(quota)
		}
		time.Sleep(2 * time.Second)
		caller, end := context.WithCancel(context.Background())
		end()
		got, err := g.scope.Quarto(caller, g.doc)
		var actual *parser.LimitExceeded
		if got.Status() != Unavailable || !errors.Is(err, context.Canceled) || !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &actual) || *actual != *want {
			t.Fatal("combined controls lost", got.Status(), err)
		}
	})
}

func TestQuartoProviderConcurrentSharedParsingAndCallerStop(t *testing.T) {
	f := newQuartoFixture(t, context.Background(), "```{r shared}\n#| label: same\n```\n", parser.ParseOptions{}, true, "selected.qmd")
	var group sync.WaitGroup
	results := make(chan parser.SourceToken, 8)
	for range 8 {
		group.Go(func() {
			out, err := f.scope.Quarto(context.Background(), f.doc)
			if err != nil {
				t.Error(err)
				return
			}
			cells, err := out.Cells(context.Background())
			if err != nil || len(cells) != 1 || cells[0].Labels().Effective().Text() != "same" {
				t.Error(out, cells, err)
				return
			}
			parsed, _ := out.Parsed()
			results <- parsed.Source().Token()
		})
	}
	group.Wait()
	close(results)
	for token := range results {
		if token != f.doc.Source().Token() {
			t.Fatal("foreign shared source")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if out, err := f.scope.Quarto(ctx, f.doc); !errors.Is(err, context.Canceled) || out.Complete() {
		t.Fatal(out, err)
	}
	if out, _ := providerCells(t, f); !out.Complete() {
		t.Fatal("one caller poisoned producer")
	}
}

type targetParseQuotaContext struct {
	context.Context
	stop    func()
	stopped bool
}

func (c *targetParseQuotaContext) Err() error {
	if !c.stopped {
		var callers [32]uintptr
		frames := runtime.CallersFrames(callers[:runtime.Callers(2, callers[:])])
		for {
			frame, more := frames.Next()
			if strings.HasSuffix(frame.Function, ".(*Service).TargetParse") {
				c.stopped = true
				c.stop()
				break
			}
			if !more {
				break
			}
		}
	}
	return c.Context.Err()
}

func TestQuartoProviderReadyParseAtStoppedTargetBoundaryDoesNotAssemble(t *testing.T) {
	f := newQuartoFixture(t, context.Background(), "```{r retained}\n#| label: precise\n```\n", parser.ParseOptions{}, true, "selected.qmd")
	warm, _ := providerCells(t, f)
	expected, _ := warm.Parsed()
	var want *parser.LimitExceeded
	ctx := &targetParseQuotaContext{Context: context.Background(), stop: func() {
		if err := f.operation.Budget().ChargeWork(f.operation.Budget().Limits().WorkUnits + 1); !errors.As(err, &want) {
			t.Fatal(err)
		}
	}}
	got, err := f.scope.Quarto(ctx, f.doc)
	var actual *parser.LimitExceeded
	retained, known := got.Parsed()
	if !ctx.stopped || !errors.As(err, &actual) || *actual != *want || got.Status() != Unavailable || got.Complete() || got.KnownCellCount() != 0 || !known || retained.Availability() != resource.Available || retained.Source().Token() != expected.Source().Token() {
		t.Fatal("ready parse became a completed new query or was erased", got, err, known, ctx.stopped)
	}
	parsed, ok := retained.Parsed()
	original, _ := expected.Parsed()
	if !ok || parsed.Status != parser.Complete || parsed.Document != original.Document {
		t.Fatal("retained parse witness changed", parsed, ok)
	}
	if warm.KnownCellCount() != 1 || !warm.Complete() {
		t.Fatal("stopped query changed prior complete facts")
	}
}

func TestQuartoProviderOrdinaryCoreIntegerOptions(t *testing.T) {
	for _, value := range []string{"0x10", "0xAF", "0o10", "-12", "+12"} {
		for _, prefix := range []string{"", "!!int ", "!<tag:yaml.org,2002:int> "} {
			t.Run(prefix+value, func(t *testing.T) {
				raw := "雪\r\n\r\n```{python}\r\n#| other: " + prefix + value + "\r\n#| label: okay\r\n```\r\n"
				f := newQuartoFixture(t, context.Background(), raw, parser.ParseOptions{}, true, "selected.qmd")
				out, cells := providerCells(t, f)
				if !out.Complete() || len(cells) != 1 || cells[0].Status() != Available || cells[0].Labels().Effective().Kind() != LabelString || cells[0].Labels().Effective().Text() != "okay" {
					t.Fatal("ordinary core integer invalidated label", prefix, value, out.Status(), cells)
				}
				label := cells[0].Labels().Effective()
				span := label.Location().Span()
				start := strings.Index(raw, "okay")
				if span.Start() != start || span.End() != start+len("okay") || label.Location().Source().Token() != f.doc.Source().Token() || cells[0].Document().Source().Token() != f.doc.Source().Token() || string(f.doc.Source().Bytes()) != raw {
					t.Fatal("core option changed label/source provenance", span, start)
				}
				problems, err := out.Problems(context.Background())
				if err != nil || len(problems) != 0 {
					t.Fatal(problems, err)
				}
			})
		}
	}
	for _, spelling := range []string{"!!int 0x10", "!!int 0o10", "!<tag:yaml.org,2002:int> 0xAF", "!<tag:yaml.org,2002:int> 0o17"} {
		t.Run("numeric-label/"+spelling, func(t *testing.T) {
			raw := "```{r fallback}\n#| label: " + spelling + "\n```\n"
			f := newQuartoFixture(t, context.Background(), raw, parser.ParseOptions{}, true, "selected.qmd")
			out, cells := providerCells(t, f)
			if out.Complete() || out.Status() != Unavailable || len(cells) != 1 || cells[0].Labels().Effective().Kind() != LabelNonString || cells[0].Labels().Effective().Text() != "" {
				t.Fatal("core integer label coerced or lost kind", out.Status(), cells)
			}
			label := cells[0].Labels().Effective()
			span := label.Location().Span()
			start := strings.Index(raw, spelling)
			if span.Start() != start || span.End() != start+len(spelling) || label.Location().Source().Token() != f.doc.Source().Token() {
				t.Fatal("numeric core label lost original range", span, start)
			}
			declared, err := cells[0].DeclaredLabels(context.Background())
			if err != nil || len(declared) != 1 || declared[0].Text() != "fallback" {
				t.Fatal("numeric label fell back or invented declaration", declared, err)
			}
			problems, err := out.Problems(context.Background())
			if err != nil || len(problems) == 0 {
				t.Fatal(problems, err)
			}
			for _, problem := range problems {
				if strings.Contains(problem.Message, "core tag") {
					t.Fatal("ordinary numeric label misclassified as unsupported core", problem)
				}
			}
		})
	}
	for _, spelling := range []string{"!!int 0xGG", "!!int 0o8", "!!int 1.5", "!<tag:yaml.org,2002:int> 0xGG", "!execute 0x10"} {
		t.Run("invalid/"+spelling, func(t *testing.T) {
			raw := "```{r}\n#| other: " + spelling + "\n#| label: okay\n```\n"
			f := newQuartoFixture(t, context.Background(), raw, parser.ParseOptions{}, true, "selected.qmd")
			out, cells := providerCells(t, f)
			if out.Complete() || len(cells) != 1 || cells[0].Labels().Effective().Kind() != LabelUnknown {
				t.Fatal("invalid/custom tag accepted", spelling, out.Status(), cells)
			}
			problems, err := out.Problems(context.Background())
			if err != nil || len(problems) == 0 {
				t.Fatal(problems, err)
			}
			for _, problem := range problems {
				if !problem.Location.Span().Present() || problem.Location.Source().Token() != f.doc.Source().Token() {
					t.Fatal("tag uncertainty lost source", problem)
				}
			}
		})
	}
}

func TestQuartoProviderCoreTagOriginalRanges(t *testing.T) {
	for _, test := range []struct{ name, options, value, spelling string }{
		{"plain-string", "label: !!str okay", "okay", "!!str okay"},
		{"long-string", "label: !<tag:yaml.org,2002:str> \"雪\\u2003図\"", "雪\u2003図", "!<tag:yaml.org,2002:str> \"雪\\u2003図\""},
		{"flow-sibling", "{other: !!int 0x10, label: okay}", "okay", "okay"},
		{"multiple-tags", "{other: !!int 0x10, spare: !!int 0o10, label: !!str '雪'}", "雪", "!!str '雪'"},
		{"extra-space", "label: !!str   'snow'", "snow", "!!str   'snow'"},
		{"next-line", "other: !!int 0x10\nlabel: !!str okay", "okay", "!!str okay"},
	} {
		for _, container := range []struct{ name, open, prefix, close string }{
			{"plain", "```{python}", "#| ", "```"},
			{"quote-list", "> - ```{python}", ">   #| ", ">   ```"},
		} {
			t.Run(test.name+"/"+container.name, func(t *testing.T) {
				raw := "雪\r\n\r\n" + container.open + "\r\n" + container.prefix + strings.ReplaceAll(test.options, "\n", "\r\n"+container.prefix) + "\r\n" + container.close + "\r\n"
				f := newQuartoFixture(t, context.Background(), raw, parser.ParseOptions{}, true, "selected.qmd")
				out, cells := providerCells(t, f)
				if !out.Complete() || len(cells) != 1 || cells[0].Labels().Effective().Text() != test.value {
					t.Fatal("core tag range fixture unavailable", out.Status(), cells)
				}
				label := cells[0].Labels().Effective()
				span := label.Location().Span()
				start := strings.Index(raw, test.spelling)
				if span.Start() != start || span.End() != start+len(test.spelling) || label.Location().Source().Token() != f.doc.Source().Token() {
					t.Fatal("core tag original range drift", span, start, len(test.spelling))
				}
			})
		}
	}
}

func TestQuartoProviderStandardTagSeparators(t *testing.T) {
	for _, separator := range []string{" ", "\t", " \t", "\t \t"} {
		for _, long := range []bool{false, true} {
			integer, text := "!!int", "!!str"
			if long {
				integer, text = "!<tag:yaml.org,2002:int>", "!<tag:yaml.org,2002:str>"
			}
			taggedString := text + separator + "\"雪\t!!str\tpayload\\u2003図\""
			taggedNumber := integer + separator + "0o17"
			for _, test := range []struct {
				name, options, value, spelling string
				kind                           LabelKind
				block                          bool
			}{
				{"other-integer", "other: " + integer + separator + "0x10\nlabel: okay", "okay", "okay", LabelString, false},
				{"other-string", "other: " + text + separator + "'value'\nlabel: okay", "okay", "okay", LabelString, false},
				{"string-label", "label: " + taggedString, "雪\t!!str\tpayload\u2003図", taggedString, LabelString, false},
				{"numeric-label", "label: " + taggedNumber, "", taggedNumber, LabelNonString, false},
				{"flow-multiple", "{other: " + integer + separator + "0x10, spare: !!str\t'!!int\tpayload', label: " + taggedString + "}", "雪\t!!str\tpayload\u2003図", taggedString, LabelString, false},
				{"tagged-block", "label: " + text + separator + "|-\n  雪\t!!str\tpayload", "雪\t!!str\tpayload", text + separator + "|-", LabelString, true},
				{"plain-payload", "other: prefix !!str\tpayload\nlabel: " + text + separator + "okay", "okay", text + separator + "okay", LabelString, false},
				{"comment-payload", "other: " + integer + separator + "0x10 # !!str\tpayload\nlabel: okay", "okay", "okay", LabelString, false},
			} {
				for _, container := range []struct{ name, open, prefix, close string }{
					{"plain", "```{r fallback}", "#| ", "```"},
					{"quote-list", "> - ```{r fallback}", ">   #| ", ">   ```"},
				} {
					t.Run(test.name+"/"+container.name+"/"+text+separator, func(t *testing.T) {
						raw := "雪\r\n\r\n" + container.open + "\r\n" + container.prefix + strings.ReplaceAll(test.options, "\n", "\r\n"+container.prefix) + "\r\n" + container.close + "\r\n"
						f := newQuartoFixture(t, context.Background(), raw, parser.ParseOptions{}, true, "selected.qmd")
						out, cells := providerCells(t, f)
						wantStatus := Available
						if test.kind == LabelNonString {
							wantStatus = Unavailable
						}
						if out.Status() != wantStatus || len(cells) != 1 || cells[0].Labels().Effective().Kind() != test.kind || cells[0].Labels().Effective().Text() != test.value {
							problems, _ := out.Problems(context.Background())
							t.Fatal("standard tag separator changed semantics", test.name, separator, out.Status(), cells[0].Labels().Effective().Kind(), cells[0].Labels().Effective().Text(), problems)
						}
						label := cells[0].Labels().Effective()
						span := label.Location().Span()
						start := strings.LastIndex(raw, test.spelling)
						end := start + len(test.spelling)
						if test.block {
							payload := "雪\t!!str\tpayload\r\n"
							end = strings.Index(raw, payload) + len(payload)
						}
						if span.Start() != start || span.End() != end || label.Location().Source().Token() != f.doc.Source().Token() || cells[0].Document().View().Same(f.view) == false || string(f.doc.Source().Bytes()) != raw {
							t.Fatal("standard tag separator lost authored range/source", span, start, end)
						}
						if test.kind == LabelNonString {
							declared, err := cells[0].DeclaredLabels(context.Background())
							if err != nil || len(declared) != 1 || declared[0].Text() != "fallback" {
								t.Fatal("numeric tag fell back or invented declaration", declared, err)
							}
						}
					})
				}
			}
		}
	}
}

func TestQuartoProviderTagSeparatorPayloadAndInvalidContexts(t *testing.T) {
	for _, test := range []struct {
		name, options, value string
		valid                bool
	}{
		{"quoted-only-trap", "label: '!!str\tpayload'", "!!str\tpayload", true},
		{"block-only-trap", "label: |-\n  !!str\tpayload", "!!str\tpayload", true},
		{"escaped-tab", "label: !!str\t\"a\\tb\"", "a\tb", true},
		{"numeric-invalid", "other: !!int\tnope\nlabel: okay", "", false},
		{"custom", "other: !execute\tvalue\nlabel: okay", "", false},
		{"custom-prefix-trap", "other: !evil!!str\tvalue\nlabel: okay", "", false},
		{"unknown-core", "other: !!unknown\tvalue\nlabel: okay", "", false},
		{"tab-indentation", "\tlabel: okay", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := "```{python}\n#| " + strings.ReplaceAll(test.options, "\n", "\n#| ") + "\n```\n"
			f := newQuartoFixture(t, context.Background(), raw, parser.ParseOptions{}, true, "selected.qmd")
			out, cells := providerCells(t, f)
			if len(cells) != 1 || out.Complete() != test.valid {
				t.Fatal("tag separator changed payload/invalid context", test.name, out.Status(), cells)
			}
			if test.valid {
				if cells[0].Labels().Effective().Text() != test.value {
					t.Fatal("probe changed scalar TAB payload", cells[0].Labels().Effective(), test.value)
				}
			} else {
				problems, err := out.Problems(context.Background())
				if err != nil || len(problems) == 0 {
					t.Fatal("invalid context became clean", problems, err)
				}
			}
			if string(f.doc.Source().Bytes()) != raw {
				t.Fatal("probe changed original bytes")
			}
		})
	}
}

func TestQuartoProviderQuotedTabCursorAndAdjacency(t *testing.T) {
	for _, test := range []struct {
		name, options, value, spelling string
		valid                          bool
	}{
		{"minimal-flow", "{label: \"a\tb\"}", "a\tb", "\"a\tb\"", true},
		{"multiple", "{first: \"p\tq\", label: \"a\tb\", last: \"c\td\"}", "a\tb", "\"a\tb\"", true},
		{"sequence-close", "other: [\"a\tb\"]\nlabel: okay", "okay", "okay", true},
		{"quoted-key-colon", "{\"a\tb\": 1, label: okay}", "okay", "okay", true},
		{"line-reset", "other: \"a\tb\"\nlabel: \"c\td\"", "c\td", "\"c\td\"", true},
		{"comment-space", "label: \"a\tb\" # comment", "a\tb", "\"a\tb\"", true},
		{"newline-fold", "label: \"a\t\n b\"", "a b", "\"a\t\n b\"", true},
		{"no-separator-comment", "label: \"a\tb\"#comment", "", "", false},
		{"no-separator-word", "label: \"a\tb\"word", "", "", false},
		{"no-separator-quote", "label: \"a\tb\"\"next\"", "", "", false},
		{"unterminated", "label: \"a\tb", "", "", false},
		{"unmatched-close", "label: \"a\tb\" }", "", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := "```{python}\n#| " + strings.ReplaceAll(test.options, "\n", "\n#| ") + "\n```\n"
			f := newQuartoFixture(t, context.Background(), raw, parser.ParseOptions{}, true, "selected.qmd")
			out, cells := providerCells(t, f)
			if len(cells) != 1 || out.Complete() != test.valid {
				t.Fatal("quoted TAB compatibility changed validity", test.name, out.Status())
			}
			if test.valid {
				label := cells[0].Labels().Effective()
				span := label.Location().Span()
				spelling := strings.ReplaceAll(test.spelling, "\n", "\n#| ")
				start := strings.Index(raw, spelling)
				if label.Text() != test.value || span.Start() != start || span.End() != start+len(spelling) || label.Location().Source().Token() != f.doc.Source().Token() {
					t.Fatal("quoted TAB value/range changed", label.Text(), span, start, test.value)
				}
			} else {
				problems, err := out.Problems(context.Background())
				if err != nil || len(problems) == 0 {
					t.Fatal("malformed quoted context became clean", problems, err)
				}
			}
			if string(f.doc.Source().Bytes()) != raw {
				t.Fatal("padding changed source")
			}
		})
	}
}

func TestQuartoProviderStandardURIFlowAndPayload(t *testing.T) {
	for _, test := range []struct {
		name, options, value, spelling string
		valid                          bool
	}{
		{"minimal-long-flow", "{label: !<tag:yaml.org,2002:str> \"ab\"}", "ab", "!<tag:yaml.org,2002:str> \"ab\"", true},
		{"long-with-tab-payload", "{label: !<tag:yaml.org,2002:str> \"a\tb\"}", "a\tb", "!<tag:yaml.org,2002:str> \"a\tb\"", true},
		{"quoted-uri-after-comma", "{label: !<tag:yaml.org,2002:str> \"comma, !<tag:yaml.org,2002:str> payload\"}", "comma, !<tag:yaml.org,2002:str> payload", "!<tag:yaml.org,2002:str> \"comma, !<tag:yaml.org,2002:str> payload\"", true},
		{"quoted-uri-trap", "{other: !<tag:yaml.org,2002:str> \"!<tag:yaml.org,2002:str> payload\", label: okay}", "okay", "okay", true},
		{"plain-uri-trap", "other: prefix !<tag:yaml.org,2002:str> payload\nlabel: okay", "okay", "okay", true},
		{"block-uri-trap", "other: |-\n  !<tag:yaml.org,2002:str> payload\nlabel: okay", "okay", "okay", true},
		{"comment-uri-trap", "other: !<tag:yaml.org,2002:int> 0x10 # !<tag:yaml.org,2002:str> payload\nlabel: okay", "okay", "okay", true},
		{"missing-separator", "{label: !<tag:yaml.org,2002:str>\"ab\"}", "", "", false},
		{"unknown-uri", "{label: !<tag:yaml.org,2002:unknown> \"ab\"}", "", "", false},
		{"custom-uri", "{label: !<tag:evil.example,2002:str> \"ab\"}", "", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := "```{python}\n#| " + strings.ReplaceAll(test.options, "\n", "\n#| ") + "\n```\n"
			f := newQuartoFixture(t, context.Background(), raw, parser.ParseOptions{}, true, "selected.qmd")
			out, cells := providerCells(t, f)
			if len(cells) != 1 || out.Complete() != test.valid {
				t.Fatal("standard URI changed validity", test.name, out.Status())
			}
			if test.valid {
				label := cells[0].Labels().Effective()
				span := label.Location().Span()
				start := strings.Index(raw, test.spelling)
				if label.Text() != test.value || span.Start() != start || span.End() != start+len(test.spelling) || label.Location().Source().Token() != f.doc.Source().Token() {
					t.Fatal("URI canonicalization changed original label", label.Text(), span, start)
				}
			} else {
				problems, err := out.Problems(context.Background())
				if err != nil || len(problems) == 0 {
					t.Fatal("invalid URI became clean", problems, err)
				}
			}
			if string(f.doc.Source().Bytes()) != raw {
				t.Fatal("URI canonicalization changed source")
			}
		})
	}
}
