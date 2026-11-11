package parser_test

import (
	"context"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestHeadingScalarsOriginalOwnershipAndSemantics(t *testing.T) {
	source := parser.NewSource([]byte("# **é** &fjlig; `code`\n\nA\nB\n===\n"))
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	parsed := parser.ParseWithBudget(context.Background(), source, budget)
	got, err := parsed.Document.HeadingScalars(context.Background(), budget)
	if err != nil || len(got) != 2 {
		t.Fatal(got, err)
	}
	for i, want := range []string{"é fj code", "A\nB"} {
		var visible strings.Builder
		for _, s := range got[i] {
			if _, err := visible.WriteRune(s.Scalar); err != nil {
				t.Fatal(err)
			}
		}
		if visible.String() != want {
			t.Fatal(visible.String())
		}
	}
	if got[0][0].Token.Start() != 4 || got[0][0].Token.End() != 6 || got[0][0].Token.SourceToken() != source.Token() {
		t.Fatal(got[0][0])
	}
	if got[0][2].Token.Start() != 9 || got[0][2].Token.End() != 16 || got[0][2].Token != got[0][3].Token {
		t.Fatal(got[0])
	}
	for _, s := range got[0][5:] {
		if !s.Code || s.Token.Present() {
			t.Fatal(s)
		}
	}
	got[0][0].Scalar = 'x'
	again, err := parsed.Document.HeadingScalars(context.Background(), budget)
	if err != nil || again[0][0].Scalar != 'é' {
		t.Fatal(again, err)
	}
	foreign, _ := parser.NewOperationBudget(parser.DefaultLimits())
	if _, err := parsed.Document.HeadingScalars(context.Background(), foreign); err == nil {
		t.Fatal("foreign budget")
	}
	index := parser.NewLineIndex(source)
	if err := parsed.Document.HeadingCapitalizationCheckpoint(context.Background(), budget, index, 1, 1); err != nil {
		t.Fatal(err)
	}
	if err := parsed.Document.HeadingCapitalizationCheckpoint(context.Background(), foreign, index, 0, 0); err == nil {
		t.Fatal("foreign witness")
	}
	if err := parsed.Document.HeadingCapitalizationCheckpoint(context.Background(), budget, parser.NewLineIndex(parser.NewSource(source.Bytes())), 0, 0); err == nil {
		t.Fatal("foreign index")
	}
	constructed, err := parser.NewDocument(parsed.Document.Root(), parsed.Document.References())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := constructed.HeadingScalars(context.Background(), budget); err == nil {
		t.Fatal("constructed document")
	}
}
