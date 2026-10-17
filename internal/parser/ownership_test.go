package parser_test

import (
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestOpaqueHandleReplacementCannotMutateStoredModel(t *testing.T) {
	s := parser.NewSource([]byte("x"))
	child := node(t, parser.NodeSpec{Kind: parser.Text, Envelope: span(t, s, 0, 1)})
	root := node(t, parser.NodeSpec{Kind: parser.DocumentKind, Envelope: span(t, s, 0, 1), Children: []*parser.Node{child}})
	definition, err := parser.NewReferenceDefinition(parser.ReferenceSpec{Envelope: span(t, s, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	document, err := parser.NewDocument(root, parser.ReferenceCollection{Availability: parser.Available, LookupAvailability: parser.Unavailable, Definitions: []*parser.ReferenceDefinition{definition}})
	if err != nil {
		t.Fatal(err)
	}
	// Replacing an opaque Go struct through its exported pointer must not let
	// construction inputs or accessor outputs mutate another retained model.
	*child = parser.Node{}
	*root = parser.Node{}
	*definition = parser.ReferenceDefinition{}
	*document.Root() = parser.Node{}
	*document.Root().Children()[0] = parser.Node{}
	*document.References().Definitions[0] = parser.ReferenceDefinition{}
	visits := 0
	parser.Walk(document.Root(), func(n *parser.Node) bool { visits++; *n = parser.Node{}; return true })
	if visits != 2 || document.Root().Kind() != parser.DocumentKind || document.Root().Children()[0].Kind() != parser.Text || !document.References().Definitions[0].Properties().Envelope.Present() {
		t.Fatal("opaque handle assignment modified stored model")
	}
}
