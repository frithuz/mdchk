package integration

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestMkDocsReturnedDepthCauseAndSyntaxPair(t *testing.T) {
	limits := parser.DefaultLimits()
	limits.SyntaxDepth = 4
	scope := mkDocsPatternScopeWithLimits(t, limits)
	out, err := decodeMkDocsYAML(context.Background(), scope, parser.NewSource([]byte("nav: [[[[[guide.md]]]]]\n")))
	var quota *parser.LimitExceeded
	if !errors.As(err, &quota) || quota.Name != "syntax-depth" || quota.Observed != 5 || quota.Allowed != 4 || len(out.problems) != 0 {
		t.Fatal("returned depth cause replaced with malformed", err, out.problems)
	}
	if err := scope.operation.Budget().ChargeWork(limits.WorkUnits + 1); err == nil {
		t.Fatal("missing global work stop")
	}
	caller, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	_, err = decodeMkDocsYAML(caller, scope, parser.NewSource([]byte("nav: []\n")))
	if !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &quota) || quota.Name != "work-units" {
		t.Fatal("joined lifetime/global causes lost", err)
	}
	normal := mkDocsPatternScopeWithLimits(t, parser.DefaultLimits())
	bad, err := decodeMkDocsYAML(context.Background(), normal, parser.NewSource([]byte("nav: [broken\n")))
	if err != nil || len(bad.problems) == 0 || bad.problems[0].Message() != "Malformed MkDocs YAML." || !bad.problems[0].Location().valid() {
		t.Fatal("syntax stopped being located problem", err, bad.problems)
	}
}
func TestMkDocsSharedLiteralKeysEmptyTagsAndUnicode(t *testing.T) {
	for _, key := range []string{"\"<<\"", "'<<'", "!!str <<"} {
		scope := mkDocsPatternScopeWithLimits(t, parser.DefaultLimits())
		out, err := decodeMkDocsYAML(context.Background(), scope, parser.NewSource([]byte(key+": literal\nnav: []\n")))
		if err != nil || out.root == nil || len(out.problems) != 0 {
			t.Fatal("ordinary MkDocs string key rejected", key, err, out.problems)
		}
		v, ok := out.root.field("<<")
		if !ok || v.text != "literal" {
			t.Fatal("ordinary field omitted", key)
		}
	}
	for _, raw := range []string{"title: !!str\n", "{title: !!str }\n"} {
		scope := mkDocsPatternScopeWithLimits(t, parser.DefaultLimits())
		out, err := decodeMkDocsYAML(context.Background(), scope, parser.NewSource([]byte(raw)))
		if err != nil || out.root == nil || len(out.problems) != 0 {
			t.Fatal("empty tag rejected", err, out.problems)
		}
		v, ok := out.root.field("title")
		if !ok || v.kind != "string" || v.text != "" {
			t.Fatal("empty MkDocs string invented null")
		}
	}
	for _, content := range []string{"\u00a0ABC", "\u2003ABC", "ABC\u2003", "\u00a0"} {
		scope := mkDocsPatternScopeWithLimits(t, parser.DefaultLimits())
		source := parser.NewSource([]byte("\ufefftitle: " + content + "\r\n"))
		out, err := decodeMkDocsYAML(context.Background(), scope, source)
		if err != nil || out.root == nil {
			t.Fatal(err)
		}
		v, ok := out.root.field("title")
		if !ok || v.text != content {
			t.Fatal("Unicode semantic text lost")
		}
		text, err := source.Text(v.location.Span())
		if err != nil || text != content || v.location.Source().Token() != source.Token() {
			t.Fatal("MkDocs Unicode source truncated", text, err)
		}
	}
}

func TestMkDocsCommentedEmptyScalarTags(t *testing.T) {
	for _, tag := range []string{"!!str", "!<tag:yaml.org,2002:str>"} {
		for _, raw := range []string{"title: " + tag + " # inline\r\nother: x\r\n", "{title: " + tag + " # inline\r\n}\r\n", "{title: " + tag + " # inline\r\n, other: x}\r\n"} {
			scope := mkDocsPatternScopeWithLimits(t, parser.DefaultLimits())
			source := parser.NewSource([]byte(raw))
			out, err := decodeMkDocsYAML(context.Background(), scope, source)
			if err != nil || out.root == nil || len(out.problems) != 0 {
				t.Fatal("shared commented empty string unavailable", err, out.problems)
			}
			v, ok := out.root.field("title")
			if !ok || v.kind != "string" || v.text != "" {
				t.Fatal("shared commented tag invented scalar content")
			}
			text, err := source.Text(v.location.Span())
			if err != nil || text != tag {
				t.Fatal("shared commented tag source lost", text, err)
			}
		}
	}
	for _, tag := range []string{"!!int", "!!bool", "!!float"} {
		scope := mkDocsPatternScopeWithLimits(t, parser.DefaultLimits())
		out, err := decodeMkDocsYAML(context.Background(), scope, parser.NewSource([]byte("{x: "+tag+" # comment\n, other: x}\n")))
		if err != nil || len(out.problems) == 0 {
			t.Fatal("shared commented invalid tag invented default", tag, err, out.problems)
		}
	}
	scope := mkDocsPatternScopeWithLimits(t, parser.DefaultLimits())
	out, err := decodeMkDocsYAML(context.Background(), scope, parser.NewSource([]byte("title: !!str null # inline\nx: !!int 42 # inline\ny: !!bool true # inline\n")))
	if err != nil || out.root == nil || len(out.problems) != 0 {
		t.Fatal("authored commented native values lost", err, out.problems)
	}
	title, _ := out.root.field("title")
	if title.text != "null" {
		t.Fatal("authored null string changed")
	}
}

func TestMkDocsScalarTagAnchorOrders(t *testing.T) {
	for _, properties := range []string{"!!str &x 42", "&x !!str 42", "!!bool &x true", "&x !!bool true"} {
		scope := mkDocsPatternScopeWithLimits(t, parser.DefaultLimits())
		source := parser.NewSource([]byte("value: " + properties + "\ncopy: *x\n"))
		out, err := decodeMkDocsYAML(context.Background(), scope, source)
		if err != nil || out.root == nil || len(out.problems) != 0 {
			t.Fatal("shared scalar properties incomplete", properties, err, out.problems)
		}
		for _, key := range []string{"value", "copy"} {
			v, ok := out.root.field(key)
			if !ok {
				t.Fatal("shared scalar properties omitted field")
			}
			if strings.Contains(properties, "!!str") {
				if v.kind != "string" || v.text != "42" {
					t.Fatal("shared tagged anchor lost String42", key, v.kind, v.text)
				}
			} else if v.kind != "bool" || !v.boolean {
				t.Fatal("shared tagged anchor lost Boolean", key, v.kind, v.boolean)
			}
			want := properties
			if key == "copy" {
				want = "*x"
			}
			text, e := source.Text(v.location.Span())
			if e != nil || text != want {
				t.Fatal("shared scalar property/use source", text, e)
			}
		}
	}
}
