package rules

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/resource"
)

func unresolvedFragmentTargets(ctx context.Context, c engine.RuleContext) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 1, 0))
		if out.Err != nil {
			out = engine.RuleOutcome{Err: out.Err}
		}
	}()
	items, err := c.FragmentLinks(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	switch items.Availability {
	case parser.Available:
	case parser.Unavailable:
		return engine.RuleOutcome{IncompleteReason: "Link destination context is unavailable; the complete MD051 inventory is withheld."}
	default:
		return engine.RuleOutcome{Err: parser.ErrModel}
	}
	type check struct {
		link     parser.FragmentLink
		invalid  bool
		name     string
		resolved *resource.FragmentResolution
	}
	var checks []check
	crossFile, oversized, needAnchors := false, false, false
	previous := 0
	for _, l := range items.Links {
		if out.Err = c.SectionCheckpoint(ctx, 18+int64(len(l.Destination)), 0); out.Err != nil {
			return out
		}
		s := l.Span
		if !s.Present() || s.SourceToken() != c.Source().Token() || s.Start() < previous || s.End() < s.Start() || s.End() > c.Source().Len() {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		previous = s.End()
		switch classifyFragmentDestination(l.Destination) {
		case fragmentRemote:
		case fragmentOtherDocument:
			// An empty fragment addresses the other document itself: nothing to resolve.
			if _, fragment, _ := strings.Cut(l.Destination, "#"); fragment == "" {
				continue
			}
			resolved, err := c.ResolveFragment(ctx, l.Destination, l.Span)
			if err != nil {
				return engine.RuleOutcome{Err: err}
			}
			if resolved.Oversized {
				oversized = oversized || c.CrossFile()
				continue
			}
			if !resolved.Available {
				crossFile = crossFile || c.CrossFile()
				continue
			}
			out.Fragments = append(out.Fragments, resolved.Resolution)
			if out.Err = c.SectionCheckpoint(ctx, 2, 1); out.Err != nil {
				return out
			}
			checks = append(checks, check{link: l, resolved: &resolved.Resolution})
		case fragmentSameDocument:
			_, fragment, _ := strings.Cut(l.Destination, "#")
			name, decodeErr := url.PathUnescape(fragment)
			if decodeErr == nil && !utf8.ValidString(name) {
				decodeErr = errors.New("invalid UTF-8")
			}
			if decodeErr == nil && name == "" {
				continue
			}
			if out.Err = c.SectionCheckpoint(ctx, 2, 1); out.Err != nil {
				return out
			}
			needAnchors = true
			checks = append(checks, check{link: l, invalid: decodeErr != nil, name: name})
		}
	}
	var reasons []string
	if len(checks) != 0 {
		var occurrences []resource.AnchorOccurrence
		availability := resource.Available
		if needAnchors {
			var err error
			if occurrences, availability, err = c.AnchorOccurrences(ctx); err != nil {
				return engine.RuleOutcome{Err: err}
			}
		}
		if needAnchors && availability != resource.Available {
			reasons = append(reasons, "Anchor context is unavailable or uncertain.")
		} else {
			targets := make(map[string]struct{}, len(occurrences))
			for _, o := range occurrences {
				if out.Err = c.SectionCheckpoint(ctx, int64(len(o.ID))+2, 1); out.Err != nil {
					return out
				}
				if o.Usable {
					targets[o.ID] = struct{}{}
				}
			}
			for _, k := range checks {
				if out.Err = c.SectionCheckpoint(ctx, int64(len(k.name))+2, 0); out.Err != nil {
					return out
				}
				message := "Fragment target is not defined in this document."
				if k.resolved != nil {
					switch k.resolved.Outcome() {
					case resource.FragmentAbsentTarget:
						message = "Fragment target is not defined in the referenced document."
					case resource.FragmentMalformed:
						message = "Fragment has invalid percent-encoding and cannot name a target."
					default:
						continue
					}
				} else if k.invalid {
					message = "Fragment has invalid percent-encoding and cannot name a target."
				} else if _, found := targets[k.name]; found {
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
				out.Findings = append(out.Findings, engine.Finding{Span: k.link.Span, Message: message})
			}
		}
	}
	if oversized {
		reasons = append(reasons, "A fragment link destination exceeds the path bounds and cannot be resolved.")
	}
	if crossFile {
		reasons = append(reasons, "Cross-file fragment targets cannot be checked without resource context.")
	}
	out.IncompleteReason = strings.Join(reasons, " ")
	return out
}

type fragmentDestination int

const (
	fragmentSameDocument fragmentDestination = iota
	fragmentOtherDocument
	fragmentRemote
)

// classifyFragmentDestination inspects the raw destination only up to its first
// fragment marker. Remote and protocol-relative references are never fetched. A
// nonempty path is another document: without resource context a spelling that
// might name this document cannot be proven to do so.
func classifyFragmentDestination(destination string) fragmentDestination {
	before, _, _ := strings.Cut(destination, "#")
	if strings.HasPrefix(before, "//") {
		return fragmentRemote
	}
	for i := 0; i < len(before); i++ {
		b := before[i]
		switch {
		case b == ':':
			if i > 1 {
				return fragmentRemote
			}
			return fragmentOtherDocument
		case b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || i > 0 && (b >= '0' && b <= '9' || b == '+' || b == '-' || b == '.'):
		default:
			i = len(before)
		}
	}
	path, _, _ := strings.Cut(before, "?")
	if path == "" {
		return fragmentSameDocument
	}
	return fragmentOtherDocument
}
