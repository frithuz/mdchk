package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/frithuz/mdchk/internal/parser"
)

// reportingReserve is shared by the entire operation and starts only when
// execution stops. It cannot restart for a new callback or target. The accepted
// two-second reporting reserve uses the same finite default work profile as
// execution, independently of an already-exhausted execution allowance.
type reportingReserve struct{ budget *parser.OperationBudget }

func (r *reportingReserve) charge(units int64) error {
	if r.budget == nil {
		limits := parser.DefaultLimits()
		limits.OperationTime = 2 * time.Second
		r.budget, _ = parser.NewOperationBudget(limits)
	}
	// Zero forces an elapsed-time checkpoint even for cached/short projections.
	if err := r.budget.ChargeWork(0); err != nil {
		return err
	}
	return r.budget.ChargeWork(units)
}

type reportingStopped struct{ cause error }

func (e *reportingStopped) Error() string {
	return "diagnostic reporting reserve exhausted: " + e.cause.Error()
}
func (e *reportingStopped) Unwrap() error { return e.cause }

// projector uses the canonical LineIndex conversion. Each uncached position
// reserves its worst-case complete source-prefix cost, so distinct positions on
// one long line cannot cause unaccounted quadratic scanning. The small bounded
// cache helps repeated endpoints without allocating in proportion to findings.
type projector struct {
	ctx       context.Context
	budget    *parser.OperationBudget
	reporting *reportingReserve
	source    parser.Source
	index     parser.LineIndex
	cached    map[int]parser.Position
	stop      error
}

func (p *projector) checkpoint(units int64) error {
	if p.stop == nil {
		p.stop = p.ctx.Err()
		if p.stop == nil {
			p.stop = p.budget.ChargeWork(units)
			if p.stop == nil {
				return nil
			}
		}
	}
	if err := p.reporting.charge(units); err != nil {
		return &reportingStopped{err}
	}
	return nil
}
func (p *projector) position(offset int) (parser.Position, error) {
	if position, ok := p.cached[offset]; ok {
		if err := p.checkpoint(1); err != nil {
			return parser.Position{}, err
		}
		return position, nil
	}
	if err := p.checkpoint(int64(p.source.Len()) + 1); err != nil {
		return parser.Position{}, err
	}
	position, err := p.index.Position(offset)
	if err == nil {
		if p.cached == nil {
			p.cached = make(map[int]parser.Position)
		}
		if len(p.cached) < 256 {
			p.cached[offset] = position
		}
	}
	return position, err
}
func isInterruption(err error) bool {
	var limit *parser.LimitExceeded
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.As(err, &limit)
}
func omittedProjection(err error) bool {
	var stopped *reportingStopped
	return errors.As(err, &stopped)
}
func (p *projector) begin(f Finding) error {
	if err := p.checkpoint(int64(len(f.Message)) + 1); err != nil {
		return err
	}
	if _, err := p.source.Text(f.Span); err != nil {
		return fmt.Errorf("finding source: %w", err)
	}
	return nil
}
