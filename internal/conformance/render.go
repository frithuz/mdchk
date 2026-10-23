// Package conformance is test infrastructure, not a production HTML API.
// It renders supplied source-bound models; it never recognizes Markdown.
package conformance

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/parser"
)

// Result is the adapter's test-only, versioned outcome. HTML is meaningful only
// after a complete render. Interrupted execution is not unsupported syntax.
type Result struct {
	SchemaVersion int    `json:"schema_version"`
	Status        string `json:"status"`
	HTML          string `json:"html"`
	Reason        string `json:"reason,omitempty"`
	Message       string `json:"message,omitempty"`
}

// Evaluate consumes actual parser outcomes, rather than inferring completeness
// from an empty tree. It borrows immutable snapshots and does not change them.
func Evaluate(r parser.ParseResult) Result {
	return evaluate(r, nil, nil)
}

func evaluate(r parser.ParseResult, ctx context.Context, budget *parser.OperationBudget) Result {
	out := Result{SchemaVersion: 1}
	switch {
	case r.Failure != nil:
		out.Status, out.Reason, out.Message = "error", string(r.Failure.Code), r.Failure.Message
	case r.Interruption != "":
		out.Status, out.Reason = "interrupted", string(r.Interruption)
	case r.Status == parser.Incomplete && r.AST == parser.Partial && r.Document != nil && len(r.Uncertainty) > 0:
		out.Status, out.Reason = "pending", string(r.Uncertainty[0].Reason)
		for _, u := range r.Uncertainty {
			if u.Reason != parser.SyntaxUnimplemented {
				out.Status, out.Reason = "error", "invalid-outcome"
			}
		}
	case r.Status != parser.Complete || r.AST != parser.Available || r.Document == nil || r.Document.Root() == nil || len(r.Uncertainty) != 0 || r.Phases.Blocks != parser.PhaseComplete || r.Phases.Inlines != parser.PhaseComplete:
		out.Status, out.Reason = "error", "invalid-outcome"
	default:
		// The document must be bound to the same snapshot as the parse result.
		text, err := r.Source.Text(r.Document.Root().Span())
		if budget != nil {
			if err := budget.ChargeWork(int64(r.Source.Len())*64 + 128); err != nil {
				out.Status, out.Reason, out.Message = "error", "adapter-budget", err.Error()
				break
			}
			if err := budget.ChargeNodes(int64(r.Source.Len()) + 1); err != nil {
				out.Status, out.Reason, out.Message = "error", "adapter-budget", err.Error()
				break
			}
		}
		references := r.Document.References()
		if err != nil || !utf8.ValidString(text) || references.Availability != parser.Available || references.LookupAvailability != parser.Available {
			out.Status, out.Reason = "error", "invalid-outcome"
			break
		}
		html, err := renderDocument(r.Document, ctx, budget)
		if err != nil {
			out.Status, out.Reason, out.Message = "error", "render-metadata", err.Error()
		} else {
			out.Status, out.HTML = "rendered", html
		}
	}
	return out
}

// RenderDocument also accepts constructed model fixtures. Such a render is not
// evidence that Parse recognizes the corresponding Markdown. Errors discard all
// partial HTML. Missing semantic metadata is never guessed from raw spelling.
func RenderDocument(d *parser.Document) (string, error) {
	return renderDocument(d, nil, nil)
}

func renderDocument(d *parser.Document, ctx context.Context, budget *parser.OperationBudget) (string, error) {
	if d == nil {
		return "", fmt.Errorf("missing document")
	}
	root := d.Root()
	if root == nil {
		return "", fmt.Errorf("missing document root")
	}
	r := renderer{source: d.Source(), ctx: ctx, budget: budget}
	r.out.budget = budget
	if err := r.node(root, false); err != nil {
		return "", err
	}
	if r.out.err != nil {
		return "", r.out.err
	}
	return r.out.String(), nil
}

type renderer struct {
	source parser.Source
	out    boundedHTML
	ctx    context.Context
	budget *parser.OperationBudget
}

// Test process output is finite even for constructed models.
const maxOutputBytes = 16 << 20

type boundedHTML struct {
	strings.Builder
	err    error
	budget *parser.OperationBudget
}

func (b *boundedHTML) WriteString(s string) (int, error) {
	if b.err != nil {
		return 0, b.err
	}
	if b.budget != nil {
		if err := b.budget.ChargeWork(int64(len(s)) + 1); err != nil {
			b.err = err
			return 0, err
		}
	}
	if len(s) > maxOutputBytes-b.Len() {
		b.err = fmt.Errorf("adapter output exceeds %d bytes", maxOutputBytes)
		return 0, b.err
	}
	return b.Builder.WriteString(s)
}
func (b *boundedHTML) Write(p []byte) (int, error) { return b.WriteString(string(p)) }
func (b *boundedHTML) WriteByte(c byte) error      { _, e := b.WriteString(string([]byte{c})); return e }

// Errors are sticky and checked at the enclosing render boundary. These helpers
// cannot expose successful partial output even when an early append fails.
func (b *boundedHTML) append(s string)   { _, _ = b.WriteString(s) }
func (b *boundedHTML) appendByte(c byte) { _ = b.WriteByte(c) }

var escapeHTML = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;")

func literal(n *parser.Node) (string, error) {
	v := n.Properties().Literal
	if !v.Present || !utf8.ValidString(v.Value) {
		return "", fmt.Errorf("%s: missing or invalid semantic literal", n.Kind())
	}
	return v.Value, nil
}

func value(v parser.Value) (string, error) {
	if !v.Semantic.Present || !utf8.ValidString(v.Semantic.Value) {
		return "", fmt.Errorf("missing or invalid semantic value")
	}
	return v.Semantic.Value, nil
}

// Encode UTF-8 bytes, preserving URI punctuation and existing percent escapes.
// Attribute escaping is a separate step, after URL encoding.
func url(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'()*+,-./:;=?@_~", rune(c)) {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		}
	}
	return b.String()
}

func (r *renderer) children(n *parser.Node) error {
	for _, c := range n.Children() {
		if err := r.node(c, false); err != nil {
			return err
		}
	}
	return nil
}

func (r *renderer) wrapped(n *parser.Node, open, close string) error {
	r.out.append(open)
	if err := r.children(n); err != nil {
		return err
	}
	r.out.append(close)
	return nil
}

func (r *renderer) node(n *parser.Node, tight bool) error {
	if r.ctx != nil {
		if err := r.ctx.Err(); err != nil {
			return err
		}
	}
	if r.budget != nil {
		// Content aggregate copies are charged on every actual visit, including
		// container mappings; allowance exhaustion remains sticky after parsing.
		cost := int64(128) + int64(n.Span().End()-n.Span().Start())*64
		if err := r.budget.ChargeWork(cost); err != nil {
			return err
		}
	}
	if r.out.err != nil {
		return r.out.err
	}
	p := n.Properties()
	switch n.Kind() {
	case parser.DocumentKind:
		return r.children(n) // reference definitions are not block children
	case parser.Paragraph:
		if !tight {
			r.out.append("<p>")
		}
		if p.Task != nil {
			r.out.append("<input ")
			if p.Task.Checked {
				r.out.append("checked=\"\" ")
			}
			r.out.append("disabled=\"\" type=\"checkbox\"> ")
		}
		if err := r.children(n); err != nil {
			return err
		}
		if !tight {
			r.out.append("</p>\n")
		}
		return nil
	case parser.Heading:
		if p.Heading == nil {
			return fmt.Errorf("heading: missing level")
		}
		return r.wrapped(n, fmt.Sprintf("<h%d>", p.Heading.Level), fmt.Sprintf("</h%d>\n", p.Heading.Level))
	case parser.ThematicBreak:
		r.out.append("<hr />\n")
	case parser.BlockQuote:
		return r.wrapped(n, "<blockquote>\n", "</blockquote>\n")
	case parser.List:
		if p.List == nil || !p.List.Tight.Present || p.List.Ordered && !p.List.Start.Present {
			return fmt.Errorf("list: missing tightness/start")
		}
		tag := "ul"
		if p.List.Ordered {
			tag = "ol"
		}
		r.out.append("<" + tag)
		if p.List.Ordered && p.List.Start.Value != 1 {
			r.out.append(fmt.Sprintf(" start=\"%d\"", p.List.Start.Value))
		}
		r.out.append(">\n")
		for _, item := range n.Children() {
			if item.Kind() != parser.ListItem {
				return fmt.Errorf("list: non-item child")
			}
			if err := r.node(item, p.List.Tight.Value); err != nil {
				return err
			}
		}
		r.out.append("</" + tag + ">\n")
	case parser.ListItem:
		children := n.Children()
		r.out.append("<li>")
		if len(children) > 0 && (!tight || children[0].Kind() != parser.Paragraph) {
			r.out.appendByte('\n')
		}
		for i, c := range children {
			if err := r.node(c, tight && c.Kind() == parser.Paragraph); err != nil {
				return err
			}
			if tight && c.Kind() == parser.Paragraph && i+1 < len(children) {
				r.out.appendByte('\n')
			}
		}
		r.out.append("</li>\n")
	case parser.CodeBlock:
		if p.Code == nil {
			return fmt.Errorf("code block: missing style")
		}
		text, err := literal(n)
		if err != nil {
			return err
		}
		r.out.append("<pre><code")
		if p.Code.Info != nil {
			info, err := value(*p.Code.Info)
			if err != nil {
				return err
			}
			// CommonMark info words use its whitespace definition, not all
			// Unicode IsSpace characters.
			words := strings.FieldsFunc(info, parser.IsUnicodeWhitespace)
			if len(words) > 0 {
				r.out.append(" class=\"language-" + r.escaped(words[0]) + "\"")
			}
		}
		r.out.append(">" + r.escaped(text) + "</code></pre>\n")
	case parser.HTMLBlock, parser.InlineHTML:
		var raw strings.Builder
		if n.Content() == nil {
			return fmt.Errorf("raw HTML: missing source content")
		}
		filteredIndex := 0
		for _, segment := range n.Content() {
			text, err := r.source.Text(segment.Source)
			if err != nil || !utf8.ValidString(text) {
				return fmt.Errorf("raw HTML: invalid source segment")
			}
			// Raw spelling is protected, but CommonMark's mandatory NUL
			// replacement also applies inside HTML. This serialization never
			// changes the original snapshot or decodes entities/backslashes.
			normalized := strings.ReplaceAll(text, "\x00", "\ufffd")
			switch segment.Transformation {
			case parser.Exact:
				// Apply the mandatory substitution even when only original
				// source spelling was supplied by the model fixture/provider.
			case parser.Normalized:
				if !segment.Semantic.Present || segment.Semantic.Value != normalized {
					return fmt.Errorf("raw HTML: unknown normalization")
				}
			case parser.Escape:
				if text != "\\|" || !segment.Semantic.Present || segment.Semantic.Value != "|" {
					return fmt.Errorf("raw HTML: invalid table escape")
				}
				normalized = "|"
			case parser.PartialTab:
				// Only a residual leading tab from container indentation can
				// contribute spaces to block HTML. Other transformations and
				// inline HTML still require their existing explicit mappings.
				spaces := segment.Semantic.Value
				atLineStart := raw.Len() == 0 || strings.HasSuffix(raw.String(), "\n") || strings.HasSuffix(raw.String(), "\r")
				if n.Kind() != parser.HTMLBlock || text != "\t" || !segment.Semantic.Present || len(spaces) < 1 || len(spaces) > 3 || strings.Trim(spaces, " ") != "" || !atLineStart {
					return fmt.Errorf("raw HTML: invalid residual tab")
				}
				normalized = spaces
			default:
				return fmt.Errorf("raw HTML: unsupported transformation %q", segment.Transformation)
			}
			if p.HTML != nil {
				added := 0
				for filteredIndex < len(p.HTML.FilteredTags) {
					f := p.HTML.FilteredTags[filteredIndex]
					at := f.Affected.Start()
					if at >= segment.Source.End() {
						break
					}
					if at < segment.Source.Start() {
						return fmt.Errorf("filtered angle outside content")
					}
					if text[at-segment.Source.Start()] != '<' || segment.Transformation != parser.Exact && segment.Transformation != parser.Normalized {
						return fmt.Errorf("filtered angle mapping")
					}
					before := strings.ReplaceAll(text[:at-segment.Source.Start()], "\x00", "\ufffd")
					offset := len(before) + added
					if r.budget != nil {
						if err := r.budget.ChargeWork(int64(len(before)) + int64(len(normalized))); err != nil {
							return err
						}
					}
					normalized = normalized[:offset] + "&lt;" + normalized[offset+1:]
					added += 3
					filteredIndex++
				}
			}
			raw.WriteString(normalized)
		}
		if p.HTML != nil && filteredIndex != len(p.HTML.FilteredTags) {
			return fmt.Errorf("unmapped filtered angles")
		}
		text := raw.String()
		r.out.append(text)
		if n.Kind() == parser.HTMLBlock && !strings.HasSuffix(text, "\n") {
			r.out.appendByte('\n')
		}
	case parser.Text, parser.CodeSpan:
		text, err := literal(n)
		if err != nil {
			return err
		}
		if n.Kind() == parser.CodeSpan {
			text = "<code>" + r.escaped(text) + "</code>"
		} else {
			text = r.escaped(text)
		}
		r.out.append(text)
	case parser.SoftBreak:
		r.out.appendByte('\n')
	case parser.HardBreak:
		r.out.append("<br />\n")
	case parser.Emphasis:
		return r.wrapped(n, "<em>", "</em>")
	case parser.Strike:
		return r.wrapped(n, "<del>", "</del>")
	case parser.Table:
		return r.table(n)
	case parser.Strong:
		return r.wrapped(n, "<strong>", "</strong>")
	case parser.Link, parser.Image:
		if p.Link == nil || p.Link.Resolution != parser.Available {
			return fmt.Errorf("link/image: unavailable resolution")
		}
		destination, err := value(p.Link.Destination)
		if err != nil {
			return err
		}
		title := ""
		if p.Link.Title != nil {
			t, err := value(*p.Link.Title)
			if err != nil {
				return err
			}
			title = " title=\"" + r.escaped(t) + "\""
		}
		if n.Kind() == parser.Link {
			return r.wrapped(n, "<a href=\""+r.escaped(r.encodedURL(destination))+"\""+title+">", "</a>")
		}
		alt, err := plain(n)
		if err != nil {
			return err
		}
		r.out.append("<img src=\"" + r.escaped(r.encodedURL(destination)) + "\" alt=\"" + r.escaped(alt) + "\"" + title + " />")
	default:
		return fmt.Errorf("unsupported model kind %q", n.Kind())
	}
	return nil
}

func plain(n *parser.Node) (string, error) {
	var b strings.Builder
	for _, c := range n.Children() {
		switch c.Kind() {
		case parser.Text, parser.CodeSpan:
			text, err := literal(c)
			if err != nil {
				return "", err
			}
			b.WriteString(text)
		case parser.SoftBreak, parser.HardBreak:
			b.WriteByte('\n')
		case parser.Emphasis, parser.Strong, parser.Strike, parser.Link, parser.Image:
			text, err := plain(c)
			if err != nil {
				return "", err
			}
			b.WriteString(text)
		case parser.InlineHTML: // HTML tags do not contribute to plain alt text.
		default:
			return "", fmt.Errorf("invalid image label child %q", c.Kind())
		}
	}
	return b.String(), nil
}

func (r *renderer) table(n *parser.Node) error {
	p := n.Properties().Table
	if p == nil {
		return fmt.Errorf("table metadata missing")
	}
	rows := n.Children()
	r.out.append("<table>\n<thead>\n")
	for i, row := range rows {
		rp := row.Properties().Row
		if rp == nil {
			return fmt.Errorf("table row metadata missing")
		}
		if rp.Role == "delimiter" {
			continue
		}
		if i == 2 {
			r.out.append("<tbody>\n")
		}
		r.out.append("<tr>\n")
		cells := row.Children()
		tag := "td"
		if rp.Role == "header" {
			tag = "th"
		}
		for j := 0; j < p.Width; j++ {
			if r.out.err != nil {
				return r.out.err
			}
			r.out.append("<" + tag)
			if p.Alignments[j] != parser.AlignNone {
				r.out.append(" align=\"" + string(p.Alignments[j]) + "\"")
			}
			r.out.append(">")
			if j < len(cells) {
				if err := r.children(cells[j]); err != nil {
					return err
				}
			}
			r.out.append("</" + tag + ">\n")
		}
		r.out.append("</tr>\n")
		if i == 0 {
			r.out.append("</thead>\n")
		}
	}
	if len(rows) > 2 {
		r.out.append("</tbody>\n")
	}
	r.out.append("</table>\n")
	return nil
}

// Bound and reserve temporary URL/HTML expansion before allocating it. Reference
// values can be much larger than their source use's envelope.
func (r *renderer) escaped(text string) string {
	if r.out.err != nil {
		return ""
	}
	size := int64(len(text))
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '&':
			size += 4
		case '<', '>':
			size += 3
		case '"':
			size += 5
		}
	}
	if size > maxOutputBytes {
		r.out.err = fmt.Errorf("adapter HTML temporary exceeds limit")
		return ""
	}
	if r.budget != nil {
		if err := r.budget.ChargeWork(int64(len(text))*2 + size); err != nil {
			r.out.err = err
			return ""
		}
	}
	return escapeHTML.Replace(text)
}
func (r *renderer) encodedURL(text string) string {
	if r.out.err != nil {
		return ""
	}
	size := int64(0)
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'()*+,-./:;=?@_~", rune(c)) {
			size++
		} else {
			size += 3
		}
	}
	if size > maxOutputBytes {
		r.out.err = fmt.Errorf("adapter URL temporary exceeds limit")
		return ""
	}
	if r.budget != nil {
		if err := r.budget.ChargeWork(int64(len(text))*2 + size); err != nil {
			r.out.err = err
			return ""
		}
	}
	return url(text)
}
