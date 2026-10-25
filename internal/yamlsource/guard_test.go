package yamlsource

import (
	"errors"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/goccy/go-yaml/ast"
)

func TestDepthGuardBeforeAST(t *testing.T) {
	for _, text := range []string{"a: [[[1]]]\n", "a:\n  b:\n    c:\n      d: 1\n", "a: [{b: [1]}]\n", "a: !one !two !three 1\n", "a: &one &two &three 1\n", "a: !one [!two [1]]\n", "a:\n- b:\n  - c: 1\n"} {
		source := parser.NewSource([]byte(text))
		span, _ := source.Span(0, len(text))
		_, file, err := Parse(source, span, func(int64, int64) error { return nil }, 4, 10000)
		var quota *parser.LimitExceeded
		if file != nil || !errors.As(err, &quota) || quota.Name != "syntax-depth" {
			t.Fatalf("recursive AST admitted %q before depth guard: %v", text, err)
		}
	}
	for _, text := range []string{"a: [[1]]\n", "a: \"[[[1]]]\" # [[[\n", "a: |\n  [[[1]]]\n", "a: &one [1]\n", "a:\n- [1]\n"} {
		source := parser.NewSource([]byte(text))
		span, _ := source.Span(0, len(text))
		_, file, err := Parse(source, span, func(int64, int64) error { return nil }, 4, 10000)
		if err != nil || file == nil {
			t.Fatalf("boundary rejected %q: %v", text, err)
		}
	}
}

func TestSyntheticPositionIndexReservesBeforeAllocation(t *testing.T) {
	source := parser.NewSource([]byte("{雪: }\r\n"))
	span, _ := source.Span(0, source.Len())
	stop := false
	want := &parser.LimitExceeded{Name: "document-nodes", Observed: 101, Allowed: 100}
	d, file, err := Parse(source, span, func(int64, int64) error {
		if stop {
			return want
		}
		return nil
	}, 20, 10000)
	if err != nil || file == nil {
		t.Fatal(err)
	}
	root := file.Docs[0].Body.(*ast.MappingNode)
	stop = true
	var caught any
	func() { defer func() { caught = recover() }(); d.NodeSpan(root.Values[0].Value, nil) }()
	if d.positions != nil {
		t.Fatal("synthetic positions allocated before reservation")
	}
	failure, ok := caught.(Stop)
	if !ok {
		t.Fatal("missing canonical index admission stop", caught)
	}
	var quota *parser.LimitExceeded
	if !errors.As(failure.Cause(), &quota) || *quota != *want {
		t.Fatal("index admission replaced canonical cause", failure.Cause())
	}
}
