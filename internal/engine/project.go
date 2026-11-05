package engine

import (
	"context"
	"fmt"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// LintOnlyMessage explains the coverage that fix and inspect record for MD074.
const LintOnlyMessage = "MkDocs navigation is checked only by lint."

// LintOnlyCoverage is the coverage an operation that does not run project rules
// records for each enabled one, so its result can never imply that it passed.
func LintOnlyCoverage(rules map[string]ResolvedRule) []result.Coverage {
	if rule := rules["MD074"]; rule.Enabled && rule.Available {
		return []result.Coverage{{Rule: "MD074", Reason: result.ContextUnavailable, Message: LintOnlyMessage}}
	}
	return nil
}

// ProjectSpan projects a span of a source that is not a lint target, such as
// the MkDocs configuration, with the canonical line index and original snapshot
// identity that document findings use. Positions charge the operation budget,
// then the shared reporting reserve.
func ProjectSpan(ctx context.Context, budget *parser.OperationBudget, source parser.Source, span parser.Span) (result.Range, result.ByteRange, result.Snapshot, error) {
	if ctx == nil || budget == nil || span.SourceToken() != source.Token() {
		return result.Range{}, result.ByteRange{}, result.Snapshot{}, fmt.Errorf("span does not belong to source")
	}
	projection := projector{ctx: ctx, budget: budget, reporting: &reportingReserve{}, source: source, index: parser.NewLineIndex(source)}
	start, err := projection.position(span.Start())
	if err != nil {
		return result.Range{}, result.ByteRange{}, result.Snapshot{}, err
	}
	end, err := projection.position(span.End())
	if err != nil {
		return result.Range{}, result.ByteRange{}, result.Snapshot{}, err
	}
	return result.Range{Start: start, End: end}, result.ByteRange{Start: span.Start(), End: span.End()}, snapshot(source), nil
}
