package parser

import "sort"

// One immutable logical replay per actual container frame shares acquisition
// and complete success/failure endpoints across physical extensions and starts.
// The original source never changes within a parse. A new frame/start invalidates
// the replay; borrowed integer indexes convey no semantic node or public authority.
type pandocHeadingReplay struct {
	from       int
	input      attributeInput
	boundaries pandocHeadingBoundaries
	lines      []blockLine
	codeStops  []int
}

func (p *blockParser) pandocHeadingReplay(line blockLine) *pandocHeadingReplay {
	depth := p.containerDepth()
	key := pandocHeadingFutureKey{p.stack[depth-1].start, depth}
	if p.headingContinuations == nil {
		meterWork(p.ctx, 1)
		p.headingContinuations = make(map[pandocHeadingFutureKey]*pandocHeadingReplay)
	}
	if cached := p.headingContinuations[key]; cached != nil && cached.from <= line.start {
		return cached
	}
	meterWork(p.ctx, 5)
	r := &pandocHeadingReplay{from: line.start}
	parts := meteredSlice[Segment](p.ctx, 0)
	inList := false
	for _, frame := range p.stack {
		meterWork(p.ctx, 1)
		if frame.kind == ListItem {
			inList = true
		}
	}
	add := func(current blockLine) {
		r.lines = meteredAppend(p.ctx, r.lines, current)
		parts = meteredAppend(p.ctx, parts, current.cursor.Remaining().Segments...)
		parts = meteredAppend(p.ctx, parts, p.index.semanticText(current.end, current.next).Segments...)
		if inList {
			_, a := p.pandocListStart(current, nil, false)
			_, b := p.pandocListStart(current, nil, true)
			if a || b {
				r.codeStops = meteredAppend(p.ctx, r.codeStops, current.start)
			}
		}
	}
	// The first line already owns its actual payload cursor (not a continuation
	// cursor for an initial list marker). All following lines use copied states.
	add(line)
	states := p.pandocReplayStates()
	next := sort.Search(len(p.index.lines), func(i int) bool { meterWork(p.ctx, 1); return p.index.lines[i].start >= line.next })
	for ; next < len(p.index.lines); next++ {
		meterWork(p.ctx, 1)
		physical := p.index.lines[next]
		cursor, ok := p.pandocReplayCursor(physical, states)
		if !ok {
			break
		}
		add(p.logicalLine(physical, cursor))
	}
	r.input = newAttributeInput(p.ctx, parts)
	r.boundaries = pandocHeadingBoundaries{ctx: p.ctx, text: r.input.text}
	for _, stop := range r.codeStops {
		r.boundaries.codeBarriers = meteredAppend(p.ctx, r.boundaries.codeBarriers, r.offset(stop))
	}
	p.headingContinuations[key] = r
	return r
}

func (r *pandocHeadingReplay) offset(source int) int {
	a := r.input
	i := sort.Search(len(a.parts), func(i int) bool { meterWork(a.ctx, 1); return a.parts[i].Source.end > source })
	if i == len(a.parts) {
		return len(a.text)
	}
	before := 0
	if i > 0 {
		before = a.ends[i-1]
	}
	part := a.parts[i]
	if part.Transformation == Exact {
		return before + source - part.Source.start
	}
	return before
}

func (r *pandocHeadingReplay) physicalEnd(end int) (blockLine, bool) {
	a := r.input
	part := sort.Search(len(a.ends), func(i int) bool { meterWork(a.ctx, 1); return a.ends[i] >= end })
	rawEnd := a.parts[part].Source.end
	if a.parts[part].Transformation == Exact {
		before := 0
		if part > 0 {
			before = a.ends[part-1]
		}
		rawEnd = a.parts[part].Source.start + end - before
	}
	at := sort.Search(len(r.lines), func(i int) bool { meterWork(a.ctx, 1); return r.lines[i].end >= rawEnd })
	if at == len(r.lines) {
		return blockLine{}, false
	}
	return r.lines[at], true
}

func (p *blockParser) pandocHeadingContinuation(line blockLine, sourceOwner, sourceOpen int, kind byte, hint rune) (*pandocHeadingReplay, physicalLine, bool, bool) {
	if hint != 0 && !p.pandocHeadingFuture(hint, line.next) {
		return nil, line.physicalLine, false, false
	}
	r := p.pandocHeadingReplay(line)
	owner := r.offset(sourceOwner)
	b := &r.boundaries
	var end int
	if sourceOpen >= 0 {
		label := owner
		if kind == '!' {
			label++
		}
		fullLabel := b.bracketEnd(label)
		if fullLabel == label {
			return r, line.physicalLine, false, true
		}
		open := r.offset(sourceOpen)
		if fullLabel != open {
			end = b.inlineEnd(owner)
		} else {
			end = b.linkEnd(open)
			if end == open {
				return nil, line.physicalLine, false, true
			}
		}
	} else if kind == '[' || kind == '!' {
		end = b.inlineEnd(owner)
		if end == owner {
			return nil, line.physicalLine, false, true
		}
	} else if kind == '`' {
		end = b.codeEnd(owner)
	} else {
		end = b.opaqueEnd(owner)
	}
	if end <= owner {
		return nil, line.physicalLine, false, false
	}
	actual, ok := r.physicalEnd(end)
	if !ok || actual.next <= line.next {
		return nil, line.physicalLine, false, false
	}
	return r, actual.physicalLine, true, false
}
