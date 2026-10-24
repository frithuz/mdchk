package parser

import (
	"context"
	"slices"
	"strings"
)

// frontmatterBoundary is a lexical result, not a parsed Markdown block or
// decoded metadata. Its zero value means no recognized header. A present region
// with an absent closer means an unterminated header. All spans belong to the
// indexed immutable source. Region includes the optional BOM and physical line
// endings; opener and closer exclude both. Content excludes delimiter lines.
type frontmatterBoundary struct {
	format                          string
	region, opener, content, closer Span
}

// Frontmatter is an immutable opaque header record owned by a parsed Document.
// It is separate from Root's ordinary body children and contributes no Markdown
// nodes, references or comments. Its zero value is absent. All source spans
// retain the original snapshot, including exact physical line endings.
type Frontmatter struct{ boundary frontmatterBoundary }

func (h Frontmatter) Format() string { return h.boundary.format }

// Span includes the optional BOM and both delimiter lines with their endings.
func (h Frontmatter) Span() Span { return h.boundary.region }

// Opener and Closer omit the BOM and physical ending, retaining trailing tabs
// and spaces. An absent Closer means the recognized header reaches EOF without
// termination; it never means an empty complete header.
func (h Frontmatter) Opener() Span { return h.boundary.opener }
func (h Frontmatter) Closer() Span { return h.boundary.closer }

// Content is the exact opaque payload between delimiter lines; an empty payload
// is a present zero-width span. No metadata decoding or normalization occurs.
func (h Frontmatter) Content() Span { return h.boundary.content }

// BodyOffset is the first original byte after the header, or EOF when unclosed.
func (h Frontmatter) BodyOffset() int { return h.boundary.region.End() }

// detectFrontmatter borrows already validated configuration fields without
// retaining or changing formats. Flavor is intentionally not an input: enabling
// recognition and selecting formats are independent of Markdown interpretation.
// The caller must perform source admission before constructing the line index.
// Public parsing invokes this only through explicit frontmatter options.
func detectFrontmatter(index LineIndex, enabled bool, formats []string) (frontmatterBoundary, error) {
	if !enabled || len(index.lines) == 0 {
		return frontmatterBoundary{}, nil
	}
	first := index.lines[0]
	start := first.start
	if strings.HasPrefix(index.source.content, "\xef\xbb\xbf") {
		start += 3
	}
	marker, err := frontmatterDelimiter(index.ctx, index.source.content[start:first.end])
	if err != nil {
		return frontmatterBoundary{}, err
	}
	format := ""
	switch marker {
	case "---":
		format = "yaml"
	case "+++":
		format = "toml"
	}
	if format == "" || !slices.Contains(formats, format) {
		return frontmatterBoundary{}, nil
	}
	header := frontmatterBoundary{
		format:  format,
		region:  index.span(0, index.source.Len()),
		opener:  index.span(start, first.end),
		content: index.span(first.next, index.source.Len()),
	}
	for _, line := range index.lines[1:] {
		marker, err := frontmatterDelimiter(index.ctx, index.source.content[line.start:line.end])
		if err != nil {
			return frontmatterBoundary{}, err
		}
		if format == "yaml" && (marker == "---" || marker == "...") || format == "toml" && marker == "+++" {
			header.region = index.span(0, line.next)
			header.content = index.span(first.next, line.start)
			header.closer = index.span(line.start, line.end)
			break
		}
	}
	return header, nil
}

func frontmatterDelimiter(ctx context.Context, text string) (string, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return "", err
		}
	}
	meterWork(ctx, len(text)+1)
	if len(text) < 3 {
		return "", nil
	}
	marker := text[:3]
	if marker != "---" && marker != "..." && marker != "+++" {
		return "", nil
	}
	for i := 3; i < len(text); i++ {
		if i&255 == 0 && scannerInterrupted(ctx) {
			return "", ctx.Err()
		}
		if text[i] != ' ' && text[i] != '\t' {
			return "", nil
		}
	}
	return marker, nil
}
