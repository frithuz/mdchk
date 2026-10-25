package metadata

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestDecodeAuthoredNestedAliasBindings(t *testing.T) {
	f, r := decoded(t, "---\na: &x old\nmap: &saved {inner: *x}\nseq: &sequence [*x]\nb: &x new\ncopy: *saved\nlist: *sequence\ndirect: *x\n---")
	if !r.Complete() {
		t.Fatal("stable aliases incomplete")
	}
	for _, key := range []string{"map", "copy"} {
		v := fieldValue(t, fieldValue(t, r.Root(), key), "inner")
		text, _ := v.Scalar()
		if text != "old" {
			t.Fatal("authored nested alias changed", key, text)
		}
	}
	for _, key := range []string{"seq", "list"} {
		v, _ := fieldValue(t, r.Root(), key).KnownElement(0)
		text, _ := v.Scalar()
		if text != "old" {
			t.Fatal("authored sequence alias changed", key, text)
		}
	}
	if text, _ := fieldValue(t, r.Root(), "direct").Scalar(); text != "new" {
		t.Fatal("later direct alias did not rebind", text)
	}
	copy := fieldValue(t, r.Root(), "copy")
	child, _ := copy.KnownEntry(0)
	if child.Span().Present() || sourceText(t, f.b, child.KeySpan()) != "*saved" || sourceText(t, f.b, child.Value().Span()) != "*saved" {
		t.Fatal("alias descendants lost outer use")
	}
	_, incomplete := decoded(t, "---\nmap: &saved {inner: *future}\nseq: &sequence [*future]\nfuture: &future later\ncopy: *saved\nlist: *sequence\n---")
	if incomplete.Complete() {
		t.Fatal("unresolved aliases retroactively completed")
	}
	if fieldValue(t, fieldValue(t, incomplete.Root(), "copy"), "inner").Kind() != Unknown {
		t.Fatal("unresolved authored alias became resolved")
	}
	v, _ := fieldValue(t, incomplete.Root(), "list").KnownElement(0)
	if v.Kind() != Unknown {
		t.Fatal("unresolved sequence alias became resolved")
	}
}
func TestDecodeYAMLSyntaxEmptyAndUnicodeContent(t *testing.T) {
	for _, payload := range []string{"", " \t\n", " \t\r\n", " \t\r"} {
		_, r := decoded(t, "---\n"+payload+"---")
		if !r.Complete() || r.Root().Kind() != Mapping || r.Root().EntryCount() != 0 {
			t.Fatal("syntax empty rejected", payload)
		}
	}
	for _, payload := range []string{"\u00a0", "\u2003", "\u2028", "\u2029", "\u0085"} {
		f, r := decoded(t, "---\n"+payload+"\n---")
		if r.Complete() || r.Root().Kind() != Unknown || r.ProblemCount() == 0 {
			t.Fatal("Unicode scalar minted empty authority", payload)
		}
		title, err := r.Title(context.Background(), f.b)
		if err != nil || title.Known {
			t.Fatal("Unicode scalar minted missing title")
		}
		orders, err := r.Ordering(context.Background(), f.b, OrderOptions{Recursive: true})
		if err != nil || len(orders) != 0 {
			t.Fatal("Unicode scalar minted ordering")
		}
		p, _ := r.KnownProblem(0)
		if p.Location.SourceToken() != f.b.Source().Token() || sourceText(t, f.b, p.Location) != payload {
			t.Fatal("foreign Unicode problem")
		}
	}
}
func TestDecodeUnicodeContentRanges(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n"} {
		for _, content := range []string{"\u00a0ABC", "\u2003ABC", "ABC\u00a0", "ABC\u2003", "\u00a0", "\u2003"} {
			raw := "\ufeff---" + ending + "title: " + content + ending + content + ": value" + ending + "quoted: \"" + content + "\"" + ending + "---"
			f, r := decoded(t, raw)
			if !r.Complete() {
				t.Fatal("Unicode mapping incomplete")
			}
			v := fieldValue(t, r.Root(), "title")
			if sourceText(t, f.b, v.Span()) != content {
				t.Fatal("Unicode content span truncated", content, sourceText(t, f.b, v.Span()))
			}
			title, err := r.Title(context.Background(), f.b)
			if err != nil {
				t.Fatal(err)
			}
			if title.Present && sourceText(t, f.b, title.Location) != content {
				t.Fatal("title source truncated")
			}
			e, _ := r.Root().KnownEntry(1)
			if e.Key() != content || sourceText(t, f.b, e.KeySpan()) != content {
				t.Fatal("Unicode key truncated")
			}
			q := fieldValue(t, r.Root(), "quoted")
			if sourceText(t, f.b, q.Span()) != "\""+content+"\"" {
				t.Fatal("quoted Unicode source")
			}
			first, _ := r.Root().KnownEntry(0)
			if sourceText(t, f.b, first.Span()) != "title: "+content+ending {
				t.Fatal("Unicode entry span")
			}
		}
	}
}
func TestDecodeOrdinaryMergeSpellingAndEmptyStringTags(t *testing.T) {
	for _, key := range []string{"\"<<\"", "'<<'", "!!str <<", "!<tag:yaml.org,2002:str> <<"} {
		f, r := decoded(t, "---\n"+key+": literal\n---")
		if !r.Complete() || r.Root().EntryCount() != 1 {
			t.Fatal("ordinary merge spelling rejected", key)
		}
		e, _ := r.Root().KnownEntry(0)
		if e.Key() != "<<" || sourceText(t, f.b, e.KeySpan()) != key {
			t.Fatal("ordinary key source", key, sourceText(t, f.b, e.KeySpan()))
		}
	}
	_, duplicate := decoded(t, "---\n\"<<\": one\n!!str <<: two\n---")
	if duplicate.Complete() {
		t.Fatal("ordinary equivalent key duplicates accepted")
	}
	_, merge := decoded(t, "---\n<<: {x: 1}\n---")
	if merge.Complete() {
		t.Fatal("actual merge accepted")
	}
	for _, payload := range []string{"title: !!str\n", "{title: !!str }\n", "title: \"\"\n"} {
		f, r := decoded(t, "---\n"+payload+"---")
		v := fieldValue(t, r.Root(), "title")
		text, _ := v.Scalar()
		if !r.Complete() || v.Kind() != String || text != "" {
			t.Fatal("empty string invented null", payload, text)
		}
		if strings.Contains(payload, "!!str") && sourceText(t, f.b, v.Span()) != "!!str" {
			t.Fatal("empty tag source span", sourceText(t, f.b, v.Span()))
		}
		title, err := r.Title(context.Background(), f.b)
		if err != nil || !title.Known || title.Present {
			t.Fatal("empty string minted positive title", title, err)
		}
	}
	f, r := decoded(t, "---\ntitle: !!str null\nomitted:\n!!str : value\n---")
	if !r.Complete() {
		t.Fatal("authored null/empty key incomplete")
	}
	title, err := r.Title(context.Background(), f.b)
	if err != nil || title.Text != "null" || !title.Present {
		t.Fatal("authored null string lost")
	}
	if fieldValue(t, r.Root(), "omitted").Kind() != Null {
		t.Fatal("omitted untagged changed")
	}
	if text, _ := fieldValue(t, r.Root(), "").Scalar(); text != "value" {
		t.Fatal("empty tagged key invented null")
	}
}
func TestDecodeImplicitNullWideFlowPositions(t *testing.T) {
	for _, n := range []int{50, 100, 200, 400, 600} {
		var payload strings.Builder
		payload.WriteString("{ ")
		for i := 0; i < n; i++ {
			if i > 0 {
				payload.WriteString(", ")
			}
			fmt.Fprintf(&payload, "雪%d: ", i)
		}
		payload.WriteString("}\r\n")
		raw := "\ufeff---\r\n" + payload.String() + "---"
		limits := parser.DefaultLimits()
		limits.WorkUnits = 1000000
		budget := mustBudget(t, limits)
		f := bindBudgetFixture(t, raw, budget, context.Background())
		r, err := Decode(context.Background(), f.b)
		if err != nil || !r.Complete() || r.Root().EntryCount() != n {
			t.Fatal("linear null index failed", n, err)
		}
		for i := 0; i < n; i++ {
			e, _ := r.Root().KnownEntry(i)
			needle := fmt.Sprintf("雪%d:", i)
			want := strings.Index(raw, needle) + len(needle)
			if e.Value().Kind() != Null || e.Value().Span().Start() != want || e.Value().Span().End() != want {
				t.Fatal("implicit Unicode point changed", i, e.Value().Span(), want)
			}
		}
	}
	limits := parser.DefaultLimits()
	limits.WorkUnits = 5000
	budget := mustBudget(t, limits)
	f := bindBudgetFixture(t, "---\na: &saved {雪: , other: }\nb: *saved\n---", budget, context.Background())
	r, err := Decode(context.Background(), f.b)
	if err == nil {
		if !r.Complete() {
			t.Fatal("alias null incomplete")
		}
		v := fieldValue(t, fieldValue(t, r.Root(), "b"), "雪")
		if sourceText(t, f.b, v.Span()) != "*saved" {
			t.Fatal("null alias use lost")
		}
	} else {
		var quota *parser.LimitExceeded
		if !errors.As(err, &quota) || quota.Name != "work-units" {
			t.Fatal("noncanonical work stop", err)
		}
	}
}

func TestDecodeImplicitNullAfterReconciledTag(t *testing.T) {
	for _, tag := range []string{"!!str", "!<tag:yaml.org,2002:str>"} {
		raw := "---\n{a: " + tag + " text, 雪: , b: }\n---"
		_, r := decoded(t, raw)
		if !r.Complete() {
			t.Fatal("tag/null incomplete")
		}
		for _, key := range []string{"雪", "b"} {
			v := fieldValue(t, r.Root(), key)
			want := strings.Index(raw, key+":") + len(key+":")
			if v.Span().Start() != want || v.Span().End() != want {
				t.Fatal("null after reconciled tag shifted", key, v.Span().Start(), want)
			}
		}
	}
	for _, payload := range []string{"{x: !!int }\n", "{x: !!bool, y: ok}\n", "x: !!float\n", "x: !!int \"\"\n"} {
		_, r := decoded(t, "---\n"+payload+"---")
		if r.Complete() || r.ProblemCount() == 0 {
			t.Fatal("empty invalid Core tag invented native default", payload)
		}
	}
}

func TestDecodeImplicitNullLowWorkStop(t *testing.T) {
	var payload strings.Builder
	payload.WriteByte('{')
	for i := 0; i < 600; i++ {
		if i > 0 {
			payload.WriteString(", ")
		}
		fmt.Fprintf(&payload, "k%d: ", i)
	}
	payload.WriteString("}\n")
	limits := parser.DefaultLimits()
	limits.WorkUnits = 500000
	budget := mustBudget(t, limits)
	f := bindBudgetFixture(t, "---\n"+payload.String()+"---", budget, context.Background())
	_, err := Decode(context.Background(), f.b)
	var quota *parser.LimitExceeded
	if !errors.As(err, &quota) || quota.Name != "work-units" || quota.Allowed != 500000 {
		t.Fatal("wide null map lost canonical low-work stop", err)
	}
}

func TestDecodeCommentedEmptyCoreTags(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n"} {
		for _, tag := range []string{"!!str", "!<tag:yaml.org,2002:str>"} {
			for _, payload := range []string{"title: " + tag + " # inline\nother: x\n", "{title: " + tag + " # inline\n}\n", "{title: " + tag + " # inline\n, other: x}\n", "title: " + tag + " # inline\n"} {
				raw := "\ufeff---" + ending + strings.ReplaceAll(payload, "\n", ending) + "---"
				f, r := decoded(t, raw)
				if !r.Complete() {
					t.Fatal("commented empty string unavailable", tag, payload, r.ProblemCount())
				}
				v := fieldValue(t, r.Root(), "title")
				text, _ := v.Scalar()
				if v.Kind() != String || text != "" {
					t.Fatal("comment invented empty string content", text)
				}
				if sourceText(t, f.b, v.Span()) != tag {
					t.Fatal("commented tag source swallowed presentation", sourceText(t, f.b, v.Span()), tag)
				}
				title, err := r.Title(context.Background(), f.b)
				if err != nil || !title.Known || title.Present {
					t.Fatal("commented empty title authority", title, err)
				}
				if strings.HasPrefix(payload, "title:") {
					first, _ := r.Root().KnownEntry(0)
					if sourceText(t, f.b, first.Span()) != "title: "+tag+" # inline"+ending {
						t.Fatal("commented physical entry range")
					}
				}
			}
		}
	}
}
func TestDecodeCommentedInvalidCoreTags(t *testing.T) {
	for _, tag := range []string{"!!int", "!!bool", "!!float", "!<tag:yaml.org,2002:int>", "!<tag:yaml.org,2002:bool>", "!<tag:yaml.org,2002:float>"} {
		for _, tail := range []string{", other: x}", "}"} {
			f, r := decoded(t, "---\n{x: "+tag+" # comment\n"+tail+"\n---")
			if r.Complete() || r.ProblemCount() == 0 {
				t.Fatal("commented empty tag invented native default", tag, tail)
			}
			p, _ := r.KnownProblem(0)
			if !p.Location.Present() || p.Location.SourceToken() != f.b.Source().Token() {
				t.Fatal("unlocated invalid commented tag")
			}
			title, err := r.Title(context.Background(), f.b)
			if err != nil || title.Known {
				t.Fatal("invalid commented tag minted missing title")
			}
		}
	}
	for _, payload := range []string{"title: !!str null # inline\n", "title: !!str # comment\n  actual\nother: x\n", "x: !!int 42 # inline\n", "x: !!bool true # inline\n"} {
		_, r := decoded(t, "---\n"+payload+"---")
		if !r.Complete() {
			t.Fatal("authored commented native value lost", payload)
		}
		if strings.HasPrefix(payload, "title") {
			want := "null"
			if strings.Contains(payload, "actual") {
				want = "actual"
			}
			if text, _ := fieldValue(t, r.Root(), "title").Scalar(); text != want {
				t.Fatal("comment consumed authored string", text, want)
			}
		}
	}
	for _, payload := range []string{"title: !!str # inline\n<<: {x: 1}\n", "{title: !!str # inline\n, <<: {x: 1}}\n"} {
		_, r := decoded(t, "---\n"+payload+"---")
		if r.Complete() || r.ProblemCount() == 0 {
			t.Fatal("empty tag consumed sibling merge")
		}
	}
}

func TestDecodeCommentedEmptyTagPreservesStandaloneRestriction(t *testing.T) {
	f, r := decoded(t, "---\nz: !!str # inline\n# standalone\na: x\n---")
	if !r.Complete() {
		t.Fatal("comment-separated empty string unavailable")
	}
	orders, err := r.Ordering(context.Background(), f.b, OrderOptions{})
	if err != nil || len(orders) != 1 || !orders[0].Violation() || orders[0].MoveEligibility() != ReportOnly {
		t.Fatal("commented empty tag lost standalone restriction", orders, err)
	}
}

func TestDecodeScalarTagAnchorOrders(t *testing.T) {
	for _, tc := range []struct {
		tag, content, text string
		kind               Kind
	}{
		{"!!str", "hello", "hello", String}, {"!!str", "42", "42", String}, {"!!int", "42", "42", Integer},
		{"!<tag:yaml.org,2002:str>", "42", "42", String},
	} {
		for _, properties := range []string{tc.tag + " &x " + tc.content, "&x " + tc.tag + " " + tc.content} {
			t.Run(properties, func(t *testing.T) {
				f, r := decoded(t, "---\ntitle: "+properties+"\ncopy: *x\n---")
				if !r.Complete() || r.ProblemCount() != 0 {
					t.Fatal("scalar properties incomplete", r.ProblemCount())
				}
				for _, key := range []string{"title", "copy"} {
					v := fieldValue(t, r.Root(), key)
					text, ok := v.Scalar()
					if !ok || v.Kind() != tc.kind || text != tc.text {
						t.Fatal("tagged anchor lost native semantics", key, v.Kind(), text)
					}
					want := properties
					if key == "copy" {
						want = "*x"
					}
					if sourceText(t, f.b, v.Span()) != want {
						t.Fatal("scalar property/use source", key, sourceText(t, f.b, v.Span()))
					}
				}
				if tc.kind == String {
					title, err := r.Title(context.Background(), f.b)
					if err != nil || !title.Known || !title.Present || title.Text != tc.text || sourceText(t, f.b, title.Location) != properties {
						t.Fatal("tagged anchor lost known title", title, err)
					}
				}
				orders, err := r.Ordering(context.Background(), f.b, OrderOptions{})
				if err != nil || len(orders) != 1 || !orders[0].Violation() || orders[0].MoveEligibility() != ReportOnly {
					t.Fatal("scalar properties gained movement authority", orders, err)
				}
			})
		}
	}
}
func TestDecodeTaggedAnchorSavedBindings(t *testing.T) {
	for _, properties := range []string{"!!str &x 42", "&x !!str 42"} {
		f, r := decoded(t, "---\na: "+properties+"\nsaved: &s [*x]\nb: &x replacement\ncopy: *s\ndirect: *x\n---")
		if !r.Complete() {
			t.Fatal("tagged saved graph incomplete")
		}
		for _, key := range []string{"saved", "copy"} {
			v, _ := fieldValue(t, r.Root(), key).KnownElement(0)
			text, _ := v.Scalar()
			if v.Kind() != String || text != "42" {
				t.Fatal("tagged saved binding changed", key, v.Kind(), text)
			}
		}
		direct := fieldValue(t, r.Root(), "direct")
		text, _ := direct.Scalar()
		if text != "replacement" || sourceText(t, f.b, direct.Span()) != "*x" {
			t.Fatal("tagged direct rebind lost")
		}
	}
}

func TestDecodeTaggedAnchorOmittedAndInvalidContent(t *testing.T) {
	for _, properties := range []string{"!!str &x", "&x !!str"} {
		for _, suffix := range []string{"", " # inline"} {
			_, r := decoded(t, "---\ntitle: "+properties+suffix+"\ncopy: *x\n---")
			if !r.Complete() {
				t.Fatal("omitted tagged anchor unavailable", properties, suffix)
			}
			for _, key := range []string{"title", "copy"} {
				v := fieldValue(t, r.Root(), key)
				text, _ := v.Scalar()
				if v.Kind() != String || text != "" {
					t.Fatal("omitted tagged anchor invented content", properties, key, v.Kind(), text)
				}
			}
		}
	}
	for _, properties := range []string{"!!int &x invalid", "&x !!int invalid", "!!bool &x", "&x !!bool", "!!int &x # comment", "&x !!int # comment"} {
		_, r := decoded(t, "---\nvalue: "+properties+"\ncopy: *x\n---")
		if r.Complete() || fieldValue(t, r.Root(), "copy").Kind() != Unknown {
			t.Fatal("invalid tagged anchor acquired native default", properties)
		}
	}
}
