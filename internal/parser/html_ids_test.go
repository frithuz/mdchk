package parser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func htmlIDsFixture(t *testing.T, text string, options ParseOptions) (Source, *Document, *OperationBudget) {
	t.Helper()
	source := NewSource([]byte(text))
	budget, err := NewOperationBudget(DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	parsed := ParseWithOptions(context.Background(), source, budget, options)
	if parsed.Status != Complete || parsed.Document == nil {
		t.Fatal("fixture did not complete parsing", parsed)
	}
	return source, parsed.Document, budget
}

func TestHTMLIDsAttributeSemanticsAndSource(t *testing.T) {
	tests := []struct {
		name, text string
		values     []string
		effective  []bool
	}{
		{"duplicate-case", `<p ID = "A&amp;B" id='ignored' ID>`, []string{"A&B", "ignored", ""}, []bool{true, false, false}},
		{"empty-bare-unquoted", `<i id=foo /><b id=''></b><span id></span><i id=slash/>`, []string{"foo", "", "", "slash/"}, []bool{true, true, true, true}},
		{"attribute-references", `<a id="&copy! &copy= &notit; &notin; &#65 &#x80; &#0; &#x110000; &#00000000000000000000000000000000000065x">`, []string{"©! &copy= &notit; ∉ A € � � Ax"}, []bool{true}},
		{"literal-backslash-percent", `<a id='a\&amp;b%20c'>`, []string{`a\&b%20c`}, []bool{true}},
		{"line-preprocessing", "<div\r\n id = \"a\r\nb\rc\">", []string{"a\nb\nc"}, []bool{true}},
		{"mapped-container", "> <span\r\n> id=\"a\r\n> b\">", []string{"a\nb"}, []bool{true}},
		{"quoted-tag-not-element", `<p title='<b id="fake">' id="real">`, []string{"real"}, []bool{true}},
		{"value-case-not-name", `<a Id="Case"><b ID="case">`, []string{"Case", "case"}, []bool{true, true}},
		{"nul-preprocessing", "<a id=\"a\x00b\">", []string{"a�b"}, []bool{true}},
		{"reference-not-preprocessed-twice", `<a id="&#13;&NewLine;&amp;amp;">`, []string{"\r\n&amp;"}, []bool{true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, document, budget := htmlIDsFixture(t, tt.text, ParseOptions{})
			out, err := document.HTMLIDs(context.Background(), budget)
			if err != nil || out.Availability != Available || out.Source != source {
				t.Fatal("HTML inventory unavailable", out, err)
			}
			var values []string
			var effective []bool
			last := -1
			for _, occurrence := range out.Occurrences {
				values = append(values, occurrence.Value.Semantic.Value)
				effective = append(effective, occurrence.Effective)
				if !occurrence.Value.Semantic.Present || occurrence.Attribute.Start() <= last {
					t.Fatal("lost source order/present empty semantic", occurrence)
				}
				last = occurrence.Attribute.Start()
				if _, err := source.Text(occurrence.Tag); err != nil {
					t.Fatal("foreign tag span", err)
				}
				attr, err := source.Text(occurrence.Attribute)
				if err != nil || len(attr) < 2 || !strings.EqualFold(attr[:2], "id") {
					t.Fatal("attribute does not retain its actual spelling", attr, err)
				}
				var semantic strings.Builder
				for _, part := range occurrence.Value.Segments {
					if _, err := source.Text(part.Source); err != nil || !part.Semantic.Present {
						t.Fatal("foreign/unavailable semantic contribution", part, err)
					}
					semantic.WriteString(part.Semantic.Value)
				}
				if semantic.String() != occurrence.Value.Semantic.Value {
					t.Fatal("semantic contribution mismatch", semantic.String(), occurrence.Value.Semantic)
				}
			}
			if !reflect.DeepEqual(values, tt.values) || !reflect.DeepEqual(effective, tt.effective) {
				t.Fatal("wrong effective ID inventory", values, effective, "want", tt.values, tt.effective)
			}
		})
	}
}

func TestHTMLIDsContextAndOwnership(t *testing.T) {
	frontmatter, err := NewFrontmatterOptions(true, nil)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, text string
		options    ParseOptions
		want       []string
		available  Availability
	}{
		{"inline-and-alt", "Text <span id=live>ok</span> ![<i id=alt>image](x) `<b id=code>`", ParseOptions{}, []string{"live"}, Available},
		{"fenced", "```html\n<b id=fake>\n```\n\n<i id=live>\n", ParseOptions{}, []string{"live"}, Available},
		{"indented", "    <i id=fake>\n\n<i id=live>\n", ParseOptions{}, []string{"live"}, Available},
		{"frontmatter", "---\nvalue: <i id=fake>\n---\n<i id=live>\n", ParseOptions{Frontmatter: frontmatter}, []string{"live"}, Available},
		{"comment-tail", "<!-- <b id=fake> --> <i id=live>\n", ParseOptions{}, []string{"live"}, Available},
		{"cdata-tail", "<![CDATA[<b id=fake>]]><i id=live>\n", ParseOptions{}, []string{"live"}, Available},
		{"pi-tail", "<?x <b id=fake> ?><i id=live>\n", ParseOptions{}, []string{"live"}, Available},
		{"declaration", "<!DOCTYPE html id=fake><i id=live>\n", ParseOptions{}, []string{"live"}, Available},
		{"raw-script", "<script id=outer>var x='<span id=fake>';</script><i id=after>\n", ParseOptions{}, []string{"outer", "after"}, Available},
		{"raw-textarea", "<textarea id=outer><b id=fake></textarea><i id=after>\n", ParseOptions{}, []string{"outer", "after"}, Available},
		{"pre-children", "<pre id=outer><code id=real></code></pre>\n", ParseOptions{}, []string{"outer", "real"}, Available},
		{"gfm-table-mapping", "| h |\n| - |\n| <i id='a\\|b'> |\n", ParseOptions{GFM: true}, []string{"a|b"}, Available},
		{"closing-and-name", `<a name=not-id></a><i id=live></i>`, ParseOptions{}, []string{"live"}, Available},
		{"gfm-filter-uncertain", "<script id=filtered><b id=not-guessed></script>\n", ParseOptions{GFM: true}, nil, Unavailable},
		{"malformed-relevant", "<div id='unfinished\n<b id=not-guessed>\n", ParseOptions{}, nil, Unavailable},
		{"raw-cross-block", "<div><script>\n\n# Not a safe context\n\n<span id=not-guessed>\n", ParseOptions{}, nil, Unavailable},
		{"script-double-escape", "<script><!--<script></script><b id=not-guessed></script>\n", ParseOptions{}, nil, Unavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, document, budget := htmlIDsFixture(t, tt.text, tt.options)
			out, err := document.HTMLIDs(context.Background(), budget)
			if err != nil || out.Availability != tt.available {
				t.Fatal("wrong context availability", out, err)
			}
			var got []string
			for _, record := range out.Occurrences {
				if record.Effective {
					got = append(got, record.Value.Semantic.Value)
				}
			}
			if !reflect.DeepEqual(got, tt.want) || out.Availability != Available && len(out.Uncertainty) == 0 {
				t.Fatal("protected/unproved context became an ID inventory", got, tt.want, out)
			}
		})
	}
}

func TestHTMLIDsWitnessStopsAndDefensiveValues(t *testing.T) {
	source, document, budget := htmlIDsFixture(t, `<a id="owned&amp;value">`, ParseOptions{})
	out, err := document.HTMLIDs(context.Background(), budget)
	if err != nil || len(out.Occurrences) != 1 {
		t.Fatal(out, err)
	}
	out.Occurrences[0].Value.Segments[0].Semantic = Some("changed")
	out.Occurrences[0].Value.Semantic = Some("changed")
	again, err := document.HTMLIDs(context.Background(), budget)
	if err != nil || again.Occurrences[0].Value.Semantic.Value != "owned&value" || again.Source != source {
		t.Fatal("returned inventory aliases Document", again, err)
	}
	foreign, _ := NewOperationBudget(DefaultLimits())
	if out, err := document.HTMLIDs(context.Background(), foreign); !errors.Is(err, ErrModel) || out.Availability != Unavailable {
		t.Fatal("foreign budget gained parse authority", out, err)
	}
	constructed, err := NewDocument(document.Root(), document.References())
	if err != nil {
		t.Fatal(err)
	}
	if out, err := constructed.HTMLIDs(context.Background(), budget); !errors.Is(err, ErrModel) || out.Availability != Unavailable {
		t.Fatal("constructed model gained actual HTML recognition", out, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if out, err := document.HTMLIDs(ctx, budget); !errors.Is(err, context.Canceled) || len(out.Occurrences) != 0 || out.Availability != Unavailable {
		t.Fatal("cancel published partial inventory", out, err)
	}
	if err := budget.ChargeWork(budget.Limits().WorkUnits); err == nil {
		t.Fatal("fixture did not exhaust actual work budget")
	}
	var limit *LimitExceeded
	if out, err := document.HTMLIDs(context.Background(), budget); !errors.As(err, &limit) || len(out.Occurrences) != 0 || out.Availability != Unavailable {
		t.Fatal("quota published partial inventory", out, err)
	}
}

func TestHTMLIDsWithholdsPartlyBuiltInventoryOnActualStops(t *testing.T) {
	_, document, budget := htmlIDsFixture(t, `<i id=one><i id=two><i id=three>`, ParseOptions{})
	// Leave enough actual shared allowance to construct the first occurrence,
	// then exhaust it while constructing a subsequent one. Nothing is published.
	if err := budget.ChargeNodes(budget.Limits().TotalNodes - budget.nodes - 8); err != nil {
		t.Fatal(err)
	}
	out, err := document.HTMLIDs(context.Background(), budget)
	var limit *LimitExceeded
	if !errors.As(err, &limit) || limit.Name != "total-nodes" || out.Availability != Unavailable || len(out.Occurrences) != 0 {
		t.Fatal("partly constructed inventory escaped the actual quota", out, err)
	}

	_, document, budget = htmlIDsFixture(t, `<i id=one>`, ParseOptions{})
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if out, err := document.HTMLIDs(ctx, budget); !errors.Is(err, context.DeadlineExceeded) || out.Availability != Unavailable || len(out.Occurrences) != 0 {
		t.Fatal("expired deadline became a complete empty inventory", out, err)
	}
}

func TestHTMLIDsPinnedEntityAttributeContext(t *testing.T) {
	raw, err := os.ReadFile("testdata/html5/entities.json")
	if err != nil {
		t.Fatal(err)
	}
	var entries map[string]struct{ Characters string }
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	slices.Sort(names)
	var markdown strings.Builder
	want := make([]string, 0, len(names)*2)
	for _, name := range names {
		markdown.WriteString(`<i id="` + name + `">`)
		want = append(want, entries[name].Characters)
		if !strings.HasSuffix(name, ";") {
			markdown.WriteString(`<i id="` + name + `=">`)
			want = append(want, name+"=")
		}
	}
	_, document, budget := htmlIDsFixture(t, markdown.String(), ParseOptions{})
	out, err := document.HTMLIDs(context.Background(), budget)
	if err != nil || out.Availability != Available || len(out.Occurrences) != len(want) {
		t.Fatal("pinned HTML reference inventory incomplete", out.Availability, len(out.Occurrences), len(want), err)
	}
	for i, occurrence := range out.Occurrences {
		if occurrence.Value.Semantic != Some(want[i]) || !occurrence.Effective {
			t.Fatalf("pinned reference %d: %#v want %q", i, occurrence, want[i])
		}
	}
}

type htmlIDCancellingContext struct {
	context.Context
	cancel       context.CancelFunc
	checks, stop int
}

func (c *htmlIDCancellingContext) Err() error {
	c.checks++
	if c.checks == c.stop {
		c.cancel() // Done, Deadline and Err all belong to the real underlying context.
	}
	return c.Context.Err()
}

func TestHTMLIDsCancellationCannotBecomeOnlyUncertainty(t *testing.T) {
	_, document, budget := htmlIDsFixture(t, "<script id=filtered><b id=not-guessed></script>\n", ParseOptions{GFM: true})
	observed := false
	for stop := 1; stop <= 32; stop++ {
		t.Run(fmt.Sprint(stop), func(t *testing.T) {
			base, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx := &htmlIDCancellingContext{Context: base, cancel: cancel, stop: stop}
			out, err := document.HTMLIDs(ctx, budget)
			if base.Err() != nil {
				observed = true
				if !errors.Is(err, context.Canceled) || out.Availability != Unavailable || len(out.Occurrences) != 0 {
					t.Fatal("actual stop was erased by context uncertainty", out, err)
				}
			} else if err != nil || out.Availability != Unavailable {
				t.Fatal("unsupported context had incorrect outcome", out, err)
			}
		})
	}
	if !observed {
		t.Fatal("no actual cancellation was triggered")
	}
}

func TestHTMLIDsForeignEntryWithholdsCompleteInventory(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, row := range []struct{ name, text, entry string }{
			{"svg-title", `<svg><title><span id=x>live</span></title></svg>`, `<svg>`},
			{"svg-description", `<svg><desc><span id=x>live</span></desc></svg>`, `<svg>`},
			{"svg-foreign-object", `<svg><foreignObject><span id=x>live</span></foreignObject></svg>`, `<svg>`},
			{"math-integration", `<math><annotation-xml encoding="text/html"><span id=x>live</span></annotation-xml></math>`, `<math>`},
			{"math-text-integration", `<math><mtext><span id=x>live</span></mtext></math>`, `<math>`},
			{"mixed-case-and-attributes", `<SVG id=outer><title><i id=x></i></title></SVG>`, `<SVG id=outer>`},
			{"inline-entry", `Text <math id=outer><mtext><i id=x></i></mtext></math>`, `<math id=outer>`},
			{"no-partial-earlier-ids", `<i id=earlier></i><svg><title><i id=x></i></title></svg>`, `<svg>`},
			{"mapped-container", "> <svg\r\n> id=outer><title><i id=x></i></title></svg>\r\n", "<svg\r\n> id=outer>"},
		} {
			t.Run(fmt.Sprintf("gfm=%v/%s", gfm, row.name), func(t *testing.T) {
				source, document, budget := htmlIDsFixture(t, row.text, ParseOptions{GFM: gfm})
				out, err := document.HTMLIDs(context.Background(), budget)
				if err != nil || out.Source != source || out.Availability != Unavailable || len(out.Occurrences) != 0 || len(out.Uncertainty) != 1 {
					t.Fatal("foreign entry granted complete/partial inventory", out, err)
				}
				entry, err := source.Text(out.Uncertainty[0])
				if err != nil || entry != row.entry {
					t.Fatal("foreign entry uncertainty lost source ownership", entry, err)
				}
			})
		}
	}
}

func TestHTMLIDsForeignSpellingKeepsProvenProtectedContexts(t *testing.T) {
	for _, gfm := range []bool{false, true} {
		for _, text := range []string{
			"`<svg><title><i id=fake></i></title></svg>` <i id=real></i>",
			"```html\n<math><mtext><i id=fake></i></mtext></math>\n```\n\n<i id=real></i>\n",
			`<!-- <svg><title><i id=fake></i></title></svg> --><i id=real></i>`,
			`<p title='<math><mtext><i id=fake></i></mtext></math>' id=real></p>`,
			`![<svg><title><i id=fake></i></title></svg>](x) <i id=real></i>`,
		} {
			_, document, budget := htmlIDsFixture(t, text, ParseOptions{GFM: gfm})
			out, err := document.HTMLIDs(context.Background(), budget)
			if err != nil || out.Availability != Available || len(out.Uncertainty) != 0 || len(out.Occurrences) != 1 || out.Occurrences[0].Value.Semantic != Some("real") {
				t.Fatal("protected foreign spelling changed inventory", gfm, text, out, err)
			}
		}
		for _, name := range []string{"title", "script", "textarea", "style"} {
			text := "<" + name + " id=outer><svg><title><i id=fake></i></title></svg></" + name + "><i id=after></i>"
			_, document, budget := htmlIDsFixture(t, text, ParseOptions{GFM: gfm})
			out, err := document.HTMLIDs(context.Background(), budget)
			if err != nil {
				t.Fatal(err)
			}
			if gfm {
				if out.Availability != Unavailable || len(out.Occurrences) != 0 || len(out.Uncertainty) == 0 {
					t.Fatal("filtered raw prefix guessed context", name, out)
				}
			} else if out.Availability != Available || len(out.Uncertainty) != 0 || len(out.Occurrences) != 2 || out.Occurrences[0].Value.Semantic != Some("outer") || out.Occurrences[1].Value.Semantic != Some("after") {
				t.Fatal("ordinary HTML raw ownership changed", name, out)
			}
		}
	}
}
