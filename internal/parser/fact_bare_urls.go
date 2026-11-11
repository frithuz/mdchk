package parser

import (
	"context"
	"errors"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// BareURLInventory contains detached original prose URL envelopes. Unknown
// necessary provenance atomically withholds the complete inventory.
type BareURLInventory struct {
	Spans        []Span
	Availability Availability
}

// Exact text borrows one contiguous interval from the immutable source. Only
// transformed contributions need a record; their source spelling stays atomic.
type bareURLMapping struct {
	start, end int
	value      string
}
type bareURLScanner struct {
	ctx                context.Context
	budget             *OperationBudget
	source             Source
	start, end         int
	present            bool
	mappings           []bareURLMapping
	spans              []Span
	unknown, uncertain bool
}

func (s *bareURLScanner) charge(work, nodes int64) error {
	return headingFactCharge(s.ctx, s.budget, work, nodes)
}
func bareURLGrow[T any](s *bareURLScanner, values []T) ([]T, error) {
	if err := s.charge(1, 0); err != nil {
		return nil, err
	}
	if len(values) < cap(values) {
		return values, nil
	}
	if cap(values) > int(^uint(0)>>1)/2 {
		return nil, ErrModel
	}
	capacity := max(1, cap(values)*2)
	if err := s.charge(int64(len(values))+int64(capacity)+1, int64(capacity)); err != nil {
		return nil, err
	}
	next := make([]T, len(values), capacity)
	copy(next, values)
	return next, nil
}
func (s *bareURLScanner) extend(start, end int) error {
	if err := s.charge(2, 0); err != nil {
		return err
	}
	if start == end {
		return nil
	}
	if s.present && s.end != start {
		if err := s.flush(); err != nil {
			return err
		}
	}
	if !s.present {
		s.start = start
		s.present = true
	}
	s.end = end
	return nil
}
func (s *bareURLScanner) appendMapping(m bareURLMapping) error {
	if m.end <= m.start {
		return ErrModel
	}
	if err := s.extend(m.start, m.end); err != nil {
		return err
	}
	var err error
	s.mappings, err = bareURLGrow(s, s.mappings)
	if err != nil {
		return err
	}
	if err = s.charge(1, 1); err != nil {
		return err
	}
	s.mappings = append(s.mappings, m)
	return nil
}
func (s *bareURLScanner) appendSpan(span Span) error {
	var err error
	s.spans, err = bareURLGrow(s, s.spans)
	if err != nil {
		return err
	}
	if err = s.charge(1, 1); err != nil {
		return err
	}
	s.spans = append(s.spans, span)
	return nil
}

// URI schemes are ASCII case-insensitive. Scheme recognition always uses
// original exact spelling; decoded characters cannot create a scheme.
func bareURLScheme(text string) int {
	for _, scheme := range []string{"https://", "http://", "ftp://"} {
		if len(text) >= len(scheme) && strings.EqualFold(text[:len(scheme)], scheme) {
			return len(scheme)
		}
	}
	return 0
}
func bareURLBoundaryRune(r rune) bool {
	return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("_+.-", r)
}
func bareURLBoundary(value string) bool {
	r, _ := utf8.DecodeLastRuneInString(value)
	return bareURLBoundaryRune(r)
}
func bareURLStop(value string) bool {
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.Is(unicode.Cc, r) || strings.ContainsRune("<>\"'", r) {
			return true
		}
	}
	return false
}

// candidate consumes one lexical candidate without retrying overlapping starts.
// mapping points at the first transformed contribution after its exact scheme.
func (s *bareURLScanner) candidate(start, schemeEnd, mapping int) (int, error) {
	if s.unknown {
		s.uncertain = true
	}
	cursor, j := schemeEnd, mapping
	opens, closes := 0, 0
	for cursor < s.end {
		if err := s.charge(16, 0); err != nil {
			return 0, err
		}
		if j < len(s.mappings) && s.mappings[j].start < cursor {
			return 0, ErrModel
		}
		if j < len(s.mappings) && s.mappings[j].start == cursor {
			m := s.mappings[j]
			if err := s.charge(int64(len(m.value))+2*int64(m.end-m.start)+1, 0); err != nil {
				return 0, err
			}
			if bareURLStop(m.value) {
				break
			}
			original := s.source.content[m.start:m.end]
			opens += strings.Count(original, "(")
			closes += strings.Count(original, ")")
			cursor = m.end
			j++
			continue
		}
		exactEnd := s.end
		if j < len(s.mappings) {
			exactEnd = s.mappings[j].start
		}
		r, width := utf8.DecodeRuneInString(s.source.content[cursor:exactEnd])
		if r == utf8.RuneError && width == 1 {
			return 0, ErrModel
		}
		if bareURLStop(s.source.content[cursor : cursor+width]) {
			break
		}
		if r == '(' {
			opens++
		} else if r == ')' {
			closes++
		}
		cursor += width
	}
	stop := cursor
	for stop > schemeEnd {
		if err := s.charge(4, 0); err != nil {
			return 0, err
		}
		c := s.source.content[stop-1]
		if barePunctuation(c) {
			stop--
			continue
		}
		if c == ')' && closes > opens {
			stop--
			closes--
			continue
		}
		if c == ';' {
			at := stop - 1
			for at > schemeEnd && asciiAlphanumeric(s.source.content[at-1]) {
				if err := s.charge(2, 0); err != nil {
					return 0, err
				}
				at--
			}
			if at < stop-1 && at > schemeEnd && s.source.content[at-1] == '&' {
				stop = at - 1
				continue
			}
		}
		break
	}
	// Cleanup cannot split a transformed source contribution. Exact source
	// punctuation is ASCII, so other endpoints stay at Unicode boundaries.
	for at := j - 1; at >= mapping && s.mappings[at].end > stop; at-- {
		if err := s.charge(2, 0); err != nil {
			return 0, err
		}
		if s.mappings[at].start < stop {
			stop = s.mappings[at].start
		}
	}
	hostEnd, hostLetter := schemeEnd, false
	for hostEnd < stop {
		if err := s.charge(8, 0); err != nil {
			return 0, err
		}
		r, width := utf8.DecodeRuneInString(s.source.content[hostEnd:stop])
		if strings.ContainsRune("/?#", r) {
			break
		}
		hostLetter = hostLetter || unicode.IsLetter(r) || unicode.IsDigit(r)
		hostEnd += width
	}
	if stop > schemeEnd && hostEnd > schemeEnd && hostLetter && !s.unknown {
		span, err := s.source.Span(start, stop)
		if err != nil {
			return 0, err
		}
		if err = s.appendSpan(span); err != nil {
			return 0, err
		}
	}
	return cursor, nil
}

func (s *bareURLScanner) flush() error {
	defer func() { s.present = false; s.mappings = s.mappings[:0] }()
	boundary := true
	mapping := 0
	for cursor := s.start; s.present && cursor < s.end; {
		if err := s.charge(32, 0); err != nil {
			return err
		}
		for mapping < len(s.mappings) && s.mappings[mapping].end <= cursor {
			if err := s.charge(2, 0); err != nil {
				return err
			}
			mapping++
		}
		if mapping < len(s.mappings) && s.mappings[mapping].start < cursor {
			return ErrModel
		}
		if mapping < len(s.mappings) && s.mappings[mapping].start == cursor {
			m := s.mappings[mapping]
			boundary = bareURLBoundary(m.value)
			cursor = m.end
			mapping++
			continue
		}
		exactEnd := s.end
		if mapping < len(s.mappings) {
			exactEnd = s.mappings[mapping].start
		}
		r, width := utf8.DecodeRuneInString(s.source.content[cursor:exactEnd])
		if r == utf8.RuneError && width == 1 {
			return ErrModel
		}
		scheme := 0
		if boundary {
			scheme = bareURLScheme(s.source.content[cursor:exactEnd])
		}
		if scheme != 0 {
			end, err := s.candidate(cursor, cursor+scheme, mapping)
			if err != nil {
				return err
			}
			cursor = end
			boundary = false
			continue
		}
		boundary = bareURLBoundaryRune(r)
		cursor += width
	}
	return s.charge(1, 0)
}

// text validates every original mapping, including clean text. Exact intervals
// borrow the immutable source; no per-byte or per-rune records are allocated.
func (s *bareURLScanner) text(n *Node) (bool, error) {
	if err := s.charge(16, 0); err != nil {
		return false, err
	}
	if !n.spec.Properties.Literal.Present || len(n.spec.Content) == 0 {
		return false, nil
	}
	previous := n.spec.Envelope.start
	for _, part := range n.spec.Content {
		if err := s.charge(24, 0); err != nil {
			return false, err
		}
		if part.Transformation == MappingUnavailable || !part.Semantic.Present {
			return false, nil
		}
		if part.Transformation == Synthetic {
			if err := s.flush(); err != nil {
				return false, err
			}
			continue
		}
		if !part.Source.present {
			return false, nil
		}
		if part.Source.SourceToken() != s.source.Token() || part.Source.start < previous || part.Source.end < part.Source.start || !contains(n.spec.Envelope, part.Source) {
			return false, ErrModel
		}
		previous = part.Source.end
		switch part.Transformation {
		case Normalized, PartialTab:
			if err := s.flush(); err != nil {
				return false, err
			}
			continue
		case Exact, Escape, Entity:
		default:
			return false, ErrModel
		}
		if err := s.charge(int64(len(part.Semantic.Value))+1, 0); err != nil {
			return false, err
		}
		if part.Transformation == Exact {
			if s.source.content[part.Source.start:part.Source.end] != part.Semantic.Value {
				return false, ErrModel
			}
			if err := s.extend(part.Source.start, part.Source.end); err != nil {
				return false, err
			}
		} else if err := s.appendMapping(bareURLMapping{start: part.Source.start, end: part.Source.end, value: part.Semantic.Value}); err != nil {
			return false, err
		}
	}
	return true, s.charge(1, 0)
}

// A bare GFM email/www owner can supply an already-started FTP or uppercase
// HTTP URL's payload. Its visible bytes remain atomic, and its synthetic
// destination cannot start or supply a scheme.
func (s *bareURLScanner) barePayload(n *Node) error {
	link := n.spec.Properties.Link
	work := int64(len(n.spec.Content))*32 + int64(len(link.Label.Semantic.Value))*8 + int64(len(link.Destination.Semantic.Value))*8 + 64
	if err := s.charge(work, 0); err != nil {
		return err
	}
	if err := validateBareSpec(n.spec); err != nil {
		return err
	}
	for _, part := range n.spec.Content {
		if part.Transformation == Exact && s.source.content[part.Source.start:part.Source.end] != part.Semantic.Value {
			return ErrModel
		}
	}
	return s.appendMapping(bareURLMapping{start: n.spec.Envelope.start, end: n.spec.Envelope.end, value: link.Label.Semantic.Value})
}

// BareURLs observes only HTTP, HTTPS and FTP prose on the completed original
// parse. Returned spans are detached and carry no correction authority.
func (d *Document) BareURLs(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) (out BareURLInventory, err error) {
	if err = d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0))
		if err != nil {
			out = BareURLInventory{}
		} else if out.Availability != Available {
			out.Spans = nil
		}
	}()
	s := bareURLScanner{ctx: ctx, budget: budget, source: index.source}
	type frame struct {
		node               *Node
		prose, exit, label bool
	}
	var stack []frame
	push := func(f frame) error {
		var e error
		stack, e = bareURLGrow(&s, stack)
		if e != nil {
			return e
		}
		if e = s.charge(1, 1); e != nil {
			return e
		}
		stack = append(stack, f)
		return nil
	}
	if err = push(frame{node: d.root}); err != nil {
		return out, err
	}
	raw := conflictRawState{}
	rawKnown := true
	if err = s.charge(2, 2); err != nil {
		return out, err
	}
	htmlWork := &parserContext{Context: ctx, budget: &parseBudget{operation: budget}}
	for {
		if err = s.charge(32, 0); err != nil {
			return out, err
		}
		if len(stack) == 0 {
			break
		}
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := f.node
		if n == nil || !n.spec.Envelope.present || n.spec.Envelope.SourceToken() != index.source.Token() || n.spec.Envelope.start < 0 || n.spec.Envelope.end < n.spec.Envelope.start || n.spec.Envelope.end > index.source.Len() {
			return out, ErrModel
		}
		if f.exit {
			if n.Kind() != Text {
				if err = s.flush(); err != nil {
					return out, err
				}
			}
			continue
		}
		prose := f.prose
		label := f.label
		barePayload := n.Kind() == Link && n.spec.Properties.Link != nil && n.spec.Properties.Link.Origin == "bare-gfm" && bareURLScheme(index.source.content[n.spec.Envelope.start:n.spec.Envelope.end]) == 0
		if n.Kind() != Text && !barePayload {
			if err = s.flush(); err != nil {
				return out, err
			}
		}
		switch n.Kind() {
		case Paragraph, Heading:
			prose = true
		case TableCell:
			if n.spec.Properties.Cell == nil {
				return BareURLInventory{Availability: Unavailable}, nil
			}
			prose = !n.spec.Properties.Cell.Alignment.Present
		case DocumentKind, BlockQuote, List, ListItem, Table, TableRow, HTMLBlock, CodeBlock:
			prose = false
		case Text:
			if prose && !label && (raw.name == "" || !rawKnown) {
				s.unknown = !rawKnown
				var known bool
				known, err = s.text(n)
				if err != nil {
					return out, err
				}
				if !known {
					return BareURLInventory{Availability: Unavailable}, nil
				}
			} else if err = s.flush(); err != nil {
				return out, err
			}
		case Link:
			if n.spec.Properties.Link == nil {
				return BareURLInventory{Availability: Unavailable}, nil
			}
			if n.spec.Properties.Link.Origin == "bare-gfm" && prose && !label && (raw.name == "" || !rawKnown) {
				text := index.source.content[n.spec.Envelope.start:n.spec.Envelope.end]
				if err = s.charge(int64(len(text))+1, 0); err != nil {
					return out, err
				}
				if bareURLScheme(text) == 0 {
					s.unknown = !rawKnown
					if err = s.barePayload(n); err != nil {
						return out, err
					}
				} else {
					if !rawKnown {
						s.uncertain = true
					} else if err = s.appendSpan(n.spec.Envelope); err != nil {
						return out, err
					}
				}
			}
			if n.spec.Properties.Link.Origin == "bare-gfm" {
				continue
			}
			// Explicit labels can contain real raw HTML transitions, while
			// all their URL spelling stays exempt. Images remain opaque below.
			label = true
		case Image, CodeSpan:
			continue // Whole owners are opaque, including raw tags in image alternatives.
		case InlineHTML, SoftBreak, HardBreak, ThematicBreak, Emphasis, Strong, Strike:
			// Boundaries retain their actual local inline owner or opaque spelling.
		default:
			return out, ErrModel
		}
		if n.Kind() == HTMLBlock {
			var available bool
			raw, _, available, err = conflictHTMLRaw(htmlWork, index.source, n, raw)
			if err != nil {
				return out, err
			}
			rawKnown = rawKnown && available
			continue // HTML-owned spelling and closing tails were consumed once.
		}
		if err = s.charge(2*int64(len(n.rawTag))+int64(len(raw.name))+1, 0); err != nil {
			return out, err
		}
		if n.rawTag != "" {
			if raw.name == "" && !n.rawClosing {
				raw = conflictRawState{name: n.rawTag, start: n.spec.Envelope.start}
			} else if raw.name == n.rawTag && n.rawClosing {
				raw = conflictRawState{}
			}
		}
		if n.Kind() == CodeBlock {
			continue
		}
		if err = push(frame{node: n, exit: true, label: label}); err != nil {
			return out, err
		}
		for group := 0; group < 2; group++ {
			children := n.htmlTail
			if group == 1 {
				children = n.spec.Children
			}
			if err = s.charge(int64(len(children))+3, 0); err != nil {
				return out, err
			}
			for i := len(children) - 1; i >= 0; i-- {
				if err = push(frame{node: children[i], prose: prose, label: label}); err != nil {
					return out, err
				}
			}
		}
	}
	if err = s.flush(); err != nil {
		return out, err
	}
	// Source ordering is explicit; private HTML tails need not have global preorder
	// offsets. Reserve comparisons before sorting the detached output.
	err = nestedExampleLocked(ctx, budget, index, func(work *parserContext, _ LineIndex) error {
		slices.SortFunc(s.spans, func(a, b Span) int {
			meterWork(work, 4)
			if a.start < b.start {
				return -1
			}
			if a.start > b.start {
				return 1
			}
			return 0
		})
		return nil
	})
	if err != nil {
		return out, err
	}
	if s.uncertain {
		return BareURLInventory{Availability: Unavailable}, nil
	}
	return BareURLInventory{Spans: s.spans, Availability: Available}, nil
}
