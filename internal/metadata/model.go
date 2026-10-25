// Package metadata defines source-owned metadata values and consumer policy.
// Format adapters decode recognized YAML and TOML on demand.
// No model or eligibility value authorizes an edit to opaque frontmatter.
package metadata

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/parser"
)

var ErrContract = errors.New("invalid metadata source or operation contract")

type HeaderState string

const (
	Absent       HeaderState = "absent"
	Unterminated HeaderState = "unterminated"
	Undecoded    HeaderState = "undecoded"
)

// Binding is one genuine recognized header and its original shared operation.
// No constructor accepts source ranges or a caller-supplied completeness flag.
type Binding struct{ state *binding }
type binding struct {
	source    parser.Source
	header    parser.Frontmatter
	budget    *parser.OperationBudget
	operation context.Context
	state     HeaderState
}

func Bind(ctx, operation context.Context, budget *parser.OperationBudget, document *parser.Document) (Binding, error) {
	if ctx == nil || operation == nil || budget == nil || document == nil {
		return Binding{}, ErrContract
	}
	source := document.Source()
	if source.Token() == (parser.SourceToken{}) {
		return Binding{}, ErrContract
	}
	b := Binding{&binding{source: source, budget: budget, operation: operation, state: Absent}}
	header, present := document.Frontmatter()
	if !present {
		return b, b.charge(ctx, 1, 2)
	}
	if !header.OwnsOperation(budget) {
		return Binding{}, ErrContract
	}
	b.state.header = header
	b.state.state = Undecoded
	if err := b.charge(ctx, 1, 2); err != nil {
		return Binding{}, err
	}
	if !header.Closer().Present() {
		b.state.state = Unterminated
	}
	return b, nil
}
func (b Binding) State() HeaderState {
	if b.state == nil {
		return Absent
	}
	return b.state.state
}
func (b Binding) Source() parser.Source {
	if b.state == nil {
		return parser.Source{}
	}
	return b.state.source
}
func (b Binding) Header() parser.Frontmatter {
	if b.state == nil {
		return parser.Frontmatter{}
	}
	return b.state.header
}
func (b Binding) charge(ctx context.Context, work, nodes int64) error {
	if b.state == nil || ctx == nil {
		return ErrContract
	}
	var documentStop error
	if b.state.header.OwnsOperation(b.state.budget) {
		documentStop = b.state.header.ReserveRecords(ctx, b.state.budget, 0)
	}
	if err := errors.Join(ctx.Err(), b.state.operation.Err(), documentStop, b.state.budget.ChargeWork(0)); err != nil {
		return err
	}
	if err := b.state.budget.ChargeWork(work); err != nil {
		return errors.Join(ctx.Err(), b.state.operation.Err(), err)
	}
	var err error
	if b.state.header.OwnsOperation(b.state.budget) {
		err = b.state.header.ReserveRecords(ctx, b.state.budget, nodes)
	} else {
		err = b.state.budget.ChargeNodes(nodes)
	}
	if err != nil {
		return errors.Join(ctx.Err(), b.state.operation.Err(), err)
	}
	return errors.Join(ctx.Err(), b.state.operation.Err())
}
func (b Binding) owns(span parser.Span) bool {
	if b.state == nil || b.State() != Undecoded || !span.Present() || span.SourceToken() != b.state.source.Token() {
		return false
	}
	payload := b.state.header.Content()
	return span.Start() >= payload.Start() && span.End() <= payload.End()
}

type Kind string

const (
	Unknown        Kind = "unknown"
	Mapping        Kind = "mapping"
	Sequence       Kind = "sequence"
	String         Kind = "string"
	Integer        Kind = "integer"
	Float          Kind = "float"
	Boolean        Kind = "boolean"
	Null           Kind = "null"
	OffsetDateTime Kind = "offset-date-time"
	LocalDateTime  Kind = "local-date-time"
	LocalDate      Kind = "local-date"
	LocalTime      Kind = "local-time"
)

// Value is immutable. Span is the original authored spelling or alias use;
// synthesized TOML parent maps have an absent Span. Scalar keeps native kind
// and exact semantic representation separate from original source spelling.
// Numeric semantics use canonical decimal text (including float specials), so
// integers never pass through float64. Temporal semantics retain native kind
// and canonical civil/offset spelling, never an implicit server timezone.
type Value struct{ state *value }
type value struct {
	owner        *binding
	kind         Kind
	span         parser.Span
	scalar       string
	boolean      bool
	entries      []Entry
	elements     []Value
	complete     bool
	depth        int64
	problems     []Problem
	restrictions MoveRestrictions
}

func (v Value) Kind() Kind {
	if v.state == nil {
		return Unknown
	}
	return v.state.kind
}
func (v Value) Span() parser.Span {
	if v.state == nil {
		return parser.Span{}
	}
	return v.state.span
}
func (v Value) Complete() bool { return v.state != nil && v.state.complete }
func (v Value) Scalar() (string, bool) {
	switch v.Kind() {
	case String, Integer, Float, OffsetDateTime, LocalDateTime, LocalDate, LocalTime:
		return v.state.scalar, true
	default:
		return "", false
	}
}
func (v Value) Boolean() (bool, bool) {
	if v.Kind() != Boolean {
		return false, false
	}
	return v.state.boolean, true
}
func (v Value) EntryCount() int {
	if v.state == nil {
		return 0
	}
	return len(v.state.entries)
}
func (v Value) KnownEntry(i int) (Entry, bool) {
	if i < 0 || i >= v.EntryCount() {
		return Entry{}, false
	}
	return v.state.entries[i], true
}
func (v Value) ElementCount() int {
	if v.state == nil {
		return 0
	}
	return len(v.state.elements)
}
func (v Value) KnownElement(i int) (Value, bool) {
	if i < 0 || i >= v.ElementCount() {
		return Value{}, false
	}
	return v.state.elements[i], true
}

// Entry distinguishes an authored contiguous entry from a synthesized table path.
// Missing source spans never become a guessed envelope for noncontiguous TOML.
type Entry struct {
	key           string
	keySpan, span parser.Span
	value         Value
}

func (e Entry) Key() string          { return e.key }
func (e Entry) KeySpan() parser.Span { return e.keySpan }
func (e Entry) Span() parser.Span    { return e.span }
func (e Entry) Value() Value         { return e.value }

// Private builders are the decoder boundary, not public data admission.
// They freeze collections and reject foreign source records before publication.
func scalar(ctx context.Context, b Binding, span parser.Span, kind Kind, text string, boolean bool) (Value, error) {
	if !b.owns(span) {
		return Value{}, ErrContract
	}
	if err := b.charge(ctx, int64(len(text))+1, int64(len(text))+1); err != nil {
		return Value{}, err
	}
	if !utf8.ValidString(text) {
		return Value{}, ErrContract
	}
	switch kind {
	case Unknown, String, Integer, Float, Boolean, Null, OffsetDateTime, LocalDateTime, LocalDate, LocalTime:
	default:
		return Value{}, ErrContract
	}
	return Value{&value{owner: b.state, kind: kind, span: span, scalar: text, boolean: boolean, complete: kind != Unknown, depth: 1}}, nil
}
func entry(ctx context.Context, b Binding, key string, keySpan, span parser.Span, v Value) (Entry, error) {
	if b.state == nil || v.state == nil || v.state.owner != b.state || !utf8.ValidString(key) {
		return Entry{}, ErrContract
	}
	if keySpan.Present() && !b.owns(keySpan) || span.Present() && !b.owns(span) {
		return Entry{}, ErrContract
	}
	if span.Present() && (!keySpan.Present() || keySpan.Start() < span.Start() || keySpan.End() > span.End() || v.Span().Present() && (v.Span().Start() < span.Start() || v.Span().End() > span.End())) {
		return Entry{}, ErrContract
	}
	if err := b.charge(ctx, int64(len(key))+1, int64(len(key))+1); err != nil {
		return Entry{}, err
	}
	return Entry{key, keySpan, span, v}, nil
}
func collection(ctx context.Context, b Binding, span parser.Span, kind Kind, entries []Entry, elements []Value, restrictions MoveRestrictions) (Value, error) {
	if b.state == nil || b.State() != Undecoded || span.Present() && !b.owns(span) || kind != Mapping && kind != Sequence || kind == Mapping && len(elements) != 0 || kind == Sequence && len(entries) != 0 {
		return Value{}, ErrContract
	}
	if err := b.charge(ctx, int64(len(entries)+len(elements))+1, int64(len(entries)+len(elements))+1); err != nil {
		return Value{}, err
	}
	complete := true
	depth := int64(1)
	var problems []Problem
	seen := map[string]bool{}
	for _, e := range entries {
		if e.value.state == nil || e.value.state.owner != b.state {
			return Value{}, ErrContract
		}
		if err := b.charge(ctx, int64(len(e.key))+1, 1); err != nil {
			return Value{}, err
		}
		if seen[e.key] {
			complete = false
			if err := b.charge(ctx, 1, 2); err != nil {
				return Value{}, err
			}
			location := e.keySpan
			if !location.Present() {
				location = b.Header().Content()
			}
			problems = append(problems, Problem{location, DuplicateKey})
		}
		depth = max(depth, e.value.state.depth+1)
		seen[e.key] = true
		if span.Present() && (e.span.Present() && !within(span, e.span) || e.keySpan.Present() && !within(span, e.keySpan) || e.value.Span().Present() && !within(span, e.value.Span())) {
			return Value{}, ErrContract
		}
		complete = complete && e.value.Complete()
		restrictions |= e.value.state.restrictions
	}
	for _, v := range elements {
		if v.state == nil || v.state.owner != b.state {
			return Value{}, ErrContract
		}
		if span.Present() && v.Span().Present() && !within(span, v.Span()) {
			return Value{}, ErrContract
		}
		complete = complete && v.Complete()
		restrictions |= v.state.restrictions
		depth = max(depth, v.state.depth+1)
	}
	if depth > b.state.budget.Limits().SyntaxDepth {
		return Value{}, &parser.LimitExceeded{Name: "syntax-depth", Observed: depth, Allowed: b.state.budget.Limits().SyntaxDepth}
	}
	return Value{&value{owner: b.state, kind: kind, span: span, entries: append([]Entry(nil), entries...), elements: append([]Value(nil), elements...), complete: complete, depth: depth, problems: problems, restrictions: restrictions}}, nil
}

// Result has no public constructor for trusted decoded completeness. The zero
// value is unavailable. Decode is the only source-based producer.
type Result struct {
	owner    *binding
	root     Value
	problems []Problem
	complete bool
}
type Problem struct {
	Location parser.Span
	Code     ProblemCode
}
type ProblemCode string

const (
	Malformed    ProblemCode = "malformed"
	Unsupported  ProblemCode = "unsupported"
	DuplicateKey ProblemCode = "duplicate-key"
)

func (r Result) Root() Value       { return r.root }
func (r Result) Complete() bool    { return r.owner != nil && r.complete }
func (r Result) ProblemCount() int { return len(r.problems) }
func (r Result) KnownProblem(i int) (Problem, bool) {
	if i < 0 || i >= len(r.problems) {
		return Problem{}, false
	}
	return r.problems[i], true
}
func result(ctx context.Context, b Binding, root Value, problems []Problem) (Result, error) {
	if b.state == nil || b.State() != Undecoded || root.state == nil || root.Kind() != Mapping && root.Kind() != Unknown || root.state.owner != b.state || root.Span() != b.Header().Content() {
		return Result{}, ErrContract
	}
	if err := b.charge(ctx, int64(len(problems))+1, int64(len(problems))+1); err != nil {
		return Result{}, err
	}
	for _, p := range problems {
		if !b.owns(p.Location) {
			return Result{}, ErrContract
		}
	}
	out := Result{owner: b.state, root: root, problems: append([]Problem(nil), problems...), complete: root.Complete() && len(problems) == 0}
	var collect func(Value) error
	collect = func(v Value) error {
		if err := b.charge(ctx, int64(len(v.state.problems)+len(v.state.entries)+len(v.state.elements))+1, int64(len(v.state.problems))); err != nil {
			return err
		}
		out.problems = append(out.problems, v.state.problems...)
		for _, e := range v.state.entries {
			if err := collect(e.value); err != nil {
				return err
			}
		}
		for _, e := range v.state.elements {
			if err := collect(e); err != nil {
				return err
			}
		}
		return nil
	}
	if err := collect(root); err != nil {
		return Result{}, err
	}
	return out, nil
}

func within(outer, inner parser.Span) bool {
	return outer.SourceToken() == inner.SourceToken() && inner.Start() >= outer.Start() && inner.End() <= outer.End()
}
