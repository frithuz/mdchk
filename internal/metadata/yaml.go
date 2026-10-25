package metadata

import (
	"context"
	"errors"
	"strings"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/yamlsource"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/token"
)

type yamlProjection struct {
	ctx          context.Context
	b            Binding
	source       *yamlsource.Decoder
	anchors      map[string]ast.Node
	bindings     map[*ast.AliasNode]ast.Node
	active       map[ast.Node]bool
	problems     []Problem
	restrictions MoveRestrictions
}

func decodeYAML(ctx context.Context, b Binding) (out Result, err error) {
	defer func() {
		if caught := recover(); caught != nil {
			if stop, ok := caught.(yamlsource.Stop); ok {
				err = stop.Cause()
			} else {
				panic(caught)
			}
		}
		err = errors.Join(err, b.charge(ctx, 0, 0))
	}()
	text, _ := b.Source().Text(b.Header().Content())
	if strings.Trim(text, " \t\r\n") == "" {
		root, e := collection(ctx, b, b.Header().Content(), Mapping, nil, nil, 0)
		if e != nil {
			return Result{}, e
		}
		return result(ctx, b, root, nil)
	}
	d, file, e := yamlsource.Parse(b.Source(), b.Header().Content(), func(w, n int64) error { return b.charge(ctx, w, n) }, b.state.budget.Limits().SyntaxDepth, b.state.budget.Limits().DocumentNodes)
	var limit *parser.LimitExceeded
	if errors.As(e, &limit) {
		return Result{}, e
	}
	if e != nil || file == nil || d.HasInvalidQuotes() {
		return decodeFailure(ctx, b, Malformed, b.Header().Content())
	}
	if d.IssueCount() > 0 {
		span, _, _ := d.Issue(0)
		return decodeFailure(ctx, b, Malformed, span)
	}
	p := yamlProjection{ctx: ctx, b: b, source: d, anchors: map[string]ast.Node{}, bindings: map[*ast.AliasNode]ast.Node{}, active: map[ast.Node]bool{}}
	// Library-owned comment tokens distinguish comments from # inside strings.
	if len(file.Docs) == 0 {
		root, e := collection(ctx, b, b.Header().Content(), Mapping, nil, nil, 0)
		if e != nil {
			return Result{}, e
		}
		return result(ctx, b, root, nil)
	}
	if len(file.Docs) != 1 || file.Docs[0].Body == nil {
		return decodeFailure(ctx, b, Malformed, b.Header().Content())
	}
	root, e := p.project(file.Docs[0].Body, 1, parser.Span{}, true)
	if e != nil {
		return Result{}, e
	}
	if root.Kind() != Mapping {
		return decodeFailure(ctx, b, Unsupported, root.Span())
	}
	return result(ctx, b, root, p.problems)
}
func (p *yamlProjection) problem(span parser.Span) (Value, error) {
	if err := p.b.charge(p.ctx, 1, 1); err != nil {
		return Value{}, err
	}
	p.problems = append(p.problems, Problem{span, Unsupported})
	return scalar(p.ctx, p.b, span, Unknown, "", false)
}
func (p *yamlProjection) cover(a, b parser.Span) parser.Span {
	if !a.Present() {
		return b
	}
	if !b.Present() {
		return a
	}
	span, _ := p.b.Source().Span(min(a.Start(), b.Start()), max(a.End(), b.End()))
	return span
}
func (p *yamlProjection) project(n ast.Node, depth int64, use parser.Span, root bool) (Value, error) {
	if err := p.b.charge(p.ctx, 1, 1); err != nil {
		return Value{}, err
	}
	if depth > p.b.state.budget.Limits().SyntaxDepth {
		return Value{}, &parser.LimitExceeded{Name: "syntax-depth", Observed: depth, Allowed: p.b.state.budget.Limits().SyntaxDepth}
	}
	if n == nil {
		return scalar(p.ctx, p.b, p.b.Header().Content(), Null, "", false)
	}
	span := p.source.NodeSpan(n, nil)
	if use.Present() {
		span = use
	}
	if !p.b.owns(span) {
		span = p.b.Header().Content()
	}
	if p.active[n] {
		return p.problem(span)
	}
	p.active[n] = true
	defer delete(p.active, n)
	switch v := n.(type) {
	case *ast.MappingKeyNode:
		return p.project(v.Value, depth, use, root)
	case *ast.AnchorNode:
		p.restrictions |= AnchorsOrAliases
		if !use.Present() {
			p.anchors[v.Name.GetToken().Value] = v.Value
		}
		out, err := p.project(v.Value, depth, use, root)
		if err != nil {
			return Value{}, err
		}
		if !use.Present() && !root {
			out.state.span = p.cover(span, out.Span())
		}
		out.state.restrictions |= AnchorsOrAliases
		return out, nil
	case *ast.AliasNode:
		p.restrictions |= AnchorsOrAliases
		if !use.Present() {
			use = p.source.NodeSpan(n, v)
		}
		target, visited := p.bindings[v]
		if !visited {
			if err := p.b.charge(p.ctx, int64(len(v.Value.GetToken().Value))+1, 1); err != nil {
				return Value{}, err
			}
			target = p.anchors[v.Value.GetToken().Value]
			p.bindings[v] = target
		}
		if target == nil {
			return p.problem(use)
		}
		out, err := p.project(target, depth, use, root)
		if err == nil {
			out.state.restrictions |= AnchorsOrAliases
		}
		return out, err
	case *ast.TagNode:
		tag := v.Start.Value
		if strings.HasPrefix(tag, "!!") {
			tag = tag[2:]
		} else if strings.HasPrefix(tag, "!<tag:yaml.org,2002:") && strings.HasSuffix(tag, ">") {
			tag = strings.TrimSuffix(strings.TrimPrefix(tag, "!<tag:yaml.org,2002:"), ">")
		} else {
			return p.problem(span)
		}
		childSpan := p.source.NodeSpan(v.Value, nil)
		if !use.Present() {
			span = p.cover(span, childSpan)
		}
		if tag == "map" || tag == "seq" {
			out, err := p.project(v.Value, depth, use, root)
			if err != nil {
				return Value{}, err
			}
			want := Mapping
			if tag == "seq" {
				want = Sequence
			}
			if out.Kind() != want {
				return p.problem(span)
			}
			if !root {
				out.state.span = p.cover(span, out.Span())
			}
			return out, nil
		}
		child := v.Value
		if anchor, ok := child.(*ast.AnchorNode); ok {
			// Both properties describe this scalar. Bind the tagged node, not
			// its untagged content, so numeric-looking strings stay strings.
			name := anchor.Name.GetToken().Value
			if err := p.b.charge(p.ctx, int64(len(name))+1, 1); err != nil {
				return Value{}, err
			}
			p.restrictions |= AnchorsOrAliases
			if !use.Present() {
				p.anchors[name] = v
			}
			child = anchor.Value
			if !use.Present() {
				span = p.cover(span, p.source.NodeSpan(child, nil))
			}
		}
		text, ok := yamlsource.TaggedScalar(child)
		if !ok {
			return p.problem(span)
		}
		if tag == "int" || tag == "float" {
			if err := reserveNumeric(p.ctx, p.b, text); err != nil {
				return Value{}, err
			}
		}
		kind, semantic, boolean, ok := yamlNative(text, tag, false)
		if !ok {
			return p.problem(span)
		}
		return scalar(p.ctx, p.b, span, kind, semantic, boolean)
	case *ast.MappingNode:
		if err := p.b.charge(p.ctx, int64(len(v.Values))+1, 2*int64(len(v.Values))+1); err != nil {
			return Value{}, err
		}
		entries := make([]Entry, 0, len(v.Values))
		var loc parser.Span
		restrictions := MoveRestrictions(0)
		incomplete := false
		for _, field := range v.Values {
			key, err := p.project(field.Key, depth+1, use, false)
			if err != nil {
				return Value{}, err
			}
			value, err := p.project(field.Value, depth+1, use, false)
			if err != nil {
				return Value{}, err
			}
			keyText, ok := key.Scalar()
			if key.Kind() != String || !ok {
				incomplete = true
				if _, err := p.problem(key.Span()); err != nil {
					return Value{}, err
				}
				continue
			}
			entrySpan := parser.Span{}
			if !use.Present() && !v.IsFlowStyle {
				entrySpan = p.wholeEntry(key.Span(), value.Span())
			}
			item, err := entry(p.ctx, p.b, keyText, key.Span(), entrySpan, value)
			if err != nil {
				return Value{}, err
			}
			entries = append(entries, item)
			loc = p.cover(loc, p.cover(key.Span(), value.Span()))
			loc = p.cover(loc, entrySpan)
		}
		if use.Present() {
			loc = use
			restrictions |= AnchorsOrAliases
		} else if v.IsFlowStyle {
			loc = p.cover(span, loc)
			if v.End != nil {
				loc = p.cover(loc, p.source.TokenSpan(v.End))
			}
		} else {
			loc = p.cover(span, loc)
		}
		if root {
			loc = p.b.Header().Content()
		}
		if !loc.Present() {
			loc = span
		}
		restrictions |= p.commentRestrictions(n)
		out, err := collection(p.ctx, p.b, loc, Mapping, entries, nil, restrictions|p.restrictions)
		if err == nil && incomplete {
			out.state.complete = false
		}
		return out, err
	case *ast.SequenceNode:
		if err := p.b.charge(p.ctx, int64(len(v.Values))+1, int64(len(v.Values))+1); err != nil {
			return Value{}, err
		}
		values := make([]Value, 0, len(v.Values))
		loc := span
		for _, child := range v.Values {
			value, err := p.project(child, depth+1, use, false)
			if err != nil {
				return Value{}, err
			}
			values = append(values, value)
			loc = p.cover(loc, value.Span())
		}
		if v.End != nil && !use.Present() {
			loc = p.cover(loc, p.source.TokenSpan(v.End))
		}
		if use.Present() {
			loc = use
		}
		return collection(p.ctx, p.b, loc, Sequence, nil, values, p.commentRestrictions(n)|p.restrictions)
	case *ast.MergeKeyNode:
		return p.problem(span)
	case *ast.LiteralNode:
		if v.Value == nil {
			return p.problem(span)
		}
		return scalar(p.ctx, p.b, span, String, v.Value.Value, false)
	default:
		tk := n.GetToken()
		if tk == nil {
			return p.problem(span)
		}
		text := tk.Value
		quoted := tk.Type == token.SingleQuoteType || tk.Type == token.DoubleQuoteType
		if v, ok := n.(*ast.StringNode); ok {
			text = v.Value
		}
		if !quoted && (coreInteger.MatchString(text) || decimalNumber.MatchString(text)) {
			if err := reserveNumeric(p.ctx, p.b, text); err != nil {
				return Value{}, err
			}
		}
		kind, semantic, boolean, ok := yamlNative(text, "", quoted)
		if !ok {
			return p.problem(span)
		}
		return scalar(p.ctx, p.b, span, kind, semantic, boolean)
	}
}
func yamlNative(text, tag string, quoted bool) (Kind, string, bool, bool) {
	if quoted && tag == "" || tag == "str" {
		return String, text, false, true
	}
	if tag == "null" || tag == "" {
		if text == "" || text == "~" || text == "null" || text == "Null" || text == "NULL" {
			return Null, "", false, true
		}
		if tag != "" {
			return Unknown, "", false, false
		}
	}
	if tag == "bool" || tag == "" {
		switch text {
		case "true", "True", "TRUE":
			return Boolean, "", true, true
		case "false", "False", "FALSE":
			return Boolean, "", false, true
		}
		if tag != "" {
			return Unknown, "", false, false
		}
	}
	if tag == "int" || tag == "" {
		if coreInteger.MatchString(text) {
			semantic, ok := canonicalInteger(text)
			return Integer, semantic, false, ok
		}
		if tag != "" {
			return Unknown, "", false, false
		}
	}
	if tag == "float" || tag == "" {
		if decimalNumber.MatchString(text) || yamlCoreSpecial(text) {
			semantic, ok := canonicalFloat(text)
			return Float, semantic, false, ok
		}
		if tag != "" {
			return Unknown, "", false, false
		}
	}
	if tag != "" {
		return Unknown, "", false, false
	}
	return String, text, false, true
}
func (p *yamlProjection) wholeEntry(key, value parser.Span) parser.Span {
	if !key.Present() || !value.Present() {
		return parser.Span{}
	}
	source := p.b.Source()
	payload := p.b.Header().Content()
	text, _ := source.Text(payload)
	start := key.Start() - payload.Start()
	end := value.End() - payload.Start()
	// A key has a physical entry only if nothing but indentation precedes it.
	line := strings.LastIndexAny(text[:start], "\r\n") + 1
	if strings.Trim(text[line:start], " \t") != "" {
		return parser.Span{}
	}
	if end > 0 && (text[end-1] == '\n' || text[end-1] == '\r') {
		return decodeSpan(p.b, line, end)
	}
	for end < len(text) && text[end] != '\r' && text[end] != '\n' {
		end++
	}
	if end < len(text) {
		if text[end] == '\r' && end+1 < len(text) && text[end+1] == '\n' {
			end += 2
		} else {
			end++
		}
	}
	return decodeSpan(p.b, line, end)
}

// Comments are inspected from maintained AST tokens, never a raw # heuristic.
func (p *yamlProjection) commentRestrictions(n ast.Node) MoveRestrictions {
	if p.source.HasStandaloneComments() {
		return StandaloneComments
	}
	return 0
}

func yamlCoreSpecial(text string) bool {
	switch text {
	case ".nan", ".NaN", ".NAN", ".inf", ".Inf", ".INF", "+.inf", "+.Inf", "+.INF", "-.inf", "-.Inf", "-.INF":
		return true
	default:
		return false
	}
}
