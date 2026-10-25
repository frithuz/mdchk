package yamlsource

import (
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/goccy/go-yaml/ast"
	yamlparser "github.com/goccy/go-yaml/parser"
	"github.com/goccy/go-yaml/token"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Parse reserves lexical storage before every library entry and checks token depth
// before the recursive AST parser. Payload is a span in the original source.
func Parse(source parser.Source, payload parser.Span, checkpoint func(int64, int64) error, depth, nodeLimit int64) (*Decoder, *ast.File, error) {
	block := Location{source, payload}
	charge := func(work, nodes int64) {
		if err := checkpoint(work, nodes); err != nil {
			panic(yamlStop{err})
		}
	}
	charge(8*int64((payload.End()-payload.Start()))+1, 4)
	raw, _ := source.Text(payload)
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
			span, _ := source.Span(payload.Start()+cursor, payload.Start()+i)
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
		span, _ = source.Span(payload.Start()+cr, payload.Start()+end)
		maps = append(maps, yamlMap{start, normalized.Len(), span, false})
		cursor = end
	}
	if cursor < len(raw) {
		start := normalized.Len()
		normalized.WriteString(raw[cursor:])
		span, _ := source.Span(payload.Start()+cursor, payload.End())
		maps = append(maps, yamlMap{start, normalized.Len(), span, true})
	}
	input = normalized.String()
	result := yamlResult{block: block}
	mapper := yamlMapper{source: source, body: maps, lines: []yamlLine{{0, len(input), 0}}, charge: charge}
	d := yamlDecoder{checkpoint: checkpoint, depth: depth, nodeLimit: nodeLimit, text: input, mapper: mapper, out: &result, anchors: map[string]ast.Node{}, bindings: map[*ast.AliasNode]ast.Node{}, active: map[ast.Node]bool{}}
	d.starts = []int{0}
	for i := range input {
		if input[i] == '\n' {
			charge(1, 1)
			d.starts = append(d.starts, i+1)
		}
	}
	adapted, e := d.tagSeparatorInput()
	if e != nil {
		return nil, nil, e
	}
	tokens, e := d.lexTokens(adapted)
	if e != nil {
		return nil, nil, e
	}
	for _, tk := range tokens {
		if (tk.Type == token.LiteralType || tk.Type == token.FoldedType) && !yamlBlockHeader.MatchString(tk.Value) {
			d.issue(tk, "Invalid YAML block-scalar header.")
		}
	}
	d.prepareOffsets(tokens)
	for _, tk := range tokens {
		if tk.Type == token.CommentType {
			start := d.offset(tk)
			if start >= 0 {
				line := strings.LastIndexByte(d.text[:start], '\n') + 1
				if strings.Trim(d.text[line:start], " \t") == "" {
					d.standalone = true
				}
			}
		}
	}

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
		if !ValidNumericEscapes(d.text[start:end]) {
			invalidQuotes[tk] = true
			d.issue(tk, "Malformed YAML quoted escape.")
		}
	}
	tokens = d.adaptScalarTags(yamlNormalizeEmptyBlocks(tokens))
	file, e := yamlparser.Parse(tokens, 0, yamlparser.AllowDuplicateMapKey())
	d.invalidQuotes = invalidQuotes
	return &d, file, e

}
func ValidNumericEscapes(raw string) bool {
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

// The maintained lexer proves tag/scalar/delimiter ownership. Reconcile two
// pinned AST limitations: an explicit string tag owns a literal merge spelling,
// and omitted scalar content (including comments) must not become invented
// native defaults. Block ownership comes from the existing guarded token pass.
func (d *yamlDecoder) adaptScalarTags(tokens token.Tokens) token.Tokens {
	d.charge(3*int64(len(tokens))+1, 2*int64(len(tokens))+1)
	out := make(token.Tokens, 0, 2*len(tokens))
	for i := 0; i < len(tokens); i++ {
		tk := tokens[i]
		out = append(out, tk)
		if tk.Type != token.TagType {
			continue
		}
		tag := tk.Value
		if tag != "!!str" && tag != "!!int" && tag != "!!float" && tag != "!!bool" && tag != "!!null" {
			continue
		}
		j := i + 1
		for j < len(tokens) && tokens[j].Type == token.CommentType {
			d.charge(1, 0)
			j++
		}
		// The maintained lexer splits &name into two tokens. A tag before
		// that property still owns the following scalar (including omission).
		// Keep both authored properties before any inserted empty token.
		after := i
		if j+1 < len(tokens) && tokens[j].Type == token.AnchorType && tokens[j+1].Type == token.StringType {
			d.charge(3, 0)
			after = j + 1
			j += 2
			for j < len(tokens) && tokens[j].Type == token.CommentType {
				d.charge(1, 0)
				j++
			}
		}
		omitted := j == len(tokens)
		var next *token.Token
		if !omitted {
			next = tokens[j]
			omitted = next.Type == token.MappingEndType || next.Type == token.SequenceEndType || next.Type == token.CollectEntryType || next.Type == token.MappingValueType
			if owner, ok := d.tagOwners[tk]; ok && next.Position != nil && tk.Position != nil && next.Position.Line > tk.Position.Line && next.Position.Column <= owner {
				omitted = true
			}
		}
		if !omitted {
			// Conversion belongs to this tag's content only. In particular a
			// following sibling merge key remains a merge, even after comments.
			if tag == "!!str" && next.Type == token.MergeKeyType {
				next.Type = token.StringType
			}
			continue
		}
		start := d.offset(tk)
		if start < 0 {
			continue
		}
		end := start + len(tag)
		if actual, ok := d.canonicalTags[start]; ok {
			end = actual
		}
		pos := *tk.Position
		pos.Column += len(tag)
		if after > i {
			last := tokens[after]
			end = d.offset(last) + len(last.Value)
			pos = *last.Position
			pos.Column += len(last.Value)
			out = append(out, tokens[i+1:after+1]...)
			i = after
		}
		empty := &token.Token{Type: token.StringType, Position: &pos}
		d.offsets[empty] = yamlTokenRange{end, end}
		out = append(out, empty)
	}
	for i, tk := range out {
		tk.Prev = nil
		tk.Next = nil
		if i > 0 {
			tk.Prev = out[i-1]
		}
		if i+1 < len(out) {
			tk.Next = out[i+1]
		}
	}
	return out
}
