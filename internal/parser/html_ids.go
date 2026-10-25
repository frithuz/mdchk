package parser

import (
	"context"
	"html"
	"sort"
	"strconv"
	"strings"
)

// HTMLIDOccurrence is a complete start tag's authored id attribute. Attribute
// includes its spelling and optional quotes; Value excludes quotes and retains
// mapped original segments independently of its once-decoded HTML semantics.
// Later duplicate id/ID attributes remain occurrences but are not Effective.
type HTMLIDOccurrence struct {
	Tag, Attribute Span
	Value          Value
	Effective      bool
}

// HTMLIDCollection is a source-owned derived inventory. Only Available grants
// complete inventory authority. Unavailable inventories contain no occurrences;
// Uncertainty locates supported syntax whose HTML context cannot be established.
// Every call returns private slice storage, without changing the Document.
type HTMLIDCollection struct {
	Source       Source
	Availability Availability
	Occurrences  []HTMLIDOccurrence
	Uncertainty  []Span
}

// HTMLIDs derives bounded HTML ID facts from an actual completed parse in this
// same operation. It reuses the parser's complete-tag lexer and source mappings;
// constructed Documents cannot manufacture recognized attributes. HTML attribute
// entities differ from Markdown text entities and never decode backslash escapes.
// A stop withholds the entire inventory, retaining its typed error.
func (d *Document) HTMLIDs(ctx context.Context, budget *OperationBudget) (out HTMLIDCollection, err error) {
	if ctx == nil || budget == nil || d == nil || !d.parsedComplete || budget.identity == nil || d.commentOperation != budget.identity {
		return HTMLIDCollection{Availability: Unavailable}, ErrModel
	}
	out = HTMLIDCollection{Source: d.Source(), Availability: Unavailable}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	defer func() {
		if stopped := recover(); stopped != nil {
			if limit, ok := stopped.(budgetStop); ok {
				copy := *limit.exceeded
				out = HTMLIDCollection{Source: d.Source(), Availability: Unavailable}
				err = &copy
				return
			}
			panic(stopped)
		}
	}()
	work := &parserContext{Context: ctx, budget: &parseBudget{operation: budget}}
	meterRecords(work, 1)
	c := htmlIDCollector{ctx: work, source: d.Source()}
	if err = c.collect(d.root); err != nil {
		return out, err
	}
	if err = c.check(); err != nil {
		return out, err
	}
	if len(c.uncertainty) != 0 {
		out.Uncertainty = c.uncertainty
		return out, nil
	}
	out.Availability, out.Occurrences = Available, c.occurrences
	return out, nil
}

type htmlIDCollector struct {
	ctx         context.Context
	source      Source
	raw         string
	occurrences []HTMLIDOccurrence
	uncertainty []Span
}

// htmlIDRawText is deliberately separate from Markdown's form-1 HTML block
// predicate: pre is opaque Markdown, but its real HTML child elements have IDs.
func htmlIDRawText(name string) bool {
	switch name {
	case "script", "style", "textarea", "title", "xmp", "iframe", "noembed", "noframes", "plaintext":
		return true
	default:
		return false
	}
}

func (c *htmlIDCollector) check() error {
	meterWork(c.ctx, 0)
	return c.ctx.Err()
}

func (c *htmlIDCollector) uncertain(span Span) {
	c.uncertainty = meteredAppend(c.ctx, c.uncertainty, span)
}

func (c *htmlIDCollector) collect(root *Node) error {
	type frame struct {
		node *Node
		exit bool
	}
	stack := meteredAppend(c.ctx, []frame(nil), frame{node: root})
	for len(stack) != 0 {
		if err := c.check(); err != nil {
			return err
		}
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := f.node
		meterWork(c.ctx, 1)
		if f.exit {
			if c.raw != "" && n.Span().End() < c.source.Len() {
				// Browser raw-text state spanning independently parsed Markdown
				// blocks cannot grant a complete static inventory.
				c.uncertain(n.Span())
				return nil
			}
			continue
		}
		if n.Kind() == Image {
			continue // its description becomes alt text, not child HTML elements
		}
		if n.Kind() == HTMLBlock || n.Kind() == InlineHTML {
			if err := c.scan(n); err != nil {
				return err
			}
			if len(c.uncertainty) != 0 {
				return nil
			}
			if n.Kind() == HTMLBlock && c.raw != "" && n.Span().End() < c.source.Len() {
				c.uncertain(n.Span())
				return nil
			}
			continue // the mapped owner already includes its physical-line tails
		}
		if n.Kind() == Paragraph || n.Kind() == Heading || n.Kind() == TableCell {
			stack = meteredAppend(c.ctx, stack, frame{node: n, exit: true})
		}
		meterWork(c.ctx, len(n.spec.Children))
		for i := len(n.spec.Children) - 1; i >= 0; i-- {
			stack = meteredAppend(c.ctx, stack, frame{node: n.spec.Children[i]})
		}
	}
	return nil
}

func (c *htmlIDCollector) scan(n *Node) error {
	scanner := inlineScanner{ctx: c.ctx}
	mapping := emphasisContent{scanner: &scanner, parts: n.spec.Content, starts: meteredSlice[int](c.ctx, len(n.spec.Content)+1)}
	var text strings.Builder
	for i, part := range n.spec.Content {
		if !part.Semantic.Present {
			c.uncertain(n.Span())
			return nil
		}
		meteredWriteString(c.ctx, &text, part.Semantic.Value)
		mapping.starts[i+1] = text.Len()
	}
	scanner.text = text.String()
	var filteredStarts map[int]bool
	if p := n.spec.Properties.HTML; p != nil && len(p.FilteredTags) != 0 {
		meterRecords(c.ctx, 1+len(p.FilteredTags))
		filteredStarts = make(map[int]bool, len(p.FilteredTags))
		for _, fact := range p.FilteredTags {
			meterWork(c.ctx, 1)
			filteredStarts[fact.Affected.Start()] = true
		}
	}
	for at := 0; at < len(scanner.text); {
		meterWork(c.ctx, 1)
		if at%256 == 0 {
			if err := c.check(); err != nil {
				return err
			}
		}
		if scanner.text[at] != '<' {
			at++
			continue
		}
		if c.raw != "" {
			if c.raw == "plaintext" {
				return nil
			}
			if c.raw == "script" && meteredHasPrefix(c.ctx, scanner.text[at:], "<!--") {
				// Script escape/double-escape recovery is outside this narrow
				// static lexer, rather than authority to invent absent IDs.
				c.uncertain(n.Span())
				return nil
			}
			name, closing, _ := htmlIDTagName(c.ctx, scanner.text, at)
			if !closing || name != c.raw {
				at++
				continue
			}
			end, _ := scanner.inlineHTMLEnd(at)
			if end == 0 {
				c.uncertain(n.Span())
				return nil
			}
			c.raw, at = "", end
			continue
		}
		end, _ := scanner.inlineHTMLEnd(at)
		if end == 0 {
			if strings.HasPrefix(scanner.text[at:], "<!--") || strings.HasPrefix(scanner.text[at:], "<?") || strings.HasPrefix(scanner.text[at:], "<!") {
				// A parser-confirmed unclosed opaque block lasts to EOF. Any
				// other boundary would require unsupported HTML recovery.
				if n.Kind() != HTMLBlock || n.Span().End() != c.source.Len() {
					c.uncertain(n.Span())
				}
				return nil
			}
			name, _, _ := htmlIDTagName(c.ctx, scanner.text, at)
			if name != "" {
				c.uncertain(n.Span())
				return nil
			}
			at++
			continue
		}
		name, closing, nameEnd := htmlIDTagName(c.ctx, scanner.text, at)
		if name == "" || closing {
			at = end
			continue
		}
		point, err := htmlIDPoint(mapping, at)
		if err != nil {
			return err
		}
		filtered := filteredStarts[point.Start()]
		if !filtered && (name == "svg" || name == "math") {
			// Foreign content and its HTML integration points do not share
			// ordinary HTML raw-text ownership (notably SVG title). This
			// projection has no namespace/tree-construction proof, so entry
			// withholds the whole inventory before attributes or children can
			// grant complete presence/absence or suffix-reservation authority.
			parts, err := mapping.slice(at, end)
			if err != nil {
				return err
			}
			c.uncertain(contentEnvelope(parts))
			return nil
		}
		if !filtered {
			if err := c.attributes(mapping, at, end, nameEnd); err != nil {
				return err
			}
		}
		if htmlIDRawText(name) {
			if filtered {
				// Escaped raw openings no longer establish ordinary raw-text
				// ownership. Do not guess how later payload tags render.
				c.uncertain(n.Span())
				return nil
			}
			c.raw = name
		}
		at = end
	}
	return nil
}

func htmlIDTagName(ctx context.Context, text string, at int) (name string, closing bool, end int) {
	start := at + 1
	if start < len(text) && text[start] == '/' {
		closing = true
		start++
	}
	end = start
	if start >= len(text) || !asciiLetter(text[start]) {
		return "", closing, end
	}
	for end < len(text) && (asciiAlphanumeric(text[end]) || text[end] == '-') {
		meterWork(ctx, 1)
		if end%256 == 0 && interruption(ctx) != "" {
			return "", closing, end
		}
		end++
	}
	meterWork(ctx, end-start)
	return strings.ToLower(text[start:end]), closing, end
}

func htmlIDPoint(mapping emphasisContent, at int) (Span, error) {
	i := sort.Search(len(mapping.parts), func(i int) bool {
		meterWork(mapping.scanner.ctx, 1)
		return mapping.starts[i+1] > at
	})
	if i == len(mapping.parts) && i != 0 && at == mapping.starts[i] {
		i--
	}
	if i < len(mapping.parts) && at >= mapping.starts[i] && at <= mapping.starts[i+1] {
		part := mapping.parts[i]
		span := part.Source
		offset := at - mapping.starts[i]
		if part.Transformation == Exact {
			span.start += offset
		} else if offset == len(part.Semantic.Value) {
			span.start = span.end
		} else if offset != 0 {
			return Span{}, ErrModel
		}
		span.end = span.start
		return span, nil
	}
	return Span{}, ErrModel
}

func (c *htmlIDCollector) attributes(mapping emphasisContent, start, end, nameEnd int) error {
	text := mapping.scanner.text
	tagParts, err := mapping.slice(start, end)
	if err != nil {
		return err
	}
	tag := contentEnvelope(tagParts)
	seen := false
	for at := nameEnd; at < end; {
		meterWork(c.ctx, 1)
		at, _ = mapping.scanner.htmlSpace(at)
		if at >= end || text[at] == '>' || text[at] == '/' {
			break
		}
		attrStart := at
		for at < end && (htmlAttributeStart(text[at]) || asciiDigit(text[at]) || text[at] == '.' || text[at] == '-') {
			meterWork(c.ctx, 1)
			at++
		}
		attrNameEnd := at
		at, _ = mapping.scanner.htmlSpace(at)
		valueStart, valueEnd := attrNameEnd, attrNameEnd
		if at < end && text[at] == '=' {
			at, _ = mapping.scanner.htmlSpace(at + 1)
			if text[at] == '\'' || text[at] == '"' {
				quote := text[at : at+1]
				valueStart = at + 1
				valueEnd = mapping.scanner.htmlEnd(quote, valueStart)
				at = valueEnd + 1
			} else {
				valueStart = at
				for at < end && !htmlIDSpace(text[at]) && text[at] != '>' {
					meterWork(c.ctx, 1)
					at++
				}
				valueEnd = at
			}
		} else {
			at = attrNameEnd
		}
		if attrNameEnd-attrStart != 2 || !strings.EqualFold(text[attrStart:attrNameEnd], "id") {
			continue
		}
		value, err := htmlIDValue(mapping, valueStart, valueEnd)
		if err != nil {
			return err
		}
		attrParts, err := mapping.slice(attrStart, at)
		if err != nil {
			return err
		}
		meterRecords(c.ctx, 1)
		c.occurrences = meteredAppend(c.ctx, c.occurrences, HTMLIDOccurrence{Tag: tag, Attribute: contentEnvelope(attrParts), Value: value, Effective: !seen})
		seen = true
	}
	return nil
}

func htmlIDSpace(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n'
}

func htmlIDValue(mapping emphasisContent, start, end int) (Value, error) {
	ctx, text := mapping.scanner.ctx, mapping.scanner.text
	var out Value
	var semantic strings.Builder
	appendRange := func(a, b int, decoded string, kind Transformation) error {
		parts, err := mapping.slice(a, b)
		if err != nil {
			return err
		}
		if kind != Exact {
			for i := range parts {
				parts[i].Transformation = kind
				parts[i].Semantic = Some("")
			}
			parts[0].Semantic = Some(decoded)
		}
		out.Segments = meteredAppend(ctx, out.Segments, parts...)
		meteredWriteString(ctx, &semantic, decoded)
		return nil
	}
	run := start
	for at := start; at < end; {
		meterWork(ctx, 1)
		if at%256 == 0 && interruption(ctx) != "" {
			return Value{}, ctx.Err()
		}
		decoded, width, kind := "", 0, Exact
		if text[at] == '&' {
			decoded, width = htmlIDEntity(ctx, text[at:end])
			kind = Entity
		} else if text[at] == '\r' {
			decoded, width, kind = "\n", 1, Normalized
			if at+1 < end && text[at+1] == '\n' {
				width++
			}
		} else if text[at] == 0 {
			decoded, width, kind = "\ufffd", 1, Normalized
		}
		if width == 0 {
			at++
			continue
		}
		if run < at {
			if err := appendRange(run, at, text[run:at], Exact); err != nil {
				return Value{}, err
			}
		}
		if err := appendRange(at, at+width, decoded, kind); err != nil {
			return Value{}, err
		}
		at += width
		run = at
	}
	if run < end {
		if err := appendRange(run, end, text[run:end], Exact); err != nil {
			return Value{}, err
		}
	}
	if start == end {
		point, err := htmlIDPoint(mapping, start)
		if err != nil {
			return Value{}, err
		}
		out.Segments = meteredAppend(ctx, out.Segments, Segment{Source: point, Transformation: Exact, Semantic: Some("")})
	}
	out.Semantic = Some(semantic.String())
	return out, nil
}

func htmlIDEntity(ctx context.Context, text string) (string, int) {
	if len(text) < 2 {
		return "", 0
	}
	if text[1] == '#' {
		at, base := 2, 10
		if at < len(text) && (text[at] == 'x' || text[at] == 'X') {
			at, base = at+1, 16
		}
		first, value := at, 0
		for at < len(text) {
			meterWork(ctx, 1)
			if at%256 == 0 && interruption(ctx) != "" {
				return "", 0
			}
			digit := -1
			switch ch := text[at]; {
			case ch >= '0' && ch <= '9':
				digit = int(ch - '0')
			case base == 16 && ch >= 'a' && ch <= 'f':
				digit = int(ch-'a') + 10
			case base == 16 && ch >= 'A' && ch <= 'F':
				digit = int(ch-'A') + 10
			}
			if digit < 0 {
				break
			}
			if value <= 0x10ffff {
				value = min(value*base+digit, 0x110000)
			}
			at++
		}
		if at == first {
			return "", 0
		}
		if at < len(text) && text[at] == ';' {
			at++
		}
		// Canonical bounded numeric spelling avoids overflow in a library
		// decoder on arbitrarily long authored digit runs. HTML's C1 and
		// invalid-scalar replacements remain the standard-library policy.
		meterWork(ctx, 40)
		return html.UnescapeString("&#" + strconv.Itoa(value) + ";"), at
	}
	best, decoded := 0, ""
	for at := 1; at < len(text) && at < maxEntityToken; at++ {
		meterWork(ctx, at+1)
		if text[at] == ';' {
			if value, ok := namedEntities[text[:at+1]]; ok {
				best, decoded = at+1, value
			}
			break
		}
		if !asciiAlphanumeric(text[at]) {
			break
		}
		if htmlIDLegacyEntities[text[:at+1]] {
			best, decoded = at+1, namedEntities[text[:at+1]+";"]
		}
	}
	if best != 0 && text[best-1] != ';' && best < len(text) && (asciiAlphanumeric(text[best]) || text[best] == '=') {
		return "", 0
	}
	return decoded, best
}
