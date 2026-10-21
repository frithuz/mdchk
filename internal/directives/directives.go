// Package directives exposes the shared engine directive processor for callers
// that need recognition and state without executing rules.
package directives

import (
	"context"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

type Operation = engine.DirectiveOperation
type Disposition = engine.DirectiveDisposition
type Token = engine.DirectiveToken
type Effect = engine.DirectiveEffect
type Candidate = engine.DirectiveCandidate
type Note = engine.DirectiveNote
type Interval = engine.DirectiveInterval
type State = engine.DirectiveState
type Result = engine.DirectiveResult

const (
	Enable       = engine.DirectiveEnable
	Disable      = engine.DirectiveDisable
	Applied      = engine.DirectiveApplied
	Blocked      = engine.DirectiveBlocked
	Inapplicable = engine.DirectiveInapplicable
	Inactive     = engine.DirectiveInactive
	Invalid      = engine.DirectiveInvalid
	Uncertain    = engine.DirectiveUncertain
)

func Process(ctx context.Context, parsed parser.ParseResult, configuration engine.DirectivePolicy, budget *parser.OperationBudget) Result {
	return engine.ProcessDirectives(ctx, parsed, configuration, budget)
}
