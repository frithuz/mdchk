package parser

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// This cursor shields selected inline consumers and their attached attributes,
// while retaining their complete source as pending text. It constructs no inline
// AST and grants neither inline completion nor explicit-anchor authority.
func (p *blockParser) pandocHeadingSuffix(line blockLine, start, end int, atx bool) ([]Segment, *Attributes, Span, physicalLine) {
	a := newAttributeInput(p.ctx, p.index.semanticText(start, end).Segments)
	last := line.physicalLine
	local := pandocHeadingBoundaries{ctx: p.ctx, text: a.text}
	boundaries := &local
	var replay *pandocHeadingReplay
	begin, limit, i := 0, len(a.text), 0
	commit := func(proposed attributeInput, physical physicalLine) {
		a = proposed
		last = physical
		p.skipUntil = max(p.skipUntil, last.next)
		if replay != nil {
			limit = replay.offset(last.end)
		} else {
			limit = len(a.text)
			boundaries.reset(a.text)
		}
	}
	tryGroup := func(at int) (*Attributes, int, attributeInput, physicalLine) {
		if at >= limit || a.text[at] != '{' {
			return nil, at, a, last
		}
		completion := attributeCompletion{}
		input := attributeInput{ctx: p.ctx, text: a.text}
		if completion.resume(input, at, false) {
			attrs, n := a.scan(at)
			if replay != nil {
				if physical, ok := replay.physicalEnd(n); ok {
					return attrs, n, a, physical.physicalLine
				}
			}
			return attrs, n, a, last
		}
		if completion.pos < len(a.text) {
			if _, ok := input.token(completion.pos); !ok {
				return nil, at, a, last
			}
		}
		if replay != nil {
			return nil, at, a, last
		} // complete frame has already been supplied
		source := a.value(at, at+1, a.text[at:at+1]).Segments[0].Source
		physicalIndex := sort.Search(len(p.index.lines), func(i int) bool { meterWork(p.ctx, 1); return p.index.lines[i].end >= source.end })
		candidate := line
		candidate.physicalLine = p.index.lines[physicalIndex]
		parts, candidateLast := p.pandocOpenerParts(candidate, source.start, p.containerDepth())
		groupInput := newAttributeInput(p.ctx, parts)
		attrs, n := groupInput.scan(0)
		if attrs == nil {
			return nil, at, a, last
		}
		proposed := a
		if candidateLast.next > last.next {
			prefix := a.value(0, at, a.text[:at]).Segments
			proposed = newAttributeInput(p.ctx, meteredAppend(p.ctx, prefix, parts...))
		}
		return attrs, at + n, proposed, candidateLast
	}
	extend := func(at int) bool {
		if replay != nil || last.next >= len(p.index.source.content) {
			return false
		}
		owner := boundaries.continuationStart(at)
		if owner < 0 {
			return false
		}
		kind := a.text[owner]
		hint := rune('}')
		switch kind {
		case '[', '!':
			if boundaries.linkContinuation(owner) {
				hint = boundaries.linkFutureHint(owner)
			} else {
				hint = 0
			}
		case '<':
			hint = '>'
		case '$':
			hint = '$'
		case '`':
			hint = '`'
		}
		if closer, ok := boundaries.texPending[owner]; ok {
			hint = closer
		}
		if kind == '<' && strings.HasPrefix(a.text[owner:], "<!--") {
			hint = -1
		}
		if kind == '\\' && strings.HasPrefix(a.text[owner:], "\\begin") {
			hint = -2
		}
		sourceOwner := a.value(owner, owner+1, a.text[owner:owner+1]).Segments[0].Source.start
		sourceAt := a.value(at, at+1, a.text[at:at+1]).Segments[0].Source.start
		sourceOpen := -1
		if kind == '[' || kind == '!' {
			label := owner
			if kind == '!' {
				label++
			}
			open := boundaries.bracketEnd(label)
			if open > label && open < len(a.text) && a.text[open] == '(' {
				sourceOpen = a.value(open, open+1, a.text[open:open+1]).Segments[0].Source.start
			}
		}
		full, physical, ok, rejected := p.pandocHeadingContinuation(line, sourceOwner, sourceOpen, kind, hint)
		if rejected && (kind == '[' || kind == '!') {
			label := owner
			if kind == '!' {
				label++
			}
			open := boundaries.bracketEnd(label)
			boundaries.labelRejected[label] = true
			if sourceOpen >= 0 {
				boundaries.links[open] = 0
				boundaries.linkPending[open] = false
			}
			if sourceOpen < 0 || full != nil {
				boundaries.brackets[label] = -1
			}
		}
		if !ok {
			return false
		}
		// Borrow the immutable shared spelling. The cursor's admitted physical limit
		// moves after each successful owner; no growing prefix is materialized/reset.
		replay = full
		a = full.input
		boundaries = &full.boundaries
		begin = full.offset(start)
		i = full.offset(sourceAt)
		commit(a, physical)
		return true
	}
	acceptEnd := func(next int) bool {
		if next <= limit {
			return true
		}
		if replay == nil {
			return false
		}
		physical, ok := replay.physicalEnd(next)
		if !ok {
			return false
		}
		commit(a, physical.physicalLine)
		return true
	}
	var attrs *Attributes
	for i < limit {
		meterWork(p.ctx, 1)
		if next := pandocHeadingEscapeEnd(p.ctx, a.text, i); next > i {
			i = next
			continue
		}
		if next := boundaries.inlineEnd(i); next > i {
			if replay == nil && (a.text[i] == '[' || a.text[i] == '!') && boundaries.continuationStart(i) >= 0 {
				if extend(i) {
					continue
				}
				next = boundaries.inlineEnd(i)
			}
			if next > i && acceptEnd(next) {
				if _, after, proposed, physical := tryGroup(next); after > next {
					commit(proposed, physical)
					i = after
				} else {
					i = next
				}
				continue
			}
		}
		if next := boundaries.opaqueEnd(i); next > i && acceptEnd(next) {
			i = next
			continue
		}
		if replay == nil && boundaries.mayContinue(i) && extend(i) {
			continue
		}
		if a.text[i] == '{' {
			candidate, after, proposed, physical := tryGroup(i)
			if candidate != nil {
				proposedLimit := len(proposed.text)
				if replay != nil {
					proposedLimit = replay.offset(physical.end)
				}
				meterWork(p.ctx, proposedLimit-after)
				if strings.Trim(proposed.text[after:proposedLimit], " \t") == "" {
					attrs = candidate
					commit(proposed, physical)
					end = i
					goto suffix
				}
			}
		}
		i++
	}
	end = limit
suffix:
	for end > begin && (a.text[end-1] == ' ' || a.text[end-1] == '\t') {
		meterWork(p.ctx, 1)
		end--
	}
	closing := Span{}
	if atx {
		tail := end
		for end > begin && a.text[end-1] == '#' {
			meterWork(p.ctx, 1)
			end--
		}
		hashes := end
		slashes := hashes
		for slashes > begin && a.text[slashes-1] == '\\' {
			meterWork(p.ctx, 1)
			slashes--
		}
		if (hashes-slashes)%2 == 1 && end < tail {
			end++
		}
		if end < tail {
			closing = a.value(end, tail, a.text[end:tail]).Segments[0].Source
		}
		for end > begin && (a.text[end-1] == ' ' || a.text[end-1] == '\t') {
			meterWork(p.ctx, 1)
			end--
		}
	}
	return a.value(begin, end, a.text[begin:end]).Segments, attrs, closing, last
}

// These bounded recognizers establish only the cursor after code, balanced
// labels and selected link sources. Their adjacent group still uses the shared
// authoritative attribute scanner. Malformed syntax falls back at its original
// cursor; whitespace-separated groups remain eligible heading suffixes.
func (b *pandocHeadingBoundaries) inlineEnd(start int) int {
	ctx, text := b.ctx, b.text
	if text[start] == '<' {
		b.angleEnds()
		return pandocHeadingAutolinkEnd(ctx, text, start, b.angles[start])
	}
	if text[start] == '`' {
		return b.codeEnd(start)
	}
	label := start
	if text[label] == '!' && label+1 < len(text) {
		label++
	}
	if text[label] != '[' || label+1 < len(text) && text[label+1] == '^' {
		return start
	}
	end := b.bracketEnd(label)
	if end == label {
		return start
	}
	if end < len(text) && text[end] == '(' {
		if target := b.linkEnd(end); target > end {
			return target
		}
	}
	if end < len(text) && text[end] == '[' {
		if reference := b.bracketEnd(end); reference > end {
			return reference
		}
	}
	return end
}

// Closer indexes belong to one replay frame, so removed quote/list prefixes
// cannot repeatedly trigger failed continuation materialization. Index original
// positions directly from existing cursors, without constructing source Values.
type pandocHeadingFutureKey struct{ start, depth int }
type pandocHeadingFutureIndex struct {
	from      int
	positions map[rune][]int
}

func (p *blockParser) pandocHeadingFuture(closer rune, from int) bool {
	depth := p.containerDepth()
	key := pandocHeadingFutureKey{p.stack[depth-1].start, depth}
	if p.headingFuture == nil {
		meterWork(p.ctx, 1)
		p.headingFuture = make(map[pandocHeadingFutureKey]pandocHeadingFutureIndex)
	}
	cached, ok := p.headingFuture[key]
	if !ok || cached.from > from {
		meterWork(p.ctx, 4)
		cached = pandocHeadingFutureIndex{from: from, positions: make(map[rune][]int)}
		states := p.pandocReplayStates()[:depth]
		next := sort.Search(len(p.index.lines), func(i int) bool { meterWork(p.ctx, 1); return p.index.lines[i].start >= from })
		for ; next < len(p.index.lines); next++ {
			meterWork(p.ctx, 1)
			physical := p.index.lines[next]
			cursor, valid := p.pandocReplayCursor(physical, states)
			if !valid {
				break
			}
			for i := cursor.offset; i < cursor.end; {
				meterWork(p.ctx, 1)
				if i+3 <= cursor.end && p.index.source.content[i:i+3] == "-->" {
					cached.positions[-1] = meteredAppend(p.ctx, cached.positions[-1], i)
				}
				if i+8 <= cursor.end && p.index.source.content[i:i+8] == ";base64," {
					cached.positions[-3] = meteredAppend(p.ctx, cached.positions[-3], i)
				}
				if i+4 <= cursor.end && p.index.source.content[i:i+4] == "\\end" {
					cached.positions[-2] = meteredAppend(p.ctx, cached.positions[-2], i)
				}
				r, n := utf8.DecodeRuneInString(p.index.source.content[i:cursor.end])
				cached.positions[r] = meteredAppend(p.ctx, cached.positions[r], i)
				i += n
			}
		}
		meterWork(p.ctx, 1)
		p.headingFuture[key] = cached
	}
	positions := cached.positions[closer]
	at := sort.Search(len(positions), func(i int) bool { meterWork(p.ctx, 1); return positions[i] >= from })
	return at < len(positions)
}
