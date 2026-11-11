// Facts for MD011.

package parser

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// SwappedLinkInventory contains only detached original candidate envelopes.
// Necessary unknown provenance atomically withholds the complete inventory.
type SwappedLinkInventory struct {
	Spans        []Span
	Availability Availability
}

type swappedLinkUnit struct {
	start, end int
	value      string
	syntax     byte
	nonblank   bool
	// exact marks one original Exact rune rather than decoded payload.
	exact bool
}

type swappedLinkScanner struct {
	ctx                context.Context
	budget             *OperationBudget
	source             Source
	units              []swappedLinkUnit
	spans              []Span
	unknown, uncertain bool
	// island replaces the MD011 candidate pass with another island consumer.
	island func() error
	// linkEnd is the end of the latest opaque Link or Image owner, or zero.
	linkEnd int
}

func (s *swappedLinkScanner) charge(work, nodes int64) error {
	return headingFactCharge(s.ctx, s.budget, work, nodes)
}

func swappedLinkGrow[T any](s *swappedLinkScanner, values []T) ([]T, error) {
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

func (s *swappedLinkScanner) appendUnit(u swappedLinkUnit) error {
	var err error
	s.units, err = swappedLinkGrow(s, s.units)
	if err != nil {
		return err
	}
	if err = s.charge(1, 1); err != nil {
		return err
	}
	s.units = append(s.units, u)
	return nil
}

// group consumes one top-level group once. Nested or mismatched groups cannot
// yield an inner candidate. An unmatched opening consumes the island remainder.
func (s *swappedLinkScanner) group(start int) (end int, flat bool, err error) {
	depth := 0
	flat = true
	opening := s.units[start].syntax
	for i := start; ; i++ {
		if err = s.charge(8, 0); err != nil {
			return 0, false, err
		}
		if i == len(s.units) {
			return i, false, nil
		}
		switch c := s.units[i].syntax; c {
		case '(', '[':
			depth++
			if depth > 1 {
				flat = false
			}
		case ')', ']':
			depth--
			if depth == 0 {
				if opening == '(' && c != ')' || opening == '[' && c != ']' {
					flat = false
				}
				return i + 1, flat, nil
			}
		default:
			// Ordinary retained semantic payload cannot establish delimiters.
		}
	}
}

func (s *swappedLinkScanner) flush() error {
	defer func() { s.units = s.units[:0] }()
	if s.island != nil {
		return s.island()
	}
	// Unknown raw state is relevant to conservative original Text spelling,
	// never decided using a successful URL or post-raw candidate selection.
	if s.unknown {
		opening := false
		for i, u := range s.units {
			if err := s.charge(4, 0); err != nil {
				return err
			}
			opening = opening || u.syntax == '('
			if opening && u.syntax == ')' && i+1 < len(s.units) && s.units[i+1].syntax == '[' && u.end == s.units[i+1].start {
				s.uncertain = true
			}
		}
	}
	completeEnd := -1
	for i := 0; ; {
		if err := s.charge(8, 0); err != nil {
			return err
		}
		if i == len(s.units) {
			return nil
		}
		c := s.units[i].syntax
		if c != '(' && c != '[' {
			i++
			continue
		}
		end, flat, err := s.group(i)
		if err != nil {
			return err
		}
		if !flat || c != '(' || end == len(s.units) || s.units[end].syntax != '[' || s.units[end-1].end != s.units[end].start {
			i = end
			continue
		}
		last, flatURL, err := s.group(end)
		if err != nil {
			return err
		}
		if !flatURL {
			i = last
			continue
		}
		if err = s.charge(12, 0); err != nil {
			return err
		}
		eligible := true
		if i > 0 {
			before := s.units[i-1].syntax
			eligible = before != '!' && ((before != ')' && before != ']') || completeEnd == i)
		}
		if last < len(s.units) && (s.units[last].syntax == ')' || s.units[last].syntax == ']') {
			eligible = false
		}
		completeEnd = last
		nonblank := false
		for j := i + 1; j < end-1; j++ {
			if err = s.charge(1, 0); err != nil {
				return err
			}
			nonblank = nonblank || s.units[j].nonblank
		}
		if eligible && nonblank {
			valid, e := s.validURL(s.units[end+1 : last-1])
			if e != nil {
				return e
			}
			if valid && !s.unknown {
				s.spans, err = swappedLinkGrow(s, s.spans)
				if err != nil {
					return err
				}
				if err = s.charge(1, 1); err != nil {
					return err
				}
				s.spans = append(s.spans, Span{source: s.source, start: s.units[i].start, end: s.units[last-1].end, present: true})
			}
		}
		i = last
	}
}

func swappedLinkHex(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'A' && b <= 'F' || b >= 'a' && b <= 'f'
}

func (s *swappedLinkScanner) validURL(units []swappedLinkUnit) (bool, error) {
	length := 0
	for _, u := range units {
		if err := s.charge(2, 0); err != nil {
			return false, err
		}
		if len(u.value) > int(^uint(0)>>1)-length {
			return false, ErrModel
		}
		length += len(u.value)
	}
	// Reserve the joined spelling and its copy before allocation. Library parsing
	// is linear in this bounded spelling; reserve scans, component copies and
	// error formatting before entering net/url, including rejected candidates.
	work, nodes := int64(length), int64(length)
	for i := 0; i < 6; i++ {
		work = addBounded(work, work)
	}
	for i := 0; i < 3; i++ {
		nodes = addBounded(nodes, nodes)
	}
	if err := s.charge(addBounded(work, 256), addBounded(nodes, 16)); err != nil {
		return false, err
	}
	var joined strings.Builder
	joined.Grow(length)
	for _, u := range units {
		joined.WriteString(u.value)
	}
	value := joined.String()
	for i := 0; ; {
		if err := s.charge(256, 0); err != nil {
			return false, err
		}
		if i == len(value) {
			break
		}
		r, width := utf8.DecodeRuneInString(value[i:])
		if r == utf8.RuneError && width == 1 {
			return false, ErrModel
		}
		if unicode.IsSpace(r) || unicode.Is(unicode.Cc, r) {
			return false, nil
		}
		if r < utf8.RuneSelf && !strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~:/?#[]@!$&'()*+,;=%", r) {
			return false, nil
		}
		if r == '%' && (i+2 >= len(value) || !swappedLinkHex(value[i+1]) || !swappedLinkHex(value[i+2])) {
			return false, nil
		}
		i += width
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return false, nil
	}
	hostname := parsed.Hostname()
	if parsed.Opaque != "" || hostname == "" {
		return false, nil
	}
	// Keep HTTP authority validity independent of net/url's legacy-colon
	// compatibility switch. Bracketed IP literals were validated by Parse.
	// Both hostname scans are included in the upfront linear reservation.
	if strings.ContainsAny(hostname, "[]") || strings.ContainsRune(hostname, ':') && parsed.Host[0] != '[' {
		return false, nil
	}
	return strings.EqualFold(parsed.Scheme, "http") || strings.EqualFold(parsed.Scheme, "https"), nil
}

// text retains semantic payload exactly once while original Exact bytes alone
// supply structure. No entity or escape is decoded a second time.
func (s *swappedLinkScanner) text(n *Node) (bool, error) {
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
		if len(s.units) != 0 && s.units[len(s.units)-1].end != part.Source.start {
			if err := s.flush(); err != nil {
				return false, err
			}
		}
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
		exact := part.Transformation == Exact
		if exact {
			if err := s.charge(int64(len(part.Semantic.Value))+1, 0); err != nil {
				return false, err
			}
			if s.source.content[part.Source.start:part.Source.end] != part.Semantic.Value {
				return false, ErrModel
			}
		}
		nonblank, barrier := false, false
		for offset := 0; ; {
			if err := s.charge(256, 0); err != nil {
				return false, err
			}
			if offset == len(part.Semantic.Value) {
				break
			}
			r, width := utf8.DecodeRuneInString(part.Semantic.Value[offset:])
			if r == utf8.RuneError && width == 1 {
				return false, ErrModel
			}
			blocked := r == '\r' || r == '\n' || unicode.Is(unicode.Cc, r) && r != '\t' && r != '\f'
			meaningful := !IsUnicodeWhitespace(r)
			if exact {
				if blocked {
					if err := s.flush(); err != nil {
						return false, err
					}
				} else {
					u := swappedLinkUnit{start: part.Source.start + offset, end: part.Source.start + offset + width, value: part.Semantic.Value[offset : offset+width], nonblank: meaningful, exact: true}
					if r == '(' || r == ')' || r == '[' || r == ']' || r == '!' {
						u.syntax = byte(r)
					}
					if err := s.appendUnit(u); err != nil {
						return false, err
					}
				}
			} else {
				nonblank = nonblank || meaningful
				barrier = barrier || blocked
			}
			offset += width
		}
		if !exact {
			if barrier {
				if err := s.flush(); err != nil {
					return false, err
				}
			} else if err := s.appendUnit(swappedLinkUnit{start: part.Source.start, end: part.Source.end, value: part.Semantic.Value, nonblank: nonblank}); err != nil {
				return false, err
			}
		}
	}
	return true, s.charge(1, 0)
}

// SwappedLinks observes conservative malformed candidates beside authoritative
// syntax. Valid constructs win; original ownership and allowance remain binding.
func (d *Document) SwappedLinks(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) (out SwappedLinkInventory, err error) {
	if err = d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0))
		if err != nil {
			out = SwappedLinkInventory{}
		} else if out.Availability != Available {
			out.Spans = nil
		}
	}()
	// Exact adjacent outer bytes are necessary even with transformed payload.
	if err = headingFactCharge(ctx, budget, int64(index.source.Len())+1, 0); err != nil {
		return out, err
	}
	if !strings.Contains(index.source.content, ")[") {
		return SwappedLinkInventory{Availability: Available}, nil
	}
	s := swappedLinkScanner{ctx: ctx, budget: budget, source: index.source}
	known, err := d.proseIslands(&s, index)
	if err != nil {
		return out, err
	}
	if !known {
		return SwappedLinkInventory{Availability: Unavailable}, nil
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
		return SwappedLinkInventory{Availability: Unavailable}, nil
	}
	return SwappedLinkInventory{Spans: s.spans, Availability: Available}, nil
}
