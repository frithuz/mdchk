package config

import (
	"errors"
	"github.com/frithuz/mdchk/internal/engine"
)

// ExclusionScope retains a valid definition's exclusions and the directory to
// which their relative patterns belong. Returned Patterns pointers are owned.
type ExclusionScope struct {
	Origin   Origin
	Patterns *Patterns
}

// PolicyState is immutable top-down scan state. The zero value is a scan root
// with no inherited restrictions or exclusions; it performs no ancestor lookup.
// A walker remains responsible for its explicit root and pruning before reads.
type PolicyState struct {
	permissions Permissions
	exclusions  []ExclusionScope
}

// Permissions returns an owned snapshot of all valid governing denials.
func (s PolicyState) Permissions() Permissions { return clonePermissions(s.permissions) }

// Exclusions returns owned origins and matcher pointers. Overwriting a returned
// matcher cannot alter this state or any derived branch.
func (s PolicyState) Exclusions() []ExclusionScope {
	out := make([]ExclusionScope, len(s.exclusions))
	for i, scope := range s.exclusions {
		out[i] = ExclusionScope{cloneOrigin(scope.Origin), ownedPatterns(scope.Patterns)}
	}
	return out
}

// Apply validates a complete decoded definition and inherited policy before
// activating any of its restrictions or exclusions. On error it returns the
// unchanged inherited state and no configuration. Decode/read errors similarly
// leave the caller's previous value untouched. No capability admission occurs.
func (s PolicyState) Apply(d *Definition, registry *engine.Registry, providers engine.ValidationProviders, c Capabilities) (PolicyState, *Configuration, error) {
	v, err := ValidateDefinition(d, registry, providers)
	if err != nil {
		return s, nil, err
	}
	if err := v.checkPolicy(s.permissions); err != nil {
		return s, nil, err
	}
	next := PolicyState{permissions: v.Permissions(s.permissions), exclusions: s.Exclusions()}
	if len(v.settings.Exclude) > 0 {
		next.exclusions = append(next.exclusions, ExclusionScope{cloneOrigin(v.definition.Provenance["exclude"]), ownedPatterns(v.exclude)})
	}
	return next, v.configuration(c, s.permissions), nil
}

func (v *Validated) checkPolicy(inherited Permissions) error {
	if len(v.settings.Exclude) > 0 && len(inherited.NestedExcludes) > 0 {
		return &PolicyError{cloneOrigin(v.definition.Provenance["exclude"]), clonePermissions(inherited).NestedExcludes}
	}
	return nil
}

// DirectiveDecision describes whether a requested rule effect may proceed. A
// blocked decision retains every global and rule-specific governing origin.
// It contains no directive parser or mutable rule-state transition.
type DirectiveDecision struct {
	Allowed      bool
	Restrictions []Origin
	Err          error
}

// DirectivePermission uses the immutable effective permission snapshot, never
// caller-mutated exported settings. Nil/unresolved views and unknown rules grant
// no authority. The returned restrictions are freshly owned on every call.
func (c *Configuration) DirectivePermission(ruleID string) DirectiveDecision {
	if c == nil || !c.knownRules[ruleID] {
		return DirectiveDecision{Err: errors.New("unresolved configuration or unknown rule")}
	}
	p := clonePermissions(c.permissions)
	restrictions := append(p.Directives, p.RuleDirectives[ruleID]...)
	return DirectiveDecision{Allowed: len(restrictions) == 0, Restrictions: restrictions}
}

// Matching never mutates the private compiled nodes, so those nodes may be
// shared. Each exposed or retained scope nevertheless owns its Patterns value:
// callers can overwrite the whole exported type through a returned pointer.
func ownedPatterns(patterns *Patterns) *Patterns {
	if patterns == nil {
		return nil
	}
	owned := *patterns
	return &owned
}
