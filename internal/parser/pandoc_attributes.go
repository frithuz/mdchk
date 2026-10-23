package parser

import (
	"context"
	"reflect"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// AttributeOccurrence retains one ordered source occurrence. Raw includes its
// sigil/key and value; Value is the decoded value, independently source mapped.
type AttributeOccurrence struct {
	Kind, Key  string
	Raw, Value Value
}
type AttributeKeyValue struct{ Key, Value string }
type Attributes struct {
	BareClass   bool
	Raw         Value
	Occurrences []AttributeOccurrence
	ID          Optional[string]
	Classes     []string
	KeyValues   []AttributeKeyValue
}

// PandocFenceProperties distinguish the selected code language from raw format.
// Info retains the complete original suffix, including whitespace.
type PandocFenceProperties struct {
	Info      Value
	Language  Optional[string]
	Classes   []string
	RawFormat *Value
}
type DivProperties struct{ Origin string }

// attributeInput maps logical bytes to the existing immutable source. Prefix
// gaps never become part of a token; CRLF remains one indivisible source unit.
type attributeInput struct {
	ctx   context.Context
	text  string
	parts []Segment
	ends  []int
}

// Normalize physical endings in separate atomic contributions. A CRLF byte
// pair remains indivisible even though its logical spelling has one byte.
func attributeParts(ctx context.Context, parts []Segment) []Segment {
	out := meteredSlice[Segment](ctx, 0)
	for _, part := range parts {
		meterWork(ctx, len(part.Semantic.Value))
		if part.Transformation != Exact {
			out = meteredAppend(ctx, out, part)
			continue
		}
		raw := part.Semantic.Value
		run := 0
		for i := 0; i < len(raw); i++ {
			if raw[i] != '\r' {
				continue
			}
			if i > run {
				s := part
				s.Source.start += run
				s.Source.end = part.Source.start + i
				s.Semantic = Some(raw[run:i])
				out = meteredAppend(ctx, out, s)
			}
			end := i + 1
			if end < len(raw) && raw[end] == '\n' {
				end++
			}
			s := part
			s.Source.start += i
			s.Source.end = part.Source.start + end
			s.Transformation = Normalized
			s.Semantic = Some("\n")
			out = meteredAppend(ctx, out, s)
			i = end - 1
			run = end
		}
		if run < len(raw) {
			s := part
			s.Source.start += run
			s.Semantic = Some(raw[run:])
			out = meteredAppend(ctx, out, s)
		}
	}
	return out
}
func newAttributeInput(ctx context.Context, parts []Segment) attributeInput {
	parts = attributeParts(ctx, parts)
	var b strings.Builder
	for _, part := range parts {
		meterWork(ctx, 1)
		meteredWriteString(ctx, &b, part.Semantic.Value)
	}
	ends := meteredSlice[int](ctx, len(parts))
	offset := 0
	for i, part := range parts {
		meterWork(ctx, 1)
		offset += len(part.Semantic.Value)
		ends[i] = offset
	}
	return attributeInput{ctx: ctx, text: b.String(), parts: parts, ends: ends}
}
func (a attributeInput) value(start, end int, semantic string) Value {
	parts := meteredSlice[Segment](a.ctx, 0)
	first := sort.Search(len(a.ends), func(i int) bool { meterWork(a.ctx, 1); return a.ends[i] > start })
	offset := 0
	if first > 0 {
		offset = a.ends[first-1]
	}
	for _, part := range a.parts[first:] {
		meterWork(a.ctx, 1)
		n := len(part.Semantic.Value)
		lo, hi := max(start, offset), min(end, offset+n)
		if lo < hi {
			s := part
			if n == part.Source.end-part.Source.start && part.Transformation == Exact {
				s.Source.start += lo - offset
				s.Source.end = s.Source.start + hi - lo
				s.Semantic = Some(part.Semantic.Value[lo-offset : hi-offset])
			}
			parts = meteredAppend(a.ctx, parts, s)
		}
		offset += n
		if offset >= end {
			break
		}
	}
	var raw strings.Builder
	for _, part := range parts {
		meteredWriteString(a.ctx, &raw, part.Semantic.Value)
	}
	meterWork(a.ctx, len(semantic))
	if raw.String() != semantic {
		for i := range parts {
			parts[i].Transformation = Normalized
			parts[i].Semantic = Optional[string]{}
		}
	}
	return Value{Segments: parts, Semantic: Some(semantic)}
}
func attributeLetter(r rune) bool   { return unicode.IsLetter(r) }
func attributeAlphaNum(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) }
func attributeID(r rune) bool       { return attributeAlphaNum(r) || strings.ContainsRune("-_:.", r) }
func (a attributeInput) runeAt(i int) (rune, int) {
	meterWork(a.ctx, 1)
	return utf8.DecodeRuneInString(a.text[i:])
}
func (a attributeInput) spaces(i int) (int, bool) {
	for i < len(a.text) && (a.text[i] == ' ' || a.text[i] == '\t') {
		meterWork(a.ctx, 1)
		i++
	}
	if i < len(a.text) && a.text[i] == '\n' {
		i++
		for i < len(a.text) && (a.text[i] == ' ' || a.text[i] == '\t') {
			meterWork(a.ctx, 1)
			i++
		}
		if i < len(a.text) && a.text[i] == '\n' {
			return i, false
		}
	}
	return i, true
}

// scanAttributes is shared by block producers and future inline producers. On
// failure it returns no records; the operation's work charges are cumulative.
func scanAttributes(ctx context.Context, parts []Segment, start int) (*Attributes, int) {
	a := newAttributeInput(ctx, parts)
	return a.scan(start)
}
func (a attributeInput) scan(start int) (*Attributes, int) {
	t := a.text
	if start >= len(t) || t[start] != '{' {
		return nil, start
	}
	i, ok := a.spaces(start + 1)
	if !ok {
		return nil, start
	}
	meterRecords(a.ctx, 1)
	out := &Attributes{}
	for i < len(t) && t[i] != '}' {
		token, valid := a.token(i)
		if !valid {
			return nil, start
		}
		begin, valueStart := token.begin, token.valueStart
		i = token.end
		kind, key, value := token.kind, token.key, token.value
		meterRecords(a.ctx, 1)
		occurrence := AttributeOccurrence{Kind: kind, Key: key, Raw: a.value(begin, i, t[begin:i]), Value: a.value(valueStart, i, value)}
		out.Occurrences = meteredAppend(a.ctx, out.Occurrences, occurrence)
		switch kind {
		case "id":
			out.ID = Some(value)
		case "class", "special":
			out.Classes = meteredAppend(a.ctx, out.Classes, value)
		default:
			switch key {
			case "id":
				out.ID = Some(value)
			case "class":
				meterWork(a.ctx, len(value))
				out.Classes = meteredAppend(a.ctx, out.Classes, strings.Fields(value)...)
			default:
				out.KeyValues = meteredAppend(a.ctx, out.KeyValues, AttributeKeyValue{key, value})
			}
		}
		var ok bool
		i, ok = a.spaces(i)
		if !ok {
			return nil, start
		}
	}
	if i == len(t) {
		return nil, start
	}
	i++
	out.Raw = a.value(start, i, t[start:i])
	return out, i
}

type attributeToken struct {
	begin, valueStart, end int
	kind, key, value       string
	pendingQuote           bool
}

// token is the single authoritative atomic grammar for recognition and later
// materialization. Completion probes retain only their cursor, never Values.
func (a attributeInput) token(i int) (attributeToken, bool) {
	t := a.text
	pendingQuote := false
	begin := i
	kind, key, value := "", "", ""
	valueStart := i
	switch t[i] {
	case '#', '.':
		sigil := t[i]
		i++
		valueStart = i
		if i == len(t) {
			return attributeToken{}, false
		}
		r, _ := a.runeAt(i)
		if sigil == '.' && !attributeLetter(r) || sigil == '#' && !attributeID(r) {
			return attributeToken{}, false
		}
		for i < len(t) {
			r, n := a.runeAt(i)
			if !attributeID(r) {
				break
			}
			i += n
		}
		value = t[valueStart:i]
		kind = "class"
		if sigil == '#' {
			kind = "id"
		}
	case '-':
		kind = "special"
		value = "unnumbered"
		i++
	default:
		r, _ := a.runeAt(i)
		if !attributeLetter(r) {
			return attributeToken{}, false
		}
		for i < len(t) {
			r, n := a.runeAt(i)
			if !attributeID(r) {
				break
			}
			i += n
		}
		key = t[begin:i]
		if i == len(t) || t[i] != '=' {
			return attributeToken{}, false
		}
		i++
		valueStart = i
		kind = "key-value"
		// enclosed rejects leading whitespace and empty bodies; its explicit empty
		// alternatives run before the unquoted fallback (which retains quote bytes).
		quoted := false
		if i < len(t) && (t[i] == '"' || t[i] == '\'') {
			q := t[i]
			if i+1 == len(t) {
				pendingQuote = true
			}
			if i+1 < len(t) && t[i+1] == q {
				i += 2
				quoted = true
			} else if i+1 < len(t) {
				r, _ := a.runeAt(i + 1)
				if !unicode.IsSpace(r) {
					var b strings.Builder
					j := i + 1
					for j < len(t) && t[j] != q {
						if t[j] == '\n' {
							next := j + 1
							for next < len(t) && (t[next] == ' ' || t[next] == '\t') {
								next++
							}
							if next < len(t) && t[next] == '\n' {
								break
							}
							meteredWriteString(a.ctx, &b, " ")
							j++
							continue
						}
						s, n := a.literal(j, true)
						meteredWriteString(a.ctx, &b, s)
						j += n
					}
					if j == len(t) {
						pendingQuote = true
					}
					if j < len(t) && t[j] == q {
						value = b.String()
						i = j + 1
						quoted = true
					}
				}
			}
		}
		if !quoted {
			var b strings.Builder
			for i < len(t) && !strings.ContainsRune(" \t\n\r}", rune(t[i])) {
				s, n := a.literal(i, false)
				meteredWriteString(a.ctx, &b, s)
				i += n
			}
			value = b.String()
		}
	}
	return attributeToken{begin: begin, valueStart: valueStart, end: i, kind: kind, key: key, value: value, pendingQuote: pendingQuote}, true
}

// attributeCompletion resumes only at an atomic attribute or spnl boundary.
// An unfinished current token can be retried, but completed prefixes are never
// rescanned or materialized merely to discover a later physical ending.
type attributeCompletion struct {
	pos            int
	started, space bool
	end            int
}

func (c *attributeCompletion) resume(a attributeInput, start int, final bool) bool {
	if !c.started {
		if start >= len(a.text) || a.text[start] != '{' {
			return false
		}
		c.pos = start + 1
		c.started = true
		c.space = true
	}
	for {
		if c.space {
			next, ok := a.spaces(c.pos)
			if !ok {
				return false
			}
			c.pos = next
			if c.pos == len(a.text) {
				return false
			}
			c.space = false
		}
		if c.pos >= len(a.text) {
			return false
		}
		if a.text[c.pos] == '}' {
			c.end = c.pos + 1
			return true
		}
		token, ok := a.token(c.pos)
		if !ok {
			return false
		}
		if token.pendingQuote && !final {
			return false
		}
		c.pos = token.end
		c.space = true
	}
}
func (a attributeInput) literal(i int, quoted bool) (string, int) {
	t := a.text
	r, n := a.runeAt(i)
	if r == '\\' && i+n < len(t) {
		next, size := a.runeAt(i + n)
		if next != '\n' && next != '\r' && !attributeAlphaNum(next) {
			return string(next), n + size
		}
	}
	if quoted && r == '&' {
		if decoded, width := pandocNumericReference(a.ctx, t[i:]); width > 0 {
			return decoded, width
		}
		if decoded, width, kind := decodeTokenContext(a.ctx, t[i:]); width > 0 && kind == Entity {
			return decoded, width
		}
	}

	return string(r), n
}
func cloneAttributes(p *Attributes) *Attributes {
	if p == nil {
		return nil
	}
	x := *p
	x.Raw = cloneValue(x.Raw)
	x.Occurrences = slices.Clone(x.Occurrences)
	for i := range x.Occurrences {
		x.Occurrences[i].Raw = cloneValue(x.Occurrences[i].Raw)
		x.Occurrences[i].Value = cloneValue(x.Occurrences[i].Value)
	}
	x.Classes = slices.Clone(x.Classes)
	x.KeyValues = slices.Clone(x.KeyValues)
	return &x
}
func validateAttributes(s NodeSpec) error {
	p := s.Properties
	if p.Attributes != nil {
		if s.Kind != Heading && s.Kind != CodeBlock && s.Kind != Div {
			return ErrModel
		}
		a := p.Attributes
		if validateValue(s.Envelope, a.Raw) != nil || len(a.Raw.Segments) == 0 {
			return ErrModel
		}
		for i, v := range a.Raw.Segments {
			if i > 0 && !validAttributeGap(a.Raw.Segments[i-1].Source, v.Source) {
				return ErrModel
			}
			if !validPandocSpan(s.Envelope, v.Source) {
				return ErrModel
			}
		}
		// Reconstruct from the retained original segments, never from claimed decoded
		// values. This also validates occurrence order, effective values and grammar.
		var parts []Segment
		index := LineIndex{source: s.Envelope.source, ctx: context.Background()}
		for _, v := range a.Raw.Segments {
			parts = append(parts, index.semanticText(v.Source.start, v.Source.end).Segments...)
		}

		var rebuilt *Attributes
		var end int
		if a.BareClass {
			if s.Kind != Div || strings.ContainsAny(a.Raw.Semantic.Value, " \t\n\r") || a.Raw.Semantic.Value == "" {
				return ErrModel
			}
			for i := 1; i < len(parts); i++ {
				if parts[i-1].Source.end != parts[i].Source.start {
					return ErrModel
				}
			}
			logical := newAttributeInput(context.Background(), parts)
			v := logical.value(0, len(logical.text), logical.text)
			rebuilt = &Attributes{BareClass: true, Raw: v, Classes: []string{logical.text}, Occurrences: []AttributeOccurrence{{Kind: "class", Raw: v, Value: v}}}
			end = len(a.Raw.Semantic.Value)
		} else {
			rebuilt, end = scanAttributes(context.Background(), parts, 0)
		}
		if rebuilt == nil || end != len(a.Raw.Semantic.Value) || !reflect.DeepEqual(rebuilt, a) {
			return ErrModel
		}
	}
	if s.Kind == CodeBlock && p.Attributes != nil && p.PandocFence == nil {
		return ErrModel
	}
	if p.Div != nil {
		if s.Kind != Div || p.Div.Origin != "fenced-native" || p.Attributes == nil || !validPandocDelimiters(s, true) {
			return ErrModel
		}
	} else if s.Kind == Div {
		return ErrModel
	}
	if p.PandocFence != nil {
		if s.Kind != CodeBlock && s.Kind != RawBlock || p.Code == nil || p.Code.Style != "fenced" || validateValue(s.Envelope, p.PandocFence.Info) != nil || !validPandocDelimiters(s, false) {
			return ErrModel
		}
		parts := originalAttributeParts(s.Envelope, p.PandocFence.Info)
		facts, attrs := scanPandocFenceInfo(context.Background(), parts)
		if !reflect.DeepEqual(facts, p.PandocFence) || !reflect.DeepEqual(attrs, p.Attributes) {
			return ErrModel
		}
		if p.PandocFence.RawFormat != nil {
			if s.Kind != RawBlock || p.PandocFence.Language.Present || p.Attributes != nil || validateValue(s.Envelope, *p.PandocFence.RawFormat) != nil || p.PandocFence.RawFormat.Semantic.Value == "" {
				return ErrModel
			}
		} else if s.Kind != CodeBlock {
			return ErrModel
		}
	} else if s.Kind == RawBlock {
		return ErrModel
	}
	return nil
}

func originalAttributeParts(envelope Span, value Value) []Segment {
	index := LineIndex{source: envelope.source, ctx: context.Background()}
	var parts []Segment
	for _, part := range value.Segments {
		if !validPandocSpan(envelope, part.Source) {
			return nil
		}
		parts = append(parts, index.semanticText(part.Source.start, part.Source.end).Segments...)
	}
	return parts
}

// Only removed recursive prefixes can separate logical attribute contributions.
// Ordinary missing bytes inside a physical line are never provenance gaps.
func validAttributeGap(previous, next Span) bool {
	if previous.source.identity != next.source.identity || previous.end > next.start {
		return false
	}
	if previous.end == next.start {
		return true
	}
	raw := previous.source.content
	if previous.end == 0 || raw[previous.end-1] != '\n' && raw[previous.end-1] != '\r' {
		return false
	}
	return strings.Trim(raw[previous.end:next.start], " >\t") == ""
}

func validPandocDelimiters(s NodeSpec, div bool) bool {
	var opening, closing Span
	for _, m := range s.Markers {
		switch m.Role {
		case "opening":
			if opening.Present() {
				return false
			}
			opening = m.Source
		case "closing":
			if closing.Present() {
				return false
			}
			closing = m.Source
		}
	}
	if !validPandocSpan(s.Envelope, opening) || opening.end-opening.start < 3 {
		return false
	}
	raw := s.Envelope.source.content
	character := raw[opening.start]
	if div {
		if character != ':' {
			return false
		}
	} else if character != '`' && character != '~' {
		return false
	}
	if strings.Trim(raw[opening.start:opening.end], string(character)) != "" {
		return false
	}
	if closing.Present() {
		if !validPandocSpan(s.Envelope, closing) || closing.start < opening.end || closing.end-closing.start < 3 || strings.Trim(raw[closing.start:closing.end], string(character)) != "" {
			return false
		}
		if !div && closing.end-closing.start < opening.end-opening.start {
			return false
		}
		if strings.Trim(raw[closing.end:s.Envelope.end], " \t\r\n") != "" {
			return false
		}
	} else if !div {
		return false
	}
	if !div {
		p := s.Properties
		if p.Code.Fence != Some(character) || p.Code.Width != Some(opening.end-opening.start) || len(s.Children) != 0 {
			return false
		}
		last := opening.end
		for i, part := range p.PandocFence.Info.Segments {
			if !validPandocSpan(s.Envelope, part.Source) || i == 0 && part.Source.start != opening.end || i > 0 && !validAttributeGap(p.PandocFence.Info.Segments[i-1].Source, part.Source) {
				return false
			}
			last = part.Source.end
		}
		for _, part := range s.Content {
			if !validPandocSpan(s.Envelope, part.Source) || part.Source.start < last || part.Source.end > closing.start {
				return false
			}
			last = part.Source.end
		}
	}
	return true
}

// The pinned attribute litChar calls the entity lookup directly, without the
// CommonMark reader's six/seven-digit lexical limit. Accumulate with a Unicode
// ceiling so arbitrary leading zeroes and oversized values stay bounded.
func pandocNumericReference(ctx context.Context, text string) (string, int) {
	if len(text) < 4 || text[0] != '&' || text[1] != '#' {
		return "", 0
	}
	i, base := 2, 10
	if i < len(text) && (text[i] == 'x' || text[i] == 'X') {
		i++
		base = 16
	}
	begin, value := i, 0
	for i < len(text) {
		meterWork(ctx, 1)
		c := text[i]
		digit := -1
		if c >= '0' && c <= '9' {
			digit = int(c - '0')
		} else if base == 16 && c >= 'a' && c <= 'f' {
			digit = int(c-'a') + 10
		} else if base == 16 && c >= 'A' && c <= 'F' {
			digit = int(c-'A') + 10
		}
		if digit < 0 {
			break
		}
		value = min(0x110000, value*base+digit)
		i++
	}
	if i == begin || i == len(text) || text[i] != ';' {
		return "", 0
	}
	r := rune(value)
	if r == 0 || !utf8.ValidRune(r) {
		r = utf8.RuneError
	}
	return string(r), i + 1
}
