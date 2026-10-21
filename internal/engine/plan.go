package engine

import (
	"context"
	"fmt"

	"github.com/frithuz/mdchk/internal/result"
)

// RuleSelection owns schema-validated settings and the registry that resolved
// them. It retains membership separately from defaults and never calls semantic
// providers again during execution.
type RuleSelection struct {
	registry *Registry
	rules    map[string]ResolvedRule
}

// SelectRules resolves all option domains, including disabled rules, once.
func (r *Registry) SelectRules(settings Settings, providers ValidationProviders) (*RuleSelection, error) {
	rules, err := r.ResolveSettings(settings, providers)
	if err != nil {
		return nil, err
	}
	return &RuleSelection{registry: r, rules: rules}, nil
}

func copyRules(rules map[string]ResolvedRule) map[string]ResolvedRule {
	out := make(map[string]ResolvedRule, len(rules))
	for id, rule := range rules {
		rule.Options = cloneOptions(rule.Options)
		out[id] = rule
	}
	return out
}

// Rules returns an independent view of the resolved catalog expectations.
func (s *RuleSelection) Rules() map[string]ResolvedRule {
	if s == nil {
		return nil
	}
	return copyRules(s.rules)
}

// ExecutionPlan is an immutable selection bound to its original executable
// registry. Operation-wide capability admission belongs to the configuration
// adapter. Disabled directives are effective denials, not supplied rule entries.
type ExecutionPlan struct {
	registry        *Registry
	rules           map[string]ResolvedRule
	documentContext DocumentContext
	directivePolicy DirectivePolicy
}

// Plan preserves validated membership and options while applying effective
// directive denials. It rejects explicit enabled unavailable rules before any
// Markdown is acquired. The context is explicit; no dialect fallback occurs.
func (s *RuleSelection) Plan(dc DocumentContext, deniedDirectives map[string]bool) (*ExecutionPlan, error) {
	if s == nil || s.registry == nil {
		return nil, fmt.Errorf("nil rule selection")
	}
	if dc.Flavor != "commonmark" && dc.Flavor != "gfm" && dc.Flavor != "pandoc" {
		return nil, fmt.Errorf("invalid Markdown flavor %q", dc.Flavor)
	}
	rules := copyRules(s.rules)
	for _, id := range s.registry.ids {
		state := rules[id]
		if state.Enabled && state.Explicit && !state.Available {
			return nil, fmt.Errorf("explicitly enabled rule %s is unavailable", id)
		}
		if deniedDirectives[id] {
			state.AllowDirectives = false
		}
		rules[id] = state
	}
	return &ExecutionPlan{registry: s.registry, rules: rules, documentContext: dc}, nil
}

// WithDirectivePolicy returns a derived plan bound to immutable configuration
// authority. Policy implementations must not grant changes through writable views.
func (p *ExecutionPlan) WithDirectivePolicy(policy DirectivePolicy) *ExecutionPlan {
	if p == nil {
		return nil
	}
	q := *p
	q.directivePolicy = policy
	return &q
}

// Rules returns independent effective settings without exposing plan authority.
func (p *ExecutionPlan) Rules() map[string]ResolvedRule {
	if p == nil {
		return nil
	}
	return copyRules(p.rules)
}

// NewInput snapshots original bytes for this plan. Lint owns the mandatory UTF-8
// gate and its work charge; acquisition adapters must not scan these bytes with
// a separate gate before passing them here. Pass their shared budget to Lint.
func (p *ExecutionPlan) NewInput(path string, content []byte) (Input, error) {
	if p == nil || p.registry == nil {
		return Input{}, fmt.Errorf("nil execution plan")
	}
	in, err := NewInput(path, content, nil, p.documentContext)
	if err != nil {
		return Input{}, err
	}
	in.plan = p
	return in, nil
}

// Lint runs the existing registry engine with this plan's immutable admitted
// settings. Inputs from another plan are rejected before analysis. Mode and the
// single operation budget remain caller-owned; Validation is already resolved.
func (p *ExecutionPlan) Lint(ctx context.Context, inputs []Input, options RunOptions) (result.Result, error) {
	if p == nil || p.registry == nil {
		return result.Result{}, fmt.Errorf("nil execution plan")
	}
	for _, in := range inputs {
		if in.plan != p {
			return result.Result{}, fmt.Errorf("input %q belongs to another execution plan", in.path)
		}
	}
	return p.registry.Lint(ctx, inputs, options)
}
