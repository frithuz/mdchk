package config

import (
	"errors"
	"github.com/frithuz/mdchk/internal/engine"
	"math"
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
type DirectiveDecision = engine.DirectiveDecision

// DirectivePermission uses the immutable effective permission snapshot, never
// caller-mutated exported settings. Nil/unresolved views and unknown rules grant
// no authority. The returned restrictions are freshly owned on every call.
func (c *Configuration) DirectivePermission(ruleID string) DirectiveDecision {
	if c == nil || !c.knownRules[ruleID] {
		return DirectiveDecision{Err: errors.New("unresolved configuration or unknown rule")}
	}
	restrictions := append(cloneDirectiveOrigins(c.permissions.Directives), cloneDirectiveOrigins(c.permissions.RuleDirectives[ruleID])...)
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

// DirectiveConfiguration returns an owned immutable-authority snapshot. Exported
// Configuration settings are inspection views and cannot grant permissions or
// change the initial rule state/options used by directive consumers.
func (c *Configuration) DirectiveConfiguration() (bool, map[string]engine.ResolvedRule, error) {
	if c == nil || c.directiveRules == nil {
		return false, nil, errors.New("unresolved directive configuration")
	}
	return len(c.permissions.Directives) == 0, cloneResolvedRules(c.directiveRules), nil
}
func cloneResolvedRules(in map[string]engine.ResolvedRule) map[string]engine.ResolvedRule {
	out := make(map[string]engine.ResolvedRule, len(in))
	for id, rule := range in {
		rule.Options = cloneOptions(rule.Options)
		out[id] = rule
	}
	return out
}

// GlobalDirectivePermission reports immutable global authority and all governing
// denials, without conflating a particular rule's own protection.
func (c *Configuration) GlobalDirectivePermission() DirectiveDecision {
	if c == nil || c.directiveRules == nil {
		return DirectiveDecision{Err: errors.New("unresolved directive configuration")}
	}
	return DirectiveDecision{Allowed: len(c.permissions.Directives) == 0, Restrictions: cloneDirectiveOrigins(c.permissions.Directives)}
}

func cloneDirectiveOrigins(in []Origin) []Origin {
	out := make([]Origin, len(in))
	for i, origin := range in {
		out[i] = cloneOrigin(origin)
	}
	return out
}

// DirectivePermissionWork returns a conservative immutable estimate for one
// owned origin copy, including record, path/property bytes and position work.
// An empty rule ID requests global-only authority. The estimate itself neither
// traverses nor copies origins, so bounded consumers reserve it before querying.
func (c *Configuration) DirectivePermissionWork(ruleID string) (int64, error) {
	if c == nil || c.directiveRules == nil {
		return 0, errors.New("unresolved directive configuration")
	}
	if ruleID != "" && !c.knownRules[ruleID] {
		return 0, errors.New("unknown directive rule")
	}
	return min(math.MaxInt64/8, c.globalDirectiveWork+c.ruleDirectiveWork[ruleID]), nil
}
func directiveOriginWork(origins []Origin) int64 {
	var units int64
	for _, origin := range origins {
		// Bound additions/multipliers before publishing the cached estimate.
		n := int64(len(origin.Path)) + int64(len(origin.PropertyPath)) + 4
		if origin.Position != nil {
			n += 2
		}
		units = min(math.MaxInt64/16, units+min(n, math.MaxInt64/16))
	}
	return units
}
