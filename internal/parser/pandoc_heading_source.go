package parser

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Source endpoints implement the pinned Markdown source/litChar/linkTitle
// alternatives. They are recognition facts only, shared by all candidate labels
// in one logical spelling. Each suffix state is computed once, including failed
// balanced groups which sourceURL can retry as literal '(' bytes. Resetting the
// heading spelling invalidates both successes and pending/failure facts.
func (b *pandocHeadingBoundaries) linkEnd(start int) int {
	if b.links == nil {
		b.linkSources()
	}
	if b.links[start] != 0 {
		return b.links[start]
	}
	return start
}

func (b *pandocHeadingBoundaries) linkContinuation(start int) bool {
	label := start
	if b.text[label] == '!' && label+1 < len(b.text) {
		label++
	}
	if b.text[label] != '[' {
		return false
	}
	end := b.bracketEnd(label)
	if end == label || end == len(b.text) || b.text[end] != '(' {
		return false
	}
	b.linkEnd(end)
	return b.linkPending[end]
}

// A successful short fallback needs lookahead only for its earlier unfinished
// alternative. Frame-local logical indexes exclude removed container prefixes.
func (b *pandocHeadingBoundaries) linkFutureHint(start int) rune {
	label := start
	if b.text[label] == '!' {
		label++
	}
	open := b.bracketEnd(label)
	if open <= label || open >= len(b.text) || b.text[open] != '(' || b.linkEnd(open) == open {
		return ')'
	}
	at := open + 1
	for at < len(b.text) && (b.text[at] == ' ' || b.text[at] == '\t') {
		meterWork(b.ctx, 1)
		at++
	}
	if at < len(b.text) && b.text[at] == '<' {
		return '>'
	}
	if at+5 <= len(b.text) && strings.EqualFold(b.text[at:at+5], "data:") {
		return -3
	}
	return ')'
}

func (b *pandocHeadingBoundaries) linkSources() {
	text, ctx := b.text, b.ctx
	n := len(text)
	b.links = meteredSlice[int](ctx, n)
	b.linkPending = meteredSlice[bool](ctx, n)
	// All arrays include EOF as a suffix. Temporary integer storage is metered,
	// rather than charged as fabricated semantic nodes for failed candidates.
	anglePending := meteredSlice[bool](ctx, n+1)
	anglePending[n] = true
	spaces := meteredSlice[int](ctx, n+1)
	balanced := meteredSlice[int](ctx, n+1)
	body := meteredSlice[int](ctx, n+1)
	bodyPending := meteredSlice[bool](ctx, n+1)
	bodyPending[n] = true
	angle := meteredSlice[int](ctx, n+1)
	quotes := [2][]int{meteredSlice[int](ctx, n+1), meteredSlice[int](ctx, n+1)}
	quoteBody := [2][]int{meteredSlice[int](ctx, n+1), meteredSlice[int](ctx, n+1)}
	quotePending := [2][]bool{meteredSlice[bool](ctx, n+1), meteredSlice[bool](ctx, n+1)}
	quoteBodyPending := [2][]bool{meteredSlice[bool](ctx, n+1), meteredSlice[bool](ctx, n+1)}
	quoteBodyPending[0][n], quoteBodyPending[1][n] = true, true
	title := meteredSlice[int](ctx, n+1)
	titlePending := meteredSlice[bool](ctx, n+1)
	titlePending[n] = true
	urlPending := meteredSlice[bool](ctx, n+1)
	url := meteredSlice[int](ctx, n+1)
	spaces[n] = n
	url[n] = n
	for i := n - 1; i >= 0; i-- {
		meterWork(ctx, 12)
		spaces[i] = i
		if text[i] == ' ' || text[i] == '\t' {
			spaces[i] = spaces[i+1]
		}
		next := pandocHeadingEscapeEnd(ctx, text, i)
		if next == i {
			_, size := utf8.DecodeRuneInString(text[i:])
			next = i + size
		}
		if text[i] == '\n' && spaces[i+1] < n && text[spaces[i+1]] == '\n' {
			next = 0
		}
		if text[i] == '(' {
			balanced[i] = body[i+1]
		}
		switch {
		case text[i] == ')':
			body[i] = i + 1
		case text[i] == '(' && balanced[i] > 0:
			body[i] = body[balanced[i]]
			bodyPending[i] = bodyPending[balanced[i]]
		case text[i] == '(':
			bodyPending[i] = bodyPending[i+1]
		case text[i] != '(' && next > 0:
			body[i] = body[next]
			bodyPending[i] = bodyPending[next]
		}
		if text[i] == '>' {
			angle[i] = i + 1
		} else if next > 0 {
			angle[i] = angle[next]
			anglePending[i] = anglePending[next]
		}
		for k, q := range []byte{'"', '\''} {
			if text[i] == q {
				r, _ := utf8.DecodeRuneInString(text[i+1:])
				if i+1 == n || !unicode.IsSpace(r) {
					quotes[k][i] = quoteBody[k][i+1]
					quotePending[k][i] = quoteBodyPending[k][i+1]
				}
				if i+1 == n || !attributeAlphaNum(r) {
					quoteBody[k][i] = i + 1
					continue
				}
				if quotes[k][i] > 0 {
					quoteBody[k][i] = quoteBody[k][quotes[k][i]]
					quoteBodyPending[k][i] = quoteBodyPending[k][quotes[k][i]]
					continue
				}
			}
			if next > 0 {
				quoteBody[k][i] = quoteBody[k][next]
				quoteBodyPending[k][i] = quoteBodyPending[k][next] || quotePending[k][i]
			}
		}
		at := spaces[i]
		if at < n && text[at] == '\n' {
			at = spaces[at+1]
		}
		if at < n && text[at] != '\n' {
			if text[at] == '"' {
				title[i] = quotes[0][at]
				titlePending[i] = quotePending[0][at]
			}
			if text[at] == '\'' {
				title[i] = quotes[1][at]
				titlePending[i] = quotePending[1][at]
			}
		}
		// many urlChunk stops on failure. Parenthesized chunks are tried before
		// litChar, whose fallback deliberately admits an unmatched literal '('.
		step := next
		unstable := false
		switch text[i] {
		case '(':
			if balanced[i] > 0 {
				step = balanced[i]
			} else {
				unstable = bodyPending[i+1]
			}
		case ')':
			step = 0
		case ' ':
			at = spaces[i]
			if at < n && strings.ContainsRune("\"')", rune(text[at])) {
				step = 0
			} else {
				step = at
			}
		case '\n':
			unstable = titlePending[i]
			if title[i] > 0 {
				step = 0
			}
		}
		if step > i {
			url[i] = url[step]
			urlPending[i] = unstable || urlPending[step]
		} else {
			url[i] = i
		}
	}
	base64, base64Pending := b.linkBase64Ends()
	for i := 0; i < n; i++ {
		meterWork(ctx, 4)
		if text[i] != '(' {
			continue
		}
		at := spaces[i+1]
		stop := url[at]
		preferredPending := at < n && text[at] == '<' && angle[at+1] == 0 && anglePending[at+1]
		if at < n && text[at] == '<' && angle[at+1] > 0 {
			stop = angle[at+1]
		} else if base64[at] > 0 {
			stop = base64[at]
		} else {
			preferredPending = preferredPending || base64Pending[at] || urlPending[at]
		}
		if title[stop] > 0 {
			stop = title[stop]
		}
		stop = spaces[stop]
		if stop < n && text[stop] == ')' {
			b.links[i] = stop + 1
			b.linkPending[i] = preferredPending
			continue
		}
		// EOF can complete on extension. An unfinished angle alternative or title
		// may also supersede the current fallback; a blank line cannot be litChar.
		b.linkPending[i] = preferredPending || stop == n
		if stop < n && (text[stop] == '"' || text[stop] == '\'') {
			k := 0
			if text[stop] == '\'' {
				k = 1
			}
			b.linkPending[i] = b.linkPending[i] || quotePending[k][stop]
		}
	}
}

// The data-URI alternative precedes sourceURL and admits even blank newlines
// in base64 data. Its restricted names, media-parameter fallback and payload
// endpoints are indexed too; no decode/allocation is needed for source bounds.
func (b *pandocHeadingBoundaries) linkBase64Ends() ([]int, []bool) {
	text, ctx := b.text, b.ctx
	n := len(text)
	ends := meteredSlice[int](ctx, n+1)
	pending := meteredSlice[bool](ctx, n+1)
	paramPending := meteredSlice[bool](ctx, n+1)
	paramPending[n] = true
	names := meteredSlice[int](ctx, n+1)
	semi := meteredSlice[int](ctx, n+1)
	equals := meteredSlice[int](ctx, n+1)
	data := meteredSlice[int](ctx, n+1)
	params := meteredSlice[int](ctx, n+1)
	names[n] = n
	semi[n] = n
	equals[n] = n
	data[n] = n
	asciiNum := func(c byte) bool { return c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' }
	hex := func(c byte) bool { return c >= '0' && c <= '9' || c >= 'A' && c <= 'F' || c >= 'a' && c <= 'f' }
	for i := n - 1; i >= 0; i-- {
		meterWork(ctx, 10)
		names[i] = i
		if asciiNum(text[i]) || strings.ContainsRune("!#$&^_.+-", rune(text[i])) {
			names[i] = names[i+1]
		}
		semi[i] = semi[i+1]
		if text[i] == ';' {
			semi[i] = i
		}
		equals[i] = i
		if text[i] == '=' {
			equals[i] = equals[i+1]
		}
		data[i] = equals[i]
		if asciiNum(text[i]) || strings.ContainsRune("/+ \t\r\n", rune(text[i])) {
			data[i] = data[i+1]
		} else if text[i] == '%' && i+2 < n && hex(text[i+1]) && hex(text[i+2]) {
			data[i] = data[i+3]
		}
		if strings.HasPrefix(text[i:], ";base64,") {
			params[i] = data[i+8]
		}
		if text[i] == ';' {
			if strings.HasPrefix(";base64,", text[i:]) {
				paramPending[i] = true
			}
			at := i + 1
			// Attoparsec's mediaParam skips Unicode isSpace, unlike skipSpaces.
			for at < n {
				r, size := utf8.DecodeRuneInString(text[at:])
				meterWork(ctx, 1)
				if !unicode.IsSpace(r) {
					break
				}
				at += size
			}
			if at == n {
				paramPending[i] = true
			}
			if at < n && asciiNum(text[at]) {
				at = names[at]
				if at == n {
					paramPending[i] = true
				}
				if at < n && text[at] == '=' {
					next := semi[at+1]
					params[i] = params[next]
					paramPending[i] = paramPending[next]
				}
			}
		}
		if i+5 <= n && strings.EqualFold(text[i:i+5], "data:") {
			at := i + 5
			if at == n {
				pending[i] = true
			}
			if at < n && asciiNum(text[at]) {
				at = names[at]
				if at == n {
					pending[i] = true
				}
				if at < n && text[at] == '/' {
					at++
					if at == n {
						pending[i] = true
					}
					if at < n && asciiNum(text[at]) {
						at = names[at]
						ends[i] = params[at]
						pending[i] = ends[i] == 0 && paramPending[at]
					}
				}
			}
		}
	}
	return ends, pending
}
