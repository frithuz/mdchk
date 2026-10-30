package rules

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

func embeddedHTML(ctx context.Context, c engine.RuleContext) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 1, 0))
		if out.Err != nil {
			out.Findings = nil
			out.IncompleteReason = ""
		}
	}()
	items, err := c.EmbeddedHTMLTags(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	allowed, err := c.EmbeddedHTMLAllowedTags(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	if items.Availability != parser.Available {
		return engine.RuleOutcome{IncompleteReason: "Embedded HTML tag context is unavailable; the complete MD033 inventory is withheld."}
	}
	return compareEmbeddedHTML(ctx, c, items.Tags, allowed)
}

func compareEmbeddedHTML(ctx context.Context, c engine.RuleContext, items []parser.EmbeddedHTMLTag, allowed []string) (out engine.RuleOutcome) {
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
		s, n := f.Tag, f.Name
		if out.Err = c.SectionCheckpoint(ctx, 96, 0); out.Err != nil {
			return out
		}
		if !s.Present() || !n.Present() || s.SourceToken() != c.Source().Token() || n.SourceToken() != s.SourceToken() || s.Start() < previous || s.End() > c.Source().Len() || s.End()-s.Start() < 3 || n.Start() < s.Start()+1 || n.End() <= n.Start() || n.End() >= s.End() {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		tag, e := c.Source().Text(s)
		if e != nil {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		name, e := c.Source().Text(n)
		if e != nil {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		begin := 1
		if tag[begin] == '/' {
			begin++
		}
		if n.Start() != s.Start()+begin || tag[0] != '<' || tag[len(tag)-1] != '>' {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		if out.Err = c.SectionCheckpoint(ctx, int64(len(name))+8, 0); out.Err != nil {
			return out
		}
		for j := 0; j < len(name); j++ {
			b := name[j]
			if !(b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' || j > 0 && (b >= '0' && b <= '9' || b == '-')) {
				return engine.RuleOutcome{Err: parser.ErrModel}
			}
		}
		// The byte after the full name must be a grammar boundary; this validates
		// association without reparsing a physical envelope containing prefix gaps.
		at := n.End() - s.Start()
		b := tag[at]
		if b != '>' && b != '/' && b != ' ' && b != '\t' && b != '\r' && b != '\n' {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		previous = s.End()
		permitted := false
		for j := 0; ; j++ {
			if out.Err = c.SectionCheckpoint(ctx, 2, 0); out.Err != nil {
				return out
			}
			if j == len(allowed) {
				break
			}
			candidate := allowed[j]
			if len(candidate) != len(name) {
				continue
			}
			if out.Err = c.SectionCheckpoint(ctx, 4*int64(len(name))+4, 0); out.Err != nil {
				return out
			}
			equal := true
			for k := range name {
				a, b := name[k], candidate[k]
				if a >= 'A' && a <= 'Z' {
					a += 'a' - 'A'
				}
				if b >= 'A' && b <= 'Z' {
					b += 'a' - 'A'
				}
				if a != b {
					equal = false
					break
				}
			}
			if equal {
				permitted = true
				break
			}
		}
		if permitted {
			continue
		}
		if out.Err = c.SectionCheckpoint(ctx, 1, 0); out.Err != nil {
			return out
		}
		if len(out.Findings) == cap(out.Findings) {
			capacity := min(len(items), max(1, cap(out.Findings)*2))
			if out.Err = c.SectionCheckpoint(ctx, int64(len(out.Findings)+capacity)+1, int64(capacity)); out.Err != nil {
				return out
			}
			next := make([]engine.Finding, len(out.Findings), capacity)
			copy(next, out.Findings)
			out.Findings = next
		}
		if out.Err = c.SectionCheckpoint(ctx, 1, 1); out.Err != nil {
			return out
		}
		out.Findings = append(out.Findings, engine.Finding{Span: s, Message: "Embedded HTML tag is not allowed."})
	}
	return out
}
