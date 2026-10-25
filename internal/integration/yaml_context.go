package integration

import (
	"context"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/yamlsource"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/token"
)

type yamlStop = yamlsource.Stop

const (
	yamlUnknown = iota
	yamlString
	yamlOther
	yamlMapping
	yamlSequence
)

type yamlDecoder struct {
	*yamlsource.Decoder
	source parser.Source
}

func (d yamlDecoder) scalarLocation(n ast.Node, use *ast.AliasNode) Location {
	return Location{d.source, d.NodeSpan(n, use)}
}
func (d yamlDecoder) tokenLocation(t *token.Token) Location {
	return Location{d.source, d.TokenSpan(t)}
}
func (d yamlDecoder) read(n ast.Node, depth int, use *ast.AliasNode, _ bool) (string, Location, int) {
	text, span, kind := d.Read(n, depth, use)
	return text, Location{d.source, span}, kind
}
func yamlTaggedScalar(n ast.Node) (string, bool) { return yamlsource.TaggedScalar(n) }
func newYAMLDecoder(ctx context.Context, s *Scope, source parser.Source) (yamlDecoder, *ast.File, error) {
	whole, _ := source.Span(0, source.Len())
	d, file, err := yamlsource.Parse(source, whole, func(w, n int64) error { return s.checkpoint(ctx, w, n) }, min(int64(64), s.operation.Budget().Limits().SyntaxDepth), s.operation.Budget().Limits().DocumentNodes)
	return yamlDecoder{d, source}, file, err
}
