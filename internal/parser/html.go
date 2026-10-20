package parser

import (
	"context"
	"strings"
)

// The active form is fixed at opening. Only the current line is searched for a
// terminator. Logical lines acquire original-source segments without prefixes;
// closure or normal EOF freezes the node once.
type htmlBlock struct {
	start, end int
	form       int
	content    []Segment
}

func (p *blockParser) htmlOpening(line blockLine, paragraphOpen bool) int {
	meterWork(p.ctx, 1)
	if line.indent >= 4 || line.first != '<' {
		return 0
	}
	s := p.index.source.content[line.contentStart:line.end]
	nameEnd := htmlTagNameEndContext(p.ctx, s, 1)
	if nameEnd > 1 && htmlRawTag(meteredString(p.ctx, s[1:nameEnd])) && (nameEnd == len(s) || htmlSpace(s[nameEnd]) || s[nameEnd] == '>') {
		return 1
	}
	switch {
	case meteredHasPrefix(p.ctx, s, "<!--"):
		return 2
	case meteredHasPrefix(p.ctx, s, "<?"):
		return 3
	case len(s) > 2 && s[1] == '!' && htmlLetter(s[2]):
		return 4
	case meteredHasPrefix(p.ctx, s, "<![CDATA["):
		return 5
	}
	start := 1
	if len(s) > 1 && s[1] == '/' {
		start++
	}
	end := htmlTagNameEndContext(p.ctx, s, start)
	if end > start && htmlBlockTag(meteredString(p.ctx, s[start:end])) && (end == len(s) || htmlSpace(s[end]) || s[end] == '>' || meteredHasPrefix(p.ctx, s[end:], "/>")) {
		return 6
	}
	if !paragraphOpen && completeHTMLTagContext(p.ctx, s) {
		return 7
	}
	return 0
}

func (p *blockParser) appendHTMLLine(line blockLine) error {
	meterWork(p.ctx, 1)
	p.html.end = line.next
	p.html.content = p.appendRawLine(p.html.content, line)
	s := p.index.source.content[line.start:line.end]
	closed := false
	switch p.html.form {
	case 1:
		// Any of these closers terminates form 1, regardless of its opener.
		for i := 0; i < len(s); i++ {
			meterWork(p.ctx, 1)
			if s[i] == '<' {
				for _, tag := range []string{"</pre>", "</script>", "</style>", "</textarea>"} {
					meterWork(p.ctx, 1)
					if len(s)-i >= len(tag) && meteredEqualFold(p.ctx, s[i:i+len(tag)], tag) {
						closed = true
					}
				}
			}
		}
	case 2:
		closed = meteredContains(p.ctx, s, "-->")
	case 3:
		closed = meteredContains(p.ctx, s, "?>")
	case 4:
		closed = meteredContains(p.ctx, s, ">")
	case 5:
		closed = meteredContains(p.ctx, s, "]]>")
	}
	if closed {
		return p.closeHTML()
	}
	return nil
}

func (p *blockParser) closeHTML() error {
	meterWork(p.ctx, 1)
	h := p.html
	if h == nil {
		return nil
	}
	node, err := meteredNode(p.ctx, NodeSpec{
		Kind: HTMLBlock, Envelope: p.index.span(h.start, h.end),
		Content:    h.content,
		Properties: Properties{HTML: &HTMLProperties{BlockForm: Some(h.form)}},
	})
	if err != nil {
		return err
	}
	p.attach(node)
	p.html = nil
	return nil
}

func htmlLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func htmlSpace(c byte) bool  { return c == ' ' || c == '\t' }

func htmlTagNameEndContext(ctx context.Context, s string, start int) int {
	meterWork(ctx, 1)
	if start >= len(s) || !htmlLetter(s[start]) {
		return start
	}
	i := start + 1
	for i < len(s) && (htmlLetter(s[i]) || s[i] >= '0' && s[i] <= '9' || s[i] == '-') {
		meterWork(ctx, 1)
		i++
	}
	return i
}

func htmlRawTag(s string) bool {
	return strings.EqualFold(s, "pre") || strings.EqualFold(s, "script") || strings.EqualFold(s, "style") || strings.EqualFold(s, "textarea")
}

func htmlBlockTag(s string) bool {
	switch strings.ToLower(s) {
	case "address", "article", "aside", "base", "basefont", "blockquote", "body", "caption", "center", "col", "colgroup", "dd", "details", "dialog", "dir", "div", "dl", "dt", "fieldset", "figcaption", "figure", "footer", "form", "frame", "frameset", "h1", "h2", "h3", "h4", "h5", "h6", "head", "header", "hr", "html", "iframe", "legend", "li", "link", "main", "menu", "menuitem", "nav", "noframes", "ol", "optgroup", "option", "p", "param", "search", "section", "summary", "table", "tbody", "td", "tfoot", "th", "thead", "title", "tr", "track", "ul":
		return true
	}
	return false
}

// Form 7 requires a complete tag on this physical line, with only space/tab
// afterward. Attribute parsing advances once through the candidate, without
// backtracking or accepting the more permissive grammar of a browser HTML parser.
func completeHTMLTagContext(ctx context.Context, s string) bool {
	meterWork(ctx, 1)
	i := 1
	closing := i < len(s) && s[i] == '/'
	if closing {
		i++
	}
	start := i
	i = htmlTagNameEndContext(ctx, s, i)
	if i == start || !closing && htmlRawTag(s[start:i]) {
		return false
	}
	for i < len(s) {
		meterWork(ctx, 1)
		beforeSpace := i
		for i < len(s) && htmlSpace(s[i]) {
			meterWork(ctx, 1)
			i++
		}
		if !closing && i < len(s) && s[i] == '/' {
			i++
			break
		}
		if i == len(s) || s[i] == '>' || closing {
			break
		}
		if beforeSpace == i || !(htmlLetter(s[i]) || s[i] == '_' || s[i] == ':') {
			return false
		}
		i++
		for i < len(s) && (htmlLetter(s[i]) || s[i] >= '0' && s[i] <= '9' || meteredContainsRune(ctx, "_.:-", rune(s[i]))) {
			meterWork(ctx, 1)
			i++
		}
		// Whitespace belongs to a value only when an equals sign follows it.
		endName := i
		for i < len(s) && htmlSpace(s[i]) {
			meterWork(ctx, 1)
			i++
		}
		if i == len(s) || s[i] != '=' {
			i = endName
			continue
		}
		i++
		for i < len(s) && htmlSpace(s[i]) {
			meterWork(ctx, 1)
			i++
		}
		if i == len(s) {
			return false
		}
		if s[i] == '\'' || s[i] == '"' {
			quote := s[i]
			i++
			for i < len(s) && s[i] != quote {
				meterWork(ctx, 1)
				i++
			}
			if i == len(s) {
				return false
			}
			i++
		} else {
			startValue := i
			for i < len(s) && !htmlSpace(s[i]) && !meteredContainsRune(ctx, "\"'=<>`", rune(s[i])) {
				meterWork(ctx, 1)
				i++
			}
			if i == startValue {
				return false
			}
		}
	}
	if i >= len(s) || s[i] != '>' {
		return false
	}
	i++
	for i < len(s) && htmlSpace(s[i]) {
		meterWork(ctx, 1)
		i++
	}
	return i == len(s)
}
