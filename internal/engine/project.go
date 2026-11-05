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

// IsLintOnlyCoverage reports whether c is the coverage LintOnlyCoverage made.
func IsLintOnlyCoverage(c result.Coverage) bool {
	return c.Rule == "MD074" && c.Reason == result.ContextUnavailable && c.Message == LintOnlyMessage && c.Range == nil
}

// SpanProjector projects spans of one source that is not a lint target, such
// as the MkDocs configuration, with the canonical line index and original
// snapshot identity that document findings use. One projector serves a whole
// operation: positions charge the operation budget and, after it stops, one
// shared reporting reserve that never restarts. Stop returns the operation
// cause the projector observed, so callers record it as an interruption.
type SpanProjector struct {
	projection projector
	snapshot   result.Snapshot
	err        error
}

// NewSpanProjector charges the line-index scan once, before any projection.
func NewSpanProjector(ctx context.Context, budget *parser.OperationBudget, source parser.Source) *SpanProjector {
	s := &SpanProjector{projection: projector{ctx: ctx, budget: budget, reporting: &reportingReserve{}, source: source}}
	if ctx == nil || budget == nil {
		s.err = fmt.Errorf("nil projection context or budget")
		return s
	}
	if s.err = s.projection.checkpoint(int64(source.Len()) + 1); s.err != nil {
		return s
	}
	s.projection.index = parser.NewLineIndex(source)
	s.snapshot = snapshot(source)
	return s
}

// Project returns span's coordinates, or the error that stopped projection.
// Once the reserve is exhausted every later call fails without new work.
func (s *SpanProjector) Project(span parser.Span) (result.Range, result.ByteRange, result.Snapshot, error) {
	if s.err != nil {
		return result.Range{}, result.ByteRange{}, result.Snapshot{}, s.err
	}
	if span.SourceToken() != s.projection.source.Token() {
		return result.Range{}, result.ByteRange{}, result.Snapshot{}, fmt.Errorf("span does not belong to source")
	}
	start, err := s.projection.position(span.Start())
	if err == nil {
		var end parser.Position
		end, err = s.projection.position(span.End())
		if err == nil {
			return result.Range{Start: start, End: end}, result.ByteRange{Start: span.Start(), End: span.End()}, s.snapshot, nil
		}
	}
	s.err = err
	return result.Range{}, result.ByteRange{}, result.Snapshot{}, err
}

// Stop is the operation budget, deadline or cancellation cause observed while
// projecting, even when the reserve let projection continue.
func (s *SpanProjector) Stop() error { return s.projection.stop }
