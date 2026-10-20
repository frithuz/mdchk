package parser

import (
	"context"
	"strings"
)

// Inline state belongs to one logical paragraph/heading, never to a document or
// another call. Offsets here address logical semantic bytes, not Source bytes.
// Later providers consume this dispatch/state boundary; they must map any split
// back through Content rather than treating logical offsets as source offsets.
type inlineDelimiter struct {
	marker        byte
	offset, width int
}

type inlineBracket struct {
	offset int
	image  bool
}

type inlineScanner struct {
	ctx          context.Context
	text         string
	delimiters   []inlineDelimiter
	brackets     []inlineBracket
	backticks    map[int][]int // raw run starts by width, even after a backslash
	backtickNext map[int]int   // monotonic next-closer cursor per width
	markers      [2]bool
	angleClose   int
	pending      bool
}

func parseInlines(ctx context.Context, document *Document, blocks PhaseState) (PhaseState, error) {
	if blocks != PhaseComplete || document.references.LookupAvailability != Available {
		return NotStarted, nil
	}
	if interruption(ctx) != "" {
		return Interrupted, nil
	}
	if !document.blocksRecognized {
		return Pending, nil
	}
	if document.inlineFree {
		return PhaseComplete, nil
	}
	state := PhaseComplete
	root, err := inlineTree(ctx, document.root, &state)
	if err != nil {
		return Pending, err
	}
	if interruption(ctx) != "" {
		return Interrupted, nil
	}
	// Publish a new immutable tree once. On interruption the already closed
	// block tree survives, without a partly constructed inline handoff.
	document.root = root
	return state, nil
}

func inlineTree(ctx context.Context, node *Node, state *PhaseState) (*Node, error) {
	if interruption(ctx) != "" {
		return node, nil
	}
	spec := node.spec // borrow immutable blocks; NewNode freezes changed children
	switch spec.Kind {
	case Paragraph, Heading:
		var text strings.Builder
		for _, part := range spec.Content {
			if interruption(ctx) != "" {
				return node, nil
			}
			if !part.Source.Present() || !part.Semantic.Present {
				return nil, ErrModel // recognized block payloads have exact mappings
			}
			text.WriteString(part.Semantic.Value)
		}
		scanner := inlineScanner{ctx: ctx, text: text.String()}
		scanner.opaqueCandidates()
		children, err := scanner.scan(spec.Content)
		if err != nil {
			return nil, err
		}
		if interruption(ctx) != "" {
			return node, nil
		}
		if scanner.pending {
			*state = Pending
		}
		spec.Children = children
	case DocumentKind, BlockQuote, List, ListItem:
		spec.Children = make([]*Node, len(node.spec.Children))
		for i, child := range node.spec.Children {
			if interruption(ctx) != "" {
				return node, nil
			}
			var err error
			spec.Children[i], err = inlineTree(ctx, child, state)
			if err != nil {
				return nil, err
			}
		}
	default:
		return node, nil // code and raw HTML are opaque, including in containers
	}
	return NewNode(spec)
}

// Dispatch consumes known text escapes/entities or defers to a missing provider.
// A potential pair is not recognized syntax: false-positive deferral is allowed,
// but unavailable syntax must never be advertised as complete literal text.
func (s *inlineScanner) dispatch(offset int) int {
	if _, width, _ := decodeToken(s.text[offset:]); width != 0 {
		return offset + width // generated characters never enter syntax dispatch
	}
	switch c := s.text[offset]; c {
	case '<':
		s.pending = s.pending || s.possibleInlineHTML(offset) // #23 owns HTML interpretation
	case '[', ']':
		if c == '[' {
			s.brackets = append(s.brackets, inlineBracket{offset: offset, image: offset > 0 && s.text[offset-1] == '!'})
		} else if len(s.brackets) != 0 {
			s.pending = true // #26–#28 own bracket precedence and resolution
			s.brackets = s.brackets[:len(s.brackets)-1]
		}
	case '*', '_':
		end := offset + 1
		for end < len(s.text) && s.text[end] == c {
			if (end-offset)%256 == 0 && interruption(s.ctx) != "" {
				return len(s.text)
			}
			end++
		}
		s.delimiters = append(s.delimiters, inlineDelimiter{marker: c, offset: offset, width: end - offset})
		index := 0
		if c == '_' {
			index = 1
		}
		s.pending = s.pending || s.markers[index] // #24/#25 settle actual flanking
		s.markers[index] = true
		return end
	}
	return offset + 1
}

// Index maximal raw backtick runs once, including runs after a backslash: code
// closers ignore ordinary-text escapes. The angle ending remains a conservative
// availability guard for #23, not recognition of inline HTML syntax.
func (s *inlineScanner) opaqueCandidates() {
	s.angleClose = strings.LastIndexByte(s.text, '>')
	for offset, checkpoint := 0, 0; offset < len(s.text); {
		if offset >= checkpoint {
			if interruption(s.ctx) != "" {
				return
			}
			checkpoint = offset + 256
		}
		if s.text[offset] != '`' {
			offset++
			continue
		}
		start := offset
		for offset < len(s.text) && s.text[offset] == '`' {
			if (offset-start)%256 == 0 && interruption(s.ctx) != "" {
				return
			}
			offset++
		}
		if s.backticks == nil {
			s.backticks = make(map[int][]int)
			s.backtickNext = make(map[int]int)
		}
		s.backticks[offset-start] = append(s.backticks[offset-start], start)
	}
}

func asciiLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
