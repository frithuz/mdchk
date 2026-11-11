package parser

import (
	"context"
	"errors"
	"math"
	"strings"
)

// UnclosedHTMLComment retains only the original four-byte unmatched opener.
// The detached span is manual evidence, never an edit.
type UnclosedHTMLComment struct{ Open Span }

// UnclosedHTMLComments is an atomic source-owned inventory. Unavailable means a
// named token in actual HTML context could not be completed; Comments is then nil.
type UnclosedHTMLComments struct {
	Comments     []UnclosedHTMLComment
	Availability Availability
}

// UnclosedHTMLComments uses only completed private production syntax and its original
// source/options/profile/index/operation. It never visits Image children or scans
// ordinary Markdown Text. HTML owners include their own tails exactly once.
func (d *Document) UnclosedHTMLComments(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) (out UnclosedHTMLComments, err error) {
	if err = d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0))
		if err != nil {
			out = UnclosedHTMLComments{}
		}
	}()
	return d.unclosedHTMLCommentsLocked(ctx, budget, index)
}

func (d *Document) unclosedHTMLCommentsLocked(ctx context.Context, budget *OperationBudget, index LineIndex) (out UnclosedHTMLComments, err error) {
	budget.mu.Lock()
	defer budget.mu.Unlock()
	defer func() {
		if stopped := recover(); stopped != nil {
			if limit, ok := stopped.(budgetStop); ok {
				copy := *limit.exceeded
				out = UnclosedHTMLComments{}
				err = &copy
				return
			}
			panic(stopped)
		}
	}()
	work := &parserContext{Context: ctx, budget: &parseBudget{operation: budget}}
	c := unclosedHTMLCommentsCollector{ctx: work, source: index.source}
	err = c.collect(d.root)
	meterWork(work, 1)
	err = errors.Join(err, ctx.Err())
	if err != nil {
		return UnclosedHTMLComments{}, err
	}
	if c.unavailable {
		return UnclosedHTMLComments{Availability: Unavailable}, nil
	}
	return UnclosedHTMLComments{Comments: c.comments, Availability: Available}, nil
}

type unclosedHTMLCommentsCollector struct {
	ctx         context.Context
	source      Source
	raw         string
	comments    []UnclosedHTMLComment
	unavailable bool
}

func (c *unclosedHTMLCommentsCollector) collect(root *Node) error {
	var stack []*Node
	stack = embeddedHTMLGrow(c.ctx, stack)
	meterRecords(c.ctx, 1)
	stack = append(stack, root)
	for {
		meterWork(c.ctx, 4)
		if err := c.ctx.Err(); err != nil {
			return err
		}
		if len(stack) == 0 {
			return nil
		}
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		meterWork(c.ctx, 16)
		if n == nil || !contains(root.spec.Envelope, n.spec.Envelope) {
			return ErrModel
		}
		if n.Kind() == Image {
			continue
		}
		if n.Kind() == HTMLBlock || n.Kind() == InlineHTML {
			if err := c.scan(n); err != nil {
				return err
			}
			if c.unavailable {
				return nil
			}
			continue // Complete mapped content already contains private closing tails.
		}
		for i := len(n.spec.Children); ; {
			meterWork(c.ctx, 2)
			if i == 0 {
				break
			}
			i--
			stack = embeddedHTMLGrow(c.ctx, stack)
			meterRecords(c.ctx, 1)
			stack = append(stack, n.spec.Children[i])
		}
	}
}

func (c *unclosedHTMLCommentsCollector) scan(n *Node) error {
	meterWork(c.ctx, 16)
	if n.spec.Properties.HTML == nil {
		return ErrModel
	}
	// Reserve the owner's complete scratch envelope before any index/cache,
	// mapping or builder allocation. The existing lexer meters actual logical
	// visits too. A cache suffix is visited at most once plus one cached hit per
	// source candidate; terminator lists and visited prefixes fit this bound.
	meterWork(c.ctx, len(n.spec.Content)+1)
	logical := int64(0)
	for _, part := range n.spec.Content {
		logical = addBounded(logical, int64(len(part.Semantic.Value)))
	}
	work, records := unclosedHTMLScratchCosts(logical, int64(len(n.spec.Content)))
	meterHTMLWork(c.ctx, work)
	// meterRecords retains both document-local and operation-wide construction
	// authority. Chunk only at the host boundary, never replace that allowance.
	for records > 0 {
		chunk := min(records, int64(int(^uint(0)>>1)))
		meterRecords(c.ctx, int(chunk))
		records -= chunk
	}
	scanner := inlineScanner{ctx: c.ctx}
	// Incoming raw protection can continue after failed lexical candidates. The
	// original lexer caches both successful and failed attribute suffix states,
	// so overlapping quoted starts cannot repeatedly parse the same suffix.
	meterRecords(c.ctx, 1)
	meterWork(c.ctx, 1)
	scanner.filterTagCache = make(map[int]int)
	mapping := emphasisContent{scanner: &scanner, parts: n.spec.Content, starts: meteredSlice[int](c.ctx, len(n.spec.Content)+1)}
	var text strings.Builder
	previous := n.spec.Envelope.start
	for i := 0; ; i++ {
		meterWork(c.ctx, 24)
		if i == len(n.spec.Content) {
			break
		}
		p := n.spec.Content[i]
		if !p.Semantic.Present || !contains(n.spec.Envelope, p.Source) || p.Source.start < previous {
			return ErrModel
		}
		previous = p.Source.end
		// Validate byte association before lexing. Only original HTML mappings are
		// admitted; logical NUL and escaped GFM pipes cannot manufacture angles/names.
		raw := c.source.content[p.Source.start:p.Source.end]
		meterWork(c.ctx, len(raw)+len(p.Semantic.Value)+16)
		switch p.Transformation {
		case Exact:
			if raw != p.Semantic.Value {
				return ErrModel
			}
		case Normalized:
			if raw != "\x00" || p.Semantic.Value != "\ufffd" {
				return ErrModel
			}
		case Escape:
			if raw != "\\|" || p.Semantic.Value != "|" {
				return ErrModel
			}
		case PartialTab:
			if raw != "\t" || len(p.Semantic.Value) > 4 {
				return ErrModel
			}
			for _, b := range p.Semantic.Value {
				if b != ' ' {
					return ErrModel
				}
			}
		default:
			return ErrModel
		}
		meteredWriteString(c.ctx, &text, p.Semantic.Value)
		mapping.starts[i+1] = text.Len()
	}
	scanner.text = text.String()
	at := 0
	form := 0
	if n.Kind() == HTMLBlock && n.spec.Properties.HTML.BlockForm.Present {
		form = n.spec.Properties.HTML.BlockForm.Value
	}
	// Independently owned inline tokens cannot manufacture a transition from
	// bytes inside a comment/PI/CDATA or quoted attribute. Its private proof is
	// required when a raw lifetime enters this owner from earlier syntax.
	if n.Kind() == InlineHTML && c.raw != "" {
		meterWork(c.ctx, 2*len(c.raw)+2*len(n.rawTag)+8)
		if !n.rawClosing || !embeddedASCIIEqual(c.raw, n.rawTag) {
			return nil
		}
	}
	// Forms 2-5 use actual block delimiter semantics (including overlapping short
	// comments), even under incoming raw protection. Their closing tails are
	// scanned below; the opaque token itself supplies no local raw transition.
	if form >= 2 && form <= 5 {
		token := [6]string{2: "-->", 3: "?>", 4: ">", 5: "]]>"}[form]
		meterWork(c.ctx, len(scanner.text)+16)
		close := strings.Index(scanner.text, token)
		if close < 0 {
			if form == 2 && c.raw == "" {
				// Form 2's original private opening marker is authoritative; the
				// mapping can begin with indentation before its actual opener.
				meterWork(c.ctx, 24)
				open := n.commentOpen
				if !open.Present() || open.SourceToken() != c.source.Token() || open.end-open.start != 4 || !contains(n.spec.Envelope, open) || c.source.content[open.start:open.end] != "<!--" || len(c.comments) > 0 && open.start < c.comments[len(c.comments)-1].Open.end {
					return ErrModel
				}
				c.comments = embeddedHTMLGrow(c.ctx, c.comments)
				meterRecords(c.ctx, 1)
				c.comments = append(c.comments, UnclosedHTMLComment{Open: open})
			}
			return nil
		}
		at = close + len(token)
	}
	form1 := c.raw == "" && form == 1
	for {
		meterWork(c.ctx, 16)
		if err := c.ctx.Err(); err != nil {
			return err
		}
		if at == len(scanner.text) {
			break
		}
		if scanner.text[at] != '<' {
			at++
			continue
		}
		// A form-1 raw lifetime ends at the first of four literal closers, without
		// attribute/quote/browser interpretation. Fixed lookahead is precharged.
		if c.raw != "" && form1 {
			meterWork(c.ctx, 128)
			found := false
			for _, token := range []string{"</pre>", "</script>", "</style>", "</textarea>"} {
				if len(scanner.text)-at >= len(token) && embeddedASCIIEqual(scanner.text[at:at+len(token)], token) {
					at += len(token)
					c.raw = ""
					form1 = false
					found = true
					break
				}
			}
			if !found {
				at++
			}
			continue
		}
		meterWork(c.ctx, 8)
		nameStart := at + 1
		closing := nameStart < len(scanner.text) && scanner.text[nameStart] == '/'
		if closing {
			nameStart++
		}
		nameEnd := htmlTagNameEndContext(c.ctx, scanner.text, nameStart)
		named := nameEnd > nameStart
		end, _ := scanner.inlineHTMLEnd(at)
		if c.raw != "" {
			meterWork(c.ctx, len(c.raw)+nameEnd-nameStart+20)
			if !named || !closing || !embeddedASCIIEqual(scanner.text[nameStart:nameEnd], c.raw) {
				// Complete quoted attributes and opaque lexical tokens are atomic, so a
				// tag-looking closer inside one cannot terminate a global lifetime.
				if end > 0 {
					at = end
					continue
				}
				if strings.HasPrefix(scanner.text[at:], "<!--") || strings.HasPrefix(scanner.text[at:], "<?") || strings.HasPrefix(scanner.text[at:], "<!") {
					return nil
				}
				at = max(at+1, nameEnd)
				continue
			}
		}
		if end == 0 {
			if named && c.raw == "" {
				c.unavailable = true
				return nil
			}
			// Unclosed actual opaque tokens protect the remaining owner. Ordinary
			// malformed non-name bytes are payload, never a guessed tag suffix.
			meterWork(c.ctx, 16)
			if c.raw == "" && strings.HasPrefix(scanner.text[at:], "<!--") {
				return c.add(mapping, at)
			}
			if c.raw == "" && (strings.HasPrefix(scanner.text[at:], "<?") || strings.HasPrefix(scanner.text[at:], "<!")) {
				return nil
			}
			at = max(at+1, nameEnd)
			continue
		}
		if !named {
			at = end
			continue
		}
		if c.raw != "" {
			c.raw = ""
		} else if !closing {
			meterWork(c.ctx, 128)
			for _, raw := range []string{"pre", "script", "style", "textarea"} {
				if embeddedASCIIEqual(scanner.text[nameStart:nameEnd], raw) {
					c.raw = raw
					break
				}
			}
		}
		at = end
	}
	return nil
}

// add maps only the complete contiguous original opener. It never returns a
// logical-source envelope that includes omitted container prefixes.
func (c *unclosedHTMLCommentsCollector) add(mapping emphasisContent, start int) error {
	meterWork(c.ctx, 32)
	a, err := htmlIDPoint(mapping, start)
	if err != nil {
		return err
	}
	b, err := htmlIDPoint(mapping, start+4)
	if err != nil {
		return err
	}
	open := Span{source: c.source, start: a.start, end: b.start, present: true}
	meterWork(c.ctx, 24)
	if open.end-open.start != 4 || c.source.content[open.start:open.end] != "<!--" || len(c.comments) > 0 && open.start < c.comments[len(c.comments)-1].Open.end {
		return ErrModel
	}
	c.comments = embeddedHTMLGrow(c.ctx, c.comments)
	meterRecords(c.ctx, 1)
	c.comments = append(c.comments, UnclosedHTMLComment{Open: open})
	return nil
}

// Keep lengths wide before multiplication. Saturation exceeds every validated
// allowance and therefore fails closed through the original operation controls.
func unclosedHTMLScratchCosts(logical, parts int64) (work, records int64) {
	if logical < 0 || parts < 0 {
		return math.MaxInt64, math.MaxInt64
	}
	scale := func(n, factor int64) int64 {
		if n > math.MaxInt64/factor {
			return math.MaxInt64
		}
		return n * factor
	}
	return addBounded(scale(logical, 8), addBounded(parts, 8)), addBounded(scale(logical, 4), addBounded(parts, 3))
}
