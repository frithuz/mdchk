package parser

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func frontmatterSelection(t *testing.T, enabled bool, formats []string) FrontmatterOptions {
	t.Helper()
	options, err := NewFrontmatterOptions(enabled, formats)
	if err != nil {
		t.Fatal(err)
	}
	return options
}

func TestFrontmatterOptionsValidation(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, formats := range [][]string{{}, {"json"}, {"YAML"}, {"yaml", "yaml"}, {"toml", "toml"}, {"yaml", "toml", "yaml"}} {
			if _, err := NewFrontmatterOptions(enabled, formats); err == nil {
				t.Fatalf("accepted invalid selection enabled=%v formats=%v", enabled, formats)
			}
		}
		options := frontmatterSelection(t, enabled, nil)
		if options.enabled != enabled || !options.yaml || !options.toml {
			t.Fatal("nil formats did not use independent defaults", options)
		}
	}
	formats := []string{"yaml"}
	options := frontmatterSelection(t, true, formats)
	formats[0] = "toml"
	if !options.yaml || options.toml {
		t.Fatal("options retained caller storage")
	}
}

func TestFrontmatterZeroOptionsPreserveLegacy(t *testing.T) {
	source := NewSource([]byte("---\nx: y\n---\n\n~~ordinary~~\n"))
	configured := ParseWithOptions(context.Background(), source, budgetFor(t, DefaultLimits()), ParseOptions{})
	legacy := ParseWithBudget(context.Background(), source, budgetFor(t, DefaultLimits()))
	_, present := configured.Document.Frontmatter()
	if configured.Status != Complete || present || !reflect.DeepEqual(configured.Document.Root(), legacy.Document.Root()) || !reflect.DeepEqual(configured.Document.References(), legacy.Document.References()) {
		t.Fatal("zero options changed legacy CommonMark behavior")
	}
}

func TestFrontmatterEnabledBoundaryFixtures(t *testing.T) {
	raw, err := os.ReadFile("testdata/frontmatter/boundaries.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []frontmatterFixture
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		for _, gfm := range []bool{false, true} {
			flavor := "commonmark"
			if gfm {
				flavor = "gfm"
			}
			t.Run(flavor+"/"+fixture.ID, func(t *testing.T) {
				source := NewSource([]byte(fixture.Input))
				options := ParseOptions{GFM: gfm, Frontmatter: frontmatterSelection(t, fixture.Enabled, fixture.Formats)}
				budget := budgetFor(t, DefaultLimits())
				result := ParseWithOptions(context.Background(), source, budget, options)
				if result.Failure != nil || result.Document == nil || result.Source != source || result.Document.Source() != source {
					t.Fatalf("lost original document: %+v", result)
				}
				root := result.Document.Root()
				if root.Span().Start() != 0 || root.Span().End() != source.Len() {
					t.Fatal("root no longer covers complete original snapshot")
				}
				header, present := result.Document.Frontmatter()
				if fixture.Want == nil {
					if present || header != (Frontmatter{}) || result.Status != Complete {
						t.Fatalf("unexpected recognition: %+v %+v", header, result)
					}
					legacy := ParseWithBudget
					if gfm {
						legacy = ParseGFMWithBudget
					}
					ordinary := legacy(context.Background(), source, budgetFor(t, DefaultLimits()))
					if !reflect.DeepEqual(root, ordinary.Document.Root()) || !reflect.DeepEqual(result.Document.References(), ordinary.Document.References()) {
						t.Fatal("disabled/unselected format changed ordinary interpretation")
					}
					return
				}
				want := fixture.Want
				if !present || header.Format() != want.Format || header.BodyOffset() != want.Region[1] {
					t.Fatalf("header identity/format/body offset: %+v", header)
				}
				checkBoundarySpan(t, source, header.Span(), &want.Region)
				checkBoundarySpan(t, source, header.Opener(), &want.Opener)
				checkBoundarySpan(t, source, header.Content(), &want.Content)
				checkBoundarySpan(t, source, header.Closer(), want.Closer)
				Walk(root, func(node *Node) bool {
					if node.Kind() != DocumentKind && node.Span().Start() < header.BodyOffset() {
						t.Fatal("header bytes leaked into ordinary AST", node.Kind())
					}
					return true
				})
				for _, definition := range result.Document.References().Definitions {
					if definition.Properties().Envelope.Start() < header.BodyOffset() {
						t.Fatal("header bytes leaked into reference inventory")
					}
				}
				if want.Closer == nil {
					if result.Status != Incomplete || result.AST != Partial || result.Interruption != "" || result.Limit != nil || result.Document.parsedComplete || len(root.Children()) != 0 || len(result.Uncertainty) != 1 || result.Uncertainty[0] != (Uncertainty{Region: header.Span(), Reason: UnterminatedFrontmatter}) {
						t.Fatalf("unterminated header became complete or cancelled: %+v", result)
					}
					if _, err := result.Document.Comments(context.Background(), budget); err == nil {
						t.Fatal("unterminated parse manufactured completed comment handoff")
					}
					if _, err := result.Document.ProtectedNodes(context.Background(), budget); err == nil {
						t.Fatal("unterminated parse manufactured completed protection handoff")
					}
				} else if result.Status != Complete || result.AST != Available || len(result.Uncertainty) != 0 {
					t.Fatalf("complete header/body unavailable: %+v", result)
				}
				// Accessors return values, not a mutable pointer into Document.
				header = Frontmatter{}
				again, stillPresent := result.Document.Frontmatter()
				if header.Span().Present() || !stillPresent || again.BodyOffset() != want.Region[1] {
					t.Fatal("replacing returned header changed stored record")
				}
			})
		}
	}
}

func TestFrontmatterBodyInventoriesAndCoordinates(t *testing.T) {
	options := ParseOptions{Frontmatter: frontmatterSelection(t, true, nil)}
	input := "---\n[r]: /header\n<!-- header -->\n```\n# hidden\n---\n[r]: /body\n[body]: /ok\n\n# visible\n[r] [body]\n\n<!-- body -->\n"
	source := NewSource([]byte(input))
	budget := budgetFor(t, DefaultLimits())
	result := ParseWithOptions(context.Background(), source, budget, options)
	if result.Status != Complete {
		t.Fatal(result)
	}
	definitions := result.Document.References().Definitions
	if len(definitions) != 2 || definitions[0].Properties().Destination.Semantic.Value != "/body" {
		t.Fatal("header reference leaked or shadowed body definition", definitions)
	}
	comments, err := result.Document.Comments(context.Background(), budget)
	if err != nil || len(comments) != 1 {
		t.Fatalf("header comment/directive leaked: %+v, %v", comments, err)
	}
	header, _ := result.Document.Frontmatter()
	if comments[0].Envelope.Start() < header.BodyOffset() {
		t.Fatal("header comment published")
	}
	links, headings := 0, 0
	Walk(result.Document.Root(), func(node *Node) bool {
		if node.Kind() == Link {
			links++
			if node.Properties().Link.Resolution != Available {
				t.Fatal("body reference not resolved")
			}
		}
		if node.Kind() == Heading {
			headings++
		}
		if node.Kind() == CodeBlock {
			t.Fatal("unclosed metadata fence consumed body")
		}
		return true
	})
	if links != 2 || headings != 1 {
		t.Fatalf("body inventory links=%d headings=%d", links, headings)
	}

	source = NewSource([]byte("\xef\xbb\xbf---\r\nx: é\r\n---\r\n# 世界\r\n"))
	result = ParseWithOptions(context.Background(), source, budgetFor(t, DefaultLimits()), options)
	header, present := result.Document.Frontmatter()
	children := result.Document.Root().Children()
	if result.Status != Complete || !present || header.Opener().Start() != 3 || header.BodyOffset() != 20 || len(children) != 1 || children[0].Kind() != Heading || children[0].Span().Start() != 20 || children[0].Span().End() != 30 {
		t.Fatalf("BOM/CRLF/body coordinates lost: %+v %+v", header, result)
	}
	position, err := NewLineIndex(source).Position(children[0].Content()[0].Source.Start())
	if err != nil || position.Line != 4 || position.Column != 3 {
		t.Fatalf("body mapped to sliced-source coordinates: %+v %v", position, err)
	}
}

func TestFrontmatterPreservesSelectedBodyFlavor(t *testing.T) {
	body := "# visible\n\n> quote\n\n- item\n\n| a |\n| - |\n| b |\n\n~~x~~ www.example.com\n\n- [x] done\n"
	for _, gfm := range []bool{false, true} {
		source := NewSource([]byte("+++\ninvalid [metadata\n+++\n" + body))
		result := ParseWithOptions(context.Background(), source, budgetFor(t, DefaultLimits()), ParseOptions{GFM: gfm, Frontmatter: frontmatterSelection(t, true, nil)})
		if result.Status != Complete {
			t.Fatal(result)
		}
		counts := map[Kind]int{}
		tasks := 0
		Walk(result.Document.Root(), func(node *Node) bool {
			counts[node.Kind()]++
			if node.Kind() == ListItem && node.Properties().Task != nil {
				tasks++
			}
			return true
		})
		if counts[Heading] != 1 || counts[BlockQuote] != 1 || counts[List] != 2 {
			t.Fatal("body containers changed", counts)
		}
		if gfm && (counts[Table] != 1 || counts[Strike] != 1 || counts[Link] != 1 || tasks != 1) {
			t.Fatal("selected GFM body lost extensions", counts, tasks)
		}
		if !gfm && (counts[Table] != 0 || counts[Strike] != 0 || counts[Link] != 0 || tasks != 0) {
			t.Fatal("frontmatter implicitly enabled GFM", counts, tasks)
		}
	}
}

func TestFrontmatterAdmissionCancellationAndBudgets(t *testing.T) {
	options := ParseOptions{Frontmatter: frontmatterSelection(t, true, nil)}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	source := NewSource([]byte("---\nx:\xff\n---\n"))
	result := ParseWithOptions(cancelled, source, budgetFor(t, DefaultLimits()), options)
	if result.Failure == nil || result.Failure.Code != InvalidUTF8 || result.Failure.Location.Start() != 6 || result.Interruption != "" {
		t.Fatal("opaque header bypassed encoding precedence", result)
	}
	limits := DefaultLimits()
	limits.DocumentBytes = 2
	requireLimit(t, ParseWithOptions(cancelled, source, budgetFor(t, limits), options), "document-bytes")
	source = NewSource([]byte("---\n" + strings.Repeat("x\n", 1000) + "---\n# body\n"))
	for _, ctx := range []context.Context{cancelled, &checkpointContext{Context: context.Background(), remaining: 10}} {
		result = ParseWithOptions(ctx, source, budgetFor(t, DefaultLimits()), options)
		if result.Status != Incomplete || result.Interruption != Cancelled || result.Document.parsedComplete || result.Failure != nil {
			t.Fatal("header scan ignored cancellation", result)
		}
	}
	limits = DefaultLimits()
	limits.DocumentNodes = 1
	requireLimit(t, ParseWithOptions(context.Background(), source, budgetFor(t, limits), options), "document-nodes")
	baseline := budgetFor(t, DefaultLimits())
	if result := ParseWithOptions(context.Background(), source, baseline, options); result.Status != Complete {
		t.Fatal(result)
	}
	limits = DefaultLimits()
	limits.WorkUnits = baseline.work + baseline.work/2
	shared := budgetFor(t, limits)
	if result := ParseWithOptions(context.Background(), source, shared, options); result.Status != Complete {
		t.Fatal(result)
	}
	requireLimit(t, ParseWithOptions(context.Background(), source, shared, options), "work-units")
}

func TestFrontmatterConcurrentOptionIsolation(t *testing.T) {
	source := NewSource([]byte("---\nx: y\n---\n# body\n"))
	enabled := frontmatterSelection(t, true, []string{"yaml"})
	unselected := frontmatterSelection(t, true, []string{"toml"})
	var workers sync.WaitGroup
	for i := range 12 {
		workers.Go(func() {
			selection := enabled
			if i%2 != 0 {
				selection = unselected
			}
			result := ParseWithOptions(context.Background(), source, budgetFor(t, DefaultLimits()), ParseOptions{GFM: i%3 == 0, Frontmatter: selection})
			_, present := result.Document.Frontmatter()
			if result.Status != Complete || present != (i%2 == 0) {
				t.Error("parse selection leaked across calls", i, result)
			}
		})
	}
	workers.Wait()
}
