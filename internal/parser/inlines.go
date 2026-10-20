package parser

import (
	"context"
	"strings"
	"unicode/utf8"
)

// Inline state belongs to one logical paragraph/heading, never to a document or
// another call. Offsets here address logical semantic bytes, not Source bytes.
// Later providers consume this dispatch/state boundary; they must map any split
// back through Content rather than treating logical offsets as source offsets.
type inlineDelimiter struct {
	marker                  byte
	offset, width, original int
	canOpen, canClose       bool
	previous, next          int
	retired                 bool
}

type inlineBracket struct {
	offset, node, bottom int
	image                bool
}

type inlineScanner struct {
	ctx              context.Context
	text             string
	delimiters       []inlineDelimiter
	delimitersLinked bool
	delimiterHead    int
	delimiterTail    int
	brackets         []inlineBracket
	inactiveBefore   int                  // original logical offsets below this cannot open links
	references       *ReferenceCollection // borrowed immutable completed document registry
	bareLinkEnds     []int                // lazily indexed raw destination endpoints; -1 means unbalanced
	backticks        map[int][]int        // raw run starts by width, even after a backslash
	backtickNext     map[int]int          // monotonic next-closer cursor per width
	htmlEnds         map[string][]int     // original logical terminators/quotes, indexed once
	pending          bool
}

func parseInlines(ctx context.Context, document *Document, blocks PhaseState) (PhaseState, error) {
	meterWork(ctx, 1)
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
	root, err := inlineTree(ctx, document.root, &state, &document.references)
	if err != nil {
		return Pending, err
	}
	if interruption(ctx) != "" {
		return Interrupted, nil
	}
	if err := validateReferenceLinks(ctx, root, document.references); err != nil {
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

func inlineTree(ctx context.Context, node *Node, state *PhaseState, references *ReferenceCollection) (*Node, error) {
	meterWork(ctx, 1)
	if interruption(ctx) != "" {
		return node, nil
	}
	checkDepth(ctx, ancestorDepth(ctx))
	spec := node.spec // borrow immutable blocks; NewNode freezes changed children
	switch spec.Kind {
	case Paragraph, Heading:
		var text strings.Builder
		for _, part := range spec.Content {
			meterWork(ctx, 1)
			if interruption(ctx) != "" {
				return node, nil
			}
			if !part.Source.Present() || !part.Semantic.Present {
				return nil, ErrModel // recognized block payloads have exact mappings
			}
			meteredWriteString(ctx, &text, part.Semantic.Value)
		}
		scannerContext := withAncestor(ctx, ancestorDepth(ctx)+1)
		checkDepth(scannerContext, ancestorDepth(scannerContext))
		scanner := inlineScanner{ctx: scannerContext, text: text.String(), references: references}
		scanner.opaqueCandidates()
		children, err := scanner.scan(spec.Content)
		if err == nil && interruption(ctx) == "" {
			children, err = scanner.emphasis(spec.Content, children)
		}
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
		spec.Children = meteredSlice[*Node](ctx,

			len(node.spec.Children))
		for i, child := range node.spec.Children {
			meterWork(ctx, 1)
			if interruption(ctx) != "" {
				return node, nil
			}
			var err error
			childContext := ctx
			if spec.Kind != DocumentKind {
				childContext = withAncestor(ctx, ancestorDepth(ctx)+1)
			}
			checkDepth(childContext, ancestorDepth(childContext))
			spec.Children[i], err = inlineTree(childContext, child, state, references)
			if err != nil {
				return nil, err
			}
		}
	default:
		return node, nil // code and raw HTML are opaque, including in containers
	}
	return meteredNode(ctx, spec)
}

// Dispatch consumes known text escapes/entities and classifies emphasis runs.
// Delimiters are classified here, after opaque constructs and decoding tokens
// have been consumed. Pairing happens at successful label boundaries and block end.
func (s *inlineScanner) dispatch(offset int) int {
	meterWork(s.ctx, 1)
	if _, width, _ := decodeTokenContext(s.ctx, s.text[offset:]); width != 0 {
		return offset + width // generated characters never enter syntax dispatch
	}
	switch c := s.text[offset]; c {
	case '*', '_':
		end := offset + 1
		for end < len(s.text) && s.text[end] == c {
			meterWork(s.ctx, 1)
			if (end-offset)%256 == 0 && interruption(s.ctx) != "" {
				return len(s.text)
			}
			end++
		}
		before, after := rune(' '), rune(' ')
		if offset > 0 {
			before, _ = utf8.DecodeLastRuneInString(s.text[:offset])
		}
		if end < len(s.text) {
			after, _ = utf8.DecodeRuneInString(s.text[end:])
		}
		open, close := delimiterFlanking(c, before, after)
		s.appendDelimiter(inlineDelimiter{marker: c, offset: offset, width: end - offset, original: end - offset, canOpen: open, canClose: close})
		return end
	}
	return offset + 1
}

// Scoped label processing can retire a suffix before scanning the next run.
// Append to the remaining live prefix without relinking or reviving old records.
func (s *inlineScanner) appendDelimiter(d inlineDelimiter) {
	meterWork(s.ctx, 1)
	i := len(s.delimiters)
	if s.delimitersLinked {
		d.previous, d.next = s.delimiterTail, i+1
		if s.delimiterTail >= 0 {
			s.delimiters[s.delimiterTail].next = i
		} else {
			s.delimiterHead = i
		}
		s.delimiterTail = i
	}
	s.delimiters = meteredAppend(s.ctx, s.delimiters, d)
}

// Index maximal raw backtick runs once, including runs after a backslash: code
// closers ignore ordinary-text escapes. HTML terminators are indexed lazily by
// the HTML recognizer, independently of these raw backtick runs.
func (s *inlineScanner) opaqueCandidates() {
	meterWork(s.ctx, 1)
	for offset, checkpoint := 0, 0; offset < len(s.text); {
		meterWork(s.ctx, 1)
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
			meterWork(s.ctx, 1)
			if (offset-start)%256 == 0 && interruption(s.ctx) != "" {
				return
			}
			offset++
		}
		if s.backticks == nil {
			s.backticks = make(map[int][]int)
			s.backtickNext = make(map[int]int)
		}
		s.backticks[offset-start] = meteredAppend(s.ctx, s.backticks[offset-start], start)
	}
}

func asciiLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
