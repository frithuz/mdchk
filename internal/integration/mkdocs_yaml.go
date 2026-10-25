package integration

import (
	"context"
	"errors"
	"strings"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/goccy/go-yaml/ast"
)

// MkDocs decodes full configuration YAML with a guarded pinned lexical adapter
// and a direct map to the acquired original source.
type mkDocsStop struct{ err error }
type mkDocsInternalError struct{}

func (*mkDocsInternalError) Error() string { return "MkDocs YAML adapter failed" }

type mkDocsValue struct {
	kind     string
	text     string
	boolean  bool
	location Location
	fields   []mkDocsField
	values   []*mkDocsValue
}
type mkDocsField struct {
	key      string
	location Location
	value    *mkDocsValue
}
type mkDocsYAML struct {
	root     *mkDocsValue
	problems []MkDocsProblem
}

func decodeMkDocsYAML(ctx context.Context, s *Scope, source parser.Source) (out mkDocsYAML, err error) {
	whole, _ := source.Span(0, source.Len())
	block := Location{source, whole}
	defer func() {
		if r := recover(); r != nil {
			if stop, ok := r.(yamlStop); ok {
				err = stop.Cause()
			} else if stop, ok := r.(mkDocsStop); ok {
				err = stop.err
			} else {
				err = &mkDocsInternalError{}
			}
		}
		err = errors.Join(err, s.checkpoint(ctx, 0, 0))
	}()
	charge := func(work, nodes int64) {
		if e := s.checkpoint(ctx, work, nodes); e != nil {
			panic(mkDocsStop{e})
		}
	}
	d, file, e := newYAMLDecoder(ctx, s, source)
	issue := func(loc Location, message string) {
		charge(int64(len(message))+1, 1)
		out.problems = append(out.problems, MkDocsProblem{loc, message})
	}
	var limit *parser.LimitExceeded
	if errors.As(e, &limit) || errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) {
		return out, e
	}
	if e != nil || file == nil {
		issue(block, "Malformed MkDocs YAML.")
		return out, nil
	}
	if len(file.Docs) != 1 || file.Docs[0].Body == nil {
		issue(block, "Expected one MkDocs configuration mapping.")
		return out, nil
	}
	root := file.Docs[0].Body
	_, _, kind := d.read(root, 0, nil, false)
	for i := 0; i < d.IssueCount(); i++ {
		span, message, _ := d.Issue(i)
		switch message {
		case "Invalid YAML block-scalar header.":
			message = "Invalid MkDocs block-scalar header."
		case "Malformed YAML quoted escape.":
			message = "Malformed MkDocs quoted escape."
		}
		issue(Location{source, span}, message)
	}
	if kind != yamlMapping {
		issue(block, "MkDocs configuration must be a mapping.")
		return out, nil
	}
	var project func(ast.Node, int, *ast.AliasNode) *mkDocsValue
	active := map[ast.Node]bool{}
	project = func(n ast.Node, depth int, use *ast.AliasNode) *mkDocsValue {
		charge(1, 1)
		if int64(depth) > min(int64(64), s.operation.Budget().Limits().SyntaxDepth) {
			panic(mkDocsStop{&parser.LimitExceeded{Name: "structured-input-depth", Observed: int64(depth), Allowed: min(int64(64), s.operation.Budget().Limits().SyntaxDepth)}})
		}
		if n == nil {
			return &mkDocsValue{kind: "unknown", location: block}
		}
		loc := d.scalarLocation(n, use)
		if active[n] {
			return &mkDocsValue{kind: "unknown", location: loc}
		}
		active[n] = true
		defer delete(active, n)
		switch v := n.(type) {
		case *ast.MappingKeyNode:
			return project(v.Value, depth+1, use)
		case *ast.AnchorNode:
			out := project(v.Value, depth+1, use)
			if use == nil {
				out.location = mkDocsCover(loc, out.location)
			}
			return out
		case *ast.AliasNode:
			target := d.Target(v)
			if target == nil {
				return &mkDocsValue{kind: "unknown", location: loc}
			}
			if use == nil {
				use = v
			}
			return project(target, depth+1, use)
		case *ast.TagNode:
			value, location, kind := d.read(v, depth, use, false)
			if kind == yamlString {
				return &mkDocsValue{kind: "string", text: value, location: location}
			}
			tag := v.Start.Value
			if tag == "!!map" || tag == "!!seq" || strings.HasSuffix(tag, ":map>") || strings.HasSuffix(tag, ":seq>") {
				return project(v.Value, depth+1, use)
			}
			if kind == yamlOther {
				child := v.Value
				if anchor, ok := child.(*ast.AnchorNode); ok {
					charge(1, 0)
					child = anchor.Value
				}
				text, ok := yamlTaggedScalar(child)
				if ok && (tag == "!!bool" || strings.HasSuffix(tag, ":bool>")) {
					return &mkDocsValue{kind: "bool", boolean: strings.EqualFold(text, "true"), location: location}
				}
				if tag == "!!null" || strings.HasSuffix(tag, ":null>") {
					return &mkDocsValue{kind: "null", location: location}
				}
				return &mkDocsValue{kind: "other", location: location}
			}
			return &mkDocsValue{kind: "unknown", location: loc}
		case *ast.MappingNode:
			charge(int64(len(v.Values))+1, int64(len(v.Values)))
			out := &mkDocsValue{kind: "map", location: loc, fields: make([]mkDocsField, 0, len(v.Values))}
			for _, entry := range v.Values {
				key := project(entry.Key, depth+1, use)
				value := project(entry.Value, depth+1, use)
				out.fields = append(out.fields, mkDocsField{key.text, key.location, value})
				if key.kind != "string" {
					out.kind = "unknown"
				}
				out.location = mkDocsCover(out.location, value.location)
			}
			if v.End != nil {
				out.location = mkDocsCover(out.location, d.tokenLocation(v.End))
			}
			return out
		case *ast.SequenceNode:
			charge(int64(len(v.Values))+1, int64(len(v.Values)))
			out := &mkDocsValue{kind: "list", location: loc, values: make([]*mkDocsValue, 0, len(v.Values))}
			for _, entry := range v.Values {
				value := project(entry, depth+1, use)
				out.values = append(out.values, value)
				out.location = mkDocsCover(out.location, value.location)
			}
			if v.End != nil {
				out.location = mkDocsCover(out.location, d.tokenLocation(v.End))
			}
			return out
		case *ast.StringNode:
			if d.InvalidQuote(v.Token) {
				return &mkDocsValue{kind: "unknown", location: loc}
			}
			value, location, kind := d.read(v, depth, use, false)
			if kind != yamlString {
				return &mkDocsValue{kind: "other", location: location}
			}
			return &mkDocsValue{kind: "string", text: value, location: location}
		case *ast.LiteralNode:
			value, location, kind := d.read(v, depth, use, false)
			if kind != yamlString {
				return &mkDocsValue{kind: "unknown", location: location}
			}
			return &mkDocsValue{kind: "string", text: value, location: location}
		case *ast.BoolNode:
			return &mkDocsValue{kind: "bool", boolean: v.Value, location: loc}
		case *ast.NullNode:
			return &mkDocsValue{kind: "null", location: loc}
		default:
			return &mkDocsValue{kind: "other", location: loc}
		}
	}
	out.root = project(root, 0, nil)
	return out, nil
}
func mkDocsCover(a, b Location) Location {
	if !a.valid() {
		return b
	}
	if !b.valid() || a.source.Token() != b.source.Token() {
		return a
	}
	span, e := a.source.Span(min(a.span.Start(), b.span.Start()), max(a.span.End(), b.span.End()))
	if e != nil {
		return a
	}
	return Location{a.source, span}
}
func (v *mkDocsValue) field(name string) (*mkDocsValue, bool) {
	if v == nil {
		return nil, false
	}
	for _, f := range v.fields {
		if f.key == name {
			return f.value, true
		}
	}
	return nil, false
}
