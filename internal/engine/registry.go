// Package engine runs read-only rules on immutable original snapshots. It has no
// filesystem, reporting, process-exit, correction-planning or write dependency.
package engine

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/frithuz/mdchk/internal/parser"
)

type FixSupport string

const (
	NoFix          FixSupport = "none"
	PlannedFix     FixSupport = "planned"
	ImplementedFix FixSupport = "implemented"
)

// Metadata describes catalog knowledge independently of executable availability.
// RequiresSyntax gates the entire callback on affirmative complete parser data.
// AppliesTo is an optional pure predicate over explicit document context. False
// proves inapplicability; it must not infer absence from missing context.
type Metadata struct {
	ID             string
	Name           string
	DefaultEnabled bool
	Options        OptionSchema
	Fix            FixSupport
	RequiresSyntax bool
	AppliesTo      func(DocumentContext) bool
}
type Registration struct {
	ID    string
	Check Check
}
type Check func(context.Context, RuleContext) RuleOutcome

// Finding spans must belong to the supplied original Source and end at scalar
// boundaries. FixEligible is a positive per-finding assessment, not metadata.
type Finding struct {
	Message     string
	Span        parser.Span
	FixEligible bool
}

// RuleOutcome requires IncompleteReason for unavailable prerequisites. Reliable
// partial findings can accompany it. Cancellation/limits are separate causes.
type RuleOutcome struct {
	Findings         []Finding
	IncompleteReason string
	Err              error
}

type Registry struct {
	metadata map[string]Metadata
	checks   map[string]Check
	ids      []string
}

var canonicalID = regexp.MustCompile(`^MD[0-9]{3}$`)

func validID(id string) bool {
	return canonicalID.MatchString(id) && id != "MD000" && !slices.Contains([]string{"MD002", "MD006", "MD008", "MD015", "MD016", "MD017"}, id)
}

// NewRegistry freezes catalog schemas and registration maps. No package-global
// mutable registry exists. Metadata returns defensive copies. MD094 is a gate
// attribution entry and cannot have a normal executable implementation.
func NewRegistry(catalog []Metadata, implementations []Registration) (*Registry, error) {
	r := &Registry{metadata: make(map[string]Metadata), checks: make(map[string]Check)}
	for _, m := range catalog {
		if !validID(m.ID) || strings.TrimSpace(m.Name) == "" {
			return nil, fmt.Errorf("invalid rule metadata %q", m.ID)
		}
		if _, ok := r.metadata[m.ID]; ok {
			return nil, fmt.Errorf("duplicate rule metadata %s", m.ID)
		}
		if m.Fix != NoFix && m.Fix != PlannedFix && m.Fix != ImplementedFix {
			return nil, fmt.Errorf("invalid fix support for %s", m.ID)
		}
		for _, k := range sortedKeys(m.Options) {
			s := m.Options[k]
			if k == "" || k == "enabled" || k == "allow_directives" {
				return nil, fmt.Errorf("invalid option name %q", k)
			}
			if s.Minimum != nil && s.Maximum != nil && *s.Minimum > *s.Maximum {
				return nil, fmt.Errorf("invalid option bounds %s.%s", m.ID, k)
			}
			if (s.Minimum != nil || s.Maximum != nil) && s.Kind != Integer || len(s.Choices) > 0 && s.Kind != String {
				return nil, fmt.Errorf("invalid option constraints %s.%s", m.ID, k)
			}
			if err := s.validate(s.Default); err != nil {
				return nil, fmt.Errorf("invalid default %s.%s: %w", m.ID, k, err)
			}
		}
		m.Options = cloneSchema(m.Options)
		r.metadata[m.ID] = m
		r.ids = append(r.ids, m.ID)
	}
	for _, v := range implementations {
		if _, ok := r.metadata[v.ID]; !ok || v.Check == nil || v.ID == "MD094" {
			return nil, fmt.Errorf("invalid executable registration %q", v.ID)
		}
		if _, ok := r.checks[v.ID]; ok {
			return nil, fmt.Errorf("duplicate executable registration %s", v.ID)
		}
		r.checks[v.ID] = v.Check
	}
	slices.Sort(r.ids)
	return r, nil
}
func (r *Registry) Metadata() []Metadata {
	out := make([]Metadata, 0, len(r.ids))
	for _, id := range r.ids {
		m := r.metadata[id]
		m.Options = cloneSchema(m.Options)
		out = append(out, m)
	}
	return out
}
func (r *Registry) Available(id string) bool {
	return id == "MD094" && r.metadata[id].ID != "" || r.checks[id] != nil
}
