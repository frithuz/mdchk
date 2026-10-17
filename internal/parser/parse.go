package parser

import (
	"context"
	"errors"
	"unicode/utf8"
)

type Status string

const (
	Complete   Status = "complete"
	Incomplete Status = "incomplete"
	Failed     Status = "failed"
)

type PhaseState string

const (
	NotStarted    PhaseState = "not-started"
	Pending       PhaseState = "pending"
	PhaseComplete PhaseState = "complete"
	Interrupted   PhaseState = "interrupted"
)

type PhaseProgress struct{ Blocks, Inlines PhaseState }

type Reason string

const (
	SyntaxUnimplemented Reason = "syntax-unimplemented"
	Cancelled           Reason = "cancelled"
	DeadlineExceeded    Reason = "deadline-exceeded"
)

// Uncertainty excludes its original-byte region from syntax-dependent work.
// Results order records by start, end and reason. The foundation emits one region.
type Uncertainty struct {
	Region Span
	Reason Reason
}

type FailureCode string

const (
	InvalidUTF8     FailureCode = "invalid-utf8"
	InternalFailure FailureCode = "internal"
)

// Failure is an operational failure, not unavailable syntax or interruption.
// Code is machine-readable; Message is explanatory, never a discrimination API.
// Parse reports InvalidUTF8 for bad encoding and InternalFailure if constructing
// its block model violates the source/model invariants.
type Failure struct {
	Code    FailureCode
	Message string
	// Location is a snapshot-owned, zero-width point at the authoritative
	// original byte offset. It is present for InvalidUTF8 and absent for an
	// unlocated failure, including synthetic InternalFailure examples.
	Location Span
}

func (f *Failure) Error() string { return string(f.Code) + ": " + f.Message }

// ParseResult keeps the snapshot even when Document is nil. Complete status is
// affirmative; neither a nil Failure nor empty children establish completeness.
// Fields are result values; mutating them does not change the immutable Document.
type ParseResult struct {
	Source       Source
	Document     *Document
	Status       Status
	AST          Availability
	Phases       PhaseProgress
	Uncertainty  []Uncertainty
	Failure      *Failure
	Interruption Reason // empty means no interruption
}

// Parse implements the core CommonMark foundation, with extensions/frontmatter
// recognition disabled. Empty/ASCII-blank input is fully parsed. Other input
// retains staged paragraph blocks and explicit whole-document uncertainty;
// inline parsing, reference collection and other block providers remain pending.
//
// The strict UTF-8 gate precedes Markdown work and retains malformed bytes without
// substitution. Its failure takes precedence over context interruption. Callers
// must pass a non-nil context. Cancellation is checked at phase boundaries and
// before block lines; full work metering/checkpoints belong to #31. Phases must
// later share the caller's operation budget across phases and candidate reparses.
func Parse(ctx context.Context, source Source) ParseResult {
	r := ParseResult{
		Source: source, Status: Failed, AST: Unavailable,
		Phases: PhaseProgress{Blocks: NotStarted, Inlines: NotStarted},
	}
	if offset := invalidUTF8Offset(source.content); offset >= 0 {
		r.Failure = &Failure{
			Code: InvalidUTF8, Message: "original snapshot is not strict UTF-8",
			Location: Span{source: source, start: offset, end: offset, present: true},
		}
		return r
	}
	// These values are known bounded by construction, without copying the source.
	span := Span{source: source, end: source.Len(), present: true}
	r.Document = &Document{
		root:       &Node{spec: NodeSpec{Kind: DocumentKind, Envelope: span}},
		references: ReferenceCollection{Availability: Unavailable, LookupAvailability: Unavailable},
	}
	r.Status, r.AST = Incomplete, Partial
	if reason := interruption(ctx); reason != "" {
		return interruptedResult(r, reason, span)
	}
	var err error
	r.Phases.Blocks, err = parseBlocks(ctx, r.Document)
	if err != nil {
		r.Status, r.AST, r.Document = Failed, Unavailable, nil
		r.Failure = &Failure{Code: InternalFailure, Message: "construct block model: " + err.Error()}
		return r
	}
	if reason := interruption(ctx); reason != "" {
		return interruptedResult(r, reason, span)
	}
	if r.Phases.Blocks != PhaseComplete {
		r.Uncertainty = []Uncertainty{{Region: span, Reason: SyntaxUnimplemented}}
		return r
	}
	r.Phases.Inlines = parseInlines(ctx, r.Document, r.Phases.Blocks)
	if reason := interruption(ctx); reason != "" {
		return interruptedResult(r, reason, span)
	}
	if r.Phases.Inlines == PhaseComplete {
		r.Status, r.AST = Complete, Available
	} else {
		r.Uncertainty = []Uncertainty{{Region: span, Reason: SyntaxUnimplemented}}
	}
	return r
}

// invalidUTF8Offset identifies the first scalar that cannot be consumed, at its
// leading byte. DecodeRuneInString's size distinguishes malformed input from a
// valid literal U+FFFD. No decoded/replacement text is stored or substituted.
func invalidUTF8Offset(content string) int {
	for offset := 0; offset < len(content); {
		r, size := utf8.DecodeRuneInString(content[offset:])
		if r == utf8.RuneError && size == 1 {
			return offset
		}
		offset += size
	}
	return -1
}

func parseInlines(ctx context.Context, document *Document, blocks PhaseState) PhaseState {
	// The reference handoff is required even if a future caller misorders phases.
	if blocks != PhaseComplete || document.references.LookupAvailability != Available {
		return NotStarted
	}
	if interruption(ctx) != "" {
		return Interrupted
	}
	if len(document.root.spec.Children) != 0 || !isBlankSource(document.Source().content) {
		return Pending // #18 supplies nonempty inline interpretation
	}
	return PhaseComplete
}

// This guard also protects the inline stub from a future incorrectly completed
// nonblank block handoff, even when the source-only root has no children.
func isBlankSource(text string) bool {
	for i := range len(text) {
		if text[i] != ' ' && text[i] != '\t' && text[i] != '\r' && text[i] != '\n' {
			return false
		}
	}
	return true
}

func interruption(ctx context.Context) Reason {
	err := ctx.Err()
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return DeadlineExceeded
	case err != nil:
		return Cancelled
	default:
		return ""
	}
}

func interruptedResult(r ParseResult, reason Reason, region Span) ParseResult {
	r.Status, r.AST, r.Interruption = Incomplete, Partial, reason
	if r.Phases.Blocks != PhaseComplete {
		r.Phases.Blocks = Interrupted
	}
	if r.Phases.Inlines != NotStarted && r.Phases.Inlines != PhaseComplete {
		r.Phases.Inlines = Interrupted
	}
	r.Document.references = ReferenceCollection{Availability: Unavailable, LookupAvailability: Unavailable}
	r.Uncertainty = []Uncertainty{{Region: region, Reason: reason}}
	return r
}
