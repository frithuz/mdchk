package metadata

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

// tomlDraft is private mutable assembly state. Only frozen model values leave
// the adapter; no upstream AST node or arena-backed Data survives an expression.
type tomlDraft struct {
	kind         Kind
	span         parser.Span
	depth        int64
	fields       []tomlField
	byKey        map[string]*tomlDraft
	elements     []*tomlDraft
	native       Value
	restrictions MoveRestrictions
}
type tomlField struct {
	key           string
	keySpan, span parser.Span
	value         *tomlDraft
}
type tomlKey struct {
	text string
	span parser.Span
	end  int
}
type tomlProducer struct {
	ctx       context.Context
	b         Binding
	text      string
	lex       tomlLexical
	duplicate parser.Span
}

func decodeTOML(ctx context.Context, b Binding) (Result, error) {
	text, _ := b.Source().Text(b.Header().Content())
	lex, err := guardTOMLLexical(ctx, b, text)
	if err != nil {
		return Result{}, err
	}
	p := tomlProducer{ctx: ctx, b: b, text: text, lex: lex}
	root := &tomlDraft{kind: Mapping, span: b.Header().Content(), depth: 1}
	if lex.standalone {
		root.restrictions |= StandaloneComments
	}
	current := root
	if err := b.charge(ctx, 0, 0); err != nil {
		return Result{}, err
	}
	var native unstable.Parser
	native.KeepComments = true
	native.Reset([]byte(text))
	for native.NextExpression() {
		if err := b.charge(ctx, 1, 0); err != nil {
			return Result{}, err
		}
		expr := native.Expression()
		if expr.Kind == unstable.Comment {
			continue
		}
		keys := p.keys(expr)
		if len(keys) == 0 {
			return decodeFailure(ctx, b, Malformed, b.Header().Content())
		}
		switch expr.Kind {
		case unstable.Table, unstable.ArrayTable:
			root.restrictions |= TOMLTables
			if expr.Kind == unstable.ArrayTable {
				root.restrictions |= TOMLArrayTables
			}
			current, err = p.table(root, keys, expr.Kind == unstable.ArrayTable)
		case unstable.KeyValue:
			err = p.assign(current, keys, expr.Value(), true)
		default:
			return decodeFailure(ctx, b, Malformed, keys[0].span)
		}
		if err != nil {
			if errors.Is(err, errTOMLShape) {
				return decodeFailure(ctx, b, Malformed, keys[0].span)
			}
			return Result{}, err
		}
	}
	if err := native.Error(); err != nil {
		location := b.Header().Content()
		var syntax *unstable.ParserError
		if errors.As(err, &syntax) {
			r := native.Range(syntax.Highlight)
			location = decodeSpan(b, int(r.Offset), int(r.Offset+r.Length))
		}
		return decodeFailure(ctx, b, Malformed, location)
	}
	if err := b.charge(ctx, int64(len(text))+1, 0); err != nil {
		return Result{}, err
	}
	// A maintained public semantic pass owns cross-expression redefinitions,
	// inline sealing, table/scalar conflicts and AOT ownership. An empty struct
	// intentionally discards scalar coercions: exact native semantics came from
	// the source AST, never float64 or time.Time. The upstream SeenTracker runs
	// on every expression, including values ignored by this target.
	var validation struct{}
	if err := toml.Unmarshal([]byte(text), &validation); err != nil {
		location := b.Header().Content()
		var problem *toml.DecodeError
		if errors.As(err, &problem) {
			row, col := problem.Position()
			location = p.position(row, col)
		}
		if p.duplicate.Present() {
			return decodeFailure(ctx, b, DuplicateKey, p.duplicate)
		}
		return decodeFailure(ctx, b, Malformed, location)
	}
	if err := b.charge(ctx, 0, 0); err != nil {
		return Result{}, err
	}
	value, err := p.freeze(root)
	if err != nil {
		return Result{}, err
	}
	return result(ctx, b, value, nil)
}

var errTOMLShape = errors.New("conflicting TOML shape")

func (p *tomlProducer) keys(expr *unstable.Node) []tomlKey {
	var out []tomlKey
	it := expr.Key()
	for it.Next() {
		n := it.Node()
		end := int(n.Raw.Offset + n.Raw.Length)
		out = append(out, tomlKey{string(n.Data), decodeSpan(p.b, int(n.Raw.Offset), end), end})
	}
	return out
}
func (p *tomlProducer) position(row, column int) parser.Span {
	offset := 0
	for row > 1 && offset < len(p.text) {
		if p.text[offset] == '\n' {
			row--
		}
		offset++
	}
	offset = min(offset+max(column-1, 0), len(p.text))
	return decodeSpan(p.b, offset, min(offset+1, len(p.text)))
}
func (p *tomlProducer) addField(parent *tomlDraft, field tomlField) {
	if parent.byKey == nil {
		parent.byKey = make(map[string]*tomlDraft)
	}
	if _, present := parent.byKey[field.key]; !present {
		parent.byKey[field.key] = field.value
	}
	parent.fields = append(parent.fields, field)
}
func (p *tomlProducer) descend(parent *tomlDraft, key string) (*tomlDraft, error) {
	child := parent.byKey[key]
	if child == nil {
		if err := tomlDepth(p.b, parent.depth+1); err != nil {
			return nil, err
		}
		child = &tomlDraft{kind: Mapping, depth: parent.depth + 1}
		p.addField(parent, tomlField{key: key, value: child})
	}
	if child.kind == Sequence && len(child.elements) > 0 {
		child = child.elements[len(child.elements)-1]
	}
	if child.kind != Mapping {
		return nil, errTOMLShape
	}
	return child, nil
}
func (p *tomlProducer) table(root *tomlDraft, keys []tomlKey, array bool) (*tomlDraft, error) {
	parent := root
	var err error
	for _, key := range keys[:len(keys)-1] {
		parent, err = p.descend(parent, key.text)
		if err != nil {
			return nil, err
		}
	}
	last := keys[len(keys)-1]
	if !array {
		return p.descend(parent, last.text)
	}
	seq := parent.byKey[last.text]
	if seq == nil {
		if err := tomlDepth(p.b, parent.depth+1); err != nil {
			return nil, err
		}
		seq = &tomlDraft{kind: Sequence, depth: parent.depth + 1, restrictions: TOMLArrayTables}
		p.addField(parent, tomlField{key: last.text, value: seq})
	}
	if seq.kind != Sequence {
		return nil, errTOMLShape
	}
	if err := tomlDepth(p.b, seq.depth+1); err != nil {
		return nil, err
	}
	child := &tomlDraft{kind: Mapping, depth: seq.depth + 1, restrictions: TOMLArrayTables}
	seq.elements = append(seq.elements, child)
	return child, nil
}
func (p *tomlProducer) assign(parent *tomlDraft, keys []tomlKey, n *unstable.Node, physical bool) error {
	if len(keys) > 1 {
		parent.restrictions |= TOMLDottedKeys
	}
	var err error
	for _, key := range keys[:len(keys)-1] {
		parent, err = p.descend(parent, key.text)
		if err != nil {
			return err
		}
	}
	if err := p.guardValue(n, parent.depth+1); err != nil {
		return err
	}
	last := keys[len(keys)-1]
	start := last.end
	for start < len(p.text) && p.text[start] != '=' {
		start++
	}
	start = p.skip(start + 1)
	v, err := p.project(n, parent.depth+1, start)
	if err != nil {
		return err
	}
	var whole parser.Span
	if physical && len(keys) == 1 {
		whole = p.physical(keys[0].span, v.span)
	}
	if parent.byKey[last.text] != nil && !p.duplicate.Present() {
		p.duplicate = last.span
	}
	p.addField(parent, tomlField{key: last.text, keySpan: last.span, span: whole, value: v})
	return nil
}

// Each expression's complete logical depth is checked iteratively before owned
// recursive projection and before the public semantic decoder. In contrast to
// the lexical recursion guard this includes dotted paths, current table depth
// and an extra sequence layer for every actual enclosing array-table element.
func (p *tomlProducer) guardValue(n *unstable.Node, depth int64) error {
	type pending struct {
		node  *unstable.Node
		depth int64
	}
	stack := []pending{{n, depth}}
	for len(stack) > 0 {
		last := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if err := p.b.charge(p.ctx, 1, 0); err != nil {
			return err
		}
		if err := tomlDepth(p.b, last.depth); err != nil {
			return err
		}
		if last.node.Kind != unstable.Array && last.node.Kind != unstable.InlineTable {
			continue
		}
		it := last.node.Children()
		for it.Next() {
			child := it.Node()
			if child.Kind == unstable.Comment {
				continue
			}
			childDepth := last.depth + 1
			if last.node.Kind == unstable.InlineTable {
				count := int64(0)
				keys := child.Key()
				for keys.Next() {
					count++
				}
				childDepth = last.depth + count
				child = child.Value()
			}
			stack = append(stack, pending{child, childDepth})
		}
	}
	return nil
}
func (p *tomlProducer) project(n *unstable.Node, depth int64, start int) (*tomlDraft, error) {
	if err := p.b.charge(p.ctx, 1, 1); err != nil {
		return nil, err
	}
	draft := &tomlDraft{depth: depth}
	if n.Kind == unstable.Array || n.Kind == unstable.InlineTable {
		end, ok := p.lex.ends[start]
		if !ok {
			return nil, errTOMLShape
		}
		draft.span = decodeSpan(p.b, start, end)
		draft.kind = Sequence
		if n.Kind == unstable.InlineTable {
			draft.kind = Mapping
		}
		cursor := start + 1
		it := n.Children()
		for it.Next() {
			child := it.Node()
			if child.Kind == unstable.Comment {
				draft.restrictions |= StandaloneComments
				continue
			}
			if draft.kind == Mapping {
				if err := p.assign(draft, p.keys(child), child.Value(), false); err != nil {
					return nil, err
				}
				continue
			}
			cursor = p.skip(cursor)
			if cursor < len(p.text) && p.text[cursor] == ',' {
				cursor = p.skip(cursor + 1)
			}
			v, err := p.project(child, depth+1, cursor)
			if err != nil {
				return nil, err
			}
			draft.elements = append(draft.elements, v)
			cursor = v.span.End() - p.b.Header().Content().Start()
		}
		return draft, nil
	}
	start = int(n.Raw.Offset)
	end := int(n.Raw.Offset + n.Raw.Length)
	draft.span = decodeSpan(p.b, start, end)
	if n.Kind == unstable.Integer || n.Kind == unstable.Float {
		if err := reserveNumeric(p.ctx, p.b, string(n.Data)); err != nil {
			return nil, err
		}
	}
	kind, text, boolean, ok := tomlNative(n.Kind, string(n.Data))
	if !ok {
		return nil, errTOMLShape
	}
	value, err := scalar(p.ctx, p.b, draft.span, kind, text, boolean)
	if err != nil {
		return nil, err
	}
	draft.kind, draft.native = kind, value
	return draft, nil
}
func (p *tomlProducer) skip(offset int) int {
	for offset < len(p.text) {
		switch p.text[offset] {
		case ' ', '\t', '\r', '\n':
			offset++
		case '#':
			for offset < len(p.text) && p.text[offset] != '\n' {
				offset++
			}
		default:
			return offset
		}
	}
	return offset
}
func (p *tomlProducer) physical(key, value parser.Span) parser.Span {
	start := key.Start() - p.b.Header().Content().Start()
	for start > 0 && p.text[start-1] != '\n' {
		if p.text[start-1] != ' ' && p.text[start-1] != '\t' {
			return parser.Span{}
		}
		start--
	}
	end := value.End() - p.b.Header().Content().Start()
	for end < len(p.text) && (p.text[end] == ' ' || p.text[end] == '\t') {
		end++
	}
	if end < len(p.text) && p.text[end] == '#' {
		for end < len(p.text) && p.text[end] != '\n' {
			end++
		}
	}
	if end < len(p.text) && p.text[end] == '\r' {
		end++
	}
	if end < len(p.text) && p.text[end] == '\n' {
		end++
	} else if end != len(p.text) {
		return parser.Span{}
	}
	return decodeSpan(p.b, start, end)
}
func (p *tomlProducer) freeze(draft *tomlDraft) (Value, error) {
	if draft.native.state != nil {
		return draft.native, nil
	}
	if err := p.b.charge(p.ctx, int64(len(draft.fields)+len(draft.elements))+1, 2*int64(len(draft.fields)+len(draft.elements))+1); err != nil {
		return Value{}, err
	}
	var entries []Entry
	var elements []Value
	for _, f := range draft.fields {
		v, err := p.freeze(f.value)
		if err != nil {
			return Value{}, err
		}
		e, err := entry(p.ctx, p.b, f.key, f.keySpan, f.span, v)
		if err != nil {
			return Value{}, err
		}
		entries = append(entries, e)
	}
	for _, child := range draft.elements {
		v, err := p.freeze(child)
		if err != nil {
			return Value{}, err
		}
		elements = append(elements, v)
	}
	return collection(p.ctx, p.b, draft.span, draft.kind, entries, elements, draft.restrictions)
}

func tomlNative(kind unstable.Kind, text string) (Kind, string, bool, bool) {
	switch kind {
	case unstable.String:
		return String, text, false, true
	case unstable.Bool:
		return Boolean, "", text == "true", text == "true" || text == "false"
	case unstable.Integer:
		canonical, ok := canonicalInteger(text)
		return Integer, canonical, false, ok
	case unstable.Float:
		canonical, ok := canonicalFloat(text)
		return Float, canonical, false, ok
	case unstable.LocalDate, unstable.LocalTime, unstable.LocalDateTime, unstable.DateTime:
		return tomlTemporal(kind, text)
	default:
		return Unknown, "", false, false
	}
}

// Validate civil fields without time.Local or precision-losing native time
// conversion. The source scanner only classifies and delimits temporal tokens;
// this validates their civil grammar/ranges and normalizes separators, optional
// seconds and fractional trailing zeros while preserving every significant digit.
// RFC 3339 second 60 is a civil leap marker; no host event table or instant
// conversion is consulted to resolve its chronology.
func tomlTemporal(kind unstable.Kind, text string) (Kind, string, bool, bool) {
	out := strings.ReplaceAll(strings.ReplaceAll(text, "t", "T"), "z", "Z")
	if len(out) > 10 && out[10] == ' ' {
		out = out[:10] + "T" + out[11:]
	}
	native := LocalDate
	hasDate := kind != unstable.LocalTime
	hasTime := kind != unstable.LocalDate
	if hasDate {
		if len(out) < 10 || out[4] != '-' || out[7] != '-' {
			return native, "", false, false
		}
		y, e1 := tomlDigits(out[:4])
		m, e2 := tomlDigits(out[5:7])
		d, e3 := tomlDigits(out[8:10])
		if e1 != nil || e2 != nil || e3 != nil || m < 1 || m > 12 {
			return native, "", false, false
		}
		days := []int{31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
		if y%4 == 0 && (y%100 != 0 || y%400 == 0) {
			days[1] = 29
		}
		if d < 1 || d > days[m-1] {
			return native, "", false, false
		}
	}
	if !hasTime {
		return native, out, false, len(out) == 10
	}
	native = LocalTime
	start := 0
	if hasDate {
		if len(out) <= 10 || out[10] != 'T' {
			return native, "", false, false
		}
		start = 11
		native = LocalDateTime
	}
	if kind == unstable.DateTime {
		native = OffsetDateTime
	}
	if len(out) < start+5 || out[start+2] != ':' {
		return native, "", false, false
	}
	h, eh := tomlDigits(out[start : start+2])
	m, em := tomlDigits(out[start+3 : start+5])
	if eh != nil || em != nil || h > 23 || m > 59 {
		return native, "", false, false
	}
	end := start + 5
	secondsPresent := false
	if end < len(out) && out[end] == ':' {
		if len(out) < end+3 {
			return native, "", false, false
		}
		s, err := tomlDigits(out[end+1 : end+3])
		if err != nil || s > 60 {
			return native, "", false, false
		}
		end += 3
		secondsPresent = true
	} else {
		out = out[:end] + ":00" + out[end:]
		end += 3
	}
	if end < len(out) && out[end] == '.' {
		if !secondsPresent {
			return native, "", false, false
		}
		fraction := end + 1
		for fraction < len(out) && out[fraction] >= '0' && out[fraction] <= '9' {
			fraction++
		}
		if fraction == end+1 {
			return native, "", false, false
		}
		trim := fraction
		for trim > end+1 && out[trim-1] == '0' {
			trim--
		}
		if trim == end+1 {
			trim = end
		}
		out = out[:trim] + out[fraction:]
		end = trim
	}
	if native == OffsetDateTime {
		zone := out[end:]
		if zone != "Z" {
			if len(zone) != 6 || zone[0] != '+' && zone[0] != '-' || zone[3] != ':' {
				return native, "", false, false
			}
			h, eh := tomlDigits(zone[1:3])
			m, em := tomlDigits(zone[4:6])
			if eh != nil || em != nil || h > 23 || m > 59 {
				return native, "", false, false
			}
			if zone == "+00:00" {
				out = out[:end] + "Z"
			}
		}
	} else if end != len(out) {
		return native, "", false, false
	}
	return native, out, false, true
}

func tomlDigits(text string) (int, error) {
	for _, digit := range text {
		if digit < '0' || digit > '9' {
			return 0, errTOMLShape
		}
	}
	return strconv.Atoi(text)
}
