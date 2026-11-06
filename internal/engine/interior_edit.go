package engine

import (
	"cmp"
	"context"
	"errors"
	"math/bits"
	"slices"

	"github.com/frithuz/mdchk/internal/parser"
)

// InteriorEditQuery conservatively proves the context of replacements anywhere in
// a document, using the same protection sources as fix.Plan. It never generates
// corrections, and the common combined-candidate checker remains authoritative.
// One query serves one rule run: its protected-span index is built at most once,
// on the first question that reaches it.
type InteriorEditQuery struct {
	parsed    parser.ParseResult
	state     DirectiveResult
	budget    *parser.OperationBudget
	protected []parser.Span
	indexed   bool
}

func NewInteriorEditQuery(parsed parser.ParseResult, state DirectiveResult, budget *parser.OperationBudget) *InteriorEditQuery {
	return &InteriorEditQuery{parsed: parsed, state: state, budget: budget}
}

// Eligible reports whether a nonempty edit of span under the rule's anchor can
// reach a correction. Insertions are withheld: Plan applies additional boundary
// policy to them that this query does not repeat. A document opening with an
// unrecognized block is withheld whole, as Plan does.
func (q *InteriorEditQuery) Eligible(ctx context.Context, id string, anchor, span parser.Span) (bool, error) {
	if ctx == nil || q.budget == nil {
		return false, errors.New("nil interior edit context or budget")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := q.budget.ChargeWork(1); err != nil {
		return false, err
	}
	p := q.parsed
	if p.Status != parser.Complete || p.AST != parser.Available || p.Phases.Blocks != parser.PhaseComplete || p.Phases.Inlines != parser.PhaseComplete || len(p.Uncertainty) != 0 || p.Document == nil || p.Document.UnrecognizedBlock() || p.Document.Source() != p.Source || q.state.Source() != p.Source || !q.state.Complete() || !q.state.InOperation(q.budget) {
		return false, nil
	}
	if _, err := p.Source.Text(span); err != nil || span.Start() == span.End() {
		return false, nil
	}
	permission, err := q.state.AllowsEdit(ctx, q.budget, id, anchor, span)
	if err != nil || !permission.Allowed {
		return false, err
	}
	if err := q.index(ctx); err != nil {
		return false, err
	}
	// The first frontier span ending after the edit start is the only candidate:
	// frontier starts and ends both increase. Intersection is half-open, so an
	// edit touching a code span or comment stays eligible.
	lo, hi := 0, len(q.protected)
	for lo < hi {
		if err := q.budget.ChargeWork(1); err != nil {
			return false, err
		}
		mid := lo + (hi-lo)/2
		if q.protected[mid].End() <= span.Start() {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo < len(q.protected) && q.protected[lo].Start() < span.End() {
		return false, nil
	}
	return true, ctx.Err()
}

// index retains a sorted frontier of protected intervals, as fix.Plan does, so
// dense nesting cannot force an all-pairs edit/protection traversal.
func (q *InteriorEditQuery) index(ctx context.Context) error {
	if q.indexed {
		return nil
	}
	spans, err := q.parsed.Document.LiteralSpans(ctx, q.budget)
	if err != nil {
		return err
	}
	comments, err := q.state.CandidateSpans(ctx, q.budget)
	if err != nil {
		return err
	}
	spans = append(spans, comments...)
	if header, present := q.parsed.Document.Frontmatter(); present {
		spans = append(spans, header.Span())
	}
	// Reserve the sort's comparisons and the frontier before constructing them.
	n := int64(len(spans))
	if err := q.budget.ChargeWork(n*int64(bits.Len64(uint64(n))) + n + 1); err != nil {
		return err
	}
	if err := q.budget.ChargeNodes(n + 1); err != nil {
		return err
	}
	slices.SortFunc(spans, func(a, b parser.Span) int {
		if c := cmp.Compare(a.Start(), b.Start()); c != 0 {
			return c
		}
		return cmp.Compare(a.End(), b.End())
	})
	out := spans[:0]
	for _, s := range spans {
		if s.Start() == s.End() {
			continue
		}
		if len(out) == 0 || s.End() > out[len(out)-1].End() {
			out = append(out, s)
		}
	}
	q.protected, q.indexed = out, true
	return ctx.Err()
}

// RuleInteriorEdits is the rule callback's view of an InteriorEditQuery for one
// rule run. It must not be retained after the callback returns.
type RuleInteriorEdits struct {
	context RuleContext
	query   *InteriorEditQuery
}

// InteriorEdits returns the eligibility query for this rule run.
func (c RuleContext) InteriorEdits() *RuleInteriorEdits {
	return &RuleInteriorEdits{context: c, query: NewInteriorEditQuery(c.parsed, c.directives, c.budget)}
}

// Eligible adds the guards of RuleContext.TerminalEditEligible to the shared
// query: hidden observations, directive state at the anchor and syntax reliability.
func (r *RuleInteriorEdits) Eligible(ctx context.Context, id string, anchor, span parser.Span) (bool, error) {
	c := r.context
	if c.observational {
		return false, c.auditCharge(ctx, 0, 0)
	}
	if c.directives.complete {
		if err := c.auditCharge(ctx, directiveLookupWork(len(c.directives.intervals[id]))+1, 0); err != nil {
			return false, err
		}
		if _, err := c.Source().Text(anchor); err != nil {
			return false, err
		}
		state, err := c.directives.anchorState(id, anchor.Start())
		if err != nil {
			return false, err
		}
		if !state.Enabled {
			return false, nil
		}
	}
	if !c.syntaxReliable {
		return false, c.Checkpoint(ctx, 1)
	}
	return r.query.Eligible(ctx, id, anchor, span)
}
