package parser

import (
	"context"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Indexes belong to one logical heading spelling. Failed candidates retain no
// syntax depth: every bracket start receives either its real end or zero, and
// every backtick suffix can query the next run of exactly its opening width.
// Integer indexes charge storage/copy work, not semantic document records.
// Multiline attached attributes can extend text; reset invalidates all indexes.
type pandocHeadingBoundaries struct {
	ctx                                      context.Context
	text                                     string
	code, brackets                           []int
	codeBarriers                             []int
	links                                    []int
	linkPending                              []bool
	angles, greater                          []int
	htmlClose, newlines                      []int
	texPending                               map[int]rune
	texResults                               map[int]int
	contexts                                 map[int]int
	texEnvs                                  map[string][][2]int
	texComments                              []int
	labelBodies, labelSteps                  []int
	labelPending, stepPending, labelRejected []bool
	texGroups, mathGroups, texOptions        []int
}

func (b *pandocHeadingBoundaries) reset(text string) {
	if len(text) != len(b.text) {
		b.code, b.brackets = nil, nil
		b.links, b.linkPending = nil, nil
		b.angles, b.greater = nil, nil
		b.htmlClose, b.newlines = nil, nil
		b.texPending = nil
		b.texResults = nil
		b.contexts = nil
		b.texEnvs = nil
		b.texComments = nil
		b.labelBodies, b.labelSteps = nil, nil
		b.labelPending, b.stepPending, b.labelRejected = nil, nil, nil
		b.texGroups, b.mathGroups, b.texOptions = nil, nil, nil
	}
	b.text = text
}

// All failed angle starts share endpoints, including a valid prefix whose
// closer never arrives. Spaces in HTML and ASCII-only autolink extra whitespace
// intentionally use different indexes.
func (b *pandocHeadingBoundaries) angleEnds() {
	if b.angles != nil {
		return
	}
	b.angles = meteredSlice[int](b.ctx, len(b.text))
	b.greater = meteredSlice[int](b.ctx, len(b.text))
	b.htmlClose = meteredSlice[int](b.ctx, len(b.text)+1)
	angle, greater, comment := 0, 0, 0
	for i := len(b.text) - 1; i >= 0; i-- {
		meterWork(b.ctx, 1)
		c := b.text[i]
		if c == '>' {
			angle = i
			greater = i
		}
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			angle = 0
		}
		if strings.HasPrefix(b.text[i:], "-->") {
			comment = i + 3
		}
		b.htmlClose[i] = comment
		b.angles[i] = angle
		b.greater[i] = greater
	}
}
func (b *pandocHeadingBoundaries) codeEnd(start int) int {
	if b.code == nil {
		b.code = meteredSlice[int](b.ctx, len(b.text))
		next := meteredSlice[int](b.ctx, len(b.text)+1)
		limit := len(b.text) + 1
		for end := len(b.text); end > 0; {
			meterWork(b.ctx, 1)
			if b.text[end-1] != '`' {
				if b.text[end-1] == '\n' {
					at := end
					for at < len(b.text) && (b.text[at] == ' ' || b.text[at] == '\t' || b.text[at] == '\r') {
						meterWork(b.ctx, 1)
						at++
					}
					if at < len(b.text) && b.text[at] == '\n' {
						limit = end - 1
					}
				}
				end--
				continue
			}
			begin := end - 1
			for begin > 0 && b.text[begin-1] == '`' {
				meterWork(b.ctx, 1)
				begin--
			}
			for i := begin; i < end; i++ {
				meterWork(b.ctx, 1)
				if next[end-i] < limit {
					b.code[i] = next[end-i]
				}
			}
			next[end-begin] = end
			end = begin
		}
	}
	if b.code[start] > 0 {
		stop := sort.Search(len(b.codeBarriers), func(i int) bool { meterWork(b.ctx, 1); return b.codeBarriers[i] > start })
		if stop < len(b.codeBarriers) && b.codeBarriers[stop] < b.code[start] {
			return start
		}
		return b.code[start]
	}
	return start
}

// Body states are entered only by the selected raw prescan from an actual
// label start. Memoized suffix dependencies share failures and pending facts;
// they do not evaluate a token skipped by an escape or a successful owner.
func (b *pandocHeadingBoundaries) bracketEnd(start int) int {
	if b.brackets == nil {
		n := len(b.text)
		b.brackets = meteredSlice[int](b.ctx, n)
		b.labelBodies = meteredSlice[int](b.ctx, n+1)
		b.labelSteps = meteredSlice[int](b.ctx, n)
		b.labelPending = meteredSlice[bool](b.ctx, n+1)
		b.stepPending = meteredSlice[bool](b.ctx, n)
		b.labelRejected = meteredSlice[bool](b.ctx, n)
		b.labelBodies[n] = -1
	}
	if b.brackets[start] == 0 {
		b.labelBody(start + 1)
		b.brackets[start] = b.labelBodies[start+1]
	}
	if b.brackets[start] > 0 {
		return b.brackets[start]
	}
	return start
}

func (b *pandocHeadingBoundaries) labelBody(start int) {
	path := meteredSlice[int](b.ctx, 0)
	path = meteredAppend(b.ctx, path, start)
	for len(path) > 0 {
		meterWork(b.ctx, 1)
		i := path[len(path)-1]
		if b.labelBodies[i] != 0 {
			path = path[:len(path)-1]
			continue
		}
		if b.labelSteps[i] == 0 {
			next := pandocHeadingEscapeEnd(b.ctx, b.text, i)
			escaped := next > i
			if !escaped && b.text[i] == '`' {
				next = b.codeEnd(i)
				if next > i && next < len(b.text) && b.text[next] == '{' {
					completion := attributeCompletion{}
					input := attributeInput{ctx: b.ctx, text: b.text}
					if !completion.resume(input, next, false) {
						// Code's optional attribute alternative can itself own the
						// physical continuation after the backtick already closed.
						if completion.pos >= len(b.text) {
							b.stepPending[i] = true
						} else if _, ok := input.token(completion.pos); ok {
							b.stepPending[i] = true
						}
					}
					if completion.resume(input, next, true) {
						next = completion.end
					}
				}
			}
			if next == i {
				next = b.opaqueEnd(i)
			}
			if next > i {
				b.labelSteps[i] = next
			} else {
				b.stepPending[i] = b.rawPending(i)
				switch b.text[i] {
				case '\n':
					b.labelSteps[i] = -1
				case ']':
					b.labelSteps[i] = -2
				case '[':
					b.labelSteps[i] = -3
				default:
					_, size := utf8.DecodeRuneInString(b.text[i:])
					b.labelSteps[i] = i + size
				}
			}
		}
		switch step := b.labelSteps[i]; step {
		case -1:
			b.labelBodies[i] = -1
			b.labelPending[i] = b.stepPending[i]
		case -2:
			b.labelBodies[i] = i + 1
		case -3:
			if b.labelBodies[i+1] == 0 {
				path = meteredAppend(b.ctx, path, i+1)
				continue
			}
			nested := b.labelBodies[i+1]
			if nested < 0 {
				b.labelBodies[i] = -1
				b.labelPending[i] = b.labelPending[i+1]
				continue
			}
			if b.labelBodies[nested] == 0 {
				path = meteredAppend(b.ctx, path, nested)
				continue
			}
			b.labelBodies[i] = b.labelBodies[nested]
			b.labelPending[i] = b.labelPending[i+1] || b.labelPending[nested]
		default:
			if b.labelBodies[step] == 0 {
				path = meteredAppend(b.ctx, path, step)
				continue
			}
			b.labelBodies[i] = b.labelBodies[step]
			b.labelPending[i] = b.stepPending[i] || b.labelPending[step]
		}
	}
}

// This is a cheap possible-start test, not a standalone HTML recognizer. Full
// grammar is evaluated only for an actual reachable owner in the shared frame.
func (b *pandocHeadingBoundaries) rawPending(start int) bool {
	switch b.text[start] {
	case '`':
		return true
	case '$':
		if start+1 < len(b.text) {
			r, _ := utf8.DecodeRuneInString(b.text[start+1:])
			return !unicode.IsSpace(r)
		}
	case '<':
		if strings.HasPrefix(b.text[start:], "<!--") {
			return true
		}
		i := start + 1
		if i < len(b.text) && b.text[i] == '/' {
			i++
		}
		if i < len(b.text) {
			r, _ := utf8.DecodeRuneInString(b.text[i:])
			return unicode.IsLetter(r) || r == '?'
		}
	case '\\':
		_, pending := b.texPending[start]
		return pending
	}
	return false
}

func (b *pandocHeadingBoundaries) continuationStart(start int) int {
	label := start
	if b.text[label] == '!' && label+1 < len(b.text) {
		label++
	}
	if b.text[label] != '[' {
		if b.mayContinue(start) {
			return start
		}
		return -1
	}
	b.bracketEnd(label)
	if b.labelRejected[label] {
		return -1
	}
	if b.linkContinuation(start) || b.labelPending[label+1] {
		return start
	}
	return -1
}

// These are raw-prescan boundaries, not inline ASTs. In particular, links and
// spans inside labels are not whole-token shields in inBalancedBrackets.
// Ordinary unmatched brackets cannot cross a heading endline. Only a
// potentially unfinished opaque token can justify continuation lookahead.
func (b *pandocHeadingBoundaries) mayContinue(start int) bool {
	text := b.text
	switch text[start] {
	case '[':
		return b.continuationStart(start) >= 0
	case '!':
		if start+1 < len(text) && text[start+1] == '[' {
			return b.continuationStart(start+1) >= 0
		}
	case '`':
		return true
	case '$':
		if start+1 < len(text) {
			r, _ := utf8.DecodeRuneInString(text[start+1:])
			return !unicode.IsSpace(r)
		}
	case '<':
		return b.rawPending(start)
	case '\\':
		b.texEnd(start)
		_, pending := b.texPending[start]
		return pending
	}
	return false
}

func (b *pandocHeadingBoundaries) opaqueEnd(start int) int {
	switch b.text[start] {
	case '$':
		return b.mathEnd(start)
	case '<':
		b.angleEnds()
		if b.greater[start] == 0 {
			return start
		}
		end, _ := b.htmlBoundary(start)
		return end
	case '\\':
		end := b.texEnd(start)
		if _, pending := b.texPending[start]; pending {
			return start
		}
		return end
	}
	return start
}

// Selected tex_math_dollars: display is tried before inline; single-dollar
// content cannot start with whitespace or end in whitespace, and its closer
// cannot precede a digit. Groups/comments/escaped bytes shield delimiters.
func (b *pandocHeadingBoundaries) mathEnd(start int) int {
	ctx, text := b.ctx, b.text
	scan := func(width int) int {
		i := start + width
		if i >= len(text) {
			return start
		}
		if width == 1 {
			r, _ := utf8.DecodeRuneInString(text[i:])
			if unicode.IsSpace(r) {
				return start
			}
		}
		content := false
		for i < len(text) {
			meterWork(ctx, 1)
			if strings.HasPrefix(text[i:], strings.Repeat("$", width)) {
				if !content {
					return start
				}
				end := i + width
				if width == 1 && end < len(text) && text[end] >= '0' && text[end] <= '9' {
					return start
				}
				return end
			}
			c := text[i]
			if c == '%' {
				end := b.commentLineEnd(i)
				if end == len(text) || pandocHeadingBlankAfter(ctx, text, end) {
					return start
				}
				i = end + 1
				content = true
				continue
			}
			if c == '\\' {
				if i+1 == len(text) {
					return start
				}
				i += 2
				content = true
				continue
			}
			if c == '{' {
				end := b.groupEnd(i, true)
				if end == i {
					return start
				}
				i = end
				content = true
				continue
			}
			if c == '\n' && pandocHeadingBlankAfter(ctx, text, i) {
				return start
			}
			if width == 1 && (c == ' ' || c == '\t' || c == '\n' || c == '\r') {
				for i < len(text) && (text[i] == ' ' || text[i] == '\t' || text[i] == '\n' || text[i] == '\r') {
					if text[i] == '\n' && pandocHeadingBlankAfter(ctx, text, i) {
						return start
					}
					meterWork(ctx, 1)
					i++
				}
				if i < len(text) && text[i] == '$' {
					return start
				}
				continue
			}
			i++
			content = true
		}
		return start
	}
	if start+1 < len(text) && text[start+1] == '$' {
		if end := scan(2); end > start {
			return end
		}
	}
	return scan(1)
}

// htmlTag uses TagSoup plus name sanity checks; the selected profile permits
// inline tags/comments (markdown_in_html_blocks), including unknown inline tag
// names. Quoted attribute values own internal angles and brackets. This raw
// recognizer keeps the exact endpoint and creates no HTML node.
func (b *pandocHeadingBoundaries) htmlBoundary(start int) (int, bool) {
	ctx, text := b.ctx, b.text
	if strings.HasPrefix(text[start:], "<!--") {
		b.angleEnds()
		if b.htmlClose[start+4] > 0 {
			return b.htmlClose[start+4], false
		}
		return start, true
	}
	i := start + 1
	if i == len(text) {
		return start, false
	}
	closing := text[i] == '/'
	if closing {
		i++
	}
	nameStart := i
	if i == len(text) {
		return start, false
	}
	pi := text[i] == '?'
	if pi {
		i++
	} else {
		r, n := utf8.DecodeRuneInString(text[i:])
		if !unicode.IsLetter(r) {
			return start, false
		}
		i += n
	}
	for i < len(text) {
		meterWork(ctx, 1)
		r, n := utf8.DecodeRuneInString(text[i:])
		if !attributeAlphaNum(r) && r != ':' && r != '-' && r != '_' {
			break
		}
		i += n
	}
	name := text[nameStart:i]
	if len(name) == 0 || name[len(name)-1] == ':' {
		return start, false
	}
	// Selected inline categories: script opens require math/tex type, style is
	// explicitly inline; block-only names cannot shield a heading label.
	meterWork(ctx, len(name)+1)
	lower := strings.ToLower(name)
	if !pi && pandocHeadingHTMLBlockTag(lower) && lower != "style" && lower != "script" {
		return start, false
	}
	script := lower == "script" && !closing
	scriptMath := false
	for i < len(text) {
		meterWork(ctx, 1)
		for i < len(text) && (text[i] == ' ' || text[i] == '\t' || text[i] == '\n' || text[i] == '\r' || text[i] == '\f') {
			meterWork(ctx, 1)
			i++
		}
		if i == len(text) {
			return start, true
		}
		if text[i] == '>' {
			if script && !scriptMath {
				return start, false
			}
			return i + 1, false
		}
		if text[i] == '/' || pi && text[i] == '?' {
			i++
			continue
		}
		if closing {
			return start, false
		}
		at := i
		r, n := utf8.DecodeRuneInString(text[i:])
		if !unicode.IsLetter(r) {
			return start, false
		}
		i += n
		for i < len(text) {
			meterWork(ctx, 1)
			r, n = utf8.DecodeRuneInString(text[i:])
			if !attributeAlphaNum(r) && r != ':' && r != '-' && r != '_' {
				break
			}
			i += n
		}
		key := text[at:i]
		for i < len(text) && (text[i] == ' ' || text[i] == '\t' || text[i] == '\n' || text[i] == '\r' || text[i] == '\f') {
			meterWork(ctx, 1)
			i++
		}
		if i == len(text) || text[i] != '=' {
			continue
		}
		i++
		for i < len(text) && (text[i] == ' ' || text[i] == '\t' || text[i] == '\n' || text[i] == '\r' || text[i] == '\f') {
			meterWork(ctx, 1)
			i++
		}
		if i == len(text) {
			return start, true
		}
		value := i
		if text[i] == '\'' || text[i] == '"' {
			quote := text[i]
			i++
			value = i
			for i < len(text) && text[i] != quote {
				meterWork(ctx, 1)
				i++
			}
			if i == len(text) {
				return start, true
			}
			if strings.EqualFold(key, "type") && strings.HasPrefix(text[value:i], "math/tex") {
				scriptMath = true
			}
			i++
		} else {
			for i < len(text) && text[i] != '>' && text[i] != ' ' && text[i] != '\t' && text[i] != '\n' && text[i] != '\r' && text[i] != '\f' {
				meterWork(ctx, 1)
				i++
			}
			if strings.EqualFold(key, "type") && strings.HasPrefix(text[value:i], "math/tex") {
				scriptMath = true
			}
		}
	}
	return start, true
}

func pandocHeadingEscapeEnd(ctx context.Context, text string, start int) int {
	if text[start] != '\\' || start+1 == len(text) {
		return start
	}
	meterWork(ctx, 1)
	r, n := utf8.DecodeRuneInString(text[start+1:])
	if r != '\n' && r != '\r' && !attributeAlphaNum(r) {
		return start + 1 + n
	}
	return start
}

// Raw TeX uses control words, not Markdown escapes. Token groups ignore TeX
// comments and control-sequence braces; optional arguments stop at the first
// ungrouped ']' (nested '[' is not balanced by bracketedToks).
// Body endpoints are computed right-to-left for every possible start. This
// preserves fallback into an earlier failed token, including starts that lay
// inside its comment or escape. A global lexical stack would lose those starts.
func (b *pandocHeadingBoundaries) groupBodies(math bool) []int {
	if math && b.mathGroups != nil {
		return b.mathGroups
	}
	if !math && b.texGroups != nil {
		return b.texGroups
	}
	text, ctx := b.text, b.ctx
	body := meteredSlice[int](ctx, len(text)+1)
	newline := len(text)
	newlineBlank := false
	for i := len(text) - 1; i >= 0; i-- {
		meterWork(ctx, 1)
		switch text[i] {
		case '}':
			body[i] = i + 1
		case '{':
			if end := body[i+1]; end > 0 {
				body[i] = body[end]
			}
		case '\\':
			if i+1 < len(text) {
				_, n := utf8.DecodeRuneInString(text[i+1:])
				body[i] = body[i+1+n]
			}
		case '%':
			if newline < len(text) && (!math || !newlineBlank) {
				body[i] = body[newline+1]
			}
		case '\n':
			newlineBlank = math && pandocHeadingBlankAfter(ctx, text, i)
			if !newlineBlank {
				body[i] = body[i+1]
			}
			newline = i
		default:
			body[i] = body[i+1]
		}
	}
	if math {
		b.mathGroups = body
	} else {
		b.texGroups = body
	}
	return body
}
func (b *pandocHeadingBoundaries) groupEnd(start int, math bool) int {
	body := b.groupBodies(math)
	if body[start+1] > 0 {
		return body[start+1]
	}
	return start
}
func (b *pandocHeadingBoundaries) texGroup(start int, optional bool) int {
	if !optional {
		return b.groupEnd(start, false)
	}
	if b.texOptions == nil {
		body := b.groupBodies(false)
		text, ctx := b.text, b.ctx
		b.texOptions = meteredSlice[int](ctx, len(text)+1)
		newline := len(text)
		for i := len(text) - 1; i >= 0; i-- {
			meterWork(ctx, 1)
			switch text[i] {
			case ']':
				b.texOptions[i] = i + 1
			case '{':
				if end := body[i+1]; end > 0 {
					b.texOptions[i] = b.texOptions[end]
				} else {
					b.texOptions[i] = b.texOptions[i+1]
				}
			case '\\':
				if i+1 < len(text) {
					_, n := utf8.DecodeRuneInString(text[i+1:])
					b.texOptions[i] = b.texOptions[i+1+n]
				}
			case '%':
				if newline < len(text) {
					b.texOptions[i] = b.texOptions[newline+1]
				}
			default:
				b.texOptions[i] = b.texOptions[i+1]
			}
			if text[i] == '\n' {
				newline = i
			}
		}
	}
	if b.texOptions[start+1] > 0 {
		return b.texOptions[start+1]
	}
	return start
}
func (b *pandocHeadingBoundaries) texEnd(start int) int {
	meterWork(b.ctx, 1)
	if end, ok := b.texResults[start]; ok {
		return end
	}
	if b.texResults == nil {
		meterWork(b.ctx, 1)
		b.texResults = make(map[int]int)
	}
	end := b.texEndUncached(start)
	meterWork(b.ctx, 1)
	b.texResults[start] = end
	return end
}
func (b *pandocHeadingBoundaries) texEndUncached(start int) int {
	ctx, text := b.ctx, b.text
	origin := start
again:
	i := start + 1
	if i == len(text) {
		return origin
	}
	r, n := utf8.DecodeRuneInString(text[i:])
	if !unicode.IsLetter(r) {
		return origin
	}
	i += n
	for i < len(text) {
		meterWork(ctx, 1)
		r, n = utf8.DecodeRuneInString(text[i:])
		if !unicode.IsLetter(r) {
			break
		}
		i += n
	}
	name := text[start+1 : i]
	// begin/end are dispatched as environments rather than arbitrary commands;
	// block commands cannot enter rawLaTeXInline's inline alternative.
	if name == "begin" {
		return b.texEnvironmentEnd(origin, i)
	}
	if name == "end" || name == "and" || pandocHeadingTeXBlockCommand(ctx, name) {
		return origin
	}
	if strings.HasPrefix(name, "start") && b.contextEnd(start) > start {
		return origin
	}
	for i < len(text) && (text[i] == ' ' || text[i] == '\t') {
		meterWork(ctx, 1)
		i++
	}
	if i < len(text) && text[i] == '*' {
		i++
		for i < len(text) && (text[i] == ' ' || text[i] == '\t') {
			meterWork(ctx, 1)
			i++
		}
	}
	meterWork(ctx, len(pandocHeadingTeXNoArgs)+len(name)+2)
	if strings.Contains(pandocHeadingTeXNoArgs, " "+name+" ") {
		for i+1 < len(text) && text[i:i+2] == "{}" {
			meterWork(ctx, 2)
			i += 2
		}
		return i
	}
	if name == "verb" || name == "Verb" {
		if i == len(text) {
			return origin
		}
		marker, size := utf8.DecodeRuneInString(text[i:])
		if attributeAlphaNum(marker) || unicode.IsSpace(marker) || marker == '\\' || marker == '%' || marker == '#' {
			return origin
		}
		i += size
		for i < len(text) {
			meterWork(ctx, 1)
			r, n := utf8.DecodeRuneInString(text[i:])
			if r == '\n' {
				return origin
			}
			i += n
			if r == marker {
				return i
			}
		}
		return origin
	}
	if name == "lstinline" || name == "mintinline" {
		for i < len(text) && text[i] == '[' {
			end := b.texGroup(i, true)
			if end == i {
				b.markTeXPending(origin, ']')
				return origin
			}
			i = pandocHeadingTeXSpaces(ctx, text, end)
		}
		if name == "mintinline" {
			if i == len(text) || text[i] != '{' {
				return origin
			}
			end := b.texGroup(i, false)
			if end == i {
				return origin
			}
			i = end
		}
		if i == len(text) {
			return origin
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		if attributeAlphaNum(r) || r == '\\' || r == '%' || unicode.IsSpace(r) {
			return origin
		}
		closing := r
		if r == '{' {
			closing = '}'
		}
		for i += size; i < len(text); {
			meterWork(ctx, 1)
			r, size = utf8.DecodeRuneInString(text[i:])
			i += size
			if r == closing {
				return i
			}
		}
		b.markTeXPending(origin, closing)
		return origin
	}
	// Formatting commands consume one tok. The raw input alternative and
	// getRawCommand fallback consume optional arguments before braced arguments.
	meterWork(ctx, 300+len(name))
	one := strings.Contains(" emph textit textsl textsc textsf textmd textrm textup texttt textbf textnormal underline mbox hbox vbox textsuperscript textsubscript ensuremath ", " "+name+" ")
	if one {
		i = pandocHeadingTeXSpaces(ctx, text, i)
	}
	if !one {
		for i < len(text) && text[i] == '[' {
			end := b.texGroup(i, true)
			if end == i {
				b.markTeXPending(origin, ']')
				return origin
			}
			i = end
			for i < len(text) && (text[i] == ' ' || text[i] == '\t') {
				meterWork(ctx, 1)
				i++
			}
		}
	}
	if one && (i == len(text) || text[i] != '{') {
		if i == len(text) {
			return origin
		}
		if text[i] == '\\' {
			start = i
			goto again
		}
		r, n := utf8.DecodeRuneInString(text[i:])
		if strings.ContainsRune("#$%&~_^\\{}", r) {
			return origin
		}
		return i + n
	}
	count := 0
	for i < len(text) && text[i] == '{' {
		end := b.texGroup(i, false)
		if end == i {
			b.markTeXPending(origin, '}')
			return origin
		}
		i = end
		count++
		if one || name == "input" {
			break
		}
	}
	if name == "input" && count == 0 {
		return origin
	}
	for i+1 < len(text) && text[i:i+2] == "{}" {
		meterWork(ctx, 2)
		i += 2
	}
	return i
}

func pandocHeadingTeXBlockCommand(ctx context.Context, name string) bool {
	meterWork(ctx, len(pandocHeadingTeXBlockCommands)+len(name)+2)
	return strings.Contains(pandocHeadingTeXBlockCommands, " "+name+" ")
}

// Factual command names from the pinned LaTeX dispatch tables. Empty-brace
// suffixes are handled separately by rawLaTeXInline itself.
const pandocHeadingTeXBlockCommands = " AddToNoCaseChangeList DeclareCaseChangeEquivalent DeclareKeys DeclareLowercaseExclusions DeclareLowercaseMapping DeclareTitlecaseExclusions DeclareTitlecaseMapping DeclareUnknownKeyHandler DeclareUppercaseExclusions DeclareUppercaseMapping ExplSyntaxOff ExplSyntaxOn PackageError ProcessKeyOptions SetKeys ShowCommand ShowEnvironment addbibresource addcontentsline address addtocontents addtocounter author bibliography bibliographystyle blockcquote blockquote caption centerline chapter closing date dedication documentclass endinput epigraph extratitle fancybreak foreignblockcquote foreignblockquote framesubtitle frametitle frontispiece graphicspath hrule hyperdef hyphenblockcquote hyphenblockquote ignore include inputminted item listoffigures listoftables lowertitleback lstinputlisting makeglossary makeindex maketitle markboth markleft markright minisec newtheorem opening par paragraph parbox part pdfannot pdfstringdef pfbreak plainbreak plainfancybreak publishers raggedright rule section setdefaultlanguage setmainlanguage signature special strut subfile subject subparagraph subsection subsubsection subtitle theoremstyle title titleformat titlehead uppertitleback usepackage write "
const pandocHeadingTeXNoArgs = " AA AE BooleanFalse BooleanTrue Huge L LARGE LaTeX Large O OE P S TeX aa addabbrvspace adddot adddotspace ae backslash bar bshyp colonhyp copyright dothyp dots dq euro faCheck faClose flq flqq footnotesize frq frqq fshyp glq glqq grq grqq guillemetleft guillemetright guillemotleft guillemotright guilsinglleft guilsinglright hfil hfill hfilneg huge hyp hyphen i j l large ldots lq mdots normalsize o oe pounds qed quotedblbase quotesinglbase rq scriptsize sep sim slash small ss textasciicircum textasciitilde textbackslash textbaht textbigcircle textblank textbrokenbar textbullet textcentoldstyle textcopyright textdagger textdegree textdollar textdong textgreater textless textlira textmu textmusicalnote textonehalf textonequarter textparagraph textpertenthousand textpeso textquotedbl textquotedblleft textquotedblright textquoteleft textquoteright textquotesingle textregistered textsection textsterling textthreequarters textthreesuperior texttwosuperior textyen tiny today vdots vfil vfill vfilneg "

// Factual block categories from the pinned TagCategories helper.
func pandocHeadingHTMLBlockTag(name string) bool {
	switch name {
	case "!DOCTYPE", "?xml", "address", "article", "aside", "bibliolist", "blockquote", "body", "calloutlist", "canvas", "caption", "case", "caution", "center", "classsynopsis", "cmdsynopsis", "col", "colgroup", "dd", "default", "details", "dir", "div", "dl", "dt", "epigraph", "equation", "example", "fieldset", "figcaption", "figure", "footer", "form", "formalpara", "frameset", "funcsynopsis", "glosslist", "h1", "h2", "h3", "h4", "h5", "h6", "head", "header", "hgroup", "hr", "html", "important", "informalequation", "informalexample", "informalfigure", "informaltable", "isindex", "itemizedlist", "li", "literallayout", "main", "mediaobject", "menu", "meta", "msgset", "nav", "noframes", "note", "ol", "orderedlist", "output", "p", "para", "pre", "procedure", "programlisting", "programlistingco", "qandaset", "screen", "screenco", "screenshot", "script", "section", "segmentedlist", "sidebar", "simpara", "simplelist", "style", "summary", "switch", "synopsis", "table", "task", "tbody", "td", "textarea", "tfoot", "th", "thead", "tip", "title", "tr", "ul", "variablelist", "warning":
		return true
	}
	return false
}

func pandocHeadingBlankAfter(ctx context.Context, text string, at int) bool {
	at++
	for at < len(text) && (text[at] == ' ' || text[at] == '\t' || text[at] == '\r') {
		meterWork(ctx, 1)
		at++
	}
	return at < len(text) && text[at] == '\n'
}
func pandocHeadingTeXSpaces(ctx context.Context, text string, at int) int {
	for at < len(text) {
		meterWork(ctx, 1)
		if text[at] == '%' {
			for at < len(text) && text[at] != '\n' {
				meterWork(ctx, 1)
				at++
			}
			continue
		}
		if text[at] != ' ' && text[at] != '\t' && text[at] != '\n' && text[at] != '\r' {
			break
		}
		at++
	}
	return at
}
func (b *pandocHeadingBoundaries) texEnvironmentEnd(start, at int) int {
	ctx, text := b.ctx, b.text
	for at < len(text) && (text[at] == ' ' || text[at] == '\t') {
		meterWork(ctx, 1)
		at++
	}
	if at == len(text) || text[at] != '{' {
		return start
	}
	end := b.texGroup(at, false)
	if end == at {
		return start
	}
	name := text[at+1 : end-1]
	const environments = " displaymath math equation equation* gather gather* multline multline* eqnarray eqnarray* align align* alignat alignat* flalign flalign* dmath dmath* dgroup dgroup* darray darray* subequations "
	meterWork(ctx, len(environments)+len(name))
	if !strings.Contains(environments, " "+name+" ") {
		return start
	}
	b.indexTeXEnvironments()
	from := end
	candidates := b.texEnvs[name]
	for {
		at := sort.Search(len(candidates), func(i int) bool { meterWork(ctx, 1); return candidates[i][0] >= from })
		if at == len(candidates) {
			b.markTeXPending(start, -2)
			return start
		}
		comment := sort.Search(len(b.texComments), func(i int) bool { meterWork(ctx, 1); return b.texComments[i] >= from })
		if comment < len(b.texComments) && b.texComments[comment] < candidates[at][0] {
			from = b.commentLineEnd(b.texComments[comment]) + 1
			continue
		}
		return candidates[at][1]
	}
}

// Missing environment closers are indexed once, rather than searched again
// from each overlapping failed begin command. Queries retain comment context
// relative to their own begin, even when that begin followed a failed token.
func (b *pandocHeadingBoundaries) indexTeXEnvironments() {
	if b.texEnvs != nil {
		return
	}
	ctx, text := b.ctx, b.text
	meterWork(ctx, 1)
	b.texEnvs = make(map[string][][2]int)
	escaped := func(at int) bool {
		count := 0
		for at > 0 && text[at-1] == '\\' {
			meterWork(ctx, 1)
			count++
			at--
		}
		return count%2 == 1
	}
	for i := 0; i < len(text); i++ {
		meterWork(ctx, 1)
		if text[i] == '%' && !escaped(i) {
			b.texComments = meteredAppend(ctx, b.texComments, i)
		}
		if text[i] != '\\' || !strings.HasPrefix(text[i:], "\\end") || escaped(i) {
			continue
		}
		j := i + 4
		if j < len(text) {
			r, _ := utf8.DecodeRuneInString(text[j:])
			if unicode.IsLetter(r) {
				continue
			}
		}
		j = pandocHeadingTeXSpaces(ctx, text, j)
		if j == len(text) || text[j] != '{' {
			continue
		}
		end := b.texGroup(j, false)
		if end == j {
			continue
		}
		name := text[j+1 : end-1]
		meterWork(ctx, len(name)+1)
		b.texEnvs[name] = meteredAppend(ctx, b.texEnvs[name], [2]int{i, end})
	}
}

// The Markdown wrapper explicitly rejects a complete raw ConTeXt environment
// before asking the LaTeX reader to recognize a control sequence.
func pandocHeadingConTeXtCompletion(ctx context.Context, text string, at int) (string, int) {
	meterWork(ctx, 6)
	if !strings.HasPrefix(text[at:], "\\start") {
		return "", at
	}
	i := at + 6
	begin := i
	if i < len(text) && text[i] == '[' {
		i++
		begin = i
		for i < len(text) {
			meterWork(ctx, 1)
			r, n := utf8.DecodeRuneInString(text[i:])
			if !unicode.IsLetter(r) && !(r >= '0' && r <= '9') && r != ' ' && r != '\t' {
				break
			}
			i += n
		}
		if i < len(text) && text[i] == ']' {
			return text[begin-1 : i+1], i + 1
		}
		return "", at
	}
	for i < len(text) {
		meterWork(ctx, 1)
		r, n := utf8.DecodeRuneInString(text[i:])
		if !unicode.IsLetter(r) {
			break
		}
		i += n
	}
	if i > begin {
		return text[begin:i], i
	}
	return "", at
}
func pandocHeadingConTeXtEnd(ctx context.Context, text string, start int) int {
	b := pandocHeadingBoundaries{ctx: ctx, text: text}
	return b.contextEnd(start)
}

// Reverse evaluation knows each later nested guard's outcome. Successful
// nested guards consume all stop tokens inside them; failed nested starts stay
// ordinary bytes. Successor indexes remove consumed stops once globally and in
// each name's candidate list, preserving backtracking without suffix rescans.
func (b *pandocHeadingBoundaries) contextEnd(start int) int {
	ctx, text := b.ctx, b.text
	if b.contexts == nil {
		type opening struct{ at, after, name int }
		type named struct {
			text          string
			stops, parent []int
		}
		type stopping struct {
			at   int
			refs [][2]int
		}
		type trieNode struct {
			edges map[byte]int
			name  int
		}
		meterWork(ctx, 2)
		b.contexts = make(map[int]int)
		ids := make(map[string]int)
		var starts []opening
		var names []named
		for i := 0; i < len(text); i++ {
			meterWork(ctx, 1)
			if text[i] != '\\' {
				continue
			}
			name, after := pandocHeadingConTeXtCompletion(ctx, text, i)
			if after == i {
				continue
			}
			meterWork(ctx, len(name)+1)
			id, ok := ids[name]
			if !ok {
				id = len(names)
				ids[name] = id
				names = meteredAppend(ctx, names, named{text: name})
			}
			starts = meteredAppend(ctx, starts, opening{i, after, id})
		}
		trie := meteredAppend(ctx, []trieNode{}, trieNode{})
		for id, name := range names {
			at := 0
			for i := 0; i < len(name.text); i++ {
				meterWork(ctx, 1)
				if trie[at].edges == nil {
					meterWork(ctx, 1)
					trie[at].edges = make(map[byte]int)
				}
				next, ok := trie[at].edges[name.text[i]]
				if !ok {
					next = len(trie)
					trie[at].edges[name.text[i]] = next
					trie = meteredAppend(ctx, trie, trieNode{})
				}
				at = next
			}
			trie[at].name = id + 1
		}
		var stops []stopping
		for i := 0; i < len(text); i++ {
			meterWork(ctx, 1)
			if text[i] != '\\' || !strings.HasPrefix(text[i:], "\\stop") {
				continue
			}
			stop := stopping{at: i}
			node := 0
			index := len(stops)
			for j := i + 5; j < len(text); j++ {
				meterWork(ctx, 1)
				next, ok := trie[node].edges[text[j]]
				if !ok {
					break
				}
				node = next
				if trie[node].name > 0 {
					id := trie[node].name - 1
					local := len(names[id].stops)
					names[id].stops = meteredAppend(ctx, names[id].stops, index)
					stop.refs = meteredAppend(ctx, stop.refs, [2]int{id, local})
				}
			}
			stops = meteredAppend(ctx, stops, stop)
		}
		parents := func(n int) []int {
			out := meteredSlice[int](ctx, n+1)
			for i := range out {
				meterWork(ctx, 1)
				out[i] = i
			}
			return out
		}
		find := func(parent []int, at int) int {
			for parent[at] != at {
				meterWork(ctx, 1)
				parent[at] = parent[parent[at]]
				at = parent[at]
			}
			meterWork(ctx, 1)
			return at
		}
		global := parents(len(stops))
		for id := range names {
			names[id].parent = parents(len(names[id].stops))
		}
		for i := len(starts) - 1; i >= 0; i-- {
			meterWork(ctx, 1)
			opening := starts[i]
			name := &names[opening.name]
			at := sort.Search(len(name.stops), func(j int) bool { meterWork(ctx, 1); return stops[name.stops[j]].at >= opening.after })
			at = find(name.parent, at)
			end := 0
			if at < len(name.stops) {
				end = stops[name.stops[at]].at + 5 + len(name.text)
			}
			meterWork(ctx, 1)
			b.contexts[opening.at] = end
			if end == 0 {
				continue
			}
			first := sort.Search(len(stops), func(j int) bool { meterWork(ctx, 1); return stops[j].at >= opening.after })
			for at := find(global, first); at < len(stops) && stops[at].at < end; at = find(global, at) {
				meterWork(ctx, 1)
				for _, ref := range stops[at].refs {
					target := &names[ref[0]]
					target.parent[ref[1]] = find(target.parent, ref[1]+1)
				}
				global[at] = find(global, at+1)
			}
		}
	}
	meterWork(ctx, 1)
	if end := b.contexts[start]; end > start {
		return end
	}
	return start
}

func (b *pandocHeadingBoundaries) commentLineEnd(from int) int {
	if b.newlines == nil {
		b.newlines = meteredSlice[int](b.ctx, 0)
		for i := 0; i < len(b.text); i++ {
			meterWork(b.ctx, 1)
			if b.text[i] == '\n' {
				b.newlines = meteredAppend(b.ctx, b.newlines, i)
			}
		}
	}
	at := sort.Search(len(b.newlines), func(i int) bool { meterWork(b.ctx, 1); return b.newlines[i] > from })
	if at < len(b.newlines) {
		return b.newlines[at]
	}
	return len(b.text)
}

func (b *pandocHeadingBoundaries) markTeXPending(start int, closer rune) {
	if b.texPending == nil {
		meterWork(b.ctx, 1)
		b.texPending = make(map[int]rune)
	}
	meterWork(b.ctx, 1)
	b.texPending[start] = closer
}
