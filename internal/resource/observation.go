package resource

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

type Existence string

const (
	File      Existence = "file"
	Directory Existence = "directory"
	Missing   Existence = "missing"
	Unknown   Existence = "unknown"
)

type Availability string

const (
	NotRequested Availability = "not-requested"
	Available    Availability = "available"
	Unavailable  Availability = "unavailable"
	Inapplicable Availability = "inapplicable"
	Uncertain    Availability = "uncertain"
	Failed       Availability = "failed"
	Interrupted  Availability = "interrupted"
)

type ProblemKind string

const (
	NoProblem          ProblemKind = ""
	PartialInventory   ProblemKind = "partial-inventory"
	MalformedReference ProblemKind = "invalid-reference"
	Blocked            ProblemKind = "blocked"
	NotSupplied        ProblemKind = "not-supplied"
	ReadFailure        ProblemKind = "read-failure"
	DecodeFailure      ProblemKind = "decode-failure"
	ParserUncertain    ProblemKind = "parser-uncertain"
	Cancelled          ProblemKind = "cancelled"
	Deadline           ProblemKind = "deadline-exceeded"
	Limited            ProblemKind = "limit-exceeded"
)

// Problem is passive evidence. Limits are copied when an observation is bound.
type Problem struct {
	Kind    ProblemKind
	Message string
	Limit   *result.LimitObservation
}

// ObservationSpec is adapter-supplied evidence, validated and copied by Record.
// Parsed producers MUST use Operation.Settings().ParseOptions(). Record validates
// source, completeness and budget ownership, but cannot authenticate parse-policy
// provenance. Parsed must be complete when Anchors is Available.
// This establishes parsed-source availability, NOT a computed anchor index.
// An actual index/projection and its algorithm remain #254's responsibility.
type ObservationSpec struct {
	Existence        Existence
	Content, Anchors Availability
	Source           parser.Source
	Validated        ContentEvidence
	Parsed           *parser.ParseResult
	Problem          Problem
}
type Observation struct {
	request          Request
	existence        Existence
	content, anchors Availability
	source           parser.Source
	problem          Problem
}

func (o Observation) Request() Request                  { return o.request }
func (o Observation) Existence() Existence              { return o.existence }
func (o Observation) ContentAvailability() Availability { return o.content }
func (o Observation) AnchorAvailability() Availability  { return o.anchors }
func (o Observation) Content() (parser.Source, bool)    { return o.source, o.content == Available }
func (o Observation) Problem() Problem                  { return copyProblem(o.problem) }
func copyProblem(p Problem) Problem {
	if p.Limit != nil {
		v := *p.Limit
		p.Limit = &v
	}
	return p
}

// ContentEvidence is operation-owned immutable UTF-8 validation evidence. Its
// source can be retained after a later anchor stop without rescanning bytes or
// charging an already exhausted execution budget. Zero/foreign evidence grants
// nothing. It does not establish inventory truth, existence or parsed policy.
type contentIdentity struct{ _ byte }

type ContentEvidence struct {
	identity  *contentIdentity
	operation *Operation
	source    parser.Source
}

func (e ContentEvidence) Source() parser.Source { return e.source }

// ValidateContent must run while execution is permitted, before optional parsing
// or anchor work. Record can use the resulting evidence after that work stops.
func (o *Operation) ValidateContent(source parser.Source) (ContentEvidence, error) {
	if o == nil || source == (parser.Source{}) || int64(source.Len()) > o.budget.Limits().DocumentBytes {
		return ContentEvidence{}, ErrContract
	}
	if err := o.Checkpoint(int64(source.Len()) + 1); err != nil {
		return ContentEvidence{}, err
	}
	if err := o.budget.ChargeNodes(1); err != nil {
		return ContentEvidence{}, err
	}
	if !utf8.Valid(source.Bytes()) {
		return ContentEvidence{}, ErrContract
	}
	if err := o.Checkpoint(0); err != nil {
		return ContentEvidence{}, err
	}
	return ContentEvidence{operation: o, source: source, identity: &contentIdentity{}}, nil
}

// Record binds completed evidence to its original request/view. It is also usable
// to retain known existence plus an interruption after cancellation. The request
// reserved its record allowance; no new execution budget is created here.
func (o *Operation) Record(request Request, spec ObservationSpec) (Observation, error) {
	if o == nil || request.view.operation != o || request.view.identity == nil || request.identity == nil {
		return Observation{}, ErrContract
	}
	if spec.Existence != File && spec.Existence != Directory && spec.Existence != Missing && spec.Existence != Unknown {
		return Observation{}, ErrContract
	}
	validAvailability := func(a Availability) bool {
		return a == NotRequested || a == Available || a == Unavailable || a == Inapplicable || a == Uncertain || a == Failed || a == Interrupted
	}
	if !validAvailability(spec.Content) || !validAvailability(spec.Anchors) {
		return Observation{}, ErrContract
	}
	switch spec.Problem.Kind {
	case NoProblem, PartialInventory, MalformedReference, Blocked, NotSupplied, ReadFailure, DecodeFailure, ParserUncertain, Cancelled, Deadline, Limited:
	default:
		return Observation{}, ErrContract
	}
	if spec.Problem.Kind == Limited {
		if spec.Problem.Limit == nil || spec.Problem.Limit.Name == "" || spec.Problem.Limit.Allowed < 0 || spec.Problem.Limit.Observed <= spec.Problem.Limit.Allowed {
			return Observation{}, ErrContract
		}
	} else if spec.Problem.Limit != nil {
		return Observation{}, ErrContract
	}
	if spec.Problem.Kind != NoProblem && spec.Problem.Message == "" {
		return Observation{}, ErrContract
	}
	if spec.Existence == Directory || spec.Existence == Missing {
		if spec.Content != Inapplicable || spec.Anchors != Inapplicable {
			return Observation{}, ErrContract
		}
	}
	if spec.Content == Available {
		if spec.Existence != File || spec.Source == (parser.Source{}) {
			return Observation{}, ErrContract
		}
		if spec.Validated.operation != nil {
			if spec.Validated.operation != o || spec.Validated.source != spec.Source {
				return Observation{}, ErrContract
			}
		} else if _, err := o.ValidateContent(spec.Source); err != nil {
			return Observation{}, err
		}
	} else if spec.Source != (parser.Source{}) || spec.Validated.operation != nil {
		return Observation{}, ErrContract
	}
	if spec.Anchors == Available {
		p := spec.Parsed
		if spec.Content != Available || p == nil || p.Document == nil || p.Source != spec.Source || p.Document.Source() != spec.Source || p.Status != parser.Complete || p.AST != parser.Available || p.Failure != nil || len(p.Uncertainty) != 0 || p.Interruption != "" {
			return Observation{}, ErrContract
		}
		if _, err := p.Document.Comments(o.ctx, o.budget); err != nil {
			return Observation{}, err
		}
	} else if spec.Parsed != nil {
		return Observation{}, ErrContract
	}
	if spec.Existence == Unknown && (spec.Content == Available || spec.Anchors == Available || spec.Content == Inapplicable || spec.Anchors == Inapplicable) {
		return Observation{}, ErrContract
	}
	failure := spec.Problem.Kind == ReadFailure || spec.Problem.Kind == DecodeFailure
	stop := spec.Problem.Kind == Cancelled || spec.Problem.Kind == Deadline || spec.Problem.Kind == Limited
	if (spec.Content == Failed || spec.Anchors == Failed) && !failure {
		return Observation{}, ErrContract
	}
	if (spec.Content == Interrupted || spec.Anchors == Interrupted) && !stop {
		return Observation{}, ErrContract
	}
	if stop && spec.Content != Interrupted && spec.Anchors != Interrupted {
		return Observation{}, ErrContract
	}
	if failure && spec.Content != Failed && spec.Anchors != Failed && spec.Existence != Unknown {
		return Observation{}, ErrContract
	}
	if (spec.Existence == Missing || spec.Existence == Directory) && spec.Problem.Kind != NoProblem {
		return Observation{}, ErrContract
	}
	if spec.Existence == Unknown && spec.Problem.Kind == NoProblem && request.Class() != Ignored {
		return Observation{}, ErrContract
	}
	if request.Class() == Ignored && (spec.Problem.Kind != NoProblem || spec.Existence != Unknown || spec.Content != NotRequested || spec.Anchors != NotRequested) {
		return Observation{}, ErrContract
	}
	if request.Class() == BlockedReference && (spec.Existence != Unknown || spec.Problem.Kind != Blocked) {
		return Observation{}, ErrContract
	}
	if request.Class() == InvalidReference && (spec.Existence != Unknown || spec.Problem.Kind != MalformedReference) {
		return Observation{}, ErrContract
	}
	if (spec.Content == Inapplicable || spec.Anchors == Inapplicable) && spec.Existence != Directory && spec.Existence != Missing {
		return Observation{}, ErrContract
	}
	if spec.Content == Uncertain || spec.Anchors == Uncertain && (spec.Content != Available || spec.Problem.Kind != ParserUncertain) {
		return Observation{}, ErrContract
	}
	return Observation{request, spec.Existence, spec.Content, spec.Anchors, spec.Source, copyProblem(spec.Problem)}, nil
}

// InterruptionProblem converts controlled runtime stops without reclassifying
// them as usage or I/O failures. The caller retains independently known facts.
func InterruptionProblem(err error) (Problem, bool) {
	switch {
	case errors.Is(err, context.Canceled):
		return Problem{Kind: Cancelled, Message: "resource work cancelled"}, true
	case errors.Is(err, context.DeadlineExceeded):
		return Problem{Kind: Deadline, Message: "resource deadline exceeded"}, true
	}
	var limit *parser.LimitExceeded
	if errors.As(err, &limit) {
		return Problem{Kind: Limited, Message: "resource budget exhausted", Limit: &result.LimitObservation{Name: limit.Name, Observed: limit.Observed, Allowed: limit.Allowed, Unit: result.LimitUnit(limit.Name)}}, true
	}
	return Problem{}, false
}

type Treatment string

const (
	Complete          Treatment = "complete"
	MissingTarget     Treatment = "missing-target"
	InapplicableCheck Treatment = "inapplicable"
	ContextIncomplete Treatment = "context-incomplete"
	IgnoredCheck      Treatment = "ignored"
)

// Treatment preserves MD057/MD051 ownership. It never produces a diagnostic or
// transfers a disabled MD057's missing-file responsibility to MD051.
func (o Observation) Treatment() Treatment {
	if o.request.view.identity == nil {
		return ContextIncomplete
	}
	if o.request.Class() == Ignored {
		return IgnoredCheck
	}
	if o.request.Class() != Local {
		return ContextIncomplete
	}
	missing := o.existence == Missing || o.request.RequiredType() == RegularFile && o.existence == Directory || o.request.RequiredType() == DirectoryType && o.existence == File
	if missing {
		if o.request.Link().Rule() == "MD051" {
			return InapplicableCheck
		}
		return MissingTarget
	}
	if o.existence == Unknown {
		return ContextIncomplete
	}
	switch o.request.Capability() {
	case ExistenceOnly:
		return Complete
	case TextContent:
		if o.content == Available {
			return Complete
		}
	case ParsedAnchors:
		if o.anchors == Available {
			return Complete
		}
	}
	return ContextIncomplete
}
