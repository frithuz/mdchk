package config

import (
	"context"
	"fmt"
	"slices"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/result"
)

// ExecutionPlan retains selected-operation admission, effective permissions and
// provenance independently of mutable Resolution views. Its engine retains the
// admitting registry and validated semantic-provider results. Only Admit creates
// a usable plan; a manually constructed Resolution cannot grant capabilities.
type ExecutionPlan struct {
	engine        *engine.ExecutionPlan
	configuration *Resolution
}

func cloneResolution(r *Resolution) *Resolution {
	q := *r
	q.plan = nil
	q.Settings.FrontmatterFormats = slices.Clone(r.Settings.FrontmatterFormats)
	q.Settings.Exclude = slices.Clone(r.Settings.Exclude)
	q.Settings.Slides = slices.Clone(r.Settings.Slides)
	q.Exclude = ownedPatterns(r.Exclude)
	q.Slides = ownedPatterns(r.Slides)
	q.Rules = make(map[string]engine.ResolvedRule, len(r.Rules))
	for id, rule := range r.Rules {
		rule.Options = cloneOptions(rule.Options)
		q.Rules[id] = rule
	}
	q.Origins = make(map[string]Origin, len(r.Origins))
	for path, origin := range r.Origins {
		q.Origins[path] = cloneOrigin(origin)
	}
	q.OwnPermissions = clonePermissions(r.OwnPermissions)
	q.EffectivePermissions = clonePermissions(r.EffectivePermissions)
	return &q
}

// Configuration returns a fresh owned view, including matcher pointers.
func (p *ExecutionPlan) Configuration() *Resolution {
	if p == nil || p.configuration == nil {
		return nil
	}
	r := cloneResolution(p.configuration)
	r.plan = p
	return r
}

// Rules returns a fresh owned view of effective catalog expectations.
func (p *ExecutionPlan) Rules() map[string]engine.ResolvedRule {
	if p == nil || p.engine == nil {
		return nil
	}
	return p.engine.Rules()
}

// NewInput snapshots target bytes with the admitted flavor and rule selection.
// It performs no filesystem access or parsing. Lint owns mandatory UTF-8 gating;
// acquisition must share its operation budget and leave that gate to Lint.
func (p *ExecutionPlan) NewInput(path string, content []byte) (engine.Input, error) {
	if p == nil || p.engine == nil {
		return engine.Input{}, fmt.Errorf("nil admitted execution plan")
	}
	return p.engine.NewInput(path, content)
}

// Lint uses the shared engine and result finalization with the original admitting
// registry. Provider validation is already complete. Pass the same budget used
// for acquisition; this call does not reset it or accept mutable settings views.
func (p *ExecutionPlan) Lint(ctx context.Context, inputs []engine.Input, options engine.RunOptions) (result.Result, error) {
	if p == nil || p.engine == nil {
		return result.Result{}, fmt.Errorf("nil admitted execution plan")
	}
	return p.engine.Lint(ctx, inputs, options)
}
