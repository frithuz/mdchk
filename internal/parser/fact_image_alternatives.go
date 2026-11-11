package parser

import (
	"context"
	"errors"
	"slices"
	"strings"
	"unicode"
)

// ImageAlternativesInventory contains detached report-only original ranges.
// Uncertainty affects only the located syntax; Spans retains independently
// established findings. An operation failure withholds the entire inventory.
type ImageAlternativesInventory struct {
	Spans, Uncertainty []Span
	Availability       Availability
}

// ImageAlternatives derives Markdown alternative text by the #28 child rules:
// semantic Text/CodeSpan, newline breaks, recursive formatting/link/image
// descriptions, and omitted HTML. Each child is visited once, without building
// repeated ancestor strings. HTML img attributes use the original HTML lexer.
func (d *Document) ImageAlternatives(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions, allowDecorative bool) (out ImageAlternativesInventory, err error) {
	if err = d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0))
		if err != nil {
			out = ImageAlternativesInventory{}
		}
	}()
	return d.imageAlternativesLocked(ctx, budget, index, allowDecorative)
}

func (d *Document) imageAlternativesLocked(ctx context.Context, budget *OperationBudget, index LineIndex, decorative bool) (out ImageAlternativesInventory, err error) {
	budget.mu.Lock()
	defer budget.mu.Unlock()
	defer func() {
		if stopped := recover(); stopped != nil {
			if limit, ok := stopped.(budgetStop); ok {
				copy := *limit.exceeded
				out, err = ImageAlternativesInventory{}, &copy
				return
			}
			panic(stopped)
		}
	}()
	work := &parserContext{Context: ctx, budget: &parseBudget{operation: budget}}
	c := imageAlternativesCollector{ctx: work, source: index.source, decorative: decorative}
	c.html = embeddedHTMLCollector{ctx: work, source: index.source, tag: c.tag}
	if err = c.collect(d.root); err != nil {
		return out, err
	}
	meterWork(work, 1)
	if err = ctx.Err(); err != nil {
		return out, err
	}
	slices.SortFunc(c.spans, func(a, b Span) int {
		meterWork(work, 2)
		if a.start != b.start {
			return a.start - b.start
		}
		return a.end - b.end
	})
	out = ImageAlternativesInventory{Spans: c.spans, Uncertainty: c.uncertainty, Availability: Available}
	if len(c.uncertainty) != 0 {
		out.Availability = Unavailable
	}
	return out, nil
}

type imageAlternativesCollector struct {
	ctx                context.Context
	source             Source
	decorative         bool
	html               embeddedHTMLCollector
	spans, uncertainty []Span
}

func (c *imageAlternativesCollector) blank(text string) bool {
	blank := true
	for _, r := range text {
		meterWork(c.ctx, 1)
		if !unicode.IsSpace(r) {
			blank = false
		}
	}
	return blank
}

func (c *imageAlternativesCollector) collect(root *Node) error {
	type frame struct {
		node          *Node
		next          int
		alt, nonblank bool
	}
	stack := embeddedHTMLGrow(c.ctx, []frame(nil))
	meterRecords(c.ctx, 1)
	stack = append(stack, frame{node: root})
	for len(stack) != 0 {
		meterWork(c.ctx, 16)
		if err := c.ctx.Err(); err != nil {
			return err
		}
		f := &stack[len(stack)-1]
		n := f.node
		if n == nil || !contains(root.spec.Envelope, n.spec.Envelope) {
			return ErrModel
		}
		if f.next == 0 {
			switch n.Kind() {
			case Image:
				if !f.alt && c.html.raw != "" {
					stack = stack[:len(stack)-1]
					continue
				}
				f.alt = true
			case HTMLBlock, InlineHTML:
				if !f.alt {
					if err := c.html.scan(n); err != nil {
						return err
					}
					if c.html.unavailable {
						span := c.html.uncertainty
						if !span.present {
							span = n.spec.Envelope
						}
						// Failed named-token completion has no safe recovery boundary.
						// Retain earlier positives; do not guess that later Markdown
						// escaped a possible quoted HTML attribute or raw opener.
						span.end = c.source.Len()
						c.uncertainty = imageAltAppend(c.ctx, c.uncertainty, span)
						return nil
					}
				}
				stack = stack[:len(stack)-1]
				continue
			case CodeBlock, ThematicBreak:
				stack = stack[:len(stack)-1]
				continue
			case Text, CodeSpan:
				if f.alt {
					for _, p := range n.spec.Content {
						meterWork(c.ctx, 4)
						if !p.Semantic.Present || !contains(n.spec.Envelope, p.Source) {
							return ErrModel
						}
						if !c.blank(p.Semantic.Value) {
							f.nonblank = true
						}
					}
				}
			case DocumentKind, BlockQuote, List, ListItem, Table, TableRow, TableCell, Paragraph, Heading, Emphasis, Strong, Strike, Link, SoftBreak, HardBreak:
			default:
				return ErrModel
			}
		}
		if f.next < len(n.spec.Children) {
			child := n.spec.Children[f.next]
			f.next++
			alt := f.alt
			stack = embeddedHTMLGrow(c.ctx, stack)
			meterRecords(c.ctx, 1)
			stack = append(stack, frame{node: child, alt: alt})
			continue
		}
		if n.Kind() == Image && !f.nonblank {
			p := n.spec.Properties.Link
			if p == nil || p.Resolution != Available || !p.Label.Semantic.Present || len(p.Label.Segments) == 0 {
				return ErrModel
			}
			var open, close Span
			for _, marker := range n.spec.Markers {
				meterWork(c.ctx, 3)
				switch marker.Role {
				case "label-open":
					open = marker.Source
				case "label-close":
					close = marker.Source
				}
			}
			if !open.present || !close.present || !contains(n.spec.Envelope, open) || !contains(n.spec.Envelope, close) || open.end > close.start {
				return ErrModel
			}
			span := Span{source: c.source, start: open.end, end: close.start, present: true}
			if !contains(n.spec.Envelope, span) {
				return ErrModel
			}
			c.spans = imageAltAppend(c.ctx, c.spans, span)
		}
		nonblank := f.nonblank
		stack = stack[:len(stack)-1]
		if nonblank && len(stack) > 0 {
			stack[len(stack)-1].nonblank = true
		}
	}
	return nil
}

func (c *imageAlternativesCollector) tag(mapping emphasisContent, start, end, nameStart, nameEnd int) error {
	text := mapping.scanner.text
	meterWork(c.ctx, nameEnd-nameStart+4)
	if text[start+1] == '/' || !embeddedASCIIEqual(text[nameStart:nameEnd], "img") {
		return nil
	}
	type attribute struct {
		name, value string
		span        Span
	}
	var attrs []attribute
	for at := nameEnd; at < end; {
		meterWork(c.ctx, 8)
		at, _ = mapping.scanner.htmlSpace(at)
		if at >= end || text[at] == '>' || text[at] == '/' {
			break
		}
		a := at
		for at < end && (htmlAttributeStart(text[at]) || asciiDigit(text[at]) || text[at] == '.' || text[at] == '-') {
			meterWork(c.ctx, 1)
			at++
		}
		b := at
		if a == b {
			return ErrModel
		}
		meterWork(c.ctx, b-a)
		name := strings.ToLower(text[a:b])
		at, _ = mapping.scanner.htmlSpace(at)
		vstart, vend := b, b
		if at < end && text[at] == '=' {
			at, _ = mapping.scanner.htmlSpace(at + 1)
			if at >= end {
				return ErrModel
			}
			if text[at] == '\'' || text[at] == '"' {
				vstart = at + 1
				vend = mapping.scanner.htmlEnd(text[at:at+1], vstart)
				at = vend + 1
			} else {
				vstart = at
				for at < end && !htmlIDSpace(text[at]) && text[at] != '>' {
					meterWork(c.ctx, 1)
					at++
				}
				vend = at
			}
		} else {
			at = b
		}
		if name != "alt" && name != "title" && name != "role" && name != "tabindex" && name != "usemap" && name != "ismap" && !strings.HasPrefix(name, "aria-") {
			continue
		}
		v, err := htmlIDValue(mapping, vstart, vend)
		if err != nil {
			return err
		}
		attrs = imageAltAppend(c.ctx, attrs, attribute{name: name, value: v.Semantic.Value, span: contentEnvelope(v.Segments)})
	}
	var alt *attribute
	for i := range attrs {
		meterWork(c.ctx, 1)
		if attrs[i].name == "alt" {
			if alt != nil {
				return c.uncertainTag(mapping, start, end)
			}
			alt = &attrs[i]
		}
	}
	if alt != nil && !c.blank(alt.value) {
		return nil
	}
	if alt == nil {
		point, err := htmlIDPoint(mapping, nameEnd)
		if err != nil {
			return err
		}
		c.spans = imageAltAppend(c.ctx, c.spans, point)
		return nil
	}
	if c.decorative && alt.value == "" {
		meterRecords(c.ctx, len(attrs)+1)
		counts := make(map[string]int, len(attrs))
		for _, a := range attrs {
			meterWork(c.ctx, len(a.name)+2)
			counts[a.name]++
		}
		conflict, ambiguous := false, false
		for _, a := range attrs {
			meterWork(c.ctx, len(a.name)+len(a.value)+8)
			switch a.name {
			case "alt":
			case "title", "role", "aria-hidden":
				if counts[a.name] > 1 {
					ambiguous = true
					continue
				}
				switch a.name {
				case "title":
					conflict = conflict || !c.blank(a.value)
				case "role":
					value := strings.Trim(a.value, " \t\n\r\f")
					conflict = conflict || (value != "" && !embeddedASCIIEqual(value, "none") && !embeddedASCIIEqual(value, "presentation"))
				case "aria-hidden":
					conflict = conflict || !embeddedASCIIEqual(a.value, "true")
				}
			default:
				// Presence alone disqualifies these attributes, even if duplicated.
				conflict = true
			}
		}
		if !conflict && ambiguous {
			return c.uncertainTag(mapping, start, end)
		}
		if !conflict {
			return nil
		}
	}
	c.spans = imageAltAppend(c.ctx, c.spans, alt.span)
	return nil
}

func (c *imageAlternativesCollector) uncertainTag(mapping emphasisContent, start, end int) error {
	parts, err := mapping.slice(start, end)
	if err != nil {
		return err
	}
	c.uncertainty = imageAltAppend(c.ctx, c.uncertainty, contentEnvelope(parts))
	return nil
}

// Reserve actual capacity and logical retained records before allocation.
func imageAltAppend[T any](ctx context.Context, items []T, item T) []T {
	items = embeddedHTMLGrow(ctx, items)
	meterRecords(ctx, 1)
	return append(items, item)
}
