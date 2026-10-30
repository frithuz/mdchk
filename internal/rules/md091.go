package rules

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

func markdownInsideHTML(ctx context.Context, c engine.RuleContext) (out engine.RuleOutcome) {
	// This distinct outer gate runs AFTER both facade and comparison publication.
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 1, 0))
		if out.Err != nil {
			out.Findings = nil
			out.IncompleteReason = ""
		}
	}()
	items, err := c.HTMLMarkdownCandidates(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	if items.Availability != parser.Available {
		return engine.RuleOutcome{IncompleteReason: "HTML Markdown candidate context is unavailable; the complete MD091 inventory is withheld."}
	}
	return compareHTMLMarkdown(ctx, c, items.Candidates)
}

func compareHTMLMarkdown(ctx context.Context, c engine.RuleContext, items []parser.HTMLMarkdownCandidate) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 1, 0))
		if out.Err != nil {
			out.Findings = nil
		}
	}()
	previous := 0
	for i := 0; ; i++ {
		if out.Err = c.SectionCheckpoint(ctx, 1, 0); out.Err != nil {
			return out
		}
		if i == len(items) {
			break
		}
		f := items[i]
		s, extent := f.Marker, f.Extent
		if out.Err = c.SectionCheckpoint(ctx, 64, 0); out.Err != nil {
			return out
		}
		if !s.Present() || !extent.Present() || s.SourceToken() != c.Source().Token() || extent.SourceToken() != s.SourceToken() || s.Start() < previous || extent.Start() != s.Start() || extent.End() < s.End() || extent.End() > c.Source().Len() || s.End() <= s.Start() || s.End()-s.Start() > 6 {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		text, err := c.Source().Text(s)
		if err != nil {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		if out.Err = c.SectionCheckpoint(ctx, int64(len(text))+8, 0); out.Err != nil {
			return out
		}
		valid := false
		switch f.Kind {
		case "link":
			valid = text == "[" && extent.End()-extent.Start() >= 5
		case "emphasis":
			valid = (text == "*" || text == "_" || text == "**" || text == "__") && extent.End()-extent.Start() > 2*(s.End()-s.Start())
		case "heading":
			valid = true
			for _, b := range text {
				if b != '#' {
					valid = false
				}
			}
		}
		if !valid {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		previous = extent.End()
		if len(out.Findings) == cap(out.Findings) {
			// Bound doubling by remaining admitted facts before host arithmetic.
			capacity := len(items)
			if cap(out.Findings) <= len(items)/2 {
				capacity = max(1, cap(out.Findings)*2)
			}
			if out.Err = c.SectionCheckpoint(ctx, int64(len(out.Findings))+int64(capacity)+1, int64(capacity)); out.Err != nil {
				return out
			}
			next := make([]engine.Finding, len(out.Findings), capacity)
			copy(next, out.Findings)
			out.Findings = next
		}
		if out.Err = c.SectionCheckpoint(ctx, 1, 1); out.Err != nil {
			return out
		}
		out.Findings = append(out.Findings, engine.Finding{Span: s, Message: "Markdown syntax is literal inside this HTML block."})
	}
	return out
}
