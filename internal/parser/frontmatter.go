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

// detectFrontmatter borrows already validated configuration fields without
// retaining or changing formats. Flavor is intentionally not an input: enabling
// recognition and selecting formats are independent of Markdown interpretation.
// The caller must perform source admission before constructing the line index.
// This helper does not enable recognition in any production parser entry point.
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
