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
// its block or inline model violates the source/model invariants.
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
// recognition disabled. Documents of blanks, indented/fenced code with known
// info semantics, opaque HTML blocks, thematic breaks and empty ATX headings
// and empty or inline-free nested block quotes and lists are fully parsed.
// Item indentation and quote markers match before leaf continuation; only
// paragraphs may continue lazily. Logical leaf segments exclude prefixes on the
// original Source.
// Eligible paragraphs promote to Setext headings with distinct raw content and
// underline ranges. Fenced payload is literal; info has decoded escape/entity
// semantics with original source contributions. Other input retains staged blocks
// and whole-document
// uncertainty. Leading definitions are collected in source order before inline
// work, with raw-label Unicode folding and first-definition lookup. Ordinary
// text-bearing blocks acquire source-preserving Text, CodeSpan, break, raw
// InlineHTML, resolved inline/autolink and nested Emphasis/Strong children. Missing
// inline handlers retain pending interpretation. Text, definition destination/title and
// fence-info values decode escapes/entities once, preserving raw label identity.
// Code spans consume opaque raw payloads before Text decoding, normalize physical
// endings and conditional ASCII edge spaces, retaining original payload/markers.
// Eligible original endings produce SoftBreak/HardBreak with distinct marker and
// ending ranges; terminal block endings and decoded characters create no breaks.
// Angle URI/email autolinks consume literal interiors, with distinct source-backed
// labels/destinations and synthetic email prefixes. Invalid forms resume ordinary
// interpretation. Inline HTML follows the pinned raw grammar, consuming opaque
// original Content with confirmed-comment opening/closing markers. Malformed
// HTML resumes ordinary interpretation. Bare autolink forms stay Text.
// Unicode-aware delimiter pairing and iterative nested construction retain exact
// consumed/leftover markers and raw parent/child Content. Scoped stack boundaries
// preserve earlier delimiter state while resolving inline-link labels. Active
// bracket openers prevent nested links; destinations/titles decode once with exact
// component ranges. Possible reference uses with definitions and images remain
// pending; a completed empty definition inventory proves reference fallback absent.
// Definition-only input with known semantics completes vacuous inline work.
// ListItems retain marker/prefix facts and ordered nested block children, with
// tab-aware continuation, blank boundaries, sibling ownership and closure. They
// acquire List parents, initial ordered starts and independently derived Tight
// facts. Supported text and inline-free lists complete; interpretation needing
// later providers retains its staged partial outcome. Non-rendered definitions
// preserve private list boundaries without becoming child nodes.
//
// The strict UTF-8 gate precedes Markdown work and retains malformed bytes without
// substitution. Its failure takes precedence over context interruption. Callers
// must pass a non-nil context. Cancellation is checked at phase boundaries and
// before block lines, during reference recognition and bounded inline scanning.
// Full work metering and index/UTF-8 checkpoints belong to #31. Phases must
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
	r.Phases.Inlines, err = parseInlines(ctx, r.Document, r.Phases.Blocks)
	if err != nil {
		r.Status, r.AST, r.Document = Failed, Unavailable, nil
		r.Failure = &Failure{Code: InternalFailure, Message: "construct inline model: " + err.Error()}
		return r
	}
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
	r.Document.inlineFree = false
	r.Document.blocksRecognized = false
	r.Uncertainty = []Uncertainty{{Region: region, Reason: reason}}
	return r
}
