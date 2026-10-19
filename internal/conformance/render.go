// Package conformance is test infrastructure, not a production HTML API.
// It renders supplied source-bound models; it never recognizes Markdown.
package conformance

import (
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
		references := r.Document.References()
		if err != nil || !utf8.ValidString(text) || references.Availability != parser.Available || references.LookupAvailability != parser.Available {
			out.Status, out.Reason = "error", "invalid-outcome"
			break
		}
		html, err := RenderDocument(r.Document)
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
	if d == nil {
		return "", fmt.Errorf("missing document")
	}
	root := d.Root()
	if root == nil {
		return "", fmt.Errorf("missing document root")
	}
	r := renderer{source: d.Source()}
	if err := r.node(root, false); err != nil {
		return "", err
	}
	return r.out.String(), nil
}

type renderer struct {
	source parser.Source
	out    strings.Builder
}

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
	r.out.WriteString(open)
	if err := r.children(n); err != nil {
		return err
	}
	r.out.WriteString(close)
	return nil
}

func (r *renderer) node(n *parser.Node, tight bool) error {
	p := n.Properties()
	switch n.Kind() {
	case parser.DocumentKind:
		return r.children(n) // reference definitions are not block children
	case parser.Paragraph:
		if tight {
			return r.children(n)
		}
		return r.wrapped(n, "<p>", "</p>\n")
	case parser.Heading:
		if p.Heading == nil {
			return fmt.Errorf("heading: missing level")
		}
		return r.wrapped(n, fmt.Sprintf("<h%d>", p.Heading.Level), fmt.Sprintf("</h%d>\n", p.Heading.Level))
	case parser.ThematicBreak:
		r.out.WriteString("<hr />\n")
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
		r.out.WriteString("<" + tag)
		if p.List.Ordered && p.List.Start.Value != 1 {
			fmt.Fprintf(&r.out, " start=\"%d\"", p.List.Start.Value)
		}
		r.out.WriteString(">\n")
		for _, item := range n.Children() {
			if item.Kind() != parser.ListItem {
				return fmt.Errorf("list: non-item child")
			}
			if err := r.node(item, p.List.Tight.Value); err != nil {
				return err
			}
		}
		r.out.WriteString("</" + tag + ">\n")
	case parser.ListItem:
		children := n.Children()
		r.out.WriteString("<li>")
		if len(children) > 0 && (!tight || children[0].Kind() != parser.Paragraph) {
			r.out.WriteByte('\n')
		}
		for i, c := range children {
			if err := r.node(c, tight && c.Kind() == parser.Paragraph); err != nil {
				return err
			}
			if tight && c.Kind() == parser.Paragraph && i+1 < len(children) {
				r.out.WriteByte('\n')
			}
		}
		r.out.WriteString("</li>\n")
	case parser.CodeBlock:
		if p.Code == nil {
			return fmt.Errorf("code block: missing style")
		}
		text, err := literal(n)
		if err != nil {
			return err
		}
		r.out.WriteString("<pre><code")
		if p.Code.Info != nil {
			info, err := value(*p.Code.Info)
			if err != nil {
				return err
			}
			// CommonMark info words use its whitespace definition, not all
			// Unicode IsSpace characters.
			words := strings.FieldsFunc(info, parser.IsUnicodeWhitespace)
			if len(words) > 0 {
				r.out.WriteString(" class=\"language-" + escapeHTML.Replace(words[0]) + "\"")
			}
		}
		r.out.WriteString(">" + escapeHTML.Replace(text) + "</code></pre>\n")
	case parser.HTMLBlock, parser.InlineHTML:
		var raw strings.Builder
		if n.Content() == nil {
			return fmt.Errorf("raw HTML: missing source content")
		}
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
			raw.WriteString(normalized)
		}
		text := raw.String()
		r.out.WriteString(text)
		if n.Kind() == parser.HTMLBlock && !strings.HasSuffix(text, "\n") {
			r.out.WriteByte('\n')
		}
	case parser.Text, parser.CodeSpan:
		text, err := literal(n)
		if err != nil {
			return err
		}
		if n.Kind() == parser.CodeSpan {
			text = "<code>" + escapeHTML.Replace(text) + "</code>"
		} else {
			text = escapeHTML.Replace(text)
		}
		r.out.WriteString(text)
	case parser.SoftBreak:
		r.out.WriteByte('\n')
	case parser.HardBreak:
		r.out.WriteString("<br />\n")
	case parser.Emphasis:
		return r.wrapped(n, "<em>", "</em>")
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
			title = " title=\"" + escapeHTML.Replace(t) + "\""
		}
		if n.Kind() == parser.Link {
			return r.wrapped(n, "<a href=\""+escapeHTML.Replace(url(destination))+"\""+title+">", "</a>")
		}
		alt, err := plain(n)
		if err != nil {
			return err
		}
		r.out.WriteString("<img src=\"" + escapeHTML.Replace(url(destination)) + "\" alt=\"" + escapeHTML.Replace(alt) + "\"" + title + " />")
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
		case parser.Emphasis, parser.Strong, parser.Link, parser.Image:
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
