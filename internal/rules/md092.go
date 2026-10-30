package rules

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

func conflictMarkers(ctx context.Context, c engine.RuleContext) (out engine.RuleOutcome) {
	// The outer publication gate is distinct from producer, facade and comparison.
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 1, 0))
		if out.Err != nil {
			out.Findings = nil
			out.IncompleteReason = ""
		}
	}()
	items, err := c.ConflictMarkers(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	if items.Availability != parser.Available {
		return engine.RuleOutcome{IncompleteReason: "Conflict-marker protection context is unavailable; the complete MD092 inventory is withheld."}
	}
	return compareConflictMarkers(ctx, c, items.Markers)
}

func compareConflictMarkers(ctx context.Context, c engine.RuleContext, markers []parser.Span) (out engine.RuleOutcome) {
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
		if i == len(markers) {
			break
		}
		s := markers[i]
		if out.Err = c.SectionCheckpoint(ctx, 32, 0); out.Err != nil {
			return out
		}
		if !s.Present() || s.SourceToken() != c.Source().Token() || s.Start() < previous || s.End() < s.Start() || s.End()-s.Start() < 7 || s.End() > c.Source().Len() {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		// Validate the whole admitted run and its physical boundaries, so an
		// invalid late fact withdraws every earlier provisional finding.
		before, err := c.Source().Span(max(0, s.Start()-1), s.Start())
		if err != nil {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		if out.Err = c.SectionCheckpoint(ctx, 4, 0); out.Err != nil {
			return out
		}
		prefix, err := c.Source().Text(before)
		if err != nil || s.Start() > 0 && prefix != "\n" && prefix != "\r" {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		text, err := c.Source().Text(s)
		if err != nil {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		if out.Err = c.SectionCheckpoint(ctx, 1, 0); out.Err != nil {
			return out
		}
		b := text[0]
		if b != '<' && b != '>' && b != '|' && b != '=' {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		for at := 0; ; at++ {
			if out.Err = c.SectionCheckpoint(ctx, 1, 0); out.Err != nil {
				return out
			}
			if at == len(text) {
				break
			}
			if out.Err = c.SectionCheckpoint(ctx, 1, 0); out.Err != nil {
				return out
			}
			if text[at] != b {
				return engine.RuleOutcome{Err: parser.ErrModel}
			}
		}
		if out.Err = c.SectionCheckpoint(ctx, 4, 0); out.Err != nil {
			return out
		}
		after, err := c.Source().Span(s.End(), s.End()+min(1, c.Source().Len()-s.End()))
		if err != nil {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		suffix, err := c.Source().Text(after)
		if err != nil || len(suffix) != 0 && suffix[0] == b {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		previous = s.End()
		if len(out.Findings) == cap(out.Findings) {
			capacity := len(markers)
			if cap(out.Findings) <= len(markers)/2 {
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
		out.Findings = append(out.Findings, engine.Finding{Span: s, Message: "Unresolved version-control conflict marker."})
	}
	return out
}
