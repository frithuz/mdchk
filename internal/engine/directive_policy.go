package engine

import (
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// DirectivePolicy is the immutable authority captured at configuration admission.
// Implementations return owned views and reserve provenance copying work before
// returning it. The engine never derives authority from mutable adapter views.
type DirectivePolicy interface {
	DirectiveConfiguration() (bool, map[string]ResolvedRule, error)
	DirectivePermission(string) DirectiveDecision
	GlobalDirectivePermission() DirectiveDecision
	DirectivePermissionWork(string) (int64, error)
}
type DirectiveDecision struct {
	Allowed      bool
	Restrictions []result.ConfigurationOrigin
	Err          error
}

type ruleDirectivePolicy struct{ rules map[string]ResolvedRule }

func (p ruleDirectivePolicy) DirectiveConfiguration() (bool, map[string]ResolvedRule, error) {
	return true, copyRules(p.rules), nil
}
func (p ruleDirectivePolicy) DirectivePermission(id string) DirectiveDecision {
	if p.rules[id].AllowDirectives {
		return DirectiveDecision{Allowed: true}
	}
	return DirectiveDecision{Restrictions: []result.ConfigurationOrigin{{PropertyPath: "rules." + id + ".allow_directives"}}}
}
func (p ruleDirectivePolicy) GlobalDirectivePermission() DirectiveDecision {
	return DirectiveDecision{Allowed: true}
}
func (p ruleDirectivePolicy) DirectivePermissionWork(id string) (int64, error) {
	if id != "" && !p.rules[id].AllowDirectives {
		return int64(len("rules..allow_directives") + len(id) + 4), nil
	}
	return 0, nil
}

// DirectiveAnalysis retains owned source-local observations needed by later
// inspection. It is deliberately separate from serialized lint records, which
// have no directive collection to reference. State and candidates are immutable.
type DirectiveAnalysis struct {
	path       string
	state      DirectiveResult
	suppressed []DirectiveSuppression
}

func (a DirectiveAnalysis) SourcePath() string     { return a.path }
func (a DirectiveAnalysis) State() DirectiveResult { return a.state }
func (a DirectiveAnalysis) Suppressed() []DirectiveSuppression {
	return append([]DirectiveSuppression{}, a.suppressed...)
}

type DirectiveSuppression struct {
	Diagnostic result.Diagnostic
	Owner      int
	Token      parser.Span
}
