package engine

import (
	"context"
	"fmt"
	"math"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// CheckUnusedSuppressions registers MD087 through the shared registry. The
// engine coordinates its analysis with actual target execution; it never calls
// this entry recursively or invokes target checks from a rule callback.
func CheckUnusedSuppressions(context.Context, RuleContext) RuleOutcome {
	return RuleOutcome{Err: fmt.Errorf("MD087 requires engine coordination")}
}

// SuppressionDecision is a source-local, immutable publication. Owner and Effect
// index the original candidate/effect arrays, not a selected inspection view.
// Token is the first actual target token; Interval is its actual disable owner.
// Reason never replaces recognition, permission, instruction or provenance.
type SuppressionDecision struct {
	Owner, Effect int
	Rule          string
	Token         parser.Span
	Interval      DirectiveInterval
	Reason        string
}

type auditOwner struct {
	decision   SuppressionDecision
	diagnostic result.Diagnostic
	capable    bool
}
type auditTarget struct {
	owners          []int
	remaining       int
	intervals       map[int]auditInterval
	decisionByOwner map[int]int
	enableAt        int // monotone would-be state; source.Len()+1 means never enabled
}
type auditInterval struct {
	interval DirectiveInterval
	final    bool
}
type suppressionAudit struct {
	rc               RuleContext
	owners           []auditOwner
	targets          map[string]*auditTarget
	pending          int
	coverageIndex    int
	registered       bool
	trace            *InspectionTrace
	decisions        []SuppressionDecision
	completeCoverage []parser.Span
	evaluation       *Evaluation
	completeReasons  []string
	out              *result.Result
}

// auditCharge checks both the invoking and originating operation before work or
// retention. It never falls back to reporting allowance after execution stops.
func (c RuleContext) auditCharge(ctx context.Context, work, nodes int64) error {
	if ctx == nil || c.operation == nil || c.budget == nil {
		return parser.ErrModel
	}
	if err := c.operation.Err(); err != nil {
		return err
	}
	if err := c.Checkpoint(ctx, work); err != nil {
		return err
	}
	return c.budget.ChargeNodes(nodes)
}

func planSuppressionAudit(ctx context.Context, r *Registry, rc RuleContext, projection *projector, snap result.Snapshot, out *result.Result, trace *InspectionTrace) (suppressionAudit, error) {
	registered := r.checks["MD087"] != nil
	// The directive pass already records actual applied disables. No scan,
	// allocation or audit charge belongs to the no-demand compatibility path.
	if !registered || !rc.directives.hasSuppression && !rc.directives.hasAuditEnable && !rc.directives.rules["MD087"].Enabled {
		return suppressionAudit{registered: registered, coverageIndex: -1}, nil
	}
	if err := rc.auditCharge(ctx, 1, 1); err != nil {
		return suppressionAudit{}, err
	}
	a := suppressionAudit{rc: rc, out: out, trace: trace, coverageIndex: -1, completeReasons: []string{}, registered: registered}
	// Private directive arrays are borrowed for this source view, not cloned.
	// Count every effect before allocating any owner, target or output storage.
	if err := rc.auditCharge(ctx, int64(len(rc.directives.candidates))+1, 1); err != nil {
		return suppressionAudit{}, err
	}
	count := 0
	for _, c := range rc.directives.candidates {
		if err := rc.auditCharge(ctx, int64(len(c.Effects))+1, 0); err != nil {
			return suppressionAudit{}, err
		}
		total, err := auditAdd(int64(count), int64(len(c.Effects)))
		if err != nil || total > int64(int(^uint(0)>>1)) {
			return suppressionAudit{}, parser.ErrModel
		}
		count = int(total)
	}
	// Per-owner planning is independent of the operation-wide output inventory.
	// Diagnostics and uncertainty coverage reserve only their actually needed
	// slots below, with geometric growth and reached-copy accounting.
	if int64(count) > (math.MaxInt64-2)/32 {
		return suppressionAudit{}, parser.ErrModel
	}
	if err := rc.auditCharge(ctx, int64(count)*32+1, int64(count)*8+2); err != nil {
		return suppressionAudit{}, err
	}
	a.owners = make([]auditOwner, 0, count)
	a.decisions = make([]SuppressionDecision, 0, count)
	if err := rc.auditCharge(ctx, 3*int64(len(rc.directives.intervals["MD087"]))+2*int64(len(r.ids))+1, 0); err != nil {
		return suppressionAudit{}, err
	}
	if trace != nil && !rc.directives.EnabledSomewhere("MD087") {
		trace.skip("MD087", "inactive")
	}
	if trace != nil && rc.directives.EnabledSomewhere("MD087") {
		span, err := rc.Source().Span(0, rc.Source().Len())
		if err != nil {
			return suppressionAudit{}, err
		}
		a.completeCoverage = []parser.Span{span}
		e := trace.entry("MD087")
		a.evaluation = e
		e.Active = "partial"
		e.Reasons = []string{"context-unavailable"}
	}
	a.targets = make(map[string]*auditTarget, min(count, len(r.ids)))
	// Preconstruct token diagnostics using the execution meter. A retained
	// diagnostic is published only when its owner is subsequently proved unused.
	strict := *projection
	strict.strict = true
	for owner, c := range rc.directives.candidates {
		for effect, e := range c.Effects {
			if err := rc.auditCharge(ctx, 1, 0); err != nil {
				return suppressionAudit{}, err
			}
			if c.Operation != DirectiveDisable || e.Disposition != DirectiveApplied {
				continue
			}
			d := SuppressionDecision{Owner: owner, Effect: effect, Rule: e.Rule, Token: e.Token, Reason: "suppression-uncertain"}
			if err := rc.auditCharge(ctx, directiveLookupWork(len(rc.directives.intervals[e.Rule]))+1, 0); err != nil {
				return suppressionAudit{}, err
			}
			interval, err := rc.directives.anchorState(e.Rule, c.Closing.End())
			if err != nil {
				return suppressionAudit{}, err
			}
			if interval.Owner != owner {
				return suppressionAudit{}, parser.ErrModel
			}
			d.Interval = interval
			entry := auditOwner{decision: d}
			if e.Rule == "MD087" {
				entry.decision.Reason = "suppression-excluded"
			} else {
				// Resolve MD087 token state without copying option storage.
				if err := rc.auditCharge(ctx, directiveLookupWork(len(rc.directives.intervals["MD087"]))+1, 0); err != nil {
					return suppressionAudit{}, err
				}
				active, err := rc.directives.anchorState("MD087", e.Token.Start())
				if err != nil {
					return suppressionAudit{}, err
				}
				if !active.Enabled {
					entry.decision.Reason = "suppression-not-requested"
				} else {
					t := a.targets[e.Rule]
					if t == nil {
						intervals := rc.directives.intervals[e.Rule]
						if err := rc.auditCharge(ctx, int64(len(intervals))*6+1, int64(len(intervals))*3+1); err != nil {
							return suppressionAudit{}, err
						}
						t = &auditTarget{owners: make([]int, 0, len(intervals)), intervals: make(map[int]auditInterval, len(intervals)), decisionByOwner: make(map[int]int, len(intervals)), enableAt: rc.Source().Len() + 1}
						for i, v := range intervals {
							if v.Owner >= 0 {
								t.intervals[v.Owner] = auditInterval{v, i == len(intervals)-1}
							}
						}
						if rc.directives.rules[e.Rule].Enabled {
							t.enableAt = 0
						} else {
							for _, v := range intervals {
								if v.Enabled && v.Owner >= 0 && v.Start < t.enableAt {
									t.enableAt = v.Start
								}
							}
						}
						a.targets[e.Rule] = t
					}
					if err := rc.auditCharge(ctx, 2, 0); err != nil {
						return suppressionAudit{}, err
					}
					v, found := t.intervals[owner]
					if !found {
						return suppressionAudit{}, parser.ErrModel
					}
					entry.decision.Interval = v.interval
					entry.capable = t.enableAt <= v.interval.Start && (v.interval.Start < v.interval.End || v.final)
					// Message construction and all token projection happen before callbacks.
					if err := rc.auditCharge(ctx, 64, 0); err != nil {
						return suppressionAudit{}, err
					}
					diagnostic, err := makeDiagnostic(rc.input, &strict, snap, r.metadata["MD087"], Finding{Span: e.Token, Message: "Suppression for " + e.Rule + " hides no findings."})
					if err != nil {
						return suppressionAudit{}, err
					}
					entry.diagnostic = diagnostic
					t.owners = append(t.owners, len(a.owners))
					t.decisionByOwner[owner] = len(a.owners)
					a.pending++
					t.remaining++
				}
			}
			a.owners = append(a.owners, entry)
			a.decisions = append(a.decisions, entry.decision)
		}
	}
	if trace != nil && len(a.decisions) != 0 {
		trace.audits = a.decisions
		trace.auditOperation = rc.operation
	}
	if a.pending > 0 {
		// Reserve before callbacks. Reuse spare batch capacity, and charge/copy
		// prior records only when actual geometric growth is reached. A later
		// clean file has no output slots to reserve and cannot recopy a prefix.
		diagnostics, err := reserveAuditSlots(ctx, rc, out.Diagnostics, a.pending)
		if err != nil {
			return suppressionAudit{}, err
		}
		out.Diagnostics = diagnostics
		coverage, err := reserveAuditSlots(ctx, rc, out.Coverage, 1)
		if err != nil {
			return suppressionAudit{}, err
		}
		out.Coverage = coverage
		a.coverageIndex = len(out.Coverage)
		out.Coverage = append(out.Coverage, result.Coverage{Path: rc.Path(), Rule: "MD087", Reason: result.ContextUnavailable, Message: "Suppression usefulness is uncertain; required target observations are incomplete."})
	}
	return a, nil
}

func (a *suppressionAudit) publish(ctx context.Context, index int, reason string) error {
	if err := a.rc.auditCharge(ctx, 4, 0); err != nil {
		return err
	}
	o := &a.owners[index]
	if reason == "suppression-unused" {
		next, err := reserveAuditSlice(ctx, a.rc, a.out.Diagnostics)
		if err != nil {
			return err
		}
		a.out.Diagnostics = next
	}
	old := o.decision.Reason
	o.decision.Reason = reason
	a.decisions[index] = o.decision
	if old == "suppression-uncertain" && reason != "suppression-uncertain" {
		a.pending--
		a.targets[o.decision.Rule].remaining--
	}
	if reason == "suppression-unused" {
		a.out.Diagnostics = append(a.out.Diagnostics, o.diagnostic)
	}
	if a.trace != nil && a.trace.publishSuppression != nil {
		a.trace.publishSuppression(o.decision)
	}
	return nil
}
func (a *suppressionAudit) start(ctx context.Context) error {
	if !a.registered {
		return nil
	}
	for i := range a.owners {
		reason := a.owners[i].decision.Reason
		if reason == "suppression-uncertain" && !a.owners[i].capable {
			reason = "suppression-unused"
		}
		if err := a.publish(ctx, i, reason); err != nil {
			return err
		}
	}
	return nil
}
func (a *suppressionAudit) demanded(id string) bool {
	t := a.targets[id]
	return t != nil && t.remaining > 0
}
func (a *suppressionAudit) positive(ctx context.Context, id string, span parser.Span) error {
	t := a.targets[id]
	if t == nil {
		return nil
	}
	if err := a.rc.auditCharge(ctx, directiveLookupWork(len(a.rc.directives.intervals[id]))+4, 0); err != nil {
		return err
	}
	if span.Start() < t.enableAt {
		return nil
	}
	state, err := a.rc.directives.anchorState(id, span.Start())
	if err != nil {
		return err
	}
	i, found := t.decisionByOwner[state.Owner]
	if found && !state.Enabled && a.owners[i].decision.Reason == "suppression-uncertain" {
		return a.publish(ctx, i, "suppression-used")
	}
	return nil
}
func (a *suppressionAudit) completeTarget(ctx context.Context, id string) error {
	t := a.targets[id]
	if t == nil {
		return nil
	}
	if err := a.rc.auditCharge(ctx, int64(len(t.owners))+1, 0); err != nil {
		return err
	}
	for _, i := range t.owners {
		if a.owners[i].decision.Reason == "suppression-uncertain" {
			if err := a.publish(ctx, i, "suppression-unused"); err != nil {
				return err
			}
		}
	}
	return nil
}
func (a *suppressionAudit) finish() {
	// These are preallocated scalar publications of already materialized facts.
	// No traversal, new proof, source access or allocation follows interruption.
	if !a.registered {
		return
	}
	if a.trace != nil && a.completeCoverage != nil && a.pending == 0 {
		e := a.evaluation
		e.Active = "complete"
		e.Reasons = a.completeReasons
		e.ActiveCoverage = a.completeCoverage
	}
	if a.pending == 0 && a.coverageIndex >= 0 {
		last := len(a.out.Coverage) - 1
		a.out.Coverage[a.coverageIndex] = a.out.Coverage[last]
		a.out.Coverage = a.out.Coverage[:last]
	}
}

// reserveAuditSlice charges the reachable copy and new retained capacity before
// growing an engine-owned output. Empty slots do not hide uncharged append work.
func reserveAuditSlice[T any](ctx context.Context, c RuleContext, items []T) ([]T, error) {
	return reserveAuditSlots(ctx, c, items, 1)
}

func reserveAuditSlots[T any](ctx context.Context, c RuleContext, items []T, slots int) ([]T, error) {
	return reserveChargedSlots(items, slots, func(work, nodes int64) error { return c.auditCharge(ctx, work, nodes) })
}

// reservePresentedSlice cannot confer audit authority on the retained record.
func reservePresentedSlice[T any](p *projector, items []T) ([]T, error) {
	return reserveChargedSlots(items, 1, p.charge)
}

func reserveChargedSlots[T any](items []T, slots int, charge func(int64, int64) error) ([]T, error) {
	if err := charge(0, 0); err != nil {
		return nil, err
	}
	required, err := auditHostSum(len(items), slots)
	if err != nil {
		return nil, err
	}
	if cap(items) >= required {
		return items, nil
	}
	// Widen before multiplication/addition. Near the host bound, exact growth
	// avoids a wrapped doubling while the original finite node meter still
	// rejects an unaffordable allocation before construction.
	wideCapacity := int64(required)
	if int64(cap(items)) <= int64(int(^uint(0)>>1))/2 {
		wideCapacity = max(wideCapacity, int64(cap(items))*2)
	}
	work, err := auditAdd(int64(len(items)), wideCapacity)
	if err != nil {
		return nil, err
	}
	work, err = auditAdd(work, 1)
	if err != nil {
		return nil, err
	}
	if err := charge(work, wideCapacity); err != nil {
		return nil, err
	}
	next := make([]T, len(items), int(wideCapacity))
	copy(next, items)
	return next, nil
}

// reserveAuditFailures leaves room for the callback's noninterrupt cause,
// incomplete coverage and one interruption even if invalid-finding retention
// later exhausts the meter. Copies of arbitrary prior-file arrays are charged
// at their actual reachable lengths/capacities before invoking the provider.
func reserveAuditFailures(ctx context.Context, c RuleContext, out *result.Result) error {
	var err error
	errors, err := reserveAuditSlots(ctx, c, out.Errors, 3)
	if err != nil {
		return err
	}
	out.Errors = errors
	coverage, err := reserveAuditSlots(ctx, c, out.Coverage, 5)
	if err != nil {
		return err
	}
	out.Coverage = coverage
	stops, err := reserveAuditSlots(ctx, c, out.Interruptions, 1)
	if err != nil {
		return err
	}
	out.Interruptions = stops
	return nil
}

// publishInvalidAuditFinding maintains the same failure reserve after each
// invalid finding. On reservation failure it uses the already reserved slots
// to publish the independently materialized internal error, then returns the
// actual retention cause. It performs no new validation after that cause.
func publishInvalidAuditFinding(ctx context.Context, c RuleContext, out *result.Result, record result.Error, coverage result.Coverage) error {
	return publishInvalidFinding(out, record, coverage, func(work, nodes int64) error { return c.auditCharge(ctx, work, nodes) })
}

func publishInvalidFinding(out *result.Result, record result.Error, coverage result.Coverage, charge func(int64, int64) error) error {
	errors, err := reserveChargedSlots(out.Errors, 4, charge)
	if err == nil {
		out.Errors = errors
		var rows []result.Coverage
		rows, err = reserveChargedSlots(out.Coverage, 6, charge)
		if err == nil {
			out.Coverage = rows
		}
	}
	// Before every call, reserveAuditFailures or the previous successful call
	// guarantees these scalar publications cannot grow their backing arrays.
	out.Errors = append(out.Errors, record)
	out.Coverage = append(out.Coverage, coverage)
	return err
}

func auditAdd(a, b int64) (int64, error) {
	if a < 0 || b < 0 || a > math.MaxInt64-b {
		return 0, parser.ErrModel
	}
	return a + b, nil
}
func auditHostSum(a, b int) (int, error) {
	total, err := auditAdd(int64(a), int64(b))
	if err != nil || total > int64(int(^uint(0)>>1)) {
		return 0, parser.ErrModel
	}
	return int(total), nil
}

func auditSerializationWork(message, path, digest string) (int64, error) {
	size, err := auditAdd(int64(len(message)), int64(len(path)))
	if err != nil {
		return 0, err
	}
	size, err = auditAdd(size, int64(len(digest)))
	if err != nil {
		return 0, err
	}
	if size > (math.MaxInt64-4096)/6 {
		return 0, parser.ErrModel
	}
	return size*6 + 4096, nil
}
