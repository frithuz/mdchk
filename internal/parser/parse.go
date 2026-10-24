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
	SyntaxUnimplemented     Reason = "syntax-unimplemented"
	Cancelled               Reason = "cancelled"
	DeadlineExceeded        Reason = "deadline-exceeded"
	LimitExceededReason     Reason = "limit-exceeded"
	UnterminatedFrontmatter Reason = "unterminated-frontmatter"
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
	Source          Source
	Document        *Document
	Status          Status
	AST             Availability
	Phases          PhaseProgress
	Uncertainty     []Uncertainty
	Failure         *Failure
	Interruption    Reason // empty means no interruption
	EffectiveLimits Limits
	Limit           *LimitExceeded
}

// Parse recognizes core CommonMark 0.31.2 with extensions/frontmatter disabled.
// It retains original-byte envelopes, markers and logical Content separately
// from decoded semantic values. Container prefixes are source gaps, not payload;
// partial tabs retain explicit overlapping source provenance.
//
// Block recognition precedes inline work. All reference definitions, including
// duplicates, retain original components in source order; normalized lookup uses
// the first definition. Inline/reference links and images resolve only after that
// document-wide inventory completes. Effective reference values are spanless
// projections; the selected definition owns their original-byte provenance.
// Text, destination/title and fence-info values decode escapes/entities once.
// Code and raw HTML payloads remain opaque. Code spans normalize physical endings
// and conditional edge spaces while retaining their original payload and markers.
// Breaks retain original physical endings separately from hard-break markers.
//
// Complete results affirm both phases, AST and reference availability, even for
// empty or definition-only input. A nil Failure or empty tree alone never proves
// completion. The source snapshot remains available on every outcome. Strict
// UTF-8 validation precedes Markdown work and context interruption; invalid bytes
// retain a typed failure at the original byte offset without source substitution.
// Callers must pass a non-nil context. Cancellation/deadline checks prevent
// unfinished inline trees or reference inventories from becoming complete data.
// Parse uses the default finite private operation profile.

func Parse(ctx context.Context, source Source) ParseResult {

	budget, _ := NewOperationBudget(DefaultLimits())
	return ParseWithBudget(ctx, source, budget)
}

// ParseWithBudget uses a validated operation shared across retries, documents and
// adapter work. The source must already be an immutable snapshot. Document-size
// admission precedes scanning, even for malformed or cancelled oversized input.
// Within admitted input, UTF-8 failure precedes context interruption.
func ParseWithBudget(ctx context.Context, source Source, budget *OperationBudget) ParseResult {
	return parseWithProfile(ctx, source, budget, coreProfile)
}

// ParseGFMWithBudget recognizes the complete approved GFM profile: CommonMark
// 0.31.2 and all five pinned extensions. It shares admission, source authority,
// recovery and the caller's cumulative operation with ParseWithBudget. No partial
// extension selection is exposed. The legacy Parse APIs remain core-only.
func ParseGFMWithBudget(ctx context.Context, source Source, budget *OperationBudget) ParseResult {
	return parseWithProfile(ctx, source, budget, gfmInProgressProfile)
}

// ParseWithOptions is the additive configured parser entry point. Zero options
// preserve ParseWithBudget behavior; GFM selects all five approved extensions,
// independently of frontmatter recognition. Legacy entry points keep recognition
// disabled. Admission, UTF-8, cancellation and cumulative budgets are unchanged.
// Recognized headers are source-owned opaque records, never ordinary body AST
// children. An unterminated header retains its record and reports incomplete
// analysis with UnterminatedFrontmatter uncertainty, without a completed-parse
// protection handoff. Consumer availability remains the caller's responsibility.
func ParseWithOptions(ctx context.Context, source Source, budget *OperationBudget, options ParseOptions) ParseResult {
	profile := coreProfile
	if options.GFM {
		profile = gfmInProgressProfile
	}
	return parseConfigured(ctx, source, budget, profile, options.Frontmatter)
}

// parserProfile keeps section-scoped verification controls private.
type parserProfile uint8

const (
	coreProfile parserProfile = iota
	gfmInProgressProfile
	tablesOnlyProfile
	tasksOnlyProfile
	strikeOnlyProfile
	bareOnlyProfile
	filterOnlyProfile
)

// enabled keeps section execution closed and preserves the integrated stage.
func (p parserProfile) enabled(extension parserProfile) bool {
	return p == gfmInProgressProfile || p == extension
}

func parseWithProfile(ctx context.Context, source Source, budget *OperationBudget, profile parserProfile) ParseResult {
	return parseConfigured(ctx, source, budget, profile, FrontmatterOptions{})
}

func parseConfigured(ctx context.Context, source Source, budget *OperationBudget, profile parserProfile, frontmatter FrontmatterOptions) (r ParseResult) {

	if ctx == nil || budget == nil {
		panic("parser: nil context or operation budget")
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	ctx = &parserContext{Context: ctx, budget: &parseBudget{operation: budget}, profile: profile, frontmatter: frontmatter}
	r = ParseResult{
		Source: source, Status: Failed, AST: Unavailable,
		Phases:          PhaseProgress{Blocks: NotStarted, Inlines: NotStarted},
		EffectiveLimits: budget.Limits(),
	}
	span := Span{source: source, end: source.Len(), present: true}
	defer func() {
		if caught := recover(); caught != nil {
			stop, ok := caught.(budgetStop)
			if !ok {
				panic(caught)
			}
			copy := *stop.exceeded
			r.Limit = &copy
			r = interruptedResult(r, LimitExceededReason, span)
		}
	}()
	if err := budget.limits.Validate(); err != nil {
		panic("parser: invalid operation budget: " + err.Error())
	}
	if int64(source.Len()) > budget.limits.DocumentBytes {
		panic(budgetStop{&LimitExceeded{"document-bytes", int64(source.Len()), budget.limits.DocumentBytes}})
	}
	meterWork(ctx, 0)
	if offset := invalidUTF8OffsetContext(ctx, source.content); offset >= 0 {
		r.Failure = &Failure{
			Code: InvalidUTF8, Message: "original snapshot is not strict UTF-8",
			Location: Span{source: source, start: offset, end: offset, present: true},
		}
		return r
	}
	// These values are known bounded by construction, without copying the source.
	meterRecords(ctx, 1)
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
	r.Phases.Inlines = Pending
	r.Phases.Inlines, err = parseInlines(ctx, r.Document, r.Phases.Blocks)
	if err != nil {
		r.Status, r.AST, r.Document = Failed, Unavailable, nil
		r.Failure = &Failure{Code: InternalFailure, Message: "construct inline model: " + err.Error()}
		return r
	}
	if profile.enabled(filterOnlyProfile) && r.Phases.Inlines == PhaseComplete && interruption(ctx) == "" {
		root, filterErr := filterHTMLTree(ctx, r.Document.root)
		if filterErr != nil {
			r.Status, r.AST, r.Document = Failed, Unavailable, nil
			r.Failure = &Failure{Code: InternalFailure, Message: "construct filtered HTML model: " + filterErr.Error()}
			return r
		}
		if interruption(ctx) == "" {
			r.Document.root = root
		}
	}
	if reason := interruption(ctx); reason != "" {
		return interruptedResult(r, reason, span)
	}
	meterWork(ctx, 0)
	if r.Phases.Inlines == PhaseComplete {
		if header, present := r.Document.Frontmatter(); present && !header.Closer().Present() {
			meterRecords(ctx, 1)
			r.Uncertainty = []Uncertainty{{Region: header.Span(), Reason: UnterminatedFrontmatter}}
			return r
		}
		r.Status, r.AST = Complete, Available
		r.Document.parsedComplete = true
		r.Document.commentOperation = budget.identity
	} else {
		r.Uncertainty = []Uncertainty{{Region: span, Reason: SyntaxUnimplemented}}
	}
	return r
}

// invalidUTF8OffsetContext identifies the first scalar that cannot be consumed, at its
// leading byte. DecodeRuneInString's size distinguishes malformed input from a
// valid literal U+FFFD. No decoded/replacement text is stored or substituted.
func invalidUTF8OffsetContext(ctx context.Context, content string) int {
	// Precharge the complete validation pass before the optimized library scan.
	meterWork(ctx, len(content))
	if utf8.ValidString(content) {
		return -1
	}
	for offset := 0; offset < len(content); {
		r, size := utf8.DecodeRuneInString(content[offset:])
		meterWork(ctx, size)
		if r == utf8.RuneError && size == 1 {
			return offset
		}
		offset += size
	}
	return -1
}

func interruption(ctx context.Context) Reason {
	meterWork(ctx, 1)
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
	if reason == LimitExceededReason && r.Phases.Inlines == PhaseComplete {
		r.Phases.Inlines = Interrupted
	}
	if r.Phases.Blocks != PhaseComplete {
		r.Phases.Blocks = Interrupted
	}
	if r.Phases.Inlines != NotStarted && r.Phases.Inlines != PhaseComplete {
		r.Phases.Inlines = Interrupted
	}
	if r.Document == nil {
		r.Document = &Document{root: &Node{spec: NodeSpec{Kind: DocumentKind, Envelope: region}}}
	}
	r.Document.references = ReferenceCollection{Availability: Unavailable, LookupAvailability: Unavailable}
	r.Document.inlineFree = false
	r.Document.blocksRecognized = false
	r.Uncertainty = []Uncertainty{{Region: region, Reason: reason}}
	return r
}
