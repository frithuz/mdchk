package integration

import (
	"context"
	"math/bits"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/token"
)

type yamlIssue struct {
	location Location
	message  string
}
type yamlResult struct {
	issues []yamlIssue
	block  Location
}

type yamlStop struct{ err error }
type yamlMap struct {
	start, end int
	span       parser.Span
	exact      bool
}
type yamlLine struct{ start, end, body int }
type yamlTokenRange struct{ start, end int }

var yamlBlockHeader = regexp.MustCompile(`^[|>](?:[1-9][+-]?|[+-][1-9]?)?$`)

// YAML 1.2.2 Core integer forms; classification only, after the scalar charge.
var yamlCoreInteger = regexp.MustCompile(`^(?:[-+]?[0-9]+|0o[0-7]+|0x[0-9a-fA-F]+)$`)
var yamlCoreNumber = regexp.MustCompile(`^[-+]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][-+]?[0-9]+)?$`)

type yamlMapper struct {
	source parser.Source
	body   []yamlMap
	lines  []yamlLine
	charge func(int64, int64)
}

func (m yamlMapper) bodyLocation(start, end int) Location {
	m.charge(int64(bits.Len(uint(len(m.body)))+1), 0)
	i := sort.Search(len(m.body), func(i int) bool { return m.body[i].end > start })
	a, b := -1, -1
	for ; i < len(m.body) && m.body[i].start < end; i++ {
		m.charge(1, 0)
		p := m.body[i]
		left, right := max(start, p.start), min(end, p.end)
		if left >= right {
			continue
		}
		x, y := p.span.Start(), p.span.End()
		if p.exact {
			x += left - p.start
			y = p.span.Start() + right - p.start
		}
		if a < 0 {
			a = x
		}
		b = y
	}
	if a < 0 {
		return Location{}
	}
	span, e := m.source.Span(a, b)
	if e != nil {
		return Location{}
	}
	return Location{m.source, span}
}
func (m yamlMapper) location(start, end int) Location {
	if start < 0 || end < start {
		return Location{}
	}
	m.charge(int64(bits.Len(uint(len(m.lines)))+1), 0)
	i := sort.Search(len(m.lines), func(i int) bool { return m.lines[i].end > start })
	a, b := -1, -1
	for ; i < len(m.lines) && m.lines[i].start < end; i++ {
		m.charge(1, 0)
		line := m.lines[i]
		left, right := max(start, line.start), min(end, line.end)
		if left >= right {
			continue
		}
		l := m.bodyLocation(line.body+left-line.start, line.body+right-line.start)
		if !l.valid() {
			return Location{}
		}
		if a < 0 {
			a = l.span.Start()
		}
		b = l.span.End()
	}
	if a < 0 {
		return Location{}
	}
	span, e := m.source.Span(a, b)
	if e != nil {
		return Location{}
	}
	return Location{m.source, span}
}

type yamlScalar int

const (
	yamlUnknown yamlScalar = iota
	yamlString
	yamlOther
	yamlMapping
	yamlSequence
)

type yamlDecoder struct {
	scope         *Scope
	ctx           context.Context
	text          string
	starts        []int
	offsets       map[*token.Token]yamlTokenRange
	positionText  string
	insertions    []yamlInsertion
	canonicalTags map[int]int
	mapper        yamlMapper
	out           *yamlResult
	anchors       map[string]ast.Node
	bindings      map[*ast.AliasNode]ast.Node
	active        map[ast.Node]bool
}

func (d *yamlDecoder) charge(work, nodes int64) {
	if e := d.scope.checkpoint(d.ctx, work, nodes); e != nil {
		panic(yamlStop{e})
	}
}

// Only spellings already interpreted by read are probe candidates. A match is
// not lexical/semantic authority: the maintained probe lexer must confirm TagType
// at that exact original source position before any final input is adapted.
var yamlStandardTags = [...]string{
	"!!str", "!!int", "!!float", "!!bool", "!!null", "!!map", "!!seq",
	"!<tag:yaml.org,2002:str>", "!<tag:yaml.org,2002:int>",
	"!<tag:yaml.org,2002:float>", "!<tag:yaml.org,2002:bool>",
	"!<tag:yaml.org,2002:null>", "!<tag:yaml.org,2002:map>", "!<tag:yaml.org,2002:seq>",
}

type yamlEdit struct {
	start, end, padding int
	replacement         string
}
type yamlURI struct {
	start, end int
	short      string
}
type yamlInsertion struct{ position, source, count, shift int }

func (d *yamlDecoder) tagSeparatorInput() (string, error) {
	// A disposable same-width lexical probe masks NONleading TABs only. Leading
	// indentation and every structural delimiter remain authored. Probe values/AST
	// never grant authority; the library alone identifies actual tag/quoted spans.
	d.charge(int64(len(d.text))+1, 0)
	leading, hasTab := true, false
	for i := 0; i < len(d.text); i++ {
		c := d.text[i]
		if c == '\r' || c == '\n' {
			leading = true
			continue
		}
		if c == '\t' {
			hasTab = hasTab || !leading
			continue
		}
		if c != ' ' {
			leading = false
		}
	}
	if !hasTab {
		d.charge(int64(len(d.text))+1, 0)
		if !strings.Contains(d.text, "!<tag:yaml.org,2002:") {
			return d.text, nil
		}
	}
	d.charge(2*int64(len(d.text))+1, 2)
	probe := []byte(d.text)
	leading = true
	for i, c := range probe {
		if c == '\r' || c == '\n' {
			leading = true
			continue
		}
		if c == '\t' {
			if !leading {
				probe[i] = ' '
			}
			continue
		}
		if c != ' ' {
			leading = false
		}
	}
	probeText := string(probe)
	tokens, err := d.lexTokens(probeText)
	if err != nil {
		return "", err
	}
	probeDecoder := *d
	probeDecoder.text = probeText
	probeDecoder.prepareOffsets(tokens)
	// scanTag splits a long standard URI at its internal comma in flow mode.
	// Collect only library-proved tag prefixes with exact supported authored URI
	// and separation. A second disposable probe must confirm lexical ownership
	// after canonicalization; earlier corrupt tokens cannot authorize payload edits.
	var uris []yamlURI
	for _, tk := range tokens {
		d.charge(1, 0)
		if tk.Type != token.TagType {
			continue
		}
		start := probeDecoder.offset(tk)
		if start < 0 {
			continue
		}
		for kind, uri := range yamlStandardTags[7:] {
			d.charge(int64(len(uri)+len(tk.Value))+1, 0)
			end := start + len(uri)
			if end >= len(d.text) || d.text[start:end] != uri || !strings.ContainsRune(" \t\r\n", rune(d.text[end])) || tk.Value != uri && tk.Value != "!<tag:yaml.org" {
				continue
			}
			short := yamlStandardTags[kind]
			if len(uris) == cap(uris) {
				d.charge(int64(len(uris))+1, 0)
			}
			d.charge(int64(len(short))+1, 1)
			uris = append(uris, yamlURI{start, end, short})
			break
		}
	}
	if len(uris) > 0 {
		d.charge(3*int64(len(probeText))+int64(len(uris))+1, 2)
		canonical := []byte(probeText)
		for _, uri := range uris {
			copy(canonical[uri.start:uri.end], uri.short)
			for i := uri.start + len(uri.short); i < uri.end; i++ {
				canonical[i] = ' '
			}
		}
		probeText = string(canonical)
		tokens, err = d.lexTokens(probeText)
		if err != nil {
			return "", err
		}
		probeDecoder.text = probeText
		probeDecoder.prepareOffsets(tokens)
	}
	var edits []yamlEdit
	uriIndex := 0
	padding := 0
	for _, tk := range tokens {
		d.charge(1, 0)
		start := probeDecoder.offset(tk)
		if start < 0 {
			continue
		}
		switch tk.Type {
		case token.TagType:
			for uriIndex < len(uris) && uris[uriIndex].start < start {
				uriIndex++
			}
			originalEnd := start + len(tk.Value)
			canonical := uriIndex < len(uris) && uris[uriIndex].start == start && uris[uriIndex].short == tk.Value
			if canonical {
				uri := uris[uriIndex]
				d.charge(int64(len(uri.short))+1, 2)
				if d.canonicalTags == nil {
					d.charge(1, 1)
					d.canonicalTags = make(map[int]int)
				}
				d.canonicalTags[start] = uri.end
				if len(edits) == cap(edits) {
					d.charge(int64(len(edits))+1, 0)
				}
				d.charge(1, 1)
				edits = append(edits, yamlEdit{start: uri.start, end: uri.end, replacement: uri.short})
				originalEnd = uri.end
			} else {
				supported := false
				for _, tag := range yamlStandardTags[:7] {
					d.charge(int64(len(tag))+1, 0)
					if tk.Value == tag {
						supported = true
						break
					}
				}
				if !supported {
					continue
				}
				d.charge(int64(len(tk.Value))+1, 0)
				if originalEnd >= len(d.text) || d.text[start:originalEnd] != tk.Value {
					continue
				}
			}
			separator := originalEnd
			tab := false
			for originalEnd < len(d.text) && (d.text[originalEnd] == ' ' || d.text[originalEnd] == '\t') {
				d.charge(1, 0)
				tab = tab || d.text[originalEnd] == '\t'
				originalEnd++
			}
			if tab {
				if len(edits) == cap(edits) {
					d.charge(int64(len(edits))+1, 0)
				}
				d.charge(1, 1)
				edits = append(edits, yamlEdit{start: separator, end: originalEnd})
			}
		case token.DoubleQuoteType:
			// The probe's complete library-owned quoted token proves scope. Its
			// original extent scanner supplies ranges only, never semantic decoding.
			end := probeDecoder.quotedTokenEnd(tk, start)
			if end <= start || end > len(d.text) || d.text[start] != '"' || d.text[end-1] != '"' {
				continue
			}
			extra := d.quotedTabPadding(start, end)
			if end < len(d.text) && !strings.ContainsRune(" \t\r\n,}]:", rune(d.text[end])) {
				d.issue(tk, "Malformed adjacency after quoted YAML.")
				continue
			}
			if extra == 0 {
				continue
			}
			if len(edits) == cap(edits) {
				d.charge(int64(len(edits))+1, 0)
			}
			d.charge(1, 1)
			edits = append(edits, yamlEdit{start: end, end: end, padding: extra})
			padding += extra
		default:
			continue
		}
	}
	if len(edits) == 0 {
		return d.text, nil
	}
	d.charge(3*int64(len(d.text))+2*int64(padding)+int64(len(edits))+1, int64(len(edits))+1)
	var final strings.Builder
	final.Grow(len(d.text) + padding)
	previous, shift := 0, 0
	for _, edit := range edits {
		final.WriteString(d.text[previous:edit.start])
		if edit.padding > 0 {
			position := final.Len()
			for range edit.padding {
				final.WriteByte(' ')
			}
			shift += edit.padding
			d.insertions = append(d.insertions, yamlInsertion{position, edit.start, edit.padding, shift})
		} else {
			final.WriteString(edit.replacement)
			for i := edit.start + len(edit.replacement); i < edit.end; i++ {
				final.WriteByte(' ')
			}
		}
		previous = edit.end
	}
	final.WriteString(d.text[previous:])
	text := final.String()
	d.positionText = text
	return text, nil
}

// Reconcile only scanDoubleQuote's extra source-cursor advances for retained raw
// inline TABs. Escapes, line-leading indentation and folded trailing whitespace
// take different pinned branches. Pending TABs are resolved once at the next
// nonspace or line boundary; there is no per-TAB suffix scan or value decoder.
func (d *yamlDecoder) quotedTabPadding(start, end int) int {
	d.charge(int64(end-start)+1, 0)
	extra, pending := 0, 0
	leading := false
	for i := start + 1; i < end-1; i++ {
		c := d.text[i]
		if c == '\r' || c == '\n' {
			pending = 0
			leading = true
			continue
		}
		if c == '\\' {
			extra += pending
			pending = 0
			leading = false
			if i+1 < end-1 {
				next := d.text[i+1]
				width := 1
				switch next {
				case 'x':
					width = 3
				case 'u':
					width = 5
				case 'U':
					width = 9
				case '\r', '\n':
					leading = true
				}
				i += min(width, end-2-i)
			}
			continue
		}
		if c == '\t' {
			if !leading && d.text[i+1] != '"' {
				pending++
			}
			continue
		}
		if c != ' ' {
			extra += pending
			pending = 0
			leading = false
		}
	}
	return extra + pending
}

func (d *yamlDecoder) sourceOffset(position int) int {
	if len(d.insertions) == 0 {
		return position
	}
	d.charge(int64(bits.Len(uint(len(d.insertions)))+1), 0)
	i := sort.Search(len(d.insertions), func(i int) bool { return d.insertions[i].position > position }) - 1
	if i < 0 {
		return position
	}
	insertion := d.insertions[i]
	if position < insertion.position+insertion.count {
		return insertion.source
	}
	return position - insertion.shift
}

func (d *yamlDecoder) lexTokens(text string) (token.Tokens, error) {
	// Reserve Scanner.Init's source/rune/token storage BEFORE dependency entry,
	// independently for each of the at most three lexes. The same operation owns
	// both, including malformed/rejected input; no fresh allowance is created.
	d.charge(8*int64(len(text))+1, 2*int64(len(text))+1)
	tokens := lexer.Tokenize(text)
	if int64(len(tokens)) > d.scope.operation.Budget().Limits().DocumentNodes {
		return nil, &parser.LimitExceeded{Name: "document-nodes", Observed: int64(len(tokens)), Allowed: d.scope.operation.Budget().Limits().DocumentNodes}
	}
	d.charge(8*int64(len(tokens))+1, 3*int64(len(tokens))+1)
	flow := 0
	depth := min(int64(64), d.scope.operation.Budget().Limits().SyntaxDepth)
	for _, tk := range tokens {
		if tk.Type == token.MappingStartType || tk.Type == token.SequenceStartType {
			flow++
		}
		if tk.Type == token.MappingEndType || tk.Type == token.SequenceEndType {
			flow--
		}
		if tk.Position != nil && int64(max(flow, tk.Position.IndentLevel)) > depth {
			return nil, &parser.LimitExceeded{Name: "structured-input-depth", Observed: int64(max(flow, tk.Position.IndentLevel)), Allowed: depth}
		}
	}
	return tokens, nil
}

// Token columns are Unicode code points. Compute each source position once in
// sorted line/column order; repeated alias visits never rescan a long flow line.
func (d *yamlDecoder) prepareOffsets(tokens token.Tokens) {
	positionText, starts := d.text, d.starts
	if d.positionText != "" {
		positionText = d.positionText
		d.charge(int64(len(positionText))+1, int64(len(d.starts)))
		starts = make([]int, 0, len(d.starts))
		starts = append(starts, 0)
		for i := 0; i < len(positionText); i++ {
			if positionText[i] == '\n' {
				starts = append(starts, i+1)
			}
		}
	}
	d.charge(int64(len(positionText))+4*int64(len(tokens))*int64(bits.Len(uint(len(tokens)))+1)+1, 2*int64(len(tokens)))
	ordered := make([]*token.Token, 0, len(tokens))
	for _, tk := range tokens {
		if tk.Position != nil && tk.Position.Line >= 1 && tk.Position.Line <= len(starts) && tk.Position.Column >= 1 {
			ordered = append(ordered, tk)
		}
	}
	sort.Slice(ordered, func(i, j int) bool {
		a, b := ordered[i].Position, ordered[j].Position
		return a.Line < b.Line || a.Line == b.Line && a.Column < b.Column
	})
	d.offsets = make(map[*token.Token]yamlTokenRange, len(ordered))
	line, column, offset, tagSpaces := 0, 1, 0, 0
	for _, tk := range ordered {
		p := tk.Position
		if p.Line != line {
			line = p.Line
			column = 1
			tagSpaces = 0
			offset = starts[line-1]
		}
		for column < p.Column+tagSpaces && offset < len(positionText) && positionText[offset] != '\n' {
			_, width := utf8.DecodeRuneInString(positionText[offset:])
			offset += width
			column++
		}
		if column == p.Column+tagSpaces {
			d.offsets[tk] = yamlTokenRange{d.sourceOffset(offset), -1}
			// The pinned scanner undercounts the first separator after a tag
			// (space, or comma in its broken URI prefix). Confirmed tag TAB
			// separators have the same byte/codepoint width. Reconcile only
			// that exact original-source boundary; the maintained AST still owns
			// decoding. Later tokens on this line share the same correction.
			if tk.Type == token.TagType && (strings.HasSuffix(tk.Origin, " ") || tk.Value == "!<tag:yaml.org") {
				d.charge(int64(len(tk.Value))+1, 0)
				end := offset + len(tk.Value)
				if end < len(positionText) && positionText[offset:end] == tk.Value && (positionText[end] == ' ' || tk.Value == "!<tag:yaml.org" && positionText[end] == ',') {
					tagSpaces++
				}
			}
		}
	}
}
func (d *yamlDecoder) offset(t *token.Token) int {
	if offset, ok := d.offsets[t]; ok {
		return offset.start
	}
	return -1
}
func (d *yamlDecoder) tokenLocation(t *token.Token) Location {
	start := d.offset(t)
	if start < 0 {
		return d.out.block
	}
	d.charge(int64(len(t.Origin))+1, 0)
	raw := strings.TrimLeftFunc(t.Origin, unicode.IsSpace)
	end := start + len(raw)
	if t.Type == token.DoubleQuoteType || t.Type == token.SingleQuoteType {
		// The pinned scanner's Origin drops Unicode-escape digits. Recover
		// only the extent of its already parsed quoted token, not its value.
		// Each extent is metered once and cached for wrapper/alias visits.
		end = d.quotedTokenEnd(t, start)
	}
	if originalEnd, ok := d.canonicalTags[start]; ok && t.Type == token.TagType {
		end = max(end, originalEnd)
	}
	if end > len(d.text) || end <= start {
		return d.out.block
	}
	l := d.mapper.location(start, end)
	if !l.valid() {
		return d.out.block
	}
	return l
}
func (d *yamlDecoder) quotedTokenEnd(t *token.Token, start int) int {
	range_ := d.offsets[t]
	if range_.end < 0 {
		quote := d.text[start]
		for i := start + 1; i < len(d.text); i++ {
			d.charge(1, 0)
			if quote == '"' && d.text[i] == '\\' && i+1 < len(d.text) {
				d.charge(1, 0)
				i++
				continue
			}
			if d.text[i] == quote {
				if quote == '\'' && i+1 < len(d.text) && d.text[i+1] == '\'' {
					d.charge(1, 0)
					i++
					continue
				}
				range_.end = i + 1
				break
			}
		}
		d.offsets[t] = range_
	}
	return range_.end
}

func (d *yamlDecoder) issue(t *token.Token, message string) {
	d.charge(int64(len(message))+1, 1)
	d.out.issues = append(d.out.issues, yamlIssue{d.tokenLocation(t), message})
}
func (d *yamlDecoder) scalarLocation(n ast.Node, use *ast.AliasNode) Location {
	if use != nil {
		start := d.offset(use.GetToken())
		if start >= 0 {
			end := start + 1 + len(use.Value.GetToken().Value)
			if end <= len(d.text) {
				return d.mapper.location(start, end)
			}
		}
		return d.out.block
	}
	if literal, ok := n.(*ast.LiteralNode); ok {
		start := d.offset(literal.Start)
		end := len(d.text)
		if literal.Value != nil && literal.Value.Token != nil && literal.Value.Token.Next != nil {
			next := literal.Value.Token.Next
			if next.Position != nil && next.Position.Line > 0 && next.Position.Line <= len(d.starts) {
				end = d.starts[next.Position.Line-1]
			}
		}
		if start >= 0 && end > start {
			return d.mapper.location(start, end)
		}
		return d.out.block
	}
	return d.tokenLocation(n.GetToken())
}
func (d *yamlDecoder) read(n ast.Node, depth int, use *ast.AliasNode, top bool) (string, Location, yamlScalar) {
	if n == nil {
		return "", d.out.block, yamlOther
	}
	d.charge(1, 1)
	limit := min(int64(64), d.scope.operation.Budget().Limits().SyntaxDepth)
	if int64(depth) > limit {
		panic(yamlStop{&parser.LimitExceeded{Name: "structured-input-depth", Observed: int64(depth), Allowed: limit}})
	}
	if d.active[n] {
		d.issue(n.GetToken(), "Cyclic YAML alias.")
		return "", d.out.block, yamlUnknown
	}
	d.active[n] = true
	defer delete(d.active, n)
	loc := d.scalarLocation(n, use)
	switch v := n.(type) {
	case *ast.MappingKeyNode:
		return d.read(v.Value, depth, use, false)
	case *ast.AnchorNode:
		if use == nil {
			name := v.Name.GetToken().Value
			d.charge(int64(len(name))+1, 1)
			d.anchors[name] = v.Value
		}
		return d.read(v.Value, depth+1, use, top)
	case *ast.AliasNode:
		target, ok := d.bindings[v]
		if !ok {
			target, ok = d.anchors[v.Value.GetToken().Value]
			if ok {
				d.bindings[v] = target
			}
		}
		if !ok {
			d.issue(v.GetToken(), "Unresolved YAML alias.")
			return "", loc, yamlUnknown
		}
		if use == nil {
			use = v
		}
		return d.read(target, depth+1, use, top)
	case *ast.TagNode:
		tag := v.Start.Value
		if strings.HasPrefix(tag, "!!") {
			tag = strings.TrimPrefix(tag, "!!")
		} else if strings.HasPrefix(tag, "!<tag:yaml.org,2002:") && strings.HasSuffix(tag, ">") {
			tag = strings.TrimSuffix(strings.TrimPrefix(tag, "!<tag:yaml.org,2002:"), ">")
		} else {
			d.issue(v.GetToken(), "Custom YAML tags are unsupported.")
			return "", loc, yamlUnknown
		}
		if tag == "map" || tag == "seq" {
			value, location, kind := d.read(v.Value, depth+1, use, top)
			want := yamlMapping
			if tag == "seq" {
				want = yamlSequence
			}
			if kind != want {
				d.issue(v.GetToken(), "YAML core tag has the wrong value kind.")
				return "", loc, yamlUnknown
			}
			return value, location, kind
		}
		value, ok := yamlTaggedScalar(v.Value)
		if !ok {
			d.issue(v.GetToken(), "YAML core tag requires a scalar.")
			return "", loc, yamlUnknown
		}
		d.charge(int64(len(value))+1, 0)
		if use == nil {
			child := d.scalarLocation(v.Value, nil)
			if child.valid() && loc.valid() {
				span, e := d.mapper.source.Span(loc.span.Start(), child.span.End())
				if e == nil {
					loc = Location{d.mapper.source, span}
				}
			}
		}
		switch tag {
		case "str":
			return value, loc, yamlString
		case "int":
			ok = yamlCoreInteger.MatchString(value)
		case "float":
			ok = yamlCoreNumber.MatchString(value) || yamlSpecialNumber(value)
		case "bool":
			ok = strings.EqualFold(value, "true") || strings.EqualFold(value, "false")
		case "null":
			ok = value == "" || value == "~" || strings.EqualFold(value, "null")
		default:
			ok = false
		}
		if !ok {
			d.issue(v.GetToken(), "Unsupported or invalid YAML core tag.")
			return "", loc, yamlUnknown
		}
		return "", loc, yamlOther
	case *ast.MergeKeyNode:
		d.issue(v.GetToken(), "Merged YAMLs are unsupported.")
		return "", loc, yamlUnknown
	case *ast.MappingNode:
		d.charge(int64(len(v.Values))+1, int64(len(v.Values)))
		seen := map[string]bool{}
		for _, entry := range v.Values {
			key, _, kind := d.read(entry.Key, depth+1, use, false)
			if kind != yamlString || key == "<<" {
				d.issue(entry.Key.GetToken(), "YAML keys must be ordinary strings.")
			}
			if seen[key] {
				d.issue(entry.Key.GetToken(), "Duplicate YAML key.")
			}
			seen[key] = true
			d.read(entry.Value, depth+1, use, false)

		}
		return "", loc, yamlMapping
	case *ast.MappingValueNode:
		d.issue(v.GetToken(), "Unsupported YAML mapping structure.")
		return "", loc, yamlUnknown
	case *ast.SequenceNode:
		for _, entry := range v.Values {
			d.read(entry, depth+1, use, false)
		}
		return "", loc, yamlSequence
	case *ast.StringNode:
		d.charge(int64(len(v.Value))+1, 0)
		if v.Token.Type == token.StringType && (yamlCoreNumber.MatchString(v.Value) || yamlSpecialNumber(v.Value)) {
			return "", loc, yamlOther
		}
		return v.Value, loc, yamlString
	case *ast.LiteralNode:
		if v.Value == nil {
			d.issue(v.GetToken(), "Malformed YAML block scalar.")
			return "", loc, yamlUnknown
		}
		d.charge(int64(len(v.Value.Value))+1, 0)
		return v.Value.Value, loc, yamlString
	case *ast.IntegerNode, *ast.FloatNode, *ast.BoolNode, *ast.NullNode, *ast.InfinityNode, *ast.NanNode:
		return "", loc, yamlOther
	default:
		d.issue(n.GetToken(), "Unsupported YAML syntax.")
		return "", loc, yamlUnknown
	}
}
func yamlTaggedScalar(n ast.Node) (string, bool) {
	switch n := n.(type) {
	case *ast.StringNode:
		return n.Value, true
	case *ast.LiteralNode:
		if n.Value != nil {
			return n.Value.Value, true
		}
	case *ast.IntegerNode, *ast.FloatNode, *ast.BoolNode, *ast.NullNode, *ast.InfinityNode, *ast.NanNode:
		return n.GetToken().Value, true
	}
	return "", false
}

func yamlSpecialNumber(value string) bool {
	v := strings.ToLower(value)
	return v == ".nan" || v == ".inf" || v == "+.inf" || v == "-.inf"
}

// Adapt only the pinned lexer's synthetic empty-block/dedent defect; all other
// invalid markers remain for its maintained parser. No source grammar is parsed.
func yamlNormalizeEmptyBlocks(tokens token.Tokens) token.Tokens {
	var header, empty *token.Token
	out := tokens[:0]
	removed := false
	for _, tk := range tokens {
		switch {
		case tk.Type == token.LiteralType || tk.Type == token.FoldedType:
			header = tk
			empty = nil
		case tk.Type == token.CommentType && header != nil && tk.Position != nil && header.Position != nil && tk.Position.Line == header.Position.Line:
		case tk.Type == token.StringType && header != nil && yamlBlankBlock(tk) && tk.Position != nil && header.Position != nil && tk.Position.Line > header.Position.Line && tk.Position.Column == 1:
			empty = tk
			header = nil
			if !strings.ContainsAny(tk.Origin, "\r\n") {
				tk.Value = ""
			}
		case tk.Type == token.InvalidType && empty != nil && empty.Next == tk && tk.Origin == "" && tk.Value == "" && tk.Error == "could not find multi-line content" && yamlEmptyErrorPosition(empty, tk):
			empty = nil
			header = nil
			removed = true
			continue
		default:
			header = nil
			empty = nil
		}
		out = append(out, tk)
	}
	if removed {
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
	}
	return out
}
func yamlBlankBlock(t *token.Token) bool {
	for _, c := range t.Origin {
		if c != ' ' && c != '\r' && c != '\n' {
			return false
		}
	}
	for _, c := range t.Value {
		if c != '\n' {
			return false
		}
	}
	return true
}
func yamlEmptyErrorPosition(empty, marker *token.Token) bool {
	if empty.Position == nil || marker.Position == nil || marker.Next == nil || marker.Next.Position == nil {
		return false
	}
	a, b, c := empty.Position, marker.Position, marker.Next.Position
	lines := strings.Count(empty.Origin, "\n")
	last := strings.LastIndexAny(empty.Origin, "\r\n")
	indent := len(empty.Origin) - last - 1
	return a.Line+lines == b.Line && a.Offset+lines == b.Offset && b.Column == 1+indent && b.Line == c.Line && b.Column == c.Column && b.Offset == c.Offset
}
