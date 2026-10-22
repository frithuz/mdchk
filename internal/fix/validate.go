package fix

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// Producer regenerates proposals against Current on every pass. It is bounded
// program code, not source-controlled data; it must checkpoint its own loops.
// A producer cannot register or supply its preservation checker.
type Producer func(context.Context, RoundContext) ([]Group, error)

// Proof is affirmative evidence from independently registered trusted code.
// Unknown is deliberately the zero value. A checker must establish rule meaning,
// including any rule-specific prohibition on newly introduced findings; comparing
// AST shape or diagnostic counts alone is insufficient.
type Proof uint8

const (
	Unknown Proof = iota
	Preserved
	Unsafe
)

type Checker func(context.Context, CheckContext) (Proof, error)
type CheckerRegistration struct {
	Rule              string
	Check             Checker
	ForbidNewFindings []string
}

// Validator freezes trusted checker registrations, independently of proposals.
// It registers no production corrections. Checkers are application code with the
// same trust/bounded-work obligations as engine rule implementations.
type Validator struct {
	registry  *engine.Registry
	checkers  map[string]Checker
	forbidden map[string][]string
}

func NewValidator(registry *engine.Registry, checks []CheckerRegistration) (*Validator, error) {
	if registry == nil {
		return nil, errors.New("nil correction registry")
	}
	ownedRegistry := *registry
	v := &Validator{registry: &ownedRegistry, checkers: map[string]Checker{}, forbidden: map[string][]string{}}
	for _, c := range checks {
		if c.Check == nil || v.checkers[c.Rule] != nil {
			return nil, errors.New("invalid or duplicate preservation checker")
		}
		known := false
		for _, meta := range registry.Metadata() {
			if meta.ID == c.Rule {
				known = true
			}
		}
		if !known {
			return nil, fmt.Errorf("unknown preservation rule %s", c.Rule)
		}
		for _, id := range c.ForbidNewFindings {
			known := id == "directive"
			for _, meta := range registry.Metadata() {
				if meta.ID == id {
					known = true
				}
			}
			if !known {
				return nil, fmt.Errorf("unknown forbidden finding rule %s", id)
			}
		}
		v.forbidden[c.Rule] = slices.Clone(c.ForbidNewFindings)
		v.checkers[c.Rule] = c.Check
	}
	return v, nil
}

// ResourceObservation is adapter-supplied first-observation authority. Known
// content and authoritative absence differ from unavailable context. This layer
// neither acquires resources nor turns them into correction targets.
type ResourceObservation struct {
	Key    string
	State  ResourceState
	Source parser.Source
}
type ResourceState string

const (
	ResourceContent     ResourceState = "content"
	ResourceAbsent      ResourceState = "absent"
	ResourceUnavailable ResourceState = "unavailable"
)

// Target binds an immutable source and admitted settings. Dependencies declare
// every other selected target consulted by producers/checkers. They form coupled
// rollback components. Resource observations are not fabricated by this package:
// UnavailableResources explicitly withholds dependent corrections. Resource-only
// sources cannot be supplied as targets merely to make them writable.
type Target struct {
	Path                 string
	Source               parser.Source
	Plan                 *engine.ExecutionPlan
	Produce              Producer
	Dependencies         []string
	UnavailableResources []string
	ResourceDependencies []string
}

// RoundContext exposes only declared source dependencies in one coordinated
// overlay. Source handles are immutable; parsed/analysis accessors return copies.
type RoundContext struct {
	path      string
	current   parser.Source
	trace     *engine.InspectionTrace
	analysis  []byte
	overlay   map[string]parser.Source
	meter     meter
	resources map[string]ResourceObservation
}

func (c RoundContext) Path() string            { return c.path }
func (c RoundContext) Current() parser.Source  { return c.current }
func (c RoundContext) Lines() parser.LineIndex { return c.trace.Lines() }
func (c RoundContext) TerminalEditEligible(ctx context.Context, id string, span parser.Span) (bool, error) {
	return engine.TerminalEditEligible(ctx, c.Parsed(), c.Directives(), id, span, c.meter.budget)
}
func (c RoundContext) Parsed() parser.ParseResult         { return c.trace.Parsed() }
func (c RoundContext) Directives() engine.DirectiveResult { return c.trace.Directives() }
func (c RoundContext) Source(path string) (parser.Source, bool) {
	s, ok := c.overlay[path]
	return s, ok
}
func (c RoundContext) Resource(key string) (ResourceObservation, bool) {
	r, ok := c.resources[key]
	return r, ok
}
func (c RoundContext) Analysis() result.Result { return decodeAnalysis(c.analysis) }

// BoundedAnalysis reserves the actual encoded collections before returning an
// owned view. Producers must not estimate analysis size from source byte count.
func (c RoundContext) BoundedAnalysis(ctx context.Context) (result.Result, error) {
	if err := ctx.Err(); err != nil {
		return result.Result{}, err
	}
	if err := reserveEncoded(c.meter, c.analysis); err != nil {
		return result.Result{}, err
	}
	out := decodeAnalysis(c.analysis)
	return out, c.Checkpoint(ctx, 0)
}

// Reserve charges callback-owned collections before constructing them.
func (c RoundContext) Reserve(ctx context.Context, work, nodes int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.meter.reserve(work, nodes)
}

func (c RoundContext) Checkpoint(ctx context.Context, work int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.meter.work(work)
}

// CheckContext describes the complete combined candidate, never an isolated edit.
// Before and After retain authoritative parses and actual shared rule results.
type CheckContext struct {
	Before, After RoundContext
	group         Group
	edits         []SharedEdit
}

func (c CheckContext) Group() Group        { return cloneGroup(c.group) }
func (c CheckContext) Edits() []SharedEdit { return StructuralPlan{edits: c.edits}.Edits() }

// BoundedViews reserves independent proposal/edit copies for trusted checkers.
func (c CheckContext) BoundedViews(ctx context.Context) (Group, []SharedEdit, error) {
	work := int64(len(c.group.ID)+len(c.group.Rule)+len(c.group.Provenance)) + 1
	nodes := int64(len(c.group.Edits)*3 + len(c.group.Requirements)*2 + len(c.edits)*4 + 4)
	if err := c.Before.Reserve(ctx, work+int64(len(c.group.Edits)+len(c.group.Requirements)+len(c.edits)), nodes); err != nil {
		return Group{}, nil, err
	}
	for _, e := range c.group.Edits {
		if err := c.Before.Checkpoint(ctx, int64(len(e.Expected)+len(e.Replacement))+1); err != nil {
			return Group{}, nil, err
		}
	}
	for _, r := range c.group.Requirements {
		if err := c.Before.Checkpoint(ctx, int64(len(r.Expected))+1); err != nil {
			return Group{}, nil, err
		}
	}
	for _, e := range c.edits {
		if err := c.Before.Reserve(ctx, int64(len(e.Edit.Expected)+len(e.Edit.Replacement)+len(e.Groups))+1, int64(len(e.Groups))); err != nil {
			return Group{}, nil, err
		}
		for _, g := range e.Groups {
			if err := c.Before.Checkpoint(ctx, int64(len(g))+1); err != nil {
				return Group{}, nil, err
			}
		}
	}
	return c.Group(), c.Edits(), c.Before.Checkpoint(ctx, 0)
}

// MapUnchanged maps an original source-owned fact into the combined candidate.
// A rewritten/intersected fact is explicitly unmappable, never guessed.
func (c CheckContext) MapUnchanged(span parser.Span) (parser.Span, bool, error) {
	return (editMapping{c.Before.current, c.After.current, c.edits, c.Before.meter}).span(span)
}

// Attempt anchors decisions to their actual round source, rather than relabeling
// intermediate offsets as original/final coordinates.
type Attempt struct {
	Round     int
	Source    parser.Source
	Decisions []Decision
}

// AnalysisFailure identifies a structured shared-engine operational failure when
// no more specific parser/callback cause exists. All error records remain in the
// corresponding analysis observation.
type AnalysisFailure struct {
	Path, Code string
	Category   result.ErrorCategory
}

func (e *AnalysisFailure) Error() string {
	return fmt.Sprintf("%s: %s shared analysis failure at %s", e.Category, e.Code, e.Path)
}

type observation struct {
	source   parser.Source
	analysis []byte
	retained *result.Result
}

// Observation retains reliable analysis even when that provisional source cannot
// be returned as a completed correction. It never authorizes saving its bytes.
type Observation struct {
	Source   parser.Source
	Analysis result.Result
}
type validatedFile struct {
	runtimeRollback     bool
	observations        []observation
	path                string
	original, candidate parser.Source
	analysis            []byte
	retained            *result.Result
	trace               *engine.InspectionTrace
	attempts            []Attempt
	reasons             []string
	rounds              int
}

// Validated owns final retained bytes and exact candidate analysis. Complete
// describes fix computation, independently of unavailable lint implementations.
// On interruption, affected candidates roll back and the typed error is returned.
type Validated struct {
	files    []validatedFile
	complete bool
}

func (v Validated) Complete() bool { return v.complete }
func (v Validated) Paths() []string {
	out := make([]string, len(v.files))
	for i, f := range v.files {
		out[i] = f.path
	}
	return out
}

type FileView struct {
	// AnalysisStarted distinguishes an entered shared analysis from a preinitialized
	// target retained after an earlier failure. Empty findings do not identify it.
	AnalysisStarted bool
	// RuntimeRollback identifies accepted attempts discarded by the returned
	// runtime failure. Completed intrinsic deferrals retain their own causes.
	RuntimeRollback     bool
	Path                string
	Original, Candidate parser.Source
	Analysis            result.Result
	Attempts            []Attempt
	Reasons             []string
	ChangingRounds      int
	Observations        []Observation
	trace               *engine.InspectionTrace
}

func (f FileView) Parsed() parser.ParseResult {
	if f.trace == nil {
		return parser.ParseResult{}
	}
	return f.trace.Parsed()
}
func (f FileView) Directives() engine.DirectiveResult {
	if f.trace == nil {
		return engine.DirectiveResult{}
	}
	return f.trace.Directives()
}
func (f FileView) Evaluations() []engine.Evaluation {
	if f.trace == nil {
		return nil
	}
	return f.trace.Evaluations()
}
func (v Validated) File(path string) (FileView, bool) {
	for _, f := range v.files {
		if f.path == path {
			a := make([]Attempt, len(f.attempts))
			for i, x := range f.attempts {
				a[i] = Attempt{x.Round, x.Source, StructuralPlan{decisions: x.Decisions}.Decisions()}
			}
			obs := make([]Observation, len(f.observations))
			for i, o := range f.observations {
				obs[i] = Observation{o.source, retainedAnalysis(o.analysis, o.retained)}
			}
			return FileView{AnalysisStarted: f.retained != nil, RuntimeRollback: f.runtimeRollback, Path: f.path, Original: f.original, Candidate: f.candidate, Analysis: retainedAnalysis(f.analysis, f.retained), Attempts: a, Reasons: slices.Clone(f.reasons), ChangingRounds: f.rounds, Observations: obs, trace: f.trace}, true
		}
	}
	return FileView{}, false
}
func decodeAnalysis(raw []byte) result.Result {
	var r result.Result
	_ = json.Unmarshal(raw, &r)
	return r
}

// After runtime exhaustion, retain the engine-owned typed result without doing
// additional fix work/encoding. External defensive views are report construction,
// not an attempt to resume the expired analysis budget.
func retainedAnalysis(raw []byte, retained *result.Result) result.Result {
	if len(raw) == 0 && retained != nil {
		raw, _ = json.Marshal(retained)
	}
	return decodeAnalysis(raw)
}

type validationState struct {
	target    Target
	trace     *engine.InspectionTrace
	analysis  []byte
	retained  *result.Result
	source    parser.Source
	resources map[string]ResourceObservation
}

func analyze(ctx context.Context, t Target, source parser.Source, m meter) (validationState, error) {
	in, err := t.Plan.NewSourceInput(t.Path, source)
	if err != nil {
		return validationState{}, err
	}
	out, trace, err := t.Plan.InspectCandidate(ctx, in, engine.RunOptions{Budget: m.budget})
	if err != nil {
		return validationState{}, err
	}
	state := validationState{target: t, trace: trace, source: source, retained: &out}
	// Reserve encoded ownership before encoding; strings include worst-case JSON
	// escapes and collections are visited/charged before construction. Decoder
	// storage for the consumer's owned view is included in this reservation.
	if err := reserveJSON(m, reflect.ValueOf(out)); err != nil {
		return state, err
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return state, err
	}
	if err := m.work(int64(len(raw))*2 + 1); err != nil {
		return state, err
	}
	state.analysis = raw
	if err := m.work(0); err != nil {
		return state, err
	}
	if trace.Interruption() != nil {
		return state, trace.Interruption()
	}
	if failures := trace.Failures(); len(failures) > 0 {
		return state, errors.Join(failures...)
	}
	if len(out.Errors) > 0 {
		if f := trace.Parsed().Failure; f != nil {
			return state, f
		}
		first := out.Errors[0]
		return state, &AnalysisFailure{Path: first.Path, Code: first.Code, Category: first.Category}
	}
	if trace.Parsed().Limit != nil {
		return state, trace.Parsed().Limit
	}
	return state, nil
}
func reserveJSON(m meter, v reflect.Value) error {
	if !v.IsValid() {
		return m.work(1)
	}
	if err := m.work(32); err != nil {
		return err
	}
	switch v.Kind() {
	case reflect.Invalid, reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr, reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128, reflect.Chan, reflect.Func, reflect.UnsafePointer:
	case reflect.Map:
		if err := m.reserve(int64(v.Len())+1, int64(v.Len())+1); err != nil {
			return err
		}
		iterator := v.MapRange()
		for iterator.Next() {
			if err := reserveJSON(m, iterator.Key()); err != nil {
				return err
			}
			if err := reserveJSON(m, iterator.Value()); err != nil {
				return err
			}
		}
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			return reserveJSON(m, v.Elem())
		}
	case reflect.String:
		return m.work(int64(v.Len())*6 + 2)
	case reflect.Slice, reflect.Array:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return reserveEncoded(m, v.Bytes())
		}
		if err := m.reserve(int64(v.Len())+1, int64(v.Len())+1); err != nil {
			return err
		}
		for i := 0; i < v.Len(); i++ {
			if err := reserveJSON(m, v.Index(i)); err != nil {
				return err
			}
		}
	case reflect.Struct:
		if err := m.reserve(1, 2); err != nil {
			return err
		}
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				if err := reserveJSON(m, v.Field(i)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// JSON object/array construction is counted independently of encoded bytes.
// Quoted braces and escapes never become artificial node consumption.
func reserveEncoded(m meter, raw []byte) error {
	if err := m.work(int64(len(raw))*3 + 1); err != nil {
		return err
	}
	quoted, escaped := false, false
	nodes := int64(1)
	for _, b := range raw {
		if quoted {
			if escaped {
				escaped = false
			} else if b == '\\' {
				escaped = true
			} else if b == '"' {
				quoted = false
			}
			continue
		}
		if b == '"' {
			quoted = true
		} else if b == '{' || b == '[' {
			nodes++
		}
	}
	return m.reserve(0, nodes*2)
}
func (s validationState) context(m meter, overlay map[string]parser.Source) RoundContext {
	return RoundContext{path: s.target.Path, current: s.source, trace: s.trace, analysis: s.analysis, overlay: overlay, meter: m, resources: s.resources}
}

// ValidationOptions carries operator-owned overlay admission and the original
// residual parser budget. Zero TotalContentBytes selects the shared 8 MiB default.
type ValidationOptions struct {
	Budget            *parser.OperationBudget
	TotalContentBytes int64
	Resources         []ResourceObservation
}

// Validate executes at most ten changing rounds per coupled component, followed
// by a read-only final planning/validation pass. No bytes are acquired or written.
// The same residual meter covers all targets, reparses, callbacks and rollbacks.
func (v *Validator) Validate(ctx context.Context, targets []Target, options ValidationOptions) (Validated, error) {
	budget := options.Budget
	limit := options.TotalContentBytes
	if limit == 0 {
		limit = 8 * 1024 * 1024
	}
	if limit < 1 || limit > 1<<60 {
		return Validated{}, errors.New("invalid candidate overlay limit")
	}
	if v == nil || ctx == nil || budget == nil {
		return Validated{}, errors.New("nil candidate validation context")
	}
	m := meter{ctx, budget}
	if err := m.work(0); err != nil {
		return Validated{}, err
	}
	if err := m.reserve(int64(len(targets))*16+1, int64(len(targets))*8+1); err != nil {
		return Validated{}, err
	}
	targets = slices.Clone(targets)
	if err := sortMetered(m, targets, func(a, b Target) int64 { return int64(len(a.Path)+len(b.Path)) + 1 }, func(a, b Target) int { return bytes.Compare([]byte(a.Path), []byte(b.Path)) }); err != nil {
		return Validated{}, err
	}
	resources := map[string]ResourceObservation{}
	resourceBytes := int64(0)
	for _, r := range options.Resources {
		if int64(r.Source.Len()) > budget.Limits().DocumentBytes {
			return Validated{}, &parser.LimitExceeded{Name: "document-bytes", Observed: int64(r.Source.Len()), Allowed: budget.Limits().DocumentBytes}
		}
		if err := m.reserve(int64(len(r.Key)+r.Source.Len())+1, 1); err != nil {
			return Validated{}, err
		}
		if r.Key == "" || resources[r.Key].Key != "" || r.State != ResourceContent && r.State != ResourceAbsent && r.State != ResourceUnavailable {
			return Validated{}, errors.New("invalid resource observation")
		}
		if r.State != ResourceContent && r.Source.Len() != 0 {
			return Validated{}, errors.New("non-content resource has bytes")
		}
		resources[r.Key] = r
		resourceBytes += int64(r.Source.Len())
	}
	index := map[string]int{}
	total := resourceBytes
	for i, t := range targets {
		if t.Path == "" || !t.Plan.UsesRegistry(v.registry) || i > 0 && targets[i-1].Path == t.Path {
			return Validated{}, errors.New("invalid or duplicate correction target")
		}
		if err := m.work(int64(t.Source.Len()+len(t.Path)+len(t.Dependencies)+len(t.UnavailableResources)) + 1); err != nil {
			return Validated{}, err
		}
		if int64(t.Source.Len()) > budget.Limits().DocumentBytes {
			return Validated{}, &parser.LimitExceeded{Name: "document-bytes", Observed: int64(t.Source.Len()), Allowed: budget.Limits().DocumentBytes}
		}
		if err := m.reserve(int64(len(t.Dependencies)+len(t.ResourceDependencies)+len(t.UnavailableResources))+1, int64(len(t.Dependencies)+len(t.ResourceDependencies)+len(t.UnavailableResources))+1); err != nil {
			return Validated{}, err
		}
		for _, keys := range [][]string{t.Dependencies, t.ResourceDependencies, t.UnavailableResources} {
			for _, key := range keys {
				if err := m.work(int64(len(key)) + 1); err != nil {
					return Validated{}, err
				}
			}
		}
		targets[i].Plan = t.Plan.Snapshot()
		targets[i].Dependencies = slices.Clone(t.Dependencies)
		targets[i].ResourceDependencies = slices.Clone(t.ResourceDependencies)
		targets[i].UnavailableResources = slices.Clone(t.UnavailableResources)
		total += int64(t.Source.Len())
		index[t.Path] = i
	}
	// Acquisition total is caller-owned; each independent overlay has this cap.
	if total > limit {
		return Validated{}, &parser.LimitExceeded{Name: "total-content-bytes", Observed: total, Allowed: limit}
	}
	u := newUnion(len(targets))
	for i, t := range targets {
		for _, dep := range t.Dependencies {
			if err := m.work(int64(len(dep)) + 1); err != nil {
				return Validated{}, err
			}
			j, ok := index[dep]
			if !ok {
				return Validated{}, fmt.Errorf("unknown correction dependency %s", dep)
			}
			u.join(i, j)
		}
	}
	states := make([]validationState, len(targets))
	initial := make([]validationState, len(targets))
	out := Validated{files: make([]validatedFile, len(targets)), complete: true}
	for i, t := range targets {
		out.files[i] = validatedFile{path: t.Path, original: t.Source, candidate: t.Source}
	}
	for i, t := range targets {
		t.Plan = targets[i].Plan
		s, err := analyze(ctx, t, t.Source, m)
		out.files[i] = validatedFile{path: t.Path, original: t.Source, candidate: t.Source, analysis: s.analysis, retained: s.retained, trace: s.trace}
		s.resources = map[string]ResourceObservation{}
		for _, key := range t.ResourceDependencies {
			if err := m.reserve(int64(len(key))+1, 1); err != nil {
				out.complete = false
				return out, err
			}
			r, ok := resources[key]
			if !ok || r.State == ResourceUnavailable {
				t.UnavailableResources = append(t.UnavailableResources, key)
			} else {
				s.resources[key] = r
			}
		}
		s.target = t
		states[i] = s
		initial[i] = s
		out.files[i] = validatedFile{path: t.Path, original: t.Source, candidate: t.Source, analysis: s.analysis, retained: s.retained, trace: s.trace}
		if err != nil {
			out.complete = false
			out.files[i].reasons = append(out.files[i].reasons, "analysis-failed")
			return out, err
		}
	}
	// Every component is closed over all declared dependencies. Independent
	// components can therefore retain converged work when a neighbor rolls back.
	components := map[int][]int{}
	var order []int
	for i := range targets {
		if err := m.reserve(1, 1); err != nil {
			out.complete = false
			return out, err
		}
		key := u.root(i)
		if len(components[key]) == 0 {
			order = append(order, key)
		}
		components[key] = append(components[key], i)
	}
	overlayBytes := total
	for _, key := range order {
		members := components[key]
		if err := m.reserve(int64(len(members))*4+1, int64(len(members))*4+1); err != nil {
			out.complete = false
			return out, err
		}
		local := make([]validationState, len(members))
		localMembers := make([]int, len(members))
		localOut := Validated{files: make([]validatedFile, len(members))}
		componentBytes := int64(0)
		for i, j := range members {
			local[i] = states[j]
			localMembers[i] = i
			localOut.files[i] = out.files[j]
			componentBytes += int64(states[j].source.Len())
		}
		reason, err := v.component(ctx, local, localMembers, &localOut, m, limit-(overlayBytes-componentBytes))
		for i, j := range members {
			out.files[j] = localOut.files[i]
		}
		if err != nil || reason != "" {
			out.complete = false
			if reason == "" {
				reason = "validation-failed"
				if errors.Is(err, context.Canceled) {
					reason = "cancelled"
				}
				var exceeded *parser.LimitExceeded
				if errors.As(err, &exceeded) {
					reason = "limit-exceeded"
				}
				if errors.Is(err, context.DeadlineExceeded) {
					reason = "deadline-exceeded"
				}
			}
			for _, j := range members {
				if err != nil {
					out.files[j].runtimeRollback = true
				}
				states[j] = initial[j]
				out.files[j].candidate = targets[j].Source
				out.files[j].analysis = initial[j].analysis
				out.files[j].retained = initial[j].retained
				out.files[j].trace = initial[j].trace
				out.files[j].reasons = append(out.files[j].reasons, reason)
			}
			if err != nil {
				return out, err
			}
		} else {
			for i, j := range members {
				states[j] = local[i]
			}
		}
		for _, j := range members {
			overlayBytes += int64(states[j].source.Len())
		}
		overlayBytes -= componentBytes
	}
	// Reanalyze every retained final source after component rollback. Results
	// never describe a mixture of provisional and retained dependency states.
	for i, s := range states {
		refreshed, err := analyze(ctx, s.target, s.source, m)
		if refreshed.trace != nil {
			if err == nil && !bytes.Equal(refreshed.analysis, s.analysis) {
				err = &AnalysisFailure{Path: s.target.Path, Code: "unstable-reanalysis", Category: result.Internal}
			}
			if err != nil {
				out.files[i].observations = []observation{{s.source, refreshed.analysis, refreshed.retained}}
			}
			out.files[i].analysis = refreshed.analysis
			out.files[i].retained = refreshed.retained
			out.files[i].trace = refreshed.trace
		}
		if err != nil {
			out.complete = false
			for j := range out.files {
				if out.files[j].candidate != targets[j].Source {
					out.files[j].runtimeRollback = true
				}
				out.files[j].candidate = targets[j].Source
				out.files[j].analysis = initial[j].analysis
				out.files[j].retained = initial[j].retained
				out.files[j].trace = initial[j].trace
				out.files[j].reasons = append(out.files[j].reasons, "validation-failed")
			}
			return out, err
		}
	}
	if err := m.work(0); err != nil {
		out.complete = false
		for i := range out.files {
			if out.files[i].candidate != targets[i].Source {
				out.files[i].runtimeRollback = true
			}
			out.files[i].candidate = targets[i].Source
			out.files[i].analysis = initial[i].analysis
			out.files[i].retained = initial[i].retained
			out.files[i].trace = initial[i].trace
			out.files[i].reasons = append(out.files[i].reasons, "interrupted")
		}
		return out, err
	}
	return out, nil
}
func componentOverlay(states []validationState, members []int) map[string]parser.Source {
	overlay := make(map[string]parser.Source, len(members))
	for _, j := range members {
		overlay[states[j].target.Path] = states[j].source
	}
	return overlay
}
func overlayDigest(states []validationState, members []int, m meter) ([32]byte, error) {
	h := sha256.New()
	for _, j := range members {
		s := states[j].source
		if err := m.work(int64(s.Len()) + 16); err != nil {
			return [32]byte{}, err
		}
		_, _ = fmt.Fprintf(h, "%d:", s.Len())
		_, _ = h.Write(s.Bytes())
	}
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return sum, nil
}
func (v *Validator) component(ctx context.Context, states []validationState, members []int, out *Validated, m meter, limit int64) (string, error) {
	seen := map[[32]byte]bool{}
	digest, err := overlayDigest(states, members, m)
	if err != nil {
		return "", err
	}
	seen[digest] = true
	for round := 0; round <= 10; round++ {
		if err := m.reserve(int64(len(members))*8+1, int64(len(members))*4+1); err != nil {
			return "", err
		}
		before := componentOverlay(states, members)
		if err := m.reserve(int64(len(states)), int64(len(states))); err != nil {
			return "", err
		}
		next := slices.Clone(states)
		plans := map[int]StructuralPlan{}
		changed := false
		unavailable := false
		for _, j := range members {
			s := states[j]
			t := s.target
			if len(t.UnavailableResources) > 0 {
				unavailable = true
				out.files[j].reasons = append(out.files[j].reasons, "context-unavailable")
				continue
			}
			if t.Produce == nil {
				continue
			}
			if t.Plan.Context().Flavor != "commonmark" || s.trace.Parsed().Status != parser.Complete || !s.trace.Directives().Complete() {
				unavailable = true
				out.files[j].reasons = append(out.files[j].reasons, "parser-uncertain")
				continue
			}
			if err := m.work(1); err != nil {
				return "", err
			}
			proposals, err := t.Produce(ctx, s.context(m, before))
			if err != nil {
				return "", err
			}
			if err := m.work(0); err != nil {
				return "", err
			}
			p, err := Plan(ctx, s.trace.Parsed(), s.trace.Directives(), v.registry, proposals, m.budget)
			if err != nil {
				return "", err
			}
			plans[j] = p
			if err := m.reserve(int64(len(p.decisions))*2+1, int64(len(p.decisions))+1); err != nil {
				return "", err
			}
			out.files[j].attempts = append(out.files[j].attempts, Attempt{round + 1, s.source, p.Decisions()})
			if bytes.Equal(p.candidate, s.source.Bytes()) {
				continue
			}
			changed = true
			if err := m.work(int64(len(p.candidate)) + 1); err != nil {
				return "", err
			}
			ns, err := analyze(ctx, t, parser.NewSource(p.candidate), m)
			if ns.trace != nil {
				out.files[j].observations = []observation{{ns.source, ns.analysis, ns.retained}}
			}
			if err != nil {
				return "", err
			}
			ns.resources = s.resources
			next[j] = ns
		}
		overlayBytes := int64(0)
		for _, s := range next {
			overlayBytes += int64(s.source.Len())
		}
		if overlayBytes > limit {
			return "", &parser.LimitExceeded{Name: "total-content-bytes", Observed: overlayBytes, Allowed: limit}
		}
		after := componentOverlay(next, members)
		unsafe := false
		failureCodes := map[int]string{}
		for _, j := range members {
			p, ok := plans[j]
			if !ok {
				continue
			}
			safe, err := commonPreservation(ctx, states[j], next[j], p, m)
			if err != nil {
				return "", err
			}
			if !safe {
				unsafe = true
				failureCodes[j] = "candidate-preservation-failed"
			}
			for _, d := range p.decisions {
				if !d.Accepted {
					continue
				}
				checker := v.checkers[d.Group.Rule]
				if checker == nil {
					unsafe = true
					failureCodes[j] = "checker-unavailable"
					continue
				}
				if err := m.work(1); err != nil {
					return "", err
				}
				// Only this group's owned callback input is constructed here. The complete
				// round edit set stays private and immutable; Edits() copies it on demand.
				copyWork := int64(len(d.Group.Edits))*4 + int64(len(d.Group.Requirements))*3 + 1
				copyNodes := int64(len(d.Group.Edits))*3 + int64(len(d.Group.Requirements))*2 + 3
				for _, e := range d.Group.Edits {
					copyWork += int64(len(e.Expected)) + int64(len(e.Replacement))
				}
				for _, r := range d.Group.Requirements {
					copyWork += int64(len(r.Expected))
				}
				if err := m.reserve(copyWork, copyNodes); err != nil {
					return "", err
				}
				checkContext := CheckContext{states[j].context(m, before), next[j].context(m, after), cloneGroup(d.Group), p.edits}
				proof, err := checker(ctx, checkContext)
				if err != nil {
					return "", err
				}
				if err := m.work(0); err != nil {
					return "", err
				}
				if proof == Preserved {
					allowed, err := preserveFindings(checkContext, v.forbidden[d.Group.Rule])
					if err != nil {
						return "", err
					}
					if !allowed {
						proof = Unsafe
						failureCodes[j] = "forbidden-new-finding"
					}
				}
				if proof != Preserved {
					unsafe = true
					if failureCodes[j] == "" {
						failureCodes[j] = "meaning-not-preserved"
					}
					if proof != Unsafe {
						unavailable = true
						failureCodes[j] = "preservation-uncertain"
					}
				}
			}
		}
		if unsafe {
			// Coupled batches are deliberately deferred together. No individually safe
			// group may escape under approval of a different combined overlay.
			for _, j := range members {
				a := out.files[j].attempts
				if len(a) == 0 || a[len(a)-1].Round != round+1 {
					continue
				}
				code := failureCodes[j]
				if code == "" {
					code = "coupled-preservation"
				}
				last := &a[len(a)-1]
				for k := range last.Decisions {
					if last.Decisions[k].Accepted {
						last.Decisions[k].Accepted = false
						last.Decisions[k].Reasons = append(last.Decisions[k].Reasons, Reason{Code: code, Owner: -1})
					}
				}
			}
			if unavailable {
				return "context-unavailable", nil
			}
			return "", nil
		}
		if !changed {
			if unavailable {
				return "context-unavailable", nil
			}
			return "", nil
		}
		if round == 10 {
			return "fix-nonconvergent", nil
		}
		digest, err = overlayDigest(next, members, m)
		if err != nil {
			return "", err
		}
		if seen[digest] {
			return "fix-nonconvergent", nil
		}
		if err := m.reserve(1, 1); err != nil {
			return "", err
		}
		seen[digest] = true
		for _, j := range members {
			if states[j].source != next[j].source {
				out.files[j].rounds++
			}
			states[j] = next[j]
			out.files[j].candidate = next[j].source
			out.files[j].analysis = next[j].analysis
			out.files[j].retained = next[j].retained
			out.files[j].trace = next[j].trace

		}
	}
	return "fix-nonconvergent", nil
}
