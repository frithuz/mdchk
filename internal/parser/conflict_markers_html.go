package parser

import (
	"context"
	"errors"
	"math"
)

type conflictRawState struct {
	name  string
	start int
}

type conflictRawByte struct{ start, end int }

// conflictHTMLRaw scans only original HTML-owned spelling for raw transitions.
// It does not recognize Markdown, acquire source/index authority, or run MD091.
// Content includes physical closing tails; the caller must not visit them again.
func conflictHTMLRaw(work *parserContext, source Source, n *Node, incoming conflictRawState) (state conflictRawState, closed []Span, available bool, err error) {
	budget := work.budget.operation
	budget.mu.Lock()
	defer budget.mu.Unlock()
	defer func() {
		if stopped := recover(); stopped != nil {
			if limit, ok := stopped.(budgetStop); ok {
				copy := *limit.exceeded
				state, closed, available, err = conflictRawState{}, nil, false, &copy
				return
			}
			panic(stopped)
		}
	}()
	state = incoming
	defer func() {
		meterWork(work, 1)
		err = errors.Join(err, work.Err())
		if err != nil || !available {
			state, closed = conflictRawState{}, nil
		}
	}()
	meterWork(work, 16)
	if n == nil || n.spec.Properties.HTML == nil || !n.spec.Properties.HTML.BlockForm.Present {
		return state, nil, false, nil
	}
	form := n.spec.Properties.HTML.BlockForm.Value
	if form < 1 || form > 7 {
		return state, nil, false, nil
	}
	logical := int64(0)
	for i := 0; ; i++ {
		meterWork(work, 1)
		if i == len(n.spec.Content) {
			break
		}
		logical = addBounded(logical, int64(len(n.spec.Content[i].Semantic.Value)))
	}
	if logical >= int64(int(^uint(0)>>1)) {
		meterHTMLWork(work, math.MaxInt64)
		return state, nil, false, ErrModel
	}
	// Reserve the existing lexer's suffix indexes, caches, visited buffers and
	// copying before use; its actual probes also charge the original allowance.
	lexerWork, records := unclosedHTMLScratchCosts(logical, int64(len(n.spec.Content)))
	meterHTMLWork(work, lexerWork)
	for {
		meterWork(work, 1)
		if records == 0 {
			break
		}
		chunk := min(records, int64(int(^uint(0)>>1)))
		meterRecords(work, int(chunk))
		records -= chunk
	}
	meterHTMLWork(work, addBounded(logical, logical))
	meterRecords(work, int(logical))
	meterRecords(work, int(logical))
	spelling := make([]byte, int(logical))
	mapping := make([]conflictRawByte, int(logical))
	at, previous := 0, n.spec.Envelope.start
	for i := 0; ; i++ {
		meterWork(work, 1)
		if i == len(n.spec.Content) {
			break
		}
		p := n.spec.Content[i]
		meterWork(work, 24)
		if !p.Semantic.Present || !contains(n.spec.Envelope, p.Source) || p.Source.start < previous || p.Source.end < p.Source.start {
			return state, nil, false, nil
		}
		raw := source.content[p.Source.start:p.Source.end]
		meterHTMLWork(work, addBounded(int64(len(raw)), addBounded(int64(len(p.Semantic.Value)), 16)))
		switch p.Transformation {
		case Exact:
			if raw != p.Semantic.Value {
				return state, nil, false, nil
			}
		case Normalized:
			if raw != "\x00" || p.Semantic.Value != "\ufffd" {
				return state, nil, false, nil
			}
		case Escape:
			if raw != "\\|" || p.Semantic.Value != "|" {
				return state, nil, false, nil
			}
		case PartialTab:
			if raw != "\t" || len(p.Semantic.Value) == 0 || len(p.Semantic.Value) > 4 {
				return state, nil, false, nil
			}
			for j := 0; ; j++ {
				meterWork(work, 1)
				if j == len(p.Semantic.Value) {
					break
				}
				if p.Semantic.Value[j] != ' ' {
					return state, nil, false, nil
				}
			}
		default:
			return state, nil, false, nil
		}
		meterWork(work, len(p.Semantic.Value))
		copy(spelling[at:], p.Semantic.Value)
		for j := 0; ; j++ {
			meterWork(work, 1)
			if j == len(p.Semantic.Value) {
				break
			}
			if err = work.Err(); err != nil {
				return state, nil, false, err
			}
			v := conflictRawByte{p.Source.start, p.Source.end}
			if p.Transformation == Exact {
				v = conflictRawByte{p.Source.start + j, p.Source.start + j + 1}
			}
			mapping[at+j] = v
		}
		at += len(p.Semantic.Value)
		previous = p.Source.end
	}
	meterWork(work, len(spelling))
	meterRecords(work, len(spelling))
	text := string(spelling)
	scanner := inlineScanner{ctx: work, text: text}
	meterWork(work, 1)
	meterRecords(work, 1)
	scanner.filterTagCache = make(map[int]int)
	at = 0
	form1End := -1
	// The parser's actual form-1 ANY4 boundary is independent of token opacity.
	// An incoming inline raw lifetime instead requires its corresponding name.
	if form == 1 && state.name == "" {
		boundary := -1
		for i := 0; ; i++ {
			meterWork(work, 1)
			if i == len(text) {
				break
			}
			if err = work.Err(); err != nil {
				return state, nil, false, err
			}
			if text[i] != '<' {
				continue
			}
			for k := 0; ; k++ {
				meterWork(work, 1)
				if k == 4 {
					break
				}
				token := [4]string{"</pre>", "</script>", "</style>", "</textarea>"}[k]
				meterWork(work, len(token)+1)
				if len(text)-i >= len(token) && embeddedASCIIEqual(text[i:i+len(token)], token) {
					boundary = i + len(token)
					break
				}
			}
			if boundary >= 0 {
				break
			}
		}
		if boundary >= 0 {
			// Scan complete tokens from the original beginning: a quoted tag
			// can straddle the lexical ANY4 boundary and still shield its tail.
			form1End = boundary
		} else {
			start := 0
			for {
				meterWork(work, 1)
				if start == len(text) || text[start] != ' ' && text[start] != '\t' {
					break
				}
				start++
			}
			for k := 0; ; k++ {
				meterWork(work, 1)
				if k == 4 {
					break
				}
				name := [4]string{"pre", "script", "style", "textarea"}[k]
				meterWork(work, len(name)+2)
				if len(text)-start > len(name) && text[start] == '<' && embeddedASCIIEqual(text[start+1:start+1+len(name)], name) {
					state = conflictRawState{name: name, start: mapping[start].start}
					return state, nil, true, nil // Known unclosed form-1 remainder.
				}
			}
			return state, nil, false, nil
		}
	}
	closeRaw := func(end int) {
		meterWork(work, 1)
		closed = conflictRawAppend(work, closed, Span{source: source, start: state.start, end: mapping[end-1].end, present: true})
		state = conflictRawState{}
	}
	for {
		meterWork(work, 1)
		if err = work.Err(); err != nil {
			return state, nil, false, err
		}
		if at == len(text) {
			break
		}
		if text[at] != '<' {
			at++
			continue
		}
		meterWork(work, 8)
		nameStart := at + 1
		closing := nameStart < len(text) && text[nameStart] == '/'
		if closing {
			nameStart++
		}
		nameEnd := htmlTagNameEndContext(work, text, nameStart)
		end, _ := scanner.inlineHTMLEnd(at)
		if end == 0 && htmlMarkdownOpaqueOpen(work, text[at:]) {
			break // Known unclosed opaque remainder has no raw transitions.
		}
		if state.name != "" {
			meterHTMLWork(work, int64(len(state.name))+int64(nameEnd-nameStart)+1)
			if end > 0 && closing && nameEnd > nameStart && embeddedASCIIEqual(text[nameStart:nameEnd], state.name) {
				closeRaw(end)
			}
		} else if end > 0 && !closing && nameEnd > nameStart && (form1End < 0 || at >= form1End) {
			meterWork(work, 128)
			for k := 0; ; k++ {
				meterWork(work, 1)
				if k == 4 {
					break
				}
				name := [4]string{"pre", "script", "style", "textarea"}[k]
				if embeddedASCIIEqual(text[nameStart:nameEnd], name) {
					state = conflictRawState{name: name, start: mapping[at].start}
					break
				}
			}
		} else if end == 0 && nameEnd > nameStart && (form1End < 0 || at >= form1End) {
			meterWork(work, 4)
			if nameEnd == len(text) || text[nameEnd] == '/' || text[nameEnd] == '>' || text[nameEnd] == ' ' || text[nameEnd] == '\t' || text[nameEnd] == '\r' || text[nameEnd] == '\n' {
				return state, nil, false, nil
			}
		}
		if end > 0 {
			at = end // Includes complete quoted attributes and opaque tokens.
		} else {
			at = max(at+1, nameEnd)
		}
	}
	available = true
	return state, closed, available, nil
}

func conflictRawAppend[T any](ctx context.Context, dst []T, value T) []T {
	meterWork(ctx, 1)
	if len(dst) == cap(dst) {
		capacity := addBounded(int64(cap(dst)), int64(cap(dst)))
		capacity = max(1, capacity)
		if capacity > int64(int(^uint(0)>>1)) {
			meterHTMLWork(ctx, math.MaxInt64)
			panic("parser: unrepresentable conflict raw capacity")
		}
		meterHTMLWork(ctx, addBounded(int64(len(dst)), addBounded(capacity, 1)))
		meterRecords(ctx, int(capacity))
		next := make([]T, len(dst), int(capacity))
		copy(next, dst)
		dst = next
	}
	meterWork(ctx, 1)
	meterRecords(ctx, 1)
	return append(dst, value)
}
