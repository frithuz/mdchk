// Package engine runs read-only rules on immutable original snapshots. It has no
// filesystem, reporting, process-exit, correction-planning or write dependency.
package engine

import (
	"context"
	"fmt"
	"slices"
	"strconv"
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
	// UsesFrontmatter explicitly permits source-metadata observations in headers.
	// It grants neither metadata decoding nor correction eligibility.
	UsesFrontmatter bool
	AppliesTo       func(DocumentContext) bool
}
type Registration struct {
	ID    string
	Check Check
	// Applies resolves option-aware intentional exceptions before syntax prerequisites.
	// It receives immutable options and explicit context and must honor operation controls.
	Applies func(context.Context, RuleContext) (bool, error)
}
type Check func(context.Context, RuleContext) RuleOutcome

// Finding spans must belong to the supplied original Source and end at scalar
// boundaries. File-wide observations use a source-owned byte-zero span; missing
// EOF material uses a zero-width span at Source.Len(). Surplus terminal material
// uses its smallest offending suffix (MD047 retains the first ending).
// Suppression uses the start anchor after finding validation. FixEligible is a
// positive per-finding assessment, not metadata.
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

type registryIdentity struct{ _ byte }

type Registry struct {
	identity *registryIdentity
	metadata map[string]Metadata
	checks   map[string]Check
	applies  map[string]func(context.Context, RuleContext) (bool, error)
	ids      []string
}

func validID(id string) bool {
	if len(id) != 5 || id[:2] != "MD" {
		return false
	}
	for _, c := range id[2:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	n, err := strconv.Atoi(id[2:])
	return err == nil && n >= 1 && n <= 95 && !slices.Contains([]int{2, 6, 8, 15, 16, 17, 78, 79}, n)
}

// NewRegistry freezes catalog schemas and registration maps. No package-global
// mutable registry exists. Metadata returns defensive copies. MD094 is a gate
// attribution entry and cannot have a normal executable implementation.
func NewRegistry(catalog []Metadata, implementations []Registration) (*Registry, error) {
	r := &Registry{identity: &registryIdentity{}, metadata: make(map[string]Metadata), checks: make(map[string]Check), applies: make(map[string]func(context.Context, RuleContext) (bool, error))}
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
			if !s.validConstraints() {
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
		if v.Applies != nil {
			r.applies[v.ID] = v.Applies
		}
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
