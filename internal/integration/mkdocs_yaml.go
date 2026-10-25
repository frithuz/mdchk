package integration

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/goccy/go-yaml/ast"
	yamlparser "github.com/goccy/go-yaml/parser"
	"github.com/goccy/go-yaml/token"
)

// MkDocs decodes full configuration YAML with a guarded pinned lexical adapter
// and a direct map to the acquired original source.
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
				err = stop.err
			} else {
				err = &mkDocsInternalError{}
			}
		}
		err = errors.Join(err, s.checkpoint(ctx, 0, 0))
	}()
	charge := func(work, nodes int64) {
		if e := s.checkpoint(ctx, work, nodes); e != nil {
			panic(yamlStop{e})
		}
	}
	charge(8*int64(source.Len())+1, 4)
	raw, _ := source.Text(whole)
	skip := 0
	if strings.HasPrefix(raw, "\xef\xbb\xbf") {
		skip = 3
	}
	input := raw[skip:]
	// Normalize YAML physical line breaks for the pinned scanner while mapping
	// every retained content byte directly back to the acquired source. CRLF
	// bytes remain original in Source and in all authored token ranges.
	charge(8*int64(len(input))+1, 2*int64(len(input))+3)
	var normalized strings.Builder
	normalized.Grow(len(input))
	maps := make([]yamlMap, 0)
	cursor := skip
	for i := skip; i < len(raw); i++ {
		if raw[i] != '\r' {
			continue
		}
		if i > cursor {
			start := normalized.Len()
			normalized.WriteString(raw[cursor:i])
			span, _ := source.Span(cursor, i)
			maps = append(maps, yamlMap{start, normalized.Len(), span, true})
		}
		end := i + 1
		if end < len(raw) && raw[end] == '\n' {
			end++
			i++
		}
		start := normalized.Len()
		normalized.WriteByte('\n')
		var span parser.Span
		// The normalization segment starts at the actual CR, including a CRLF.
		cr := i
		if raw[i] == '\n' {
			cr--
		}
		span, _ = source.Span(cr, end)
		maps = append(maps, yamlMap{start, normalized.Len(), span, false})
		cursor = end
	}
	if cursor < len(raw) {
		start := normalized.Len()
		normalized.WriteString(raw[cursor:])
		span, _ := source.Span(cursor, len(raw))
		maps = append(maps, yamlMap{start, normalized.Len(), span, true})
	}
	input = normalized.String()
	result := yamlResult{block: block}
	mapper := yamlMapper{source: source, body: maps, lines: []yamlLine{{0, len(input), 0}}, charge: charge}
	d := yamlDecoder{scope: s, ctx: ctx, text: input, mapper: mapper, out: &result, anchors: map[string]ast.Node{}, bindings: map[*ast.AliasNode]ast.Node{}, active: map[ast.Node]bool{}}
	d.starts = []int{0}
	for i := range input {
		if input[i] == '\n' {
			charge(1, 1)
			d.starts = append(d.starts, i+1)
		}
	}
	adapted, e := d.tagSeparatorInput()
	if e != nil {
		return out, e
	}
	tokens, e := d.lexTokens(adapted)
	if e != nil {
		return out, e
	}
	for _, tk := range tokens {
		if (tk.Type == token.LiteralType || tk.Type == token.FoldedType) && !yamlBlockHeader.MatchString(tk.Value) {
			d.issue(tk, "Invalid MkDocs block-scalar header.")
		}
	}
	d.prepareOffsets(tokens)
	invalidQuotes := map[*token.Token]bool{}
	for _, tk := range tokens {
		if tk.Type != token.DoubleQuoteType {
			continue
		}
		start := d.offset(tk)
		if start < 0 {
			continue
		}
		end := d.quotedTokenEnd(tk, start)
		if end <= start || end > len(d.text) {
			continue
		}
		charge(int64(end-start)+1, 1)
		if !mkDocsValidNumericEscapes(d.text[start:end]) {
			invalidQuotes[tk] = true
			d.issue(tk, "Malformed MkDocs quoted escape.")
		}
	}
	tokens = yamlNormalizeEmptyBlocks(tokens)
	file, e := yamlparser.Parse(tokens, 0, yamlparser.AllowDuplicateMapKey())
	issue := func(loc Location, message string) {
		charge(int64(len(message))+1, 1)
		out.problems = append(out.problems, MkDocsProblem{loc, message})
	}
	if e != nil {
		issue(block, "Malformed MkDocs YAML.")
		return out, nil
	}
	if len(file.Docs) != 1 || file.Docs[0].Body == nil {
		issue(block, "Expected one MkDocs configuration mapping.")
		return out, nil
	}
	root := file.Docs[0].Body
	_, _, kind := d.read(root, 0, nil, false)
	for _, p := range result.issues {
		issue(p.location, p.message)
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
			panic(yamlStop{&parser.LimitExceeded{Name: "structured-input-depth", Observed: int64(depth), Allowed: min(int64(64), s.operation.Budget().Limits().SyntaxDepth)}})
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
			return project(v.Value, depth+1, use)
		case *ast.AliasNode:
			target := d.bindings[v]
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
				text, ok := yamlTaggedScalar(v.Value)
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
			if invalidQuotes[v.Token] {
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

// The pinned dependency owns quoted token boundaries and semantic decoding.
// Validate only numeric escape syntax that its scanner otherwise accepts after
// failed hex conversion, so malformed source never becomes invented text.
func mkDocsValidNumericEscapes(raw string) bool {
	for i := 1; i < len(raw)-1; i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if i >= len(raw)-1 {
			return false
		}
		width := 0
		switch raw[i] {
		case 'x':
			width = 2
		case 'u':
			width = 4
		case 'U':
			width = 8
		default:
			continue
		}
		if i+width >= len(raw) {
			return false
		}
		digits := raw[i+1 : i+1+width]
		for j := range digits {
			c := digits[j]
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
				return false
			}
		}
		value, e := strconv.ParseUint(digits, 16, 32)
		if e != nil || value > utf8.MaxRune {
			return false
		}
		if value >= 0xD800 && value <= 0xDFFF {
			// The pinned decoder accepts an adjacent UTF-16 surrogate pair.
			// Isolated surrogates cannot denote a Unicode scalar value.
			next := i + width + 1
			if width != 4 || value > 0xDBFF || next+6 > len(raw)-1 || raw[next:next+2] != `\u` {
				return false
			}
			low, e := strconv.ParseUint(raw[next+2:next+6], 16, 16)
			if e != nil || low < 0xDC00 || low > 0xDFFF {
				return false
			}
			i += 6
		}
		i += width
	}
	return true
}
