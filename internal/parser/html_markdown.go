package parser

import (
	"context"
	"errors"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"
)

// HTMLMarkdownCandidate retains a manual opening marker and its consumed extent.
// Extent is internal recognition evidence, not a diagnostic or an edit.
type HTMLMarkdownCandidate struct {
	Marker, Extent Span
	Kind           string
}

// HTMLMarkdown is atomic. Unavailable never publishes a provisional prefix.
type HTMLMarkdown struct {
	Candidates   []HTMLMarkdownCandidate
	Availability Availability
}

// HTMLMarkdownCandidates borrows only completed original production syntax and
// its actual source, options, profile, index and cumulative operation allowance.
// The engine supplies the conjunction of caller and originating controls.
func (d *Document) HTMLMarkdownCandidates(ctx context.Context, budget *OperationBudget, index LineIndex, options ParseOptions) (out HTMLMarkdown, err error) {
	if err = d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0); err != nil {
		return out, err
	}
	defer func() {
		err = errors.Join(err, d.headingProjectionCheckpoint(ctx, budget, index, options, 1, 0))
		if err != nil {
			out = HTMLMarkdown{}
		}
	}()
	return d.htmlMarkdownLocked(ctx, budget, index)
}

func (d *Document) htmlMarkdownLocked(ctx context.Context, budget *OperationBudget, index LineIndex) (out HTMLMarkdown, err error) {
	budget.mu.Lock()
	defer budget.mu.Unlock()
	defer func() {
		if stopped := recover(); stopped != nil {
			if limit, ok := stopped.(budgetStop); ok {
				copy := *limit.exceeded
				out, err = HTMLMarkdown{}, &copy
				return
			}
			panic(stopped)
		}
	}()
	work := &parserContext{Context: ctx, budget: &parseBudget{operation: budget}}
	c := htmlMarkdownCollector{ctx: work, source: index.source}
	err = c.collect(d.root)
	meterWork(work, 1)
	err = errors.Join(err, c.stopped, ctx.Err())
	if err != nil {
		return HTMLMarkdown{}, err
	}
	if c.unavailable {
		return HTMLMarkdown{Availability: Unavailable}, nil
	}
	return HTMLMarkdown{Candidates: c.candidates, Availability: Available}, nil
}

type htmlMarkdownCollector struct {
	ctx         context.Context
	source      Source
	raw         string
	candidates  []HTMLMarkdownCandidate
	unavailable bool
	stopped     error
}

// Bounded cancellation checks complement the sticky original work/node/time
// meter. Helpers retain the first stop; the producer withholds every prefix.
func (c *htmlMarkdownCollector) interrupted() bool {
	meterWork(c.ctx, 1)
	if c.stopped == nil {
		c.stopped = c.ctx.Err()
	}
	return c.stopped != nil
}

// Every retained capacity, including scalar/byte scratch, is reserved before
// allocation. Keep arithmetic wide and fail closed before crossing host bounds.
func htmlMarkdownSlice[T any](ctx context.Context, n int) []T {
	meterHTMLWork(ctx, int64(n))
	meterRecords(ctx, n)
	return make([]T, n)
}

func htmlMarkdownGrow[T any](ctx context.Context, dst []T) []T {
	meterWork(ctx, 1)
	if len(dst) < cap(dst) {
		return dst
	}
	capacity := addBounded(int64(cap(dst)), int64(cap(dst)))
	capacity = max(1, capacity)
	if capacity > int64(int(^uint(0)>>1)) {
		meterHTMLWork(ctx, math.MaxInt64)
		panic("parser: unrepresentable HTML Markdown capacity")
	}
	meterHTMLWork(ctx, addBounded(int64(len(dst)), addBounded(capacity, 1)))
	meterRecords(ctx, int(capacity))
	next := make([]T, len(dst), int(capacity))
	copy(next, dst)
	return next
}

func (c *htmlMarkdownCollector) collect(root *Node) error {
	meterWork(c.ctx, 16)
	if root == nil || !root.spec.Envelope.present || root.spec.Envelope.source != c.source || root.spec.Envelope.start != 0 || root.spec.Envelope.end != c.source.Len() {
		return ErrModel
	}
	var stack []*Node
	stack = htmlMarkdownGrow(c.ctx, stack)
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
		if n == nil || !contains(root.spec.Envelope, n.spec.Envelope) || n.spec.Envelope.end < n.spec.Envelope.start {
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
			continue // The complete owner already includes its private tails.
		}
		for i := len(n.spec.Children); ; {
			meterWork(c.ctx, 2)
			if i == 0 {
				break
			}
			i--
			stack = htmlMarkdownGrow(c.ctx, stack)
			meterRecords(c.ctx, 1)
			stack = append(stack, n.spec.Children[i])
		}
	}
}

// One record per logical byte also holds reverse indexes. Continuation bytes
// never become scalar or delimiter starts. This is derived scratch, not a new
// Source, LineIndex, parse input, model clone or authority token.
type htmlMarkdownByte struct {
	original, originalEnd int
	eligible, gap, line   bool
	escaped               bool
	r                     rune
	width, runEnd, pair   int
	bracket, nonblank     int
	destination, linkEnd  int
	lineEnd               int
}

type htmlMarkdownOwner struct {
	text string
	data []htmlMarkdownByte
}

func (c *htmlMarkdownCollector) owner(n *Node) (htmlMarkdownOwner, error) {
	meterWork(c.ctx, 16)
	if n.spec.Properties.HTML == nil {
		return htmlMarkdownOwner{}, ErrModel
	}
	logical := int64(0)
	for _, p := range n.spec.Content {
		meterWork(c.ctx, 1)
		logical = addBounded(logical, int64(len(p.Semantic.Value)))
	}
	if logical >= int64(int(^uint(0)>>1)) {
		meterHTMLWork(c.ctx, math.MaxInt64)
		return htmlMarkdownOwner{}, ErrModel
	}
	// Reserve the existing HTML lexer's retained indexes, caches, visited suffix
	// buffers and their copying before using it. Actual lexer probes remain metered.
	work, records := unclosedHTMLScratchCosts(logical, int64(len(n.spec.Content)))
	meterHTMLWork(c.ctx, work)
	for records > 0 {
		chunk := min(records, int64(int(^uint(0)>>1)))
		meterRecords(c.ctx, int(chunk))
		records -= chunk
	}
	data := htmlMarkdownSlice[htmlMarkdownByte](c.ctx, int(logical)+1)
	bytes := htmlMarkdownSlice[byte](c.ctx, int(logical))
	at, previous := 0, n.spec.Envelope.start
	for _, p := range n.spec.Content {
		meterWork(c.ctx, 24)
		if !p.Semantic.Present || !contains(n.spec.Envelope, p.Source) || p.Source.start < previous || p.Source.end < p.Source.start {
			return htmlMarkdownOwner{}, ErrModel
		}
		raw := c.source.content[p.Source.start:p.Source.end]
		meterHTMLWork(c.ctx, addBounded(int64(len(raw)), addBounded(int64(len(p.Semantic.Value)), 16)))
		switch p.Transformation {
		case Exact:
			if raw != p.Semantic.Value {
				return htmlMarkdownOwner{}, ErrModel
			}
		case Normalized:
			if raw != "\x00" || p.Semantic.Value != "\ufffd" {
				return htmlMarkdownOwner{}, ErrModel
			}
		case Escape:
			if raw != "\\|" || p.Semantic.Value != "|" {
				return htmlMarkdownOwner{}, ErrModel
			}
		case PartialTab:
			if raw != "\t" || len(p.Semantic.Value) == 0 || len(p.Semantic.Value) > 4 {
				return htmlMarkdownOwner{}, ErrModel
			}
			for _, b := range p.Semantic.Value {
				meterWork(c.ctx, 1)
				if b != ' ' {
					return htmlMarkdownOwner{}, ErrModel
				}
			}
		default:
			return htmlMarkdownOwner{}, ErrModel
		}
		meterWork(c.ctx, len(p.Semantic.Value))
		copy(bytes[at:], p.Semantic.Value)
		for j := range len(p.Semantic.Value) {
			meterWork(c.ctx, 8)
			if j%256 == 0 && c.interrupted() {
				return htmlMarkdownOwner{}, c.stopped
			}
			v := &data[at+j]
			v.original, v.originalEnd = p.Source.start, p.Source.end
			v.eligible = p.Transformation == Exact
			if v.eligible {
				v.original += j
				v.originalEnd = v.original + 1
			}
			v.gap = j == 0 && p.Source.start != previous
		}
		at += len(p.Semantic.Value)
		previous = p.Source.end
	}
	meterWork(c.ctx, len(bytes)) // the single final string copy
	text := string(bytes)
	line, slashes := true, 0
	for at := 0; at < len(text); {
		meterWork(c.ctx, 16)
		if err := c.ctx.Err(); err != nil {
			return htmlMarkdownOwner{}, err
		}
		r, width := utf8.DecodeRuneInString(text[at:])
		meterWork(c.ctx, width)
		if r == utf8.RuneError && width == 1 {
			return htmlMarkdownOwner{}, ErrModel
		}
		v := &data[at]
		v.r, v.width, v.line, v.escaped = r, width, line, slashes%2 != 0
		if v.gap {
			slashes = 0
			v.escaped = false
		}
		if r == '\n' || r == '\r' || unicode.IsControl(r) && r != '\t' && r != '\f' {
			for j := range width {
				meterWork(c.ctx, 1)
				data[at+j].eligible = false
			}
		}
		line = r == '\n' || r == '\r'
		if r == '\\' && v.eligible {
			slashes++
		} else {
			slashes = 0
		}
		at += width
	}
	end := len(text)
	for i := len(text) - 1; i >= 0; i-- {
		meterWork(c.ctx, 3)
		if i%256 == 0 && c.interrupted() {
			return htmlMarkdownOwner{}, c.stopped
		}
		if text[i] == '\n' || text[i] == '\r' {
			end = i
		}
		data[i].lineEnd = end
	}
	return htmlMarkdownOwner{text: text, data: data}, nil
}

func (c *htmlMarkdownCollector) protect(o htmlMarkdownOwner, start, end int) {
	for i := start; i < end; i++ {
		meterWork(c.ctx, 2)
		if i%256 == 0 && c.interrupted() {
			return
		}
		o.data[i].eligible = false
	}
}

// Only confirmed lexer openers may shield an unterminated suffix. Invalid <!
// prefixes remain ordinary raw-block payload, including later raw transitions.
func htmlMarkdownOpaqueOpen(ctx context.Context, text string) bool {
	if meteredHasPrefix(ctx, text, "<!--") || meteredHasPrefix(ctx, text, "<?") || meteredHasPrefix(ctx, text, "<![CDATA[") {
		return true
	}
	if !meteredHasPrefix(ctx, text, "<!") {
		return false
	}
	// Reserve the reached length, byte and ASCII-letter decision before probing.
	meterWork(ctx, 8)
	return len(text) > 2 && asciiLetter(text[2])
}

func (c *htmlMarkdownCollector) scan(n *Node) error {
	o, err := c.owner(n)
	if err != nil {
		return err
	}
	scanner := inlineScanner{ctx: c.ctx, text: o.text}
	meterWork(c.ctx, 1)
	meterRecords(c.ctx, 1)
	scanner.filterTagCache = make(map[int]int)
	form := 0
	if n.Kind() == HTMLBlock {
		meterWork(c.ctx, 2)
		if !n.spec.Properties.HTML.BlockForm.Present || n.spec.Properties.HTML.BlockForm.Value < 1 || n.spec.Properties.HTML.BlockForm.Value > 7 {
			return ErrModel
		}
		form = n.spec.Properties.HTML.BlockForm.Value
	}
	if n.Kind() == InlineHTML && c.raw != "" {
		meterHTMLWork(c.ctx, 2*int64(len(c.raw))+2*int64(len(n.rawTag))+8)
		if !n.rawClosing || !embeddedASCIIEqual(c.raw, n.rawTag) {
			return nil
		}
	}
	at := 0
	if form >= 2 && form <= 5 {
		token := [6]string{2: "-->", 3: "?>", 4: ">", 5: "]]>"}[form]
		meterHTMLWork(c.ctx, int64(len(o.text))+16)
		close := strings.Index(o.text, token)
		if close < 0 {
			return nil
		}
		at = close + len(token)
		c.protect(o, 0, at)
	}
	// Form 1's lexical lifetime and complete tag/attribute protection are a union.
	// Locate the parser's ANY4 terminator independently of token skipping, so a
	// quoted terminator can close raw protection without exposing its attribute.
	form1End := -1
	if form == 1 && c.raw == "" {
		for i := 0; i < len(o.text); i++ {
			meterWork(c.ctx, 2)
			if i%256 == 0 && c.interrupted() {
				return c.stopped
			}
			if o.text[i] != '<' {
				continue
			}
			meterWork(c.ctx, 128)
			for _, token := range []string{"</pre>", "</script>", "</style>", "</textarea>"} {
				if len(o.text)-i >= len(token) && embeddedASCIIEqual(o.text[i:i+len(token)], token) {
					form1End = i + len(token)
					break
				}
			}
			if form1End >= 0 {
				break
			}
		}
		if form1End < 0 {
			// A confirmed unclosed form1 protects its whole owner; retain the
			// original opening raw name for later owners, as other collectors do.
			meterWork(c.ctx, 128)
			start := 0
			for start < len(o.text) && (o.text[start] == ' ' || o.text[start] == '\t') {
				meterWork(c.ctx, 1)
				start++
			}
			for _, raw := range []string{"pre", "script", "style", "textarea"} {
				if len(o.text)-start > len(raw) && embeddedASCIIEqual(o.text[start+1:start+1+len(raw)], raw) {
					c.raw = raw
					break
				}
			}
			return nil
		}
		c.protect(o, 0, form1End)
	}
	for at < len(o.text) {
		meterWork(c.ctx, 16)
		if c.stopped != nil {
			return c.stopped
		}
		if err := c.ctx.Err(); err != nil {
			return err
		}
		if o.text[at] != '<' {
			if c.raw != "" {
				c.protect(o, at, at+1)
			}
			at++
			continue
		}
		meterWork(c.ctx, 8)
		nameStart := at + 1
		closing := nameStart < len(o.text) && o.text[nameStart] == '/'
		if closing {
			nameStart++
		}
		nameEnd := htmlTagNameEndContext(c.ctx, o.text, nameStart)
		named := nameEnd > nameStart
		end, _ := scanner.inlineHTMLEnd(at)
		// Known opaque suffixes protect their remainder. This does not turn a
		// confirmed raw lifetime into uncertainty or manufacture local transitions.
		meterWork(c.ctx, 16)
		opaque := htmlMarkdownOpaqueOpen(c.ctx, o.text[at:])
		if end == 0 && opaque {
			c.protect(o, at, len(o.text))
			break
		}
		if c.raw != "" {
			meterHTMLWork(c.ctx, int64(len(c.raw))+int64(nameEnd-nameStart)+20)
			if end > 0 && named && closing && embeddedASCIIEqual(o.text[nameStart:nameEnd], c.raw) {
				c.raw = ""
				c.protect(o, at, end)
				at = end
				continue
			}
			if end > 0 {
				c.protect(o, at, end)
				at = end
			} else {
				c.protect(o, at, max(at+1, nameEnd))
				at = max(at+1, nameEnd)
			}
			continue
		}
		if end == 0 {
			meterWork(c.ctx, 8)
			plausible := named && (nameEnd == len(o.text) || o.text[nameEnd] == '/' || o.text[nameEnd] == '>' || strings.ContainsRune(" \t\r\n", rune(o.text[nameEnd])))
			if plausible {
				c.unavailable = true
				return nil
			}
			at = max(at+1, nameEnd)
			continue
		}
		c.protect(o, at, end)
		if named && !closing && (form1End < 0 || at >= form1End) {
			meterWork(c.ctx, 128)
			for _, raw := range []string{"pre", "script", "style", "textarea"} {
				if embeddedASCIIEqual(o.text[nameStart:nameEnd], raw) {
					c.raw = raw
					break
				}
			}
		}
		at = end
	}
	if n.Kind() != HTMLBlock {
		return nil
	}
	// Protection is complete before candidate masking, so an ATX finding cannot
	// hide later raw transitions needed on subsequent physical lines.
	c.codeProtection(o)
	c.imageProtection(o)
	err = c.recognize(o)
	return errors.Join(err, c.stopped)
}

func (c *htmlMarkdownCollector) islands(o htmlMarkdownOwner, visit func(int, int)) {
	for at := 0; at < len(o.text); {
		meterWork(c.ctx, 4)
		if at%256 == 0 && c.interrupted() {
			return
		}
		if !o.data[at].eligible {
			at++
			continue
		}
		start := at
		at++
		for at < len(o.text) {
			meterWork(c.ctx, 3)
			if at%256 == 0 && c.interrupted() {
				return
			}
			if !o.data[at].eligible || o.data[at].gap {
				break
			}
			at++
		}
		visit(start, at)
		if c.stopped != nil {
			return
		}
	}
}

// Maximal original runs and odd escaping are indexed once per island. A suffix
// of an escaped or overlong run never becomes another delimiter start.
func (c *htmlMarkdownCollector) runs(o htmlMarkdownOwner, a, b int) {
	for i := a; i < b; {
		meterWork(c.ctx, 8)
		if i%256 == 0 && c.interrupted() {
			return
		}
		r := o.text[i]
		if r != '*' && r != '_' && r != '`' && r != '#' {
			i++
			continue
		}
		end := i + 1
		for end < b && o.text[end] == r {
			meterWork(c.ctx, 2)
			if end%256 == 0 && c.interrupted() {
				return
			}
			end++
		}
		o.data[i].runEnd = end
		i = end
	}
}

func (c *htmlMarkdownCollector) codeProtection(o htmlMarkdownOwner) {
	c.islands(o, func(a, b int) {
		c.runs(o, a, b)
		meterWork(c.ctx, 1)
		meterRecords(c.ctx, 1)
		next := make(map[int]int)
		for i := b - 1; i >= a; i-- {
			meterWork(c.ctx, 6)
			if i%256 == 0 && c.interrupted() {
				return
			}
			v := &o.data[i]
			if o.text[i] != '`' || v.runEnd == 0 || v.escaped {
				continue
			}
			width := v.runEnd - i
			meterWork(c.ctx, 2)
			v.pair = next[width]
			if _, ok := next[width]; !ok {
				meterWork(c.ctx, 1)
				meterRecords(c.ctx, 1)
			}
			next[width] = i + 1
		}
		for i := a; i < b; {
			meterWork(c.ctx, 6)
			if i%256 == 0 && c.interrupted() {
				return
			}
			v := o.data[i]
			if o.text[i] != '`' || v.runEnd == 0 {
				i++
				continue
			}
			if v.escaped {
				i = v.runEnd
				continue
			}
			end := v.runEnd
			if v.pair > 0 && v.pair-1 > end {
				end = o.data[v.pair-1].runEnd
			}
			c.protect(o, i, end)
			i = end
		}
	})
}

// Reverse indexes make failed nested labels and long flat destinations bounded:
// no opener rescans an already visited suffix. Encoded indexes use zero as absent.
func (c *htmlMarkdownCollector) links(o htmlMarkdownOwner, a, b int) {
	bracket, nonblank, destination := 0, 0, 0
	for i := b - 1; i >= a; i-- {
		meterWork(c.ctx, 16)
		if i%256 == 0 && c.interrupted() {
			return
		}
		v := &o.data[i]
		v.bracket, v.nonblank = bracket, nonblank
		if v.width != 0 {
			if !IsUnicodeWhitespace(v.r) {
				nonblank = i + 1
			}
			if (v.r == '[' || v.r == ']') && !v.escaped {
				bracket = i + 1
			}
			if v.r == ')' {
				destination = i + 1
			} else if unicode.Is(unicode.Zs, v.r) || unicode.Is(unicode.Zl, v.r) || unicode.Is(unicode.Zp, v.r) || unicode.IsControl(v.r) || strings.ContainsRune("\\(<>`", v.r) {
				destination = 0
			}
		}
		v.destination = destination
	}
	for i := a; i < b; i++ {
		meterWork(c.ctx, 12)
		if i%256 == 0 && c.interrupted() {
			return
		}
		v := &o.data[i]
		v.linkEnd = 0
		if o.text[i] != '[' || v.escaped || v.bracket == 0 {
			continue
		}
		close := v.bracket - 1
		if o.text[close] != ']' || close+2 >= b || o.text[close+1] != '(' || v.nonblank == 0 || v.nonblank-1 >= close {
			continue
		}
		if end := o.data[close+2].destination; end != 0 {
			v.linkEnd = end
		}
	}
}

func (c *htmlMarkdownCollector) imageProtection(o htmlMarkdownOwner) {
	c.islands(o, func(a, b int) {
		c.links(o, a, b)
		for i := a; i < b; i++ {
			meterWork(c.ctx, 6)
			if i%256 == 0 && c.interrupted() {
				return
			}
			if o.text[i] == '[' && o.data[i].linkEnd != 0 && i > a && o.text[i-1] == '!' && !o.data[i-1].escaped {
				end := o.data[i].linkEnd
				c.protect(o, i-1, end)
				i = end - 1
			}
		}
	})
}

func (c *htmlMarkdownCollector) recognize(o htmlMarkdownOwner) error {
	var failure error
	consumed := 0
	c.islands(o, func(a, b int) {
		if failure != nil {
			return
		}
		c.runs(o, a, b)
		c.links(o, a, b)
		next := 0
		for i := b - 1; i >= a; i-- {
			meterWork(c.ctx, 5)
			if i%256 == 0 && c.interrupted() {
				return
			}
			v := &o.data[i]
			if (o.text[i] == '*' || o.text[i] == '_') && v.runEnd > 0 && !v.escaped {
				v.pair = next
				next = i + 1
			}
		}
		for i := a; i < b; {
			meterWork(c.ctx, 16)
			if failure = c.ctx.Err(); failure != nil {
				return
			}
			if i < consumed {
				i = min(consumed, b)
				continue
			}
			v := o.data[i]
			if v.line {
				hash := i
				for hash < b && hash-i < 3 && o.text[hash] == ' ' {
					meterWork(c.ctx, 2)
					hash++
				}
				meterWork(c.ctx, 12)
				if hash < b && o.text[hash] == '#' && !o.data[hash].escaped {
					end := o.data[hash].runEnd
					if end > hash && end-hash <= 6 && (end == len(o.text) || o.text[end] == '\r' || o.text[end] == '\n' || end < b && (o.text[end] == ' ' || o.text[end] == '\t')) {
						consumed = v.lineEnd
						failure = c.add(o, hash, end, consumed, "heading")
						if failure != nil {
							return
						}
						i = min(consumed, b)
						continue
					}
				}
			}
			if o.text[i] == '[' && v.linkEnd != 0 && !v.escaped {
				failure = c.add(o, i, i+1, v.linkEnd, "link")
				if failure != nil {
					return
				}
				i = v.linkEnd
				continue
			}
			if o.text[i] == '*' || o.text[i] == '_' {
				if v.runEnd > i {
					if !v.escaped && v.runEnd-i <= 2 && v.pair != 0 {
						close := v.pair - 1
						z := o.data[close]
						meterWork(c.ctx, 24)
						if o.text[close] == o.text[i] && z.runEnd-close == v.runEnd-i && close > v.runEnd {
							before, after := rune(' '), rune(' ')
							if i > a {
								before, _ = utf8.DecodeLastRuneInString(o.text[a:i])
							}
							if z.runEnd < b {
								after = o.data[z.runEnd].r
							}
							meterWork(c.ctx, 32)
							opens, _ := delimiterFlanking(o.text[i], before, o.data[v.runEnd].r)
							inner, _ := utf8.DecodeLastRuneInString(o.text[a:close])
							_, closes := delimiterFlanking(o.text[i], inner, after)
							if opens && closes {
								failure = c.add(o, i, v.runEnd, z.runEnd, "emphasis")
								if failure != nil {
									return
								}
								i = z.runEnd
								continue
							}
						}
					}
					i = v.runEnd
					continue
				}
			}
			i++
		}
	})
	return failure
}

func (c *htmlMarkdownCollector) add(o htmlMarkdownOwner, start, markerEnd, end int, kind string) error {
	meterWork(c.ctx, 48)
	if start < 0 || markerEnd <= start || end < markerEnd || end > len(o.text) {
		return ErrModel
	}
	s := o.data[start].original
	m := o.data[markerEnd-1].originalEnd
	e := o.data[end-1].originalEnd
	if m-s != markerEnd-start || e < m || e > c.source.Len() || len(c.candidates) != 0 && s < c.candidates[len(c.candidates)-1].Extent.end {
		return ErrModel
	}
	meterWork(c.ctx, markerEnd-start)
	if c.source.content[s:m] != o.text[start:markerEnd] {
		return ErrModel
	}
	marker := Span{source: c.source, start: s, end: m, present: true}
	extent := Span{source: c.source, start: s, end: e, present: true}
	c.candidates = htmlMarkdownGrow(c.ctx, c.candidates)
	meterRecords(c.ctx, 1)
	c.candidates = append(c.candidates, HTMLMarkdownCandidate{Marker: marker, Extent: extent, Kind: kind})
	return nil
}
