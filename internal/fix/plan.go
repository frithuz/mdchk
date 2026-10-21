// Package fix plans original-offset atomic edits without filesystem effects.
// Structural acceptance and provisional bytes are not semantic approval, public
// proposed changes, corrected content, or permission to save. See README.md.
package fix

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

// RequirementKind describes proof that remains required of candidate validation.
type RequirementKind string

const (
	Meaning            RequirementKind = "rule-meaning"
	ProtectedSyntax    RequirementKind = "protected-syntax"
	DirectiveStructure RequirementKind = "directive-structure"
	// ExactBytes additionally protects an original span at the structural stage.
	ExactBytes RequirementKind = "exact-bytes"
)

type Requirement struct {
	Kind     RequirementKind
	Span     parser.Span
	Expected []byte
}
type Edit struct {
	Span                  parser.Span
	Expected, Replacement []byte
}

// Group is producer input. ID is a stable, unique producer provenance identifier.
// Anchor is the original diagnostic span; permission is checked at its start.
type Group struct {
	ID, Rule, Provenance string
	Anchor               parser.Span
	Edits                []Edit
	Requirements         []Requirement
}
type Reason struct {
	Code    string
	Related []string
	Owner   int
	Token   parser.Span
}
type Decision struct {
	Group    Group
	Accepted bool
	Reasons  []Reason
}
type SharedEdit struct {
	Edit   Edit
	Groups []string
}

// StructuralPlan retains private owned authority. Its zero value has no completed
// plan. Accessors copy all writable collections. Candidate bytes remain provisional.
type StructuralPlan struct {
	source    parser.Source
	complete  bool
	decisions []Decision
	edits     []SharedEdit
	candidate []byte
}

func (p StructuralPlan) Complete() bool           { return p.complete }
func (p StructuralPlan) Source() parser.Source    { return p.source }
func (p StructuralPlan) ProvisionalBytes() []byte { return slices.Clone(p.candidate) }
func (p StructuralPlan) Decisions() []Decision {
	out := slices.Clone(p.decisions)
	for i := range out {
		out[i].Group = cloneGroup(out[i].Group)
		out[i].Reasons = slices.Clone(out[i].Reasons)
		for j := range out[i].Reasons {
			out[i].Reasons[j].Related = slices.Clone(out[i].Reasons[j].Related)
		}
	}
	return out
}
func (p StructuralPlan) Edits() []SharedEdit {
	out := slices.Clone(p.edits)
	for i := range out {
		out[i].Edit = cloneEdit(out[i].Edit)
		out[i].Groups = slices.Clone(out[i].Groups)
	}
	return out
}
func cloneEdit(e Edit) Edit {
	e.Expected = slices.Clone(e.Expected)
	e.Replacement = slices.Clone(e.Replacement)
	return e
}
func cloneGroup(g Group) Group {
	g.Edits = slices.Clone(g.Edits)
	for i := range g.Edits {
		g.Edits[i] = cloneEdit(g.Edits[i])
	}
	g.Requirements = slices.Clone(g.Requirements)
	for i := range g.Requirements {
		g.Requirements[i].Expected = slices.Clone(g.Requirements[i].Expected)
	}
	return g
}

type meter struct {
	ctx    context.Context
	budget *parser.OperationBudget
}

func (m meter) work(n int64) error {
	if err := m.ctx.Err(); err != nil {
		return err
	}
	return m.budget.ChargeWork(n)
}
func (m meter) reserve(work, nodes int64) error {
	if err := m.work(work); err != nil {
		return err
	}
	return m.budget.ChargeNodes(nodes)
}
func boundary(text string, at int) bool {
	return at >= 0 && at <= len(text) && (at == len(text) || utf8.RuneStart(text[at])) && !(at > 0 && at < len(text) && text[at-1] == '\r' && text[at] == '\n')
}
func intersects(a, b parser.Span) bool {
	if a.Start() == a.End() {
		return a.Start() > b.Start() && a.Start() < b.End()
	}
	return a.Start() < b.End() && b.Start() < a.End()
}

type editKey struct {
	start, end  int
	replacement string
}
type entry struct {
	edit   Edit
	groups []int
}
type union struct{ parent, size []int }

func newUnion(n int) union {
	u := union{make([]int, n), make([]int, n)}
	for i := range n {
		u.parent[i] = i
		u.size[i] = 1
	}
	return u
}
func (u union) root(i int) int {
	for i != u.parent[i] {
		u.parent[i] = u.parent[u.parent[i]]
		i = u.parent[i]
	}
	return i
}
func (u union) join(a, b int) {
	a = u.root(a)
	b = u.root(b)
	if a == b {
		return
	}
	if u.size[a] < u.size[b] {
		a, b = b, a
	}
	u.parent[b] = a
	u.size[a] += u.size[b]
}

// Plan validates atomic proposal structure and original-source permissions, then
// applies independent eligible units from the end of the original snapshot.
// The parse, directive authority and residual budget must belong to this operation.
// Any interruption returns the zero plan and its typed error, never partial bytes.
func Plan(ctx context.Context, parsed parser.ParseResult, state engine.DirectiveResult, registry *engine.Registry, proposals []Group, budget *parser.OperationBudget) (StructuralPlan, error) {
	if ctx == nil || budget == nil || registry == nil {
		return StructuralPlan{}, errors.New("nil planning context, budget or registry")
	}
	m := meter{ctx, budget}
	if err := m.work(0); err != nil {
		return StructuralPlan{}, err
	}
	source := parsed.Source
	if parsed.Document == nil || state.Source() != source {
		return StructuralPlan{}, errors.New("unresolved original parse or directive authority")
	}
	if !state.InOperation(budget) {
		return StructuralPlan{}, errors.New("foreign directive operation")
	}
	if !state.Complete() {
		if err := state.Err(); err != nil {
			return StructuralPlan{}, err
		}
		return StructuralPlan{}, errors.New("unresolved directive authority")
	}
	// The private completed-document token also rejects a caller-invented parse or
	// a reset/replaced operation budget. No inspection fields authorize this stage.
	protected, err := parsed.Document.LiteralSpans(ctx, budget)
	if err != nil {
		return StructuralPlan{}, err
	}
	if parsed.Document.Source() != source {
		return StructuralPlan{}, errors.New("foreign parsed source")
	}
	comments, err := state.CandidateSpans(ctx, budget)
	if err != nil {
		return StructuralPlan{}, err
	}
	if err := m.reserve(int64(source.Len()+len(proposals)*8+len(protected)+len(comments)), int64(len(proposals)*8+len(protected)+len(comments))); err != nil {
		return StructuralPlan{}, err
	}
	all, _ := source.Span(0, source.Len())
	text, _ := source.Text(all)
	if !utf8.ValidString(text) {
		return StructuralPlan{}, errors.New("invalid UTF-8 original source")
	}
	protected = append(protected, comments...)

	protected, err = indexSpans(m, protected)
	if err != nil {
		return StructuralPlan{}, err
	}
	groups := make([]Group, len(proposals))
	ids := make(map[string]bool, len(groups))
	for i, g := range proposals {
		if err := m.reserve(int64(len(g.ID)+len(g.Rule)+len(g.Provenance)+len(g.Edits)*4+len(g.Requirements)*3+1), int64(len(g.Edits)+len(g.Requirements)+1)); err != nil {
			return StructuralPlan{}, err
		}
		if g.ID == "" || !utf8.ValidString(g.ID) || ids[g.ID] {
			return StructuralPlan{}, errors.New("proposal IDs must be unique nonempty UTF-8")
		}
		ids[g.ID] = true
		for _, e := range g.Edits {
			if err := m.work(int64(len(e.Expected)) + int64(len(e.Replacement))); err != nil {
				return StructuralPlan{}, err
			}
		}
		for _, r := range g.Requirements {
			if err := m.work(int64(len(r.Expected))); err != nil {
				return StructuralPlan{}, err
			}
		}
		groups[i] = cloneGroup(g)
	}
	// Sorting charges every comparison before inspecting its strings.
	if err := sortMetered(m, groups, func(a, b Group) int64 { return int64(len(a.ID)) + int64(len(b.ID)) + 1 }, func(a, b Group) int { return cmp.Compare(a.ID, b.ID) }); err != nil {
		return StructuralPlan{}, err
	}
	u := newUnion(len(groups))
	decisions := make([]Decision, len(groups))
	entries := []entry{}
	dedup := map[editKey]int{}
	addReason := func(i int, code string, owner int, token parser.Span) error {
		if err := m.reserve(1, 1); err != nil {
			return err
		}
		decisions[i].Reasons = append(decisions[i].Reasons, Reason{Code: code, Owner: owner, Token: token})
		return nil
	}
	for i, g := range groups {
		decisions[i].Group = g
		support, known := state.CorrectionSupport(registry, g.Rule)
		why := ""
		if !known || support == engine.NoFix {
			why = "ineligible-rule"
		}
		if _, err := source.Text(g.Anchor); err != nil || !boundary(text, g.Anchor.Start()) || !boundary(text, g.Anchor.End()) {
			why = "invalid-anchor"
		}
		if len(g.Edits) == 0 {
			why = "empty-group"
		}
		if len(g.Requirements) == 0 {
			why = "preservation-unresolved"
		}
		if why != "" {
			if err := addReason(i, why, -1, parser.Span{}); err != nil {
				return StructuralPlan{}, err
			}
		}
		for _, r := range g.Requirements {
			if err := m.work(1); err != nil {
				return StructuralPlan{}, err
			}
			switch r.Kind {
			case Meaning, ProtectedSyntax, DirectiveStructure:
				if r.Span.Present() || len(r.Expected) > 0 {
					if err := addReason(i, "invalid-preservation", -1, parser.Span{}); err != nil {
						return StructuralPlan{}, err
					}
				}
			case ExactBytes:
				original, e := source.Text(r.Span)
				if e != nil || original != string(r.Expected) || !boundary(text, r.Span.Start()) || !boundary(text, r.Span.End()) {
					if err := addReason(i, "invalid-preservation", -1, parser.Span{}); err != nil {
						return StructuralPlan{}, err
					}
				}
			default:
				if err := addReason(i, "preservation-unresolved", -1, parser.Span{}); err != nil {
					return StructuralPlan{}, err
				}
			}
		}
		for _, e := range g.Edits {
			if err := m.reserve(int64(len(e.Expected))+int64(len(e.Replacement))+4, 3); err != nil {
				return StructuralPlan{}, err
			}
			key := editKey{e.Span.Start(), e.Span.End(), string(e.Replacement)}
			if previous, ok := dedup[key]; ok {
				u.join(i, entries[previous].groups[0])
				entries[previous].groups = append(entries[previous].groups, i)
			} else {
				dedup[key] = len(entries)
				entries = append(entries, entry{e, []int{i}})
			}
			code := ""
			original, rangeErr := source.Text(e.Span)
			switch {
			case rangeErr != nil || !boundary(text, e.Span.Start()) || !boundary(text, e.Span.End()):
				code = "invalid-span"
			case original != string(e.Expected):
				code = "precondition-mismatch"
			case !utf8.Valid(e.Replacement):
				code = "invalid-replacement"
			}
			if code == "" {
				permission, err := state.AllowsEdit(ctx, budget, g.Rule, g.Anchor, e.Span)
				if err != nil {
					return StructuralPlan{}, err
				}
				if !permission.Allowed {
					if err := addReason(i, permission.Reason, permission.Owner, permission.Token); err != nil {
						return StructuralPlan{}, err
					}
				}
				// Confirmed opaque payload/comments cannot be incidentally rewritten. Exact
				// boundary insertions still require reparsing and semantic proof downstream.
				blocked, err := protectedAt(m, protected, e.Span)
				if err != nil {
					return StructuralPlan{}, err
				}
				if blocked {
					code = "protected-syntax"
				}

			}
			if code != "" {
				if err := addReason(i, code, -1, parser.Span{}); err != nil {
					return StructuralPlan{}, err
				}
			}
		}
	}
	// Only initially valid duplicate units can impose additional preservation
	// requirements. An invalid unrelated group must not block independent work.
	initiallyInvalid := make(map[int]bool)
	for i := range groups {
		if err := m.work(1); err != nil {
			return StructuralPlan{}, err
		}
		if len(decisions[i].Reasons) > 0 {
			initiallyInvalid[u.root(i)] = true
		}
	}
	var exact []parser.Span
	for i, g := range groups {
		if initiallyInvalid[u.root(i)] {
			continue
		}
		for _, r := range g.Requirements {
			if err := m.reserve(1, 1); err != nil {
				return StructuralPlan{}, err
			}
			if r.Kind == ExactBytes {
				exact = append(exact, r.Span)
			}
		}
	}
	exact, err = indexSpans(m, exact)
	if err != nil {
		return StructuralPlan{}, err
	}
	for i, g := range groups {
		for _, e := range g.Edits {
			if err := m.work(1); err != nil {
				return StructuralPlan{}, err
			}
			if initiallyInvalid[u.root(i)] {
				continue
			}
			blocked, err := protectedAt(m, exact, e.Span)
			if err != nil {
				return StructuralPlan{}, err
			}
			if blocked {
				if err := addReason(i, "preservation-conflict", -1, parser.Span{}); err != nil {
					return StructuralPlan{}, err
				}
			}
		}
	}
	// One sweep detects overlap connectivity in O(n log n), including insertion at
	// BOTH replacement endpoints. Adjacent nonempty spans have no conflict.
	if err := sortMetered(m, entries, func(a, b entry) int64 { return int64(len(a.edit.Replacement)) + int64(len(b.edit.Replacement)) + 1 }, func(a, b entry) int {
		if c := cmp.Compare(a.edit.Span.Start(), b.edit.Span.Start()); c != 0 {
			return c
		}
		if c := cmp.Compare(a.edit.Span.End(), b.edit.Span.End()); c != 0 {
			return c
		}
		return strings.Compare(string(a.edit.Replacement), string(b.edit.Replacement))
	}); err != nil {
		return StructuralPlan{}, err
	}

	conflict := newUnion(len(groups))
	touched := make([]bool, len(groups))
	connect := func(a, b int) {
		a = entries[a].groups[0]
		b = entries[b].groups[0]
		conflict.join(u.root(a), u.root(b))
		touched[u.root(a)] = true
		touched[u.root(b)] = true
	}
	maxEnd, insert := -1, -1
	for i, e := range entries {
		if err := m.work(1); err != nil {
			return StructuralPlan{}, err
		}
		s := e.edit.Span
		if s.Start() == s.End() {
			if maxEnd >= 0 && entries[maxEnd].edit.Span.End() >= s.Start() {
				connect(maxEnd, i)
			}
			if insert >= 0 && entries[insert].edit.Span.Start() == s.Start() {
				connect(insert, i)
			}
			insert = i
		} else {
			if maxEnd >= 0 && entries[maxEnd].edit.Span.End() > s.Start() {
				connect(maxEnd, i)
			}
			if insert >= 0 && entries[insert].edit.Span.Start() == s.Start() {
				connect(insert, i)
			}
			if maxEnd < 0 || entries[maxEnd].edit.Span.End() < s.End() {
				maxEnd = i
			}
		}
	}
	// Duplicate-dependent members link to a failing group in their atomic unit.
	// Conflict members share a canonical component cause without quadratic output.
	invalid := map[int]string{}
	causes := map[int]string{}
	for i := range groups {
		if err := m.reserve(3, 2); err != nil {
			return StructuralPlan{}, err
		}
		root := u.root(i)
		if len(decisions[i].Reasons) > 0 && invalid[root] == "" {
			invalid[root] = groups[i].ID
		}
		if touched[root] {
			c := conflict.root(root)
			if causes[c] == "" {
				causes[c] = groups[i].ID
			}
		}
	}
	for i := range groups {
		if err := m.work(1); err != nil {
			return StructuralPlan{}, err
		}
		root := u.root(i)
		if cause := invalid[root]; cause != "" && cause != groups[i].ID {
			if err := m.reserve(2, 1); err != nil {
				return StructuralPlan{}, err
			}
			decisions[i].Reasons = append(decisions[i].Reasons, Reason{Code: "atomic-dependency", Related: []string{cause}, Owner: -1})
		}
		if touched[root] {
			if err := m.reserve(2, 1); err != nil {
				return StructuralPlan{}, err
			}
			decisions[i].Reasons = append(decisions[i].Reasons, Reason{Code: "conflict", Related: []string{causes[conflict.root(root)]}, Owner: -1})
		}
		decisions[i].Accepted = invalid[root] == "" && !touched[root]
	}
	accepted := []SharedEdit{}
	size := int64(source.Len())
	for _, e := range entries {
		if err := m.work(1); err != nil {
			return StructuralPlan{}, err
		}
		if !decisions[e.groups[0]].Accepted {
			continue
		}
		delta := int64(len(e.edit.Replacement)) - int64(e.edit.Span.End()-e.edit.Span.Start())
		// Input bytes were charged to the finite (at most 2^60) work quota;
		// their cumulative length therefore cannot overflow int64.
		size += delta
		if err := m.reserve(int64(len(e.groups)+1), int64(len(e.groups)+1)); err != nil {
			return StructuralPlan{}, err
		}
		names := make([]string, 0, len(e.groups))
		for _, g := range e.groups {
			if len(names) == 0 || names[len(names)-1] != groups[g].ID {
				names = append(names, groups[g].ID)
			}
		}
		accepted = append(accepted, SharedEdit{e.edit, names})
	}
	if size > budget.Limits().DocumentBytes {
		return StructuralPlan{}, &parser.LimitExceeded{Name: "document-bytes", Observed: size, Allowed: budget.Limits().DocumentBytes}
	}
	// Copy right to left into one preallocated output. This is original-offset
	// end-to-start application, not repeated whole-document splice allocation.
	if err := m.reserve(int64(source.Len())+size+1, 1); err != nil {
		return StructuralPlan{}, err
	}
	candidate := make([]byte, int(size))
	read, write := source.Len(), len(candidate)
	for i := len(accepted) - 1; i >= 0; i-- {
		if err := m.work(0); err != nil {
			return StructuralPlan{}, err
		}
		e := accepted[i].Edit
		n := read - e.Span.End()
		write -= n
		copy(candidate[write:], text[e.Span.End():read])
		write -= len(e.Replacement)
		copy(candidate[write:], e.Replacement)
		read = e.Span.Start()
	}
	if err := m.work(0); err != nil {
		return StructuralPlan{}, err
	}
	copy(candidate[:write], text[:read])
	if err := m.work(0); err != nil {
		return StructuralPlan{}, err
	}
	return StructuralPlan{source, true, decisions, accepted, candidate}, nil
}

// indexSpans retains a sorted frontier of protected original-byte intervals. Queries are logarithmic;
// dense nesting/overlap cannot force an all-pairs edit/protection traversal.
func indexSpans(m meter, spans []parser.Span) ([]parser.Span, error) {
	if err := sortMetered(m, spans, func(parser.Span, parser.Span) int64 { return 1 }, func(a, b parser.Span) int {
		if c := cmp.Compare(a.Start(), b.Start()); c != 0 {
			return c
		}
		return cmp.Compare(a.End(), b.End())
	}); err != nil {
		return nil, err
	}
	out := spans[:0]
	for _, s := range spans {
		if err := m.work(1); err != nil {
			return nil, err
		}
		if s.Start() == s.End() {
			continue
		}
		if len(out) > 0 && out[len(out)-1].End() >= s.Start() {
			previous := out[len(out)-1]
			if s.End() > previous.End() {
				out = append(out, s) // End offsets strictly increase along the frontier.
			}
		} else {
			out = append(out, s)
		}
	}
	// Retain a monotonically increasing end frontier (discard covered spans).
	return out, nil
}
func protectedAt(m meter, spans []parser.Span, e parser.Span) (bool, error) {
	lo, hi := 0, len(spans)
	for lo < hi {
		if err := m.work(1); err != nil {
			return false, err
		}
		mid := lo + (hi-lo)/2
		if spans[mid].End() <= e.Start() {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo == len(spans) {
		return false, nil
	}
	return intersects(e, spans[lo]), nil
}

// Sort stops immediately at a charged comparator checkpoint. Only the private
// stop signal is recovered; programming panics retain their ordinary behavior.
type sortStop struct{ err error }

func sortMetered[T any](m meter, values []T, cost func(T, T) int64, compare func(T, T) int) (err error) {
	defer func() {
		if v := recover(); v != nil {
			if stop, ok := v.(sortStop); ok {
				err = stop.err
			} else {
				panic(v)
			}
		}
	}()
	slices.SortFunc(values, func(a, b T) int {
		if err := m.work(cost(a, b)); err != nil {
			panic(sortStop{err})
		}
		return compare(a, b)
	})
	return m.work(0)
}
