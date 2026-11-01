package parser

import (
	"context"
	"strings"
)

// Eligibility is resolved only through the last potential original comment.
// Whole image/code owners were left opaque by model collection. HTML-owned
// tails are scanned once by conflictHTMLRaw, never visited as Markdown children.
func (m *nestedExampleModel) eligibility(ctx context.Context, budget *OperationBudget, index LineIndex) (bool, error) {
	type candidate struct {
		node, line int
		known      bool
	}
	var candidates []candidate
	charge := func(work, records int64) error { return headingFactCharge(ctx, budget, work, records) }
	for ni, entry := range m.nodes {
		if err := charge(16, 0); err != nil {
			return false, err
		}
		n := entry.node
		if n.Kind() != HTMLBlock && n.Kind() != InlineHTML {
			continue
		}
		open, close := n.commentOpen, n.commentClose
		for _, marker := range n.spec.Markers {
			if err := charge(2*int64(len(marker.Role))+1, 0); err != nil {
				return false, err
			}
			if marker.Role == "html-comment-open" {
				open = marker.Source
			}
			if marker.Role == "html-comment-close" {
				close = marker.Source
			}
		}
		at := n.spec.Envelope.start
		if open.present {
			at = open.start
		}
		line, err := fencedSpacingLine(ctx, budget, index, at)
		if err != nil {
			return false, err
		}
		l := m.lines[line]
		b := &m.bodies[l.body]
		if !b.known[line-b.first] {
			if err := charge(int64(index.lines[line].end-index.lines[line].start)+1, 0); err != nil {
				return false, err
			}
			if strings.Contains(index.source.content[index.lines[line].start:index.lines[line].end], nestedExampleOpen) {
				return false, nil
			}
			continue
		}
		if !l.opening || l.body != entry.body || l.start < n.spec.Envelope.start || l.end > n.spec.Envelope.end {
			continue
		}
		// A comment started earlier cannot activate from text in its payload.
		if open.present && open.start != l.start {
			continue
		}
		if err := charge(24, 0); err != nil {
			return false, err
		}
		known := open.present && close.present
		if known && (open.SourceToken() != index.source.Token() || close.SourceToken() != index.source.Token() || open.end != l.start+4 || close.start != l.end-3 || close.end != l.end) {
			return false, ErrModel
		}
		if !open.present && n.Kind() == HTMLBlock && n.spec.Properties.HTML != nil && n.spec.Properties.HTML.BlockForm.Present && n.spec.Properties.HTML.BlockForm.Value != 2 {
			continue
		}
		candidates, err = shellPromptGrow(ctx, budget, candidates)
		if err != nil {
			return false, err
		}
		if err := charge(1, 1); err != nil {
			return false, err
		}
		candidates = append(candidates, candidate{ni, line, known})
	}
	if len(candidates) == 0 {
		return true, nil
	}
	if err := charge(2, 2); err != nil {
		return false, err
	}
	work := &parserContext{Context: ctx, budget: &parseBudget{operation: budget}}
	raw := conflictRawState{}
	next := 0
	for ni, entry := range m.nodes {
		if err := charge(2, 0); err != nil {
			return false, err
		}
		if next == len(candidates) {
			break
		}
		n := entry.node
		if candidates[next].node == ni {
			candidate := candidates[next]
			next++
			if raw.name == "" {
				if !candidate.known {
					return false, nil
				}
				m.lines[candidate.line].eligible = true
			}
		}
		if n.Kind() == HTMLBlock {
			var available bool
			var err error
			raw, _, available, err = conflictHTMLRaw(work, index.source, n, raw)
			if err != nil || !available {
				return false, err
			}
			continue
		}
		if err := charge(int64(len(raw.name))+int64(len(n.rawTag))+1, 0); err != nil {
			return false, err
		}
		if n.rawTag != "" {
			if raw.name == "" && !n.rawClosing {
				raw = conflictRawState{name: n.rawTag, start: n.spec.Envelope.start}
			} else if raw.name == n.rawTag && n.rawClosing {
				raw = conflictRawState{}
			}
		}
	}
	return true, nil
}

// Each derived contribution retains its complete original provenance. Exact
// segments admit slicing; transformed contributions, including shared tabs, are
// atomic. These records stay private and never grant derived diagnostic tokens.
type nestedExampleMapping struct {
	start, end int
	segment    Segment
}
type nestedExamplePayload struct {
	source    Source
	mapping   []nestedExampleMapping
	opening   Span
	character byte
	width     int
	problem   NestedExampleFact
}

func (m *nestedExampleModel) prepare(ctx context.Context, index LineIndex, pair nestedExamplePair) (out nestedExamplePayload, available bool, err error) {
	b := &m.bodies[pair.body]
	p := blockParser{ctx: ctx, index: index}
	first, last := -1, -1
	for line := pair.opening + 1; line < pair.closing; line++ {
		meterWork(ctx, 4)
		if err := ctx.Err(); err != nil {
			return out, false, err
		}
		if line < b.first || line > b.last {
			return out, false, ErrModel
		}
		if !b.known[line-b.first] {
			return out, false, nil
		}
		cursor := b.cursors[line-b.first]
		cursor.index = index
		logical := p.logicalLine(index.lines[line], cursor)
		if logical.contentStart == logical.end {
			continue
		}
		if first < 0 {
			first = line
		}
		last = line
	}
	point := index.span(m.lines[pair.closing].start, m.lines[pair.closing].start)
	if first < 0 {
		out.problem = NestedExampleFact{Span: point, Kind: "opening"}
		return out, true, nil
	}
	cursor := b.cursors[first-b.first]
	cursor.index = index
	openingLine := p.logicalLine(index.lines[first], cursor)
	// fenceOpening is the actual selected Markdown grammar and info decoder.
	meterRecords(ctx, 2)
	fence := p.fenceOpening(openingLine)
	if fence == nil {
		out.problem = NestedExampleFact{Span: nestedExampleBoundary(ctx, index, openingLine, 0, 0), Kind: "opening"}
		return out, true, nil
	}
	out.opening, out.character, out.width = fence.markers[0].Source, fence.character, fence.width
	// Require the original parser's stored info authority too. Reconstructing a
	// missing private info value would turn unknown original facts into absence.
	original, err := m.originalOpening(ctx, out.opening)
	if err != nil {
		return out, false, err
	}
	if original == nil || original.spec.Properties.Code == nil || original.spec.Properties.Code.Info == nil || !original.spec.Properties.Code.Info.Semantic.Present {
		return out, false, nil
	}
	code := original.spec.Properties.Code
	if !code.Fence.Present || !code.Width.Present {
		return out, false, nil
	}
	originalOpening := original.spec.Markers[0].Source
	meterWork(ctx, 16)
	if !originalOpening.present || originalOpening.SourceToken() != index.source.Token() || originalOpening.start != out.opening.start || originalOpening.end != out.opening.end {
		return out, false, ErrModel
	}
	meterWork(ctx, len(code.Style)+len(code.Info.Semantic.Value)+len(fence.info.Semantic.Value)+24)
	if code.Style != "fenced" || !code.Fence.Present || code.Fence.Value != fence.character || !code.Width.Present || code.Width.Value != fence.width || code.Info.Semantic.Value != fence.info.Semantic.Value {
		return out, false, ErrModel
	}
	info := code.Info.Semantic.Value
	start, end := 0, 0
	for start < len(info) && (info[start] == ' ' || info[start] == '\t') {
		meterWork(ctx, 1)
		start++
	}
	end = start
	for end < len(info) && info[end] != ' ' && info[end] != '\t' {
		meterWork(ctx, 1)
		end++
	}
	meterWork(ctx, end-start+1)
	if info[start:end] != "md" && info[start:end] != "markdown" {
		span := index.span(out.opening.end, out.opening.end)
		if start != end {
			var known bool
			span, known, err = nestedExampleInfoSpan(ctx, index, *code.Info, start, end, out.opening.end, openingLine.end)
			if err != nil || !known {
				return out, known, err
			}
		}
		out.problem = NestedExampleFact{Span: span, Kind: "language"}
		return out, true, nil
	}
	if first == last {
		out.problem = NestedExampleFact{Span: point, Kind: "closing"}
		return out, true, nil
	}
	cursor = b.cursors[last-b.first]
	cursor.index = index
	closingLine := p.logicalLine(index.lines[last], cursor)
	p.fence = fence
	if !p.fenceClosing(closingLine).present {
		out.problem = NestedExampleFact{Span: nestedExampleBoundary(ctx, index, closingLine, fence.character, fence.width), Kind: "closing"}
		return out, true, nil
	}
	var text strings.Builder
	// A source byte contributes at most four payload bytes (residual tab or
	// NUL normalization); omitted prefixes only reduce this upper bound. Reserve
	// the complete body buffer before allocation, independently of later parsing.
	capacity := 4 * (index.lines[last].start - index.lines[first+1].start)
	meterWork(ctx, capacity)
	meterRecords(ctx, capacity)
	text.Grow(capacity)
	for line := first + 1; line < last; line++ {
		meterWork(ctx, 1)
		if err := ctx.Err(); err != nil {
			return out, false, err
		}
		cursor := b.cursors[line-b.first]
		cursor.index = index
		_, _ = cursor.Consume(fence.indent)
		value := cursor.Remaining()
		for _, segment := range value.Segments {
			meterWork(ctx, 1)
			begin := text.Len()
			meteredWriteString(ctx, &text, segment.Semantic.Value)
			out.mapping = nestedExampleAppend(ctx, out.mapping, nestedExampleMapping{begin, text.Len(), segment})
		}
		physical := index.lines[line]
		transformation := Exact
		if physical.next-physical.end != 1 || index.source.content[physical.end] != '\n' {
			transformation = Normalized
		}
		segment := Segment{Source: index.span(physical.end, physical.next), Transformation: transformation, Semantic: Some("\n")}
		begin := text.Len()
		meteredWriteString(ctx, &text, "\n")
		out.mapping = nestedExampleAppend(ctx, out.mapping, nestedExampleMapping{begin, text.Len(), segment})
	}
	meterWork(ctx, 1)
	meterRecords(ctx, 1)
	out.source = Source{content: text.String(), identity: &sourceIdentity{}}
	return out, true, nil
}

func nestedExampleBoundary(ctx context.Context, index LineIndex, line blockLine, character byte, width int) Span {
	meterWork(ctx, 1)
	if line.indent >= 4 {
		return index.span(line.cursor.offset, line.contentStart)
	}
	start, end := line.contentStart, line.contentStart
	text := index.source.content
	if start < line.end && (text[start] == '`' || text[start] == '~') && character != 0 {
		for end < line.end && text[end] == text[start] {
			meterWork(ctx, 1)
			end++
		}
		if text[start] != character || end-start < width {
			return index.span(start, end)
		}
		start = end
		for start < line.end && (text[start] == ' ' || text[start] == '\t') {
			meterWork(ctx, 1)
			start++
		}
		end = start
	}
	for end < line.end && text[end] != ' ' && text[end] != '\t' {
		meterWork(ctx, 1)
		end++
	}
	return index.span(start, end)
}

func nestedExampleInfoSpan(ctx context.Context, index LineIndex, info Value, start, end, lower, upper int) (Span, bool, error) {
	at, previous := 0, lower
	var result Span
	for _, segment := range info.Segments {
		meterWork(ctx, 24)
		if at >= end {
			break
		}
		if !segment.Semantic.Present || !segment.Source.present || segment.Transformation == MappingUnavailable || segment.Transformation == Synthetic {
			return Span{}, false, nil
		}
		s := segment.Source
		if s.SourceToken() != index.source.Token() || s.start < previous || s.end < s.start || s.end > upper {
			return Span{}, false, ErrModel
		}
		length := len(segment.Semantic.Value)
		if length != 0 && s.end == s.start {
			return Span{}, false, ErrModel
		}
		if length > len(info.Semantic.Value)-at {
			return Span{}, false, ErrModel
		}
		meterWork(ctx, length+1)
		if segment.Semantic.Value != info.Semantic.Value[at:at+length] {
			return Span{}, false, ErrModel
		}
		if at+length > start && at < end {
			if segment.Transformation == Exact {
				if s.end-s.start != length || index.source.content[s.start:s.end] != segment.Semantic.Value {
					return Span{}, false, ErrModel
				}
				s.start += max(0, start-at)
				s.end -= max(0, at+length-end)
			} else if segment.Transformation != Entity && segment.Transformation != Escape && segment.Transformation != Normalized && segment.Transformation != PartialTab {
				return Span{}, false, ErrModel
			}
			if !result.present {
				result = s
			} else {
				result.end = s.end
			}
		}
		previous = segment.Source.end
		at += length
	}
	return result, at >= end && result.present, nil
}

func (payload nestedExamplePayload) compare(ctx context.Context, budget *OperationBudget, d *Document) (bool, bool, error) {
	// The shared target producer validates actual opening/closing markers and
	// resolves raw lifetimes while treating whole images as opaque owners.
	var index LineIndex
	err := nestedExampleLocked(ctx, budget, LineIndex{}, func(work *parserContext, _ LineIndex) error { index = newLineIndex(work, payload.source); return nil })
	if err != nil {
		return false, false, err
	}
	targets, available, err := d.fencedSpacingTargets(ctx, budget, index)
	if err != nil || !available {
		return false, available, err
	}
	bad := false
	for _, target := range targets {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return false, false, err
		}
		if target.spec.Properties.Code.Fence.Value != payload.character {
			continue
		}
		for _, marker := range target.spec.Markers {
			if err := headingFactCharge(ctx, budget, 2*int64(len(marker.Role))+2, 0); err != nil {
				return false, false, err
			}
			if marker.Role != "opening" && marker.Role != "closing" {
				continue
			}
			known, err := payload.mappedMarker(ctx, budget, marker.Source)
			if err != nil || !known {
				return false, known, err
			}
			if marker.Source.end-marker.Source.start >= payload.width {
				bad = true
			}
		}
	}
	return bad, true, nil
}

func (payload nestedExamplePayload) mappedMarker(ctx context.Context, budget *OperationBudget, span Span) (bool, error) {
	if err := headingFactCharge(ctx, budget, 16, 0); err != nil {
		return false, err
	}
	if !span.present || span.SourceToken() != payload.source.Token() || span.start < 0 || span.end <= span.start || span.end > payload.source.Len() {
		return false, ErrModel
	}
	lo, hi := 0, len(payload.mapping)
	for lo < hi {
		if err := headingFactCharge(ctx, budget, 1, 0); err != nil {
			return false, err
		}
		mid := lo + (hi-lo)/2
		if payload.mapping[mid].end <= span.start {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	at := span.start
	for at < span.end {
		if err := headingFactCharge(ctx, budget, 24, 0); err != nil {
			return false, err
		}
		if lo == len(payload.mapping) {
			return false, nil
		}
		part := payload.mapping[lo]
		lo++
		segment := part.segment
		if part.start > at || part.end <= at || !segment.Source.present || !segment.Semantic.Present {
			return false, nil
		}
		if segment.Transformation == MappingUnavailable || segment.Transformation == Synthetic {
			return false, nil
		}
		if segment.Source.SourceToken() != payload.opening.SourceToken() || segment.Source.start < payload.opening.end || segment.Source.end < segment.Source.start || segment.Source.end > segment.Source.source.Len() {
			return false, ErrModel
		}
		// Fence delimiter bytes cannot arise from normalized NUL/tab/ending
		// contributions. Requiring exact provenance prevents invented runs.
		if segment.Transformation != Exact || segment.Source.end-segment.Source.start != part.end-part.start {
			return false, ErrModel
		}
		stop := min(span.end, part.end)
		if err := headingFactCharge(ctx, budget, int64(stop-at)+1, 0); err != nil {
			return false, err
		}
		original := segment.Source.source.content[segment.Source.start+at-part.start : segment.Source.start+stop-part.start]
		if original != payload.source.content[at:stop] {
			return false, ErrModel
		}
		at = stop
	}
	return true, nil
}
