package parser

import (
	"context"
	"errors"
)

// ShellPromptInventory owns detached original dollar spans. Unavailable context
// withholds the whole inventory; a discarded prefix proves neither use nor absence.
type ShellPromptInventory struct {
	Dollars      []Span
	Availability Availability
}

// ShellPrompts borrows the completed original production tree, source, index,
// options and allowance. Languages is a synchronous read-only borrow: it is never
// retained or mutated. Only actual fenced CodeBlocks can supply payload lines.
func (d *Document) ShellPrompts(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions, languages []string) (out ShellPromptInventory, err error) {
	if err = d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0))
		if err != nil {
			out = ShellPromptInventory{}
		} else if out.Availability != Available {
			out.Dollars = nil
		}
	}()
	charge := func(work, nodes int64) error { return headingFactCharge(ctx, budget, work, nodes) }
	if err = charge(1, 0); err != nil {
		return out, err
	}
	if len(languages) == 0 {
		return ShellPromptInventory{Availability: Available}, nil
	}
	// Select first. Unknown ownership irrelevant to every language target must
	// not manufacture coverage, and ownership after the last target is irrelevant.
	type target struct {
		node  *Node
		known bool
	}
	var selected []target
	walk := shellPromptWalk{ctx: ctx, budget: budget, source: index.source, root: d.root}
	if err = walk.push(d.root); err != nil {
		return out, err
	}
	for {
		n, e := walk.next()
		if e != nil {
			return out, e
		}
		if n == nil {
			break
		}
		if err = charge(1, 0); err != nil {
			return out, err
		}
		if n.spec.Kind != CodeBlock {
			continue
		}
		match, known, e := shellPromptLanguage(ctx, budget, n, languages)
		if e != nil {
			return out, e
		}
		if match || !known {
			selected, err = shellPromptGrow(ctx, budget, selected)
			if err != nil {
				return out, err
			}
			if err = charge(1, 1); err != nil {
				return out, err
			}
			selected = append(selected, target{node: n, known: known})
		}
	}
	if err = charge(1, 0); err != nil {
		return out, err
	}
	if len(selected) == 0 {
		return ShellPromptInventory{Availability: Available}, nil
	}
	// Reuse the traversal capacity, not a new parse/index or allowance. One HTML
	// wrapper shares the original operation across every owned HTML transition.
	if err = walk.push(d.root); err != nil {
		return out, err
	}
	raw := conflictRawState{}
	var htmlWork *parserContext
	next := 0
	for {
		if err = charge(1, 0); err != nil {
			return out, err
		}
		if next == len(selected) {
			break
		}
		n, e := walk.next()
		if e != nil {
			return out, e
		}
		if n == nil {
			return out, ErrModel
		}
		if err = charge(2, 0); err != nil {
			return out, err
		}
		if n.spec.Kind == Image {
			continue // Whole Images cannot open or close an outer raw lifetime.
		}
		if n.spec.Kind == HTMLBlock {
			if htmlWork == nil {
				if err = charge(2, 2); err != nil {
					return out, err
				}
				htmlWork = &parserContext{Context: ctx, budget: &parseBudget{operation: budget}}
			}
			var available bool
			raw, _, available, err = conflictHTMLRaw(htmlWork, index.source, n, raw)
			if err != nil {
				return out, err
			}
			if !available {
				return ShellPromptInventory{Availability: Unavailable}, nil
			}
			continue // The owned scan already includes its original closing tails.
		}
		if err = charge(int64(len(raw.name))+int64(len(n.rawTag))+1, 0); err != nil {
			return out, err
		}
		if n.rawTag != "" {
			if raw.name == "" && !n.rawClosing {
				raw = conflictRawState{name: n.rawTag, start: n.spec.Envelope.start}
			} else if raw.name == n.rawTag && n.rawClosing {
				raw = conflictRawState{}
			}
		}
		if err = charge(1, 0); err != nil {
			return out, err
		}
		if n != selected[next].node {
			continue
		}
		known := selected[next].known
		next++
		if raw.name != "" {
			continue
		}
		if !known {
			return ShellPromptInventory{Availability: Unavailable}, nil
		}
		out.Dollars, known, err = shellPromptPayload(ctx, budget, index, n, out.Dollars)
		if err != nil {
			return out, err
		}
		if !known {
			return ShellPromptInventory{Availability: Unavailable}, nil
		}
	}
	out.Availability = Available
	return out, nil
}

func shellPromptLanguage(ctx context.Context, budget *OperationBudget, n *Node, languages []string) (match, known bool, err error) {
	charge := func(work int64) error { return headingFactCharge(ctx, budget, work, 0) }
	if err = charge(16); err != nil {
		return false, false, err
	}
	code := n.spec.Properties.Code
	if code == nil {
		return false, false, nil
	}
	if err = charge(int64(len(code.Style)) + 2); err != nil {
		return false, false, err
	}
	if code.Style == "indented" {
		return false, true, nil
	}
	if code.Style != "fenced" {
		return false, false, ErrModel
	}
	if code.Info == nil || !code.Info.Semantic.Present {
		return false, false, nil
	}
	text := code.Info.Semantic.Value
	start := 0
	for {
		if err = charge(2); err != nil {
			return false, false, err
		}
		if start == len(text) || text[start] != ' ' && text[start] != '\t' {
			break
		}
		start++
	}
	if start == len(text) {
		return false, true, nil
	}
	end := start
	for {
		if err = charge(2); err != nil {
			return false, false, err
		}
		if end == len(text) || text[end] == ' ' || text[end] == '\t' {
			break
		}
		end++
	}
	for i := 0; ; i++ {
		if err = charge(2); err != nil {
			return false, false, err
		}
		if i == len(languages) {
			break
		}
		if len(languages[i]) != end-start {
			continue
		}
		if err = charge(int64(end-start) + 1); err != nil {
			return false, false, err
		}
		if text[start:end] == languages[i] {
			return true, true, nil
		}
	}
	return false, true, nil
}

// shellPromptWalk borrows actual nodes. Opaque owners have no Markdown children
// for this rule; HTMLBlock Content/tails are consumed by the existing owned scan.
type shellPromptWalk struct {
	ctx    context.Context
	budget *OperationBudget
	source Source
	root   *Node
	stack  []*Node
}

func (w *shellPromptWalk) push(n *Node) error {
	var err error
	w.stack, err = shellPromptGrow(w.ctx, w.budget, w.stack)
	if err != nil {
		return err
	}
	if err = headingFactCharge(w.ctx, w.budget, 1, 1); err != nil {
		return err
	}
	w.stack = append(w.stack, n)
	return nil
}

func (w *shellPromptWalk) next() (*Node, error) {
	charge := func(work int64) error { return headingFactCharge(w.ctx, w.budget, work, 0) }
	if err := charge(1); err != nil {
		return nil, err
	}
	if len(w.stack) == 0 {
		return nil, nil
	}
	n := w.stack[len(w.stack)-1]
	w.stack = w.stack[:len(w.stack)-1]
	if err := charge(512); err != nil {
		return nil, err
	}
	if n == nil || w.root == nil || !validKind(n.spec.Kind) || !n.spec.Envelope.present || n.spec.Envelope.SourceToken() != w.source.Token() || n.spec.Envelope.start < w.root.spec.Envelope.start || n.spec.Envelope.end > w.root.spec.Envelope.end || n.spec.Envelope.start < 0 || n.spec.Envelope.end < n.spec.Envelope.start || n.spec.Envelope.end > w.source.Len() {
		return nil, ErrModel
	}
	switch n.spec.Kind {
	case CodeBlock, CodeSpan, HTMLBlock, Image:
		return n, nil
	default:
		// Other original owners retain both ordinary children and HTML tails.
	}
	for group := 0; ; group++ {
		if err := charge(1); err != nil {
			return nil, err
		}
		if group == 2 {
			break
		}
		children := n.htmlTail
		if group == 1 {
			children = n.spec.Children
		}
		for i := len(children); ; {
			if err := charge(1); err != nil {
				return nil, err
			}
			if i == 0 {
				break
			}
			i--
			if err := w.push(children[i]); err != nil {
				return nil, err
			}
		}
	}
	return n, nil
}

// shellPromptPayload classifies full semantic payload lines without joining them
// or reconstructing parser prefixes. Every contribution is checked against the
// existing physical index; only an Exact original dollar can become a finding.
func shellPromptPayload(ctx context.Context, budget *OperationBudget, index LineIndex, n *Node, dollars []Span) ([]Span, bool, error) {
	const (
		lineEmpty byte = iota
		lineBlank
		lineDollar
		lineSeparator
		lineCommand
		lineOutput
	)
	charge := func(work, nodes int64) error { return headingFactCharge(ctx, budget, work, nodes) }
	lo, hi := 0, len(index.lines)
	for {
		if err := charge(1, 0); err != nil {
			return nil, false, err
		}
		if lo == hi {
			break
		}
		mid := lo + (hi-lo)/2
		if index.lines[mid].start <= n.spec.Envelope.start {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo == 0 {
		return nil, false, ErrModel
	}
	if err := charge(16, 0); err != nil {
		return nil, false, err
	}
	code := n.spec.Properties.Code
	if code == nil || !code.Fence.Present || code.Fence.Value != '`' && code.Fence.Value != '~' || !code.Width.Present || code.Width.Value < 3 {
		return nil, false, ErrModel
	}
	var opening, closing Span
	for i := 0; ; i++ {
		if err := charge(1, 0); err != nil {
			return nil, false, err
		}
		if i == len(n.spec.Markers) {
			break
		}
		m := n.spec.Markers[i]
		if err := charge(3*int64(len(m.Role))+16, 0); err != nil {
			return nil, false, err
		}
		if !m.Source.present || m.Source.SourceToken() != index.source.Token() || m.Source.start < n.spec.Envelope.start || m.Source.end < m.Source.start || m.Source.end > n.spec.Envelope.end {
			return nil, false, ErrModel
		}
		switch m.Role {
		case "opening":
			if opening.present {
				return nil, false, ErrModel
			}
			opening = m.Source
		case "closing":
			if closing.present {
				return nil, false, ErrModel
			}
			closing = m.Source
		case "indent-prefix":
			// Parser-owned removal may share an original TAB with payload.
		default:
			return nil, false, ErrModel
		}
	}
	if err := charge(16, 0); err != nil {
		return nil, false, err
	}
	opener := index.lines[lo-1]
	if !opening.present || opening.start < opener.start || opening.end > opener.end || opening.end-opening.start != code.Width.Value {
		return nil, false, ErrModel
	}
	// lo is the physical line after the opener. Payload lines are consecutive;
	// original container prefixes can occur only before a line's first segment.
	line := lo
	base := len(dollars)
	state, dollar, previous := lineEmpty, 0, -1
	output := false
	for i := 0; ; i++ {
		if err := charge(1, 0); err != nil {
			return nil, false, err
		}
		if i == len(n.spec.Content) {
			break
		}
		part := n.spec.Content[i]
		if err := charge(32, 0); err != nil {
			return nil, false, err
		}
		if !part.Semantic.Present || part.Transformation == MappingUnavailable {
			return nil, false, nil
		}
		if line >= len(index.lines) || !part.Source.present || part.Source.SourceToken() != index.source.Token() || part.Source.start < n.spec.Envelope.start || part.Source.end > n.spec.Envelope.end || part.Source.start < 0 || part.Source.end < part.Source.start || part.Source.end > index.source.Len() {
			return nil, false, ErrModel
		}
		physical := index.lines[line]
		if part.Source.start < physical.start || part.Source.end > physical.next || previous >= 0 && part.Source.start != previous {
			return nil, false, ErrModel
		}
		raw := index.source.content[part.Source.start:part.Source.end]
		text := part.Semantic.Value
		if err := charge(int64(len(raw))+int64(len(text))+8, 0); err != nil {
			return nil, false, err
		}
		ending := part.Source.start == physical.end && part.Source.end == physical.next && text == "\n"
		if ending {
			switch part.Transformation {
			case Exact:
				if raw != "\n" {
					return nil, false, ErrModel
				}
			case Normalized:
				if raw != "\r" && raw != "\r\n" {
					return nil, false, ErrModel
				}
			case Synthetic:
				if raw != "" || part.Source.end != index.source.Len() {
					return nil, false, ErrModel
				}
			default:
				return nil, false, ErrModel
			}
			if state == lineDollar || state == lineSeparator || state == lineOutput {
				output = true
				dollars = dollars[:base]
			} else if state == lineCommand && !output {
				var err error
				dollars, err = shellPromptGrow(ctx, budget, dollars)
				if err != nil {
					return nil, false, err
				}
				if err = charge(1, 1); err != nil {
					return nil, false, err
				}
				dollars = append(dollars, Span{source: index.source, start: dollar, end: dollar + 1, present: true})
			}
			line++
			state, previous = lineEmpty, -1
			continue
		}
		if part.Source.end > physical.end {
			return nil, false, ErrModel
		}
		switch part.Transformation {
		case Exact:
			if raw != text {
				return nil, false, ErrModel
			}
		case Normalized:
			if raw != "\x00" || text != "\ufffd" {
				return nil, false, ErrModel
			}
		case PartialTab:
			if raw != "\t" || len(text) == 0 || len(text) > 3 {
				return nil, false, ErrModel
			}
		default:
			return nil, false, ErrModel
		}
		for at := 0; ; at++ {
			if err := charge(4, 0); err != nil {
				return nil, false, err
			}
			if at == len(text) {
				break
			}
			b := text[at]
			if b == '\r' || b == '\n' || part.Transformation == PartialTab && b != ' ' {
				return nil, false, ErrModel
			}
			blank := b == ' ' || b == '\t'
			switch state {
			case lineEmpty:
				if b == '$' && part.Transformation == Exact {
					state, dollar = lineDollar, part.Source.start+at
				} else if blank {
					state = lineBlank
				} else {
					state = lineOutput
				}
			case lineBlank:
				if !blank {
					state = lineOutput
				}
			case lineDollar:
				if blank {
					state = lineSeparator
				} else {
					state = lineOutput
				}
			case lineSeparator:
				if !blank {
					state = lineCommand
				}
			case lineCommand, lineOutput:
				// A command suffix or nonprompt remains classified for this line.
			default:
				return nil, false, ErrModel
			}
		}
		previous = part.Source.end
	}
	if previous >= 0 {
		return nil, false, ErrModel // Actual fenced payload always has a final LF mapping.
	}
	if err := charge(16, 0); err != nil {
		return nil, false, err
	}
	if closing.present {
		if line >= len(index.lines) || closing.start < index.lines[line].start || closing.end > index.lines[line].end || closing.end-closing.start < code.Width.Value || n.spec.Envelope.end != index.lines[line].next {
			return nil, false, ErrModel
		}
	} else if index.lines[line-1].next != n.spec.Envelope.end {
		return nil, false, ErrModel
	}
	return dollars, true, nil
}

func shellPromptGrow[T any](ctx context.Context, budget *OperationBudget, dst []T) ([]T, error) {
	if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
		return nil, err
	}
	if len(dst) < cap(dst) {
		return dst, nil
	}
	if cap(dst) > int(^uint(0)>>1)/2 {
		return nil, ErrModel
	}
	capacity := max(1, cap(dst)*2)
	if err := headingFactCharge(ctx, budget, int64(len(dst))+int64(capacity)+1, int64(capacity)); err != nil {
		return nil, err
	}
	next := make([]T, len(dst), capacity)
	copy(next, dst)
	return next, nil
}
