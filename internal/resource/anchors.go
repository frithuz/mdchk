package resource

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/frithuz/mdchk/internal/parser"
)

type AnchorKind string

const (
	HeadingAnchorKind AnchorKind = "heading"
	HTMLAnchorKind    AnchorKind = "html"
)

// AnchorOccurrence retains authored provenance independently of effective
// membership. Ignored duplicate attributes and empty IDs remain occurrences,
// while neither grants a usable named target. Generated IDs have no authored
// attribute/value spelling: their original heading envelope owns provenance.
type AnchorOccurrence struct {
	ID                string
	Kind              AnchorKind
	Envelope          parser.Span
	Attribute         parser.Span
	Value             parser.Value
	Effective, Usable bool
}

// HeadingAnchor keeps heading-only facts for future TOC/collision consumers.
// Base is unsuffixed; ID is the allocated spelling. Explicit mode leaves both
// absent for headings without a genuinely supported explicit-heading record.
// Containers are syntax ancestry, not final TOC/marked-region eligibility.
type HeadingAnchor struct {
	Envelope   parser.Span
	Level      int
	Style      string
	Visible    string
	Base, ID   parser.Optional[string]
	Explicit   bool
	Usable     bool
	Containers []parser.HeadingContainer
}

type anchorIndexState struct {
	operation   *Operation
	key         ParseCacheKey
	headings    []HeadingAnchor
	occurrences []AnchorOccurrence
	targets     map[string][]int
}

// AnchorIndex is immutable, complete named-target authority for one exact
// admitted Source/view/policy. Zero is invalid and cannot establish absence.
// Inspection copies and lookup work use the original operation's shared meter.
type AnchorIndex struct{ state *anchorIndexState }

func (i AnchorIndex) Source() parser.Source {
	if i.state == nil {
		return parser.Source{}
	}
	return i.state.key.source
}
func (i AnchorIndex) Key() ParseCacheKey {
	if i.state == nil {
		return ParseCacheKey{}
	}
	return i.state.key
}
func (i AnchorIndex) checkpoint(ctx context.Context, work, nodes int64) error {
	if i.state == nil || ctx == nil {
		return ErrContract
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := i.state.operation.Checkpoint(work); err != nil {
		return err
	}
	return i.state.operation.budget.ChargeNodes(nodes)
}

// Lookup compares an already-decoded ID exactly, without percent/case/Unicode
// normalization. An empty successful result proves absence only in this complete
// index; a zero index or interrupted lookup returns an error instead.
func (i AnchorIndex) Lookup(ctx context.Context, id string) ([]AnchorOccurrence, error) {
	if err := i.checkpoint(ctx, int64(len(id))+1, 0); err != nil {
		return nil, err
	}
	positions := i.state.targets[id]
	if err := i.copyOccurrenceCost(ctx, positions); err != nil {
		return nil, err
	}
	out := make([]AnchorOccurrence, len(positions))
	for n, position := range positions {
		out[n] = copyAnchorOccurrence(i.state.occurrences[position])
	}
	return out, nil
}

func (i AnchorIndex) copyOccurrenceCost(ctx context.Context, positions []int) error {
	if err := i.checkpoint(ctx, int64(len(positions)), 0); err != nil {
		return err
	}
	work, nodes := int64(len(positions)), int64(len(positions))
	for _, n := range positions {
		work += int64(len(i.state.occurrences[n].Value.Segments))
		nodes += int64(len(i.state.occurrences[n].Value.Segments))
	}
	return i.checkpoint(ctx, work, nodes)
}

// Occurrences includes all source occurrences, including ineffective/empty HTML
// ID attributes and unusable empty generated spellings. Returned storage is owned
// by the caller; changing it cannot alter membership or another caller's facts.
func (i AnchorIndex) Occurrences(ctx context.Context) ([]AnchorOccurrence, error) {
	if err := i.checkpoint(ctx, 1, 0); err != nil {
		return nil, err
	}
	if err := i.checkpoint(ctx, int64(len(i.state.occurrences)), 0); err != nil {
		return nil, err
	}
	work, nodes := int64(len(i.state.occurrences)), int64(len(i.state.occurrences))
	for _, occurrence := range i.state.occurrences {
		work += int64(len(occurrence.Value.Segments))
		nodes += int64(len(occurrence.Value.Segments))
	}
	if err := i.checkpoint(ctx, work, nodes); err != nil {
		return nil, err
	}
	out := slices.Clone(i.state.occurrences)
	for n := range out {
		out[n] = copyAnchorOccurrence(out[n])
	}
	return out, nil
}

func copyAnchorOccurrence(in AnchorOccurrence) AnchorOccurrence {
	in.Value.Segments = slices.Clone(in.Value.Segments)
	return in
}

func (i AnchorIndex) Headings(ctx context.Context) ([]HeadingAnchor, error) {
	if err := i.checkpoint(ctx, 1, 0); err != nil {
		return nil, err
	}
	if err := i.checkpoint(ctx, int64(len(i.state.headings)), 0); err != nil {
		return nil, err
	}
	work, nodes := int64(len(i.state.headings)), int64(len(i.state.headings))
	for _, heading := range i.state.headings {
		work += int64(len(heading.Containers))
		nodes += int64(len(heading.Containers))
	}
	if err := i.checkpoint(ctx, work, nodes); err != nil {
		return nil, err
	}
	out := slices.Clone(i.state.headings)
	for n := range out {
		out[n].Containers = slices.Clone(out[n].Containers)
	}
	return out, nil
}

// AnchorResult separates unavailable/uncertain data from a complete empty index.
// Its opaque immutable state can only be produced by the admitted service seam.
type AnchorResult struct{ state *anchorResultState }

type anchorResultState struct {
	operation    *Operation
	key          ParseCacheKey
	availability Availability
	index        AnchorIndex
	parsed       parser.ParseResult
	problem      Problem
	validated    ContentEvidence
}

func (r AnchorResult) Availability() Availability {
	if r.state == nil {
		return Unavailable
	}
	return r.state.availability
}
func (r AnchorResult) Index() (AnchorIndex, bool) {
	if r.state == nil || r.state.availability != Available || r.state.index.state == nil {
		return AnchorIndex{}, false
	}
	return r.state.index, true
}
func (r AnchorResult) Problem() Problem {
	if r.state == nil {
		return Problem{}
	}
	return copyProblem(r.state.problem)
}

// Content retains already admitted valid bytes even when indexing is uncertain
// or stops later. It does not grant resource membership or a selected target.
func (r AnchorResult) Content() (parser.Source, ContentEvidence, bool) {
	if r.state == nil || r.state.validated.operation != r.state.operation || r.state.validated.source != r.state.key.source {
		return parser.Source{}, ContentEvidence{}, false
	}
	return r.state.key.source, r.state.validated, true
}

func (r AnchorResult) apply(spec ObservationSpec) (ObservationSpec, error) {
	if r.state == nil || spec.Content != Available || spec.Source != r.state.key.source || spec.Validated.operation != r.state.operation {
		return spec, ErrContract
	}
	spec.Anchors, spec.Problem = r.state.availability, copyProblem(r.state.problem)
	spec.Parsed, spec.Index = nil, AnchorIndex{}
	if r.state.availability == Available {
		parsed := copyCachedParse(r.state.parsed)
		spec.Parsed, spec.Index = &parsed, r.state.index
	}
	return spec, nil
}

func buildAnchorIndex(ctx context.Context, operation *Operation, key ParseCacheKey, parsed parser.ParseResult) (AnchorResult, error) {
	out := &anchorResultState{operation: operation, key: key, availability: Uncertain, problem: Problem{Kind: ParserUncertain, Message: "Required anchor syntax is unavailable or uncertain."}}
	result := AnchorResult{out}
	if parsed.Source != key.source || parsed.EffectiveLimits != operation.budget.Limits() {
		return AnchorResult{}, ErrContract
	}
	if parsed.Failure != nil {
		out.availability = Failed
		out.problem = Problem{Kind: InternalFailure, Message: parsed.Failure.Error()}
		return result, anchorInternalError(parsed.Failure)
	}
	if parsed.Status != parser.Complete || parsed.AST != parser.Available || parsed.Document == nil || parsed.Document.Source() != key.source || parsed.Phases.Blocks != parser.PhaseComplete || parsed.Phases.Inlines != parser.PhaseComplete || len(parsed.Uncertainty) != 0 || parsed.Interruption != "" || parsed.Limit != nil {
		return result, nil
	}
	check := func(work, nodes int64) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := operation.Checkpoint(work); err != nil {
			return err
		}
		return operation.budget.ChargeNodes(nodes)
	}
	stopped := func(err error) (AnchorResult, error) {
		if problem, stop := InterruptionProblem(err); stop {
			out.availability, out.problem = Interrupted, problem
			return result, err
		}
		return AnchorResult{}, err
	}
	headings, err := parsed.Document.Headings(ctx, operation.budget)
	if err != nil {
		return stopped(err)
	}
	html, err := parsed.Document.HTMLIDs(ctx, operation.budget)
	if err != nil {
		return stopped(err)
	}
	if html.Availability != parser.Available {
		return result, nil
	}
	if err := check(int64(len(headings)+len(html.Occurrences))+1, int64(len(headings)+len(html.Occurrences))*3+3); err != nil {
		return stopped(err)
	}
	index := &anchorIndexState{operation: operation, key: key, targets: map[string][]int{}}
	occupied := map[string]bool{}
	for _, id := range html.Occurrences {
		if !id.Value.Semantic.Present {
			return AnchorResult{}, ErrContract
		}
		name := id.Value.Semantic.Value
		if err := check(3*int64(len(name))+int64(len(id.Value.Segments))+1, int64(len(id.Value.Segments))); err != nil {
			return stopped(err)
		}
		usable := id.Effective && name != ""
		index.occurrences = append(index.occurrences, AnchorOccurrence{ID: name, Kind: HTMLAnchorKind, Envelope: id.Tag, Attribute: id.Attribute, Value: parser.Value{Segments: slices.Clone(id.Value.Segments), Semantic: id.Value.Semantic}, Effective: id.Effective, Usable: usable})
		if usable {
			occupied[name] = true
		}
	}
	next := map[string]int{}
	var generated []AnchorOccurrence
	for _, fact := range headings {
		if err := check(int64(len(fact.Containers))+1, int64(len(fact.Containers))); err != nil {
			return stopped(err)
		}
		heading := HeadingAnchor{Envelope: fact.Envelope, Level: fact.Level, Style: fact.Style, Visible: fact.Visible, Containers: slices.Clone(fact.Containers)}
		if key.anchor == "github" {
			base, err := anchorBase(ctx, operation, fact.Visible)
			if err != nil {
				return stopped(err)
			}
			name := base
			if err := check(int64(len(name))+1, 0); err != nil {
				return stopped(err)
			}
			if occupied[name] {
				for n := max(next[base], 1); ; n++ {
					if err := check(3*int64(len(base))+32, 1); err != nil {
						return stopped(err)
					}
					name = base + "-" + strconv.Itoa(n)
					if !occupied[name] {
						next[base] = n + 1
						break
					}
				}
			}
			occupied[name] = true
			heading.Base, heading.ID, heading.Usable = parser.Some(base), parser.Some(name), name != ""
			generated = append(generated, AnchorOccurrence{ID: name, Kind: HeadingAnchorKind, Envelope: fact.Envelope, Effective: true, Usable: name != ""})
		}
		index.headings = append(index.headings, heading)
	}
	// Explicit facts were globally reserved before generation; public occurrences
	// still follow actual source order, including duplicate authored attributes.
	count := int64(len(index.occurrences) + len(generated))
	if err := check(3*count+1, count); err != nil {
		return stopped(err)
	}
	merged := make([]AnchorOccurrence, 0, count)
	for a, b := 0, 0; a < len(index.occurrences) || b < len(generated); {
		if b == len(generated) || a < len(index.occurrences) && index.occurrences[a].Envelope.Start() <= generated[b].Envelope.Start() {
			merged = append(merged, index.occurrences[a])
			a++
		} else {
			merged = append(merged, generated[b])
			b++
		}
	}
	index.occurrences = merged
	for n, occurrence := range index.occurrences {
		if err := check(int64(len(occurrence.ID))+1, 1); err != nil {
			return stopped(err)
		}
		if occurrence.Usable {
			index.targets[occurrence.ID] = append(index.targets[occurrence.ID], n)
		}
	}
	if err := check(0, 0); err != nil {
		return stopped(err)
	}
	out.availability, out.problem, out.index, out.parsed = Available, Problem{}, AnchorIndex{index}, copyCachedParse(parsed)
	return result, nil
}

func anchorBase(ctx context.Context, operation *Operation, visible string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	// Unicode lower mapping can change UTF-8 width. Reserve an upper bound on
	// scanning/copying before construction, then meter each retained rune.
	if err := operation.Checkpoint(8*int64(len(visible)) + 1); err != nil {
		return "", err
	}
	var out strings.Builder
	for _, r := range visible {
		if err := operation.Checkpoint(1); err != nil {
			return "", err
		}
		r = unicode.ToLower(r)
		if unicode.IsPunct(r) && r != '-' && r != '_' {
			continue
		}
		if unicode.IsSpace(r) {
			r = '-'
		}
		if _, err := out.WriteRune(r); err != nil {
			return "", err
		}
	}
	return out.String(), nil
}

func anchorInternalError(failure *parser.Failure) error {
	if failure == nil {
		return errors.New("missing parser failure")
	}
	message := failure.Error()
	return &serviceReadError{io: errors.New(message), causes: []serviceIOCause{{kind: InternalFailure, message: message}}}
}
