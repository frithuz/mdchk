package rules

import (
	"context"
	"errors"
	"strings"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/resource"
)

func missingLocalTargets(ctx context.Context, c engine.RuleContext) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 1, 0))
		if out.Err != nil {
			out = engine.RuleOutcome{Err: out.Err}
		}
	}()
	items, err := c.LocalLinks(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	switch items.Availability {
	case parser.Available:
	case parser.Unavailable:
		return engine.RuleOutcome{IncompleteReason: "Link destination context is unavailable; the complete MD057 inventory is withheld."}
	default:
		return engine.RuleOutcome{Err: parser.ErrModel}
	}
	checkImages := c.CheckImages()
	unavailable, oversized := false, false
	previous := 0
	for _, l := range items.Links {
		if out.Err = c.SectionCheckpoint(ctx, 18+int64(len(l.Destination)), 0); out.Err != nil {
			return out
		}
		// An Image inside a reference Link starts within the Link's envelope, so
		// source order constrains starts only.
		s := l.Span
		if !s.Present() || s.SourceToken() != c.Source().Token() || s.Start() < previous || s.End() < s.Start() || s.End() > c.Source().Len() {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		previous = s.Start()
		if l.Image && !checkImages {
			continue
		}
		// Remote, empty, fragment-only and query-only destinations name no
		// other local resource. A fragment never affects the path check.
		if classifyFragmentDestination(l.Destination) != fragmentOtherDocument {
			continue
		}
		resolved, err := c.ResolveExistence(ctx, l.Destination, l.Span)
		if err != nil {
			return engine.RuleOutcome{Err: err}
		}
		if resolved.Oversized {
			oversized = true
			continue
		}
		if !resolved.Available {
			unavailable = true
			continue
		}
		out.Existence = append(out.Existence, resolved.Resolution)
		if out.Err = c.SectionCheckpoint(ctx, 2, 1); out.Err != nil {
			return out
		}
		var message string
		switch resolved.Resolution.Outcome() {
		case resource.ExistenceMissing:
			message = "Local link target does not exist."
		case resource.ExistenceFileWithHint:
			message = "Local link target is a file, but the destination requires a directory."
		case resource.ExistenceIndexMissing:
			message = "Directory has no configured index file."
		case resource.ExistenceIndexIsDirectory:
			message = "Directory index path is a directory, not a file."
		default:
			continue
		}
		if len(out.Findings) == cap(out.Findings) {
			capacity := max(1, cap(out.Findings)*2)
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
		out.Findings = append(out.Findings, engine.Finding{Span: l.Span, Message: message})
	}
	var reasons []string
	if oversized {
		reasons = append(reasons, "A local link destination exceeds the path bounds and cannot be resolved.")
	}
	if unavailable {
		reasons = append(reasons, "Local link targets cannot be checked without resource context.")
	}
	out.IncompleteReason = strings.Join(reasons, " ")
	return out
}
