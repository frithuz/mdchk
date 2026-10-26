package inspect

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// Section distinguishes unavailable work from a successfully empty collection.
type Section[T any] struct {
	Availability string   `json:"availability"`
	Reasons      []string `json:"reasons"`
	CauseIDs     []string `json:"cause_ids,omitempty"`
	Data         *T       `json:"data,omitempty"`
}

func causes[T any](section *Section[T], ids []string, reason string) {
	if section.Availability == "unavailable" || section.Availability == "partial" {
		section.CauseIDs = slices.Clone(ids)
		if reason != "" && len(section.Reasons) == 1 && section.Reasons[0] == "context-unavailable" {
			section.Reasons = []string{reason}
		}
	}
}

func available[T any](v T) Section[T] {
	return Section[T]{Availability: "available", Reasons: []string{}, Data: &v}
}
func unavailable[T any](reason string) Section[T] {
	return Section[T]{Availability: "unavailable", Reasons: []string{reason}}
}
func notRequested[T any]() Section[T] {
	return Section[T]{Availability: "not-requested", Reasons: []string{}}
}

// DocumentContextData describes acquired annotation and opaque header recognition,
// never metadata interpretation or a resource capability.
type DocumentContextData struct {
	Slides               bool        `json:"slides"`
	MatchedSlidePatterns []string    `json:"matched_slide_patterns"`
	Frontmatter          string      `json:"frontmatter"`
	SlideMatch           *SlideMatch `json:"slide_match,omitempty"`
}
type SlideMatch struct {
	Pattern string      `json:"pattern"`
	Index   int         `json:"index"`
	Origin  SlideOrigin `json:"origin"`
}
type SlideOrigin struct {
	Path         string               `json:"path"`
	PropertyPath string               `json:"property_path"`
	Position     *SlideOriginPosition `json:"position,omitempty"`
}

// SlideOriginPosition keeps the declared JSON spelling independently of the
// parser's internal Position value and the diagnostic Range wire adapter.
type SlideOriginPosition struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

type InspectionFile struct {
	ResolvedPath          string         `json:"resolved_path"`
	BinaryVersion         string         `json:"binary_version"`
	SizeBytes             int64          `json:"size_bytes"`
	SizeDisplay           string         `json:"size_display"`
	SHA1                  string         `json:"sha1"`
	SHA256                string         `json:"sha256"`
	NormallyExcluded      bool           `json:"normally_excluded"`
	NormallyExcludedKnown bool           `json:"normally_excluded_known"`
	SelectedLines         *LineSelection `json:"selected_lines,omitempty"`
	PhysicalLineCount     int            `json:"physical_line_count"`
	SelectedRegion        *Region        `json:"selected_region,omitempty"`
}

// Projection is version 1's typed inspection data. Analyze supplies full source
// facts; Filter adds selected presentation and counts. Resources remain separate.
type Projection struct {
	ProjectionVersion int                               `json:"projection_version"`
	File              InspectionFile                    `json:"file"`
	Configuration     Section[result.ConfigurationData] `json:"configuration"`
	Flavor            Section[string]                   `json:"flavor"`
	DocumentContext   Section[json.RawMessage]          `json:"document_context"`
	AST               Section[AST]                      `json:"ast"`
	Rules             Section[[]Rule]                   `json:"rules"`
	RuleCounts        Section[json.RawMessage]          `json:"rule_counts"`
	Directives        Section[[]Directive]              `json:"directives"`
	RawCandidates     Section[[]RawCandidate]           `json:"raw_candidates"`
	Resources         Section[json.RawMessage]          `json:"resources"`
	ParsingNotes      Section[[]result.Note]            `json:"parsing_notes"`
}
type AST struct {
	ProjectionVersion int             `json:"projection_version"`
	Profile           string          `json:"profile"`
	Snapshot          result.Snapshot `json:"snapshot"`
	Root              Node            `json:"root"`
	References        []Reference     `json:"references,omitempty"`
	Frontmatter       *Frontmatter    `json:"frontmatter,omitempty"`
}

// SourceRegion retains original byte and public scalar coordinates.
type SourceRegion struct {
	Range     result.Range     `json:"range"`
	ByteRange result.ByteRange `json:"byte_range"`
}

// Frontmatter is the opaque source record, separate from ordinary body nodes.
// An absent Closer identifies an unterminated protected header, not empty data.
type Frontmatter struct {
	Format        string           `json:"format"`
	Range         result.Range     `json:"range"`
	ByteRange     result.ByteRange `json:"byte_range"`
	Opener        SourceRegion     `json:"opener"`
	Content       SourceRegion     `json:"content"`
	Closer        *SourceRegion    `json:"closer,omitempty"`
	BodyOffset    int              `json:"body_offset"`
	ContextOnly   bool             `json:"context_only"`
	ExtendsRegion bool             `json:"extends_region,omitempty"`
}

type Node struct {
	ID            string            `json:"id"`
	Kind          string            `json:"kind"`
	Range         result.Range      `json:"range"`
	ByteRange     result.ByteRange  `json:"byte_range"`
	ContextOnly   bool              `json:"context_only"`
	ExtendsRegion bool              `json:"extends_region,omitempty"`
	Properties    map[string]any    `json:"properties"`
	RawAttributes []json.RawMessage `json:"raw_attributes"`
	Children      []Node            `json:"children"`
}
type Marker struct {
	Role      string           `json:"role"`
	ByteRange result.ByteRange `json:"byte_range"`
}
type Segment struct {
	ByteRange      *result.ByteRange `json:"byte_range,omitempty"`
	Transformation string            `json:"transformation"`
	Semantic       *string           `json:"semantic,omitempty"`
}
type Value struct {
	Segments []Segment `json:"segments"`
	Semantic *string   `json:"semantic,omitempty"`
}

// Task retains original token spellings and discontiguous marker contributions.
// Paragraph identifies the first direct paragraph, independently of its content.
type Task struct {
	Paragraph     result.ByteRange   `json:"paragraph"`
	Marker        result.ByteRange   `json:"marker"`
	Opening       result.ByteRange   `json:"opening"`
	Check         result.ByteRange   `json:"check"`
	Closing       result.ByteRange   `json:"closing"`
	Separator     result.ByteRange   `json:"separator"`
	Segments      []result.ByteRange `json:"segments"`
	Checked       bool               `json:"checked"`
	CheckSpelling string             `json:"check_spelling"`
}

// FilteredTag describes a fact on its unchanged raw HTML owner. A nil Tag
// means no complete tag was proved; Prefix and Affected still retain authority.
type FilteredTag struct {
	Name     string           `json:"name"`
	Closing  bool             `json:"closing"`
	Prefix   Value            `json:"prefix"`
	Affected result.ByteRange `json:"affected"`
	Tag      *Value           `json:"tag,omitempty"`
}
type Reference struct {
	ByteRange       result.ByteRange `json:"byte_range"`
	Markers         []Marker         `json:"markers"`
	Label           Value            `json:"label"`
	Destination     Value            `json:"destination"`
	Title           *Value           `json:"title,omitempty"`
	NormalizedLabel *string          `json:"normalized_label,omitempty"`
	Effective       bool             `json:"effective"`
	ContextOnly     bool             `json:"context_only,omitempty"`
}
type Interval struct {
	Range       result.Range     `json:"range"`
	ByteRange   result.ByteRange `json:"byte_range"`
	Enabled     bool             `json:"enabled"`
	Origin      string           `json:"origin"`
	DirectiveID string           `json:"directive_id,omitempty"`
	ContextOnly bool             `json:"context_only,omitempty"`
}
type EvaluationCoverage struct {
	Availability string             `json:"availability"`
	Ranges       []result.Range     `json:"ranges"`
	ByteRanges   []result.ByteRange `json:"byte_ranges"`
}
type SourceCoverage struct {
	Active        EvaluationCoverage `json:"active"`
	Observational EvaluationCoverage `json:"observational"`
}
type Rule struct {
	Rule                    string         `json:"rule"`
	SelectedEnabled         *bool          `json:"selected_enabled,omitempty"`
	Implementation          string         `json:"implementation"`
	ConfiguredEnabled       bool           `json:"configured_enabled"`
	AllowDirectives         bool           `json:"allow_directives"`
	Options                 engine.Options `json:"options"`
	EffectiveState          string         `json:"effective_state"`
	Intervals               []Interval     `json:"intervals"`
	ActiveEvaluation        string         `json:"active_evaluation"`
	ObservationalEvaluation string         `json:"observational_evaluation"`
	Reasons                 []string       `json:"reasons"`
	SourceCoverage          SourceCoverage `json:"source_coverage"`
}
type DirectiveEffect struct {
	Rule       string              `json:"rule"`
	Permission string              `json:"permission"`
	Effect     string              `json:"effect"`
	Ranges     []result.Range      `json:"ranges"`
	Provenance []result.Provenance `json:"permission_provenance,omitempty"`
}
type Directive struct {
	ID            string            `json:"id"`
	ContextOnly   bool              `json:"context_only,omitempty"`
	Raw           string            `json:"raw"`
	Range         result.Range      `json:"range"`
	ByteRange     result.ByteRange  `json:"byte_range"`
	ClosingOffset int               `json:"closing_offset"`
	Operation     string            `json:"operation,omitempty"`
	State         string            `json:"state"`
	Effects       []DirectiveEffect `json:"effects"`
	NamedTokens   []NamedToken      `json:"named_tokens,omitempty"`
	ResolvedRules []string          `json:"resolved_rules,omitempty"`
}

// NamedToken preserves each original directive token, including duplicates.
type NamedToken struct {
	Name      string           `json:"name"`
	ByteRange result.ByteRange `json:"byte_range"`
}

// RawCandidate inventories bytes without interpreting directive syntax or permissions.
// Base64 preserves malformed UTF-8 without replacement characters.
type RawCandidate struct {
	ByteRange   result.ByteRange `json:"byte_range"`
	RawBase64   string           `json:"raw_base64"`
	Context     string           `json:"context"`
	DirectiveID string           `json:"directive_id,omitempty"`
	ContextOnly bool             `json:"context_only,omitempty"`
}

type AnalyzeOptions struct{ ObserveInactive bool }

// Analysis owns its returned projection and envelope. Mutating these data values
// cannot alter the acquired snapshot, execution authority or subsequent analyses.
type Analysis struct {
	Projection  Projection
	Result      result.Result
	snapshot    *Snapshot
	wire        []byte
	records     []result.AnalysisRecord
	filterNodes int64
	cause       error
}

// Analyze uses the acquisition's remaining operation budget. It reads no files,
// substitutes no configuration and invokes the shared lint pipeline exactly once.
// Typed interruptions return completed observations together with their cause.
func Analyze(ctx context.Context, s *Snapshot, options AnalyzeOptions) (Analysis, error) {
	if ctx == nil || s == nil || s.budget == nil {
		return Analysis{}, errors.New("nil inspection context or snapshot")
	}
	f := s.file
	snap := result.Snapshot{View: "original", SHA256: f.SHA256, SizeBytes: s.source.Len()}
	p := Projection{ProjectionVersion: 1, File: InspectionFile{ResolvedPath: f.Path, BinaryVersion: f.Version, SizeBytes: f.SizeBytes, SizeDisplay: f.SizeDisplay, SHA1: f.SHA1, SHA256: f.SHA256, NormallyExcluded: s.exclusion.Excluded, NormallyExcludedKnown: s.exclusion.Known, PhysicalLineCount: s.lines.LineCount()}, Configuration: unavailable[result.ConfigurationData]("configuration-invalid"), Flavor: unavailable[string]("configuration-invalid"), DocumentContext: unavailable[json.RawMessage]("context-unavailable"), AST: unavailable[AST]("context-unavailable"), Rules: unavailable[[]Rule]("context-unavailable"), RuleCounts: notRequested[json.RawMessage](), Directives: unavailable[[]Directive]("context-unavailable"), RawCandidates: unavailable[[]RawCandidate]("context-unavailable"), Resources: notRequested[json.RawMessage](), ParsingNotes: unavailable[[]result.Note]("context-unavailable")}
	out := result.NewLint("cli")
	out.Operation = "inspect"
	out.CheckedFiles = []string{f.Path}
	out.Files = []result.File{{Path: f.Path, Analysis: "not-started", WriteStatus: "not-requested", ReturnStatus: "not-requested", InputSnapshot: &snap, ErrorIDs: []string{}, CoverageIDs: []string{}, ChangeIDs: []string{}}}
	build := builder{ctx: ctx, budget: s.budget, source: s.source, index: s.lines}
	var finish func(error) (Analysis, error)
	finish = func(cause error) (Analysis, error) {
		engine.FinalizeLintResult(&out)
		ids := []string{}
		for _, e := range out.Errors {
			ids = append(ids, e.ID)
		}
		for _, i := range out.Interruptions {
			ids = append(ids, i.ID)
		}
		reason := ""
		if len(out.Interruptions) > 0 {
			reason = out.Interruptions[0].Reason
		}
		causes(&p.Configuration, ids, reason)
		causes(&p.Flavor, ids, reason)
		causes(&p.DocumentContext, ids, reason)
		causes(&p.AST, ids, reason)
		causes(&p.Rules, ids, reason)
		causes(&p.Directives, ids, reason)
		causes(&p.RawCandidates, ids, reason)
		causes(&p.ParsingNotes, ids, reason)
		out.Operation = "inspect"
		out.Status = "inspected"
		if cause != nil {
			out.Status = "incomplete"
		}
		for _, e := range out.Errors {
			if e.Scope == "operation" {
				out.Status = "error"
			}
		}
		out.Complete = out.Complete && p.AST.Availability == "available" && p.Rules.Availability == "available" && p.Directives.Availability == "available"
		raw, _ := json.Marshal(p)
		out.Inspection = raw
		analysis := Analysis{Projection: p, Result: out, snapshot: s, records: out.AnalysisRecords(), cause: cause}
		if cause == nil {
			// Reserve the independent authority image and its structural scan
			// before encoding it. A stopped authority build retains completed
			// public sections and a canonical interruption, never a reset meter.
			work := 8*int64(len(raw)) + 65536
			for _, d := range out.Diagnostics {
				work += 8 * int64(len(d.Message)+len(d.Path)+512)
			}
			for _, d := range out.SuppressedDiagnostics {
				work += 8 * int64(len(d))
			}
			for _, d := range out.Coverage {
				work += 8 * int64(len(d.Message)+len(d.Path)+512)
			}
			for _, d := range out.Errors {
				work += 8 * int64(len(d.Message)+len(d.Path)+512)
			}
			for _, d := range out.Notes {
				work += 8 * int64(len(d))
			}
			if err := build.authority(&analysis, work); err != nil {
				reason := "limit-exceeded"
				if errors.Is(err, context.Canceled) {
					reason = "cancelled"
				}
				if errors.Is(err, context.DeadlineExceeded) {
					reason = "deadline-exceeded"
				}
				interruption := result.Interruption{Path: f.Path, Reason: reason, Phase: "analysis", Message: err.Error()}
				var limit *parser.LimitExceeded
				if errors.As(err, &limit) {
					interruption.Limit = &result.LimitObservation{Name: limit.Name, Observed: limit.Observed, Allowed: limit.Allowed, Unit: result.LimitUnit(limit.Name)}
				}
				out.Interruptions = append(out.Interruptions, interruption)
				return finish(err)
			}
		}
		return analysis, cause
	}
	stop := func(err error) (Analysis, error) {
		reason := "limit-exceeded"
		if errors.Is(err, context.Canceled) {
			reason = "cancelled"
		}
		if errors.Is(err, context.DeadlineExceeded) {
			reason = "deadline-exceeded"
		}
		interruption := result.Interruption{Path: f.Path, Reason: reason, Phase: "analysis", Message: err.Error()}
		var limit *parser.LimitExceeded
		if errors.As(err, &limit) {
			interruption.Limit = &result.LimitObservation{Name: limit.Name, Observed: limit.Observed, Allowed: limit.Allowed, Unit: result.LimitUnit(limit.Name)}
		}
		out.Interruptions = append(out.Interruptions, interruption)
		return finish(err)
	}
	initialWork := 64*int64(s.source.Len()) + 262144
	for _, source := range s.Sources() {
		initialWork += 128 * source.SizeBytes
	}
	if err := build.charge(initialWork); err != nil {
		return stop(err)
	}
	rawCandidates, err := build.inventory()
	if err != nil {
		return stop(err)
	}
	p.RawCandidates = available(rawCandidates)
	if s.configErr != nil {
		out.Errors = append(out.Errors, result.Error{Category: result.Configuration, Code: "configuration-invalid", Scope: "observation", Path: f.Path, Message: s.configErr.Error()})
	} else if s.admissionErr != nil {
		out.Errors = append(out.Errors, result.Error{Category: result.Configuration, Code: "capability-unavailable", Scope: "observation", Path: f.Path, Message: s.admissionErr.Error()})
	}
	if failure := s.lines.EncodingFailure(); failure != nil {
		configured := s.Configuration()
		enabled := false
		if configured != nil {
			enabled = configured.Rules["MD094"].Enabled
			p.Configuration = available(configured.Shared())
			p.Flavor = available(configured.Settings.Flavor)
		}
		validation := engine.ValidateEncoding(f.Path, s.source, enabled)
		if validation.Error != nil {
			validation.Error.Scope = "observation"
			out.Errors = append(out.Errors, *validation.Error)
		}
		out.Files[0].Analysis = "failed"
		out.Coverage = append(out.Coverage, result.Coverage{Path: f.Path, Reason: "decoding-failed", Message: "Parsed inspection sections are unavailable."})
		p.AST = unavailable[AST]("decoding-failed")
		p.Rules = unavailable[[]Rule]("decoding-failed")
		p.Directives = unavailable[[]Directive]("decoding-failed")
		p.ParsingNotes = unavailable[[]result.Note]("decoding-failed")
		return finish(nil)
	}
	configuration := s.Configuration()
	if configuration != nil {
		shared := configuration.Shared()
		for i := range shared.Sources {
			for _, source := range s.Sources() {
				if source.Path == shared.Sources[i].Path {
					shared.Sources[i].SHA256 = source.SHA256
				}
			}
		}
		p.Configuration = available(shared)
		p.Flavor = available(configuration.Settings.Flavor)
		build.profile = configuration.Settings.Flavor
	}
	if s.configErr != nil || s.plan == nil {
		reason := "configuration-invalid"
		if s.configErr == nil {
			reason = "capability-unavailable"
		}

		p.AST = unavailable[AST](reason)
		p.Rules = unavailable[[]Rule](reason)
		p.Directives = unavailable[[]Directive](reason)
		p.ParsingNotes = unavailable[[]result.Note](reason)
		return finish(nil)
	}
	input, err := s.plan.NewSourceInput(f.Path, s.source)
	if err != nil {
		return Analysis{}, err
	}
	var fullRange result.Range
	prepared := false
	projectContext := func(trace *engine.InspectionTrace) error {
		if dc, known := trace.DocumentContext(); known && dc.Slides.Known {
			frontmatter := "disabled"
			parsedContext := trace.Parsed()
			contextReady := true
			if configuration.Settings.FrontmatterEnabled {
				frontmatter = "absent"
				if parsedContext.Document != nil {
					if header, present := parsedContext.Document.Frontmatter(); present {
						frontmatter = header.Format()
						if !header.Closer().Present() {
							frontmatter = "unterminated"
						}
					}
				} else {
					contextReady = false
				}
			}
			if !contextReady {
				return nil
			}
			contextData, contextErr := build.slideContext(dc.Slides, frontmatter)
			if contextErr != nil {
				return contextErr
			}
			p.DocumentContext = available(contextData)
		}
		return nil
	}
	prepare := func(trace *engine.InspectionTrace) error {
		parsed := trace.Parsed()
		build.source = parsed.Source
		build.selectedPath = configuration.Selected.Path
		build.knownRules = s.plan.Rules()
		// Reserve bounded final trace publication now, before callback interruption.
		if err := build.charge(int64(len(build.knownRules))*4096 + 8*int64(s.source.Len()+1)); err != nil {
			return err
		}
		if err := projectContext(trace); err != nil {
			return err
		}
		var err error
		fullRange, err = build.rangeOf(0, s.source.Len())
		if err != nil {
			return err
		}
		ds, err := build.directives(trace.Directives())
		if err != nil {
			return err
		}
		p.Directives = available(ds)
		rs, err := build.rules(trace, build.knownRules)
		if err != nil {
			return err
		}
		p.Rules = available(rs)
		if parsed.Document != nil {
			ast, err := build.ast(parsed.Document, snap)
			if err != nil {
				return err
			}
			p.AST = available(ast)
			if parsed.Status != parser.Complete || parsed.AST != parser.Available || len(parsed.Uncertainty) > 0 {
				p.AST.Availability, p.AST.Reasons = "partial", []string{"parser-uncertain"}
			}
		}
		notes, err := build.parsingNotes(parsed, f.Path, snap)
		if err != nil {
			return err
		}
		p.ParsingNotes = available(notes)
		prepared = true
		return nil
	}
	active, trace, err := s.plan.Inspect(ctx, input, engine.RunOptions{Mode: "cli", Budget: s.budget}, engine.InspectionOptions{ObserveInactive: options.ObserveInactive, Prepare: prepare})
	if err != nil {
		return Analysis{}, err
	}
	out = active
	if !prepared && len(out.Interruptions) == 0 {
		if contextErr := projectContext(trace); contextErr != nil {
			return stop(contextErr)
		}
	}
	for i := range out.Errors {
		if out.Errors[i].Code == "invalid-utf8" {
			out.Errors[i].Scope = "observation"
		}
	}
	if s.lines.EncodingFailure() != nil {
		p.AST = unavailable[AST]("decoding-failed")
		p.Rules = unavailable[[]Rule]("decoding-failed")
		p.Directives = unavailable[[]Directive]("decoding-failed")
		p.ParsingNotes = unavailable[[]result.Note]("decoding-failed")
		return finish(nil)
	}
	if len(out.Interruptions) > 0 && !prepared {
		var cause error
		if limit := trace.Parsed().Limit; limit != nil {
			cause = limit
		}
		if cause == nil {
			cause = ctx.Err()
		}
		if cause == nil {
			cause = s.budget.ChargeWork(0)
		}
		if cause == nil {
			cause = errors.New(out.Interruptions[0].Message)
		}
		return finish(cause)
	}
	parsed := trace.Parsed()
	if parsed.Failure != nil {
		p.AST = unavailable[AST]("parser-failure")
		return finish(nil)
	}
	if parsed.Interruption != "" {
		var cause error
		if parsed.Limit != nil {
			cause = parsed.Limit
		}
		if cause == nil {
			cause = ctx.Err()
		}
		if cause == nil {
			cause = errors.New(string(parsed.Interruption))
		}
		return finish(cause)
	}
	if !prepared {
		exempt := false
		for _, evaluation := range trace.Evaluations() {
			if slices.Contains(evaluation.Reasons, "inapplicable") {
				exempt = true
				break
			}
		}
		if exempt {
			build.knownRules = s.plan.Rules()
			rows, rowErr := build.rules(trace, build.knownRules)
			if rowErr != nil {
				return stop(rowErr)
			}
			p.Rules = Section[[]Rule]{Availability: "partial", Reasons: []string{"context-unavailable"}, Data: &rows}
		}
	}
	if !prepared && parsed.Document != nil && len(parsed.Uncertainty) > 0 {
		ast, err := build.ast(parsed.Document, snap)
		if err != nil {
			return stop(err)
		}
		p.AST = Section[AST]{Availability: "partial", Reasons: []string{"parser-uncertain"}, Data: &ast}
		notes, err := build.parsingNotes(parsed, f.Path, snap)
		if err != nil {
			return stop(err)
		}
		p.ParsingNotes = available(notes)
		if header, present := parsed.Document.Frontmatter(); present {
			for i := range rawCandidates {
				span := rawCandidates[i].ByteRange
				if header.Span().Start() <= span.Start && span.End <= header.Span().End() {
					rawCandidates[i].Context = "literal"
				}
			}
			p.RawCandidates = available(rawCandidates)
		}
	}
	if prepared {
		for i, evaluation := range trace.Evaluations() {
			r := &(*p.Rules.Data)[i]
			r.ActiveEvaluation = evaluation.Active
			r.ObservationalEvaluation = evaluation.Observational
			r.Reasons = slices.Clone(evaluation.Reasons)
			coverage := func(spans []parser.Span, status string) EvaluationCoverage {
				out := EvaluationCoverage{Availability: "unknown", Ranges: []result.Range{}, ByteRanges: []result.ByteRange{}}
				if status == "not-requested" || status == "skipped" {
					out.Availability = "not-requested"
				}
				if len(spans) > 0 {
					out.Availability = "complete"
					out.Ranges = []result.Range{fullRange}
					out.ByteRanges = []result.ByteRange{{Start: 0, End: s.source.Len()}}
				}
				return out
			}
			r.SourceCoverage.Active = coverage(evaluation.ActiveCoverage, evaluation.Active)
			r.SourceCoverage.Observational = coverage(evaluation.ObservationalCoverage, evaluation.Observational)
		}
		candidatesByStart := map[int]int{}
		confirmed := trace.Directives().Candidates()
		for i, c := range confirmed {
			candidatesByStart[c.Span.Start()] = i
		}
		for i := range rawCandidates {
			rawCandidates[i].Context = "literal"
			if j, ok := candidatesByStart[rawCandidates[i].ByteRange.Start]; ok {
				rawCandidates[i].Context = "confirmed"
				rawCandidates[i].DirectiveID = directiveID(j)
				if (*p.Directives.Data)[j].State == "literal" {
					rawCandidates[i].Context = "literal"
				}
			}
		}
		p.RawCandidates = available(rawCandidates)
	}
	// Exact suppressed ownership is retained by the engine even though ordinary
	// lint's wire subset intentionally omits links to unpublished directives.
	suppressed := trace.Suppressed()
	for _, record := range out.AnalysisRecords() {
		if a, ok := record.(engine.DirectiveAnalysis); ok {
			suppressed = append(suppressed, a.Suppressed()...)
		}
	}
	out.SuppressedDiagnostics = []json.RawMessage{}
	for _, hidden := range suppressed {
		kind := "configuration"
		id := ""
		if hidden.Owner >= 0 {
			kind = "directive"
			id = directiveID(hidden.Owner)
		}
		raw, _ := json.Marshal(result.SuppressedDiagnostic{Diagnostic: hidden.Diagnostic, SuppressedBy: result.Suppression{Kind: kind, DirectiveID: id}})
		out.SuppressedDiagnostics = append(out.SuppressedDiagnostics, raw)
	}
	out.Errors = append(out.Errors, trace.Errors()...)
	for _, evaluation := range trace.Evaluations() {
		if evaluation.Observational == "partial" || evaluation.Observational == "failed" {
			out.Coverage = append(out.Coverage, result.Coverage{Path: f.Path, Rule: evaluation.Rule, Reason: result.ContextUnavailable, Message: "Observational rule evaluation did not complete."})
		}
	}
	if trace.Interruption() != nil {
		return stop(trace.Interruption())
	}
	if len(out.Interruptions) > 0 {
		cause := ctx.Err()
		if cause == nil {
			cause = s.budget.ChargeWork(0)
		}
		if cause == nil {
			cause = errors.New(out.Interruptions[0].Message)
		}
		return finish(cause)
	}
	if err := build.charge(0); err != nil {
		return stop(err)
	}
	return finish(nil)
}

type builder struct {
	ctx          context.Context
	budget       *parser.OperationBudget
	source       parser.Source
	index        parser.LineIndex
	nextNode     int
	positions    map[int]parser.Position
	selectedPath string
	profile      string
	knownRules   map[string]engine.ResolvedRule
}

func (b *builder) charge(n int64) error {
	if err := b.ctx.Err(); err != nil {
		return err
	}
	return b.budget.ChargeWork(n)
}
func (b *builder) rangeOf(start, end int) (result.Range, error) {
	position := func(at int) (parser.Position, error) {
		if p, ok := b.positions[at]; ok {
			return p, b.charge(1)
		}
		p, err := b.index.PositionBounded(b.ctx, b.budget, at)
		if err == nil {
			if b.positions == nil {
				b.positions = map[int]parser.Position{}
			}
			if len(b.positions) < 256 {
				b.positions[at] = p
			}
		}
		return p, err
	}
	a, err := position(start)
	if err != nil {
		return result.Range{}, err
	}
	z, err := position(end)
	return result.Range{Start: a, End: z}, err
}
func bytesOf(s parser.Span) result.ByteRange { return result.ByteRange{Start: s.Start(), End: s.End()} }
func byteRanges(parts []parser.Span) []result.ByteRange {
	out := make([]result.ByteRange, 0, len(parts))
	for _, span := range parts {
		out = append(out, bytesOf(span))
	}
	return out
}
func optionalSpan(s parser.Span) *result.ByteRange {
	if !s.Present() {
		return nil
	}
	out := bytesOf(s)
	return &out
}
func optional[T any](v parser.Optional[T]) *T {
	if !v.Present {
		return nil
	}
	return &v.Value
}
func value(v parser.Value) Value { return Value{segments(v.Segments), optional(v.Semantic)} }
func valuePointer(v *parser.Value) *Value {
	if v == nil {
		return nil
	}
	out := value(*v)
	return &out
}
func segments(parts []parser.Segment) []Segment {
	out := []Segment{}
	for _, s := range parts {
		p := Segment{Transformation: string(s.Transformation), Semantic: optional(s.Semantic)}
		if s.Source.Present() {
			r := bytesOf(s.Source)
			p.ByteRange = &r
		}
		out = append(out, p)
	}
	return out
}
func markers(parts []parser.Marker) []Marker {
	out := []Marker{}
	for _, m := range parts {
		out = append(out, Marker{m.Role, bytesOf(m.Source)})
	}
	return out
}
func (b *builder) node(n *parser.Node) (Node, error) {
	if err := b.charge(128 * int64(n.Span().End()-n.Span().Start()+1)); err != nil {
		return Node{}, err
	}
	if err := b.budget.ChargeNodes(1); err != nil {
		return Node{}, err
	}
	r, err := b.rangeOf(n.Span().Start(), n.Span().End())
	if err != nil {
		return Node{}, err
	}
	b.nextNode++
	out := Node{ID: fmt.Sprintf("node-%d", b.nextNode), Kind: string(n.Kind()), Range: r, ByteRange: bytesOf(n.Span()), Properties: map[string]any{}, RawAttributes: []json.RawMessage{}, Children: []Node{}}
	p := n.Properties()
	// Reserve extension projection records before constructing owned slices and
	// Values. The enclosing source precharge covers their byte copies; semantic
	// padding is a count and never reserves fabricated source cells.
	records := int64(0)
	if p.Table != nil {
		records += int64(len(p.Table.Alignments))
	}
	if p.Row != nil {
		records += int64(len(p.Row.Separators)) + 2
	}
	if p.Cell != nil {
		records += int64(len(p.Cell.EscapedPipes)) + 1
	}
	if p.Task != nil {
		records += int64(len(p.Task.Segments)) + 8
	}
	if p.HTML != nil && b.profile == "gfm" {
		for _, tag := range p.HTML.FilteredTags {
			records += 3 + int64(len(tag.Prefix.Segments))
			if tag.Tag != nil {
				records += 1 + int64(len(tag.Tag.Segments))
			}
		}
	}
	if err := b.budget.ChargeNodes(records); err != nil {
		return Node{}, err
	}
	props := out.Properties
	props["markers"] = markers(n.Markers())
	props["content"] = segments(n.Content())
	if p.Literal.Present {
		props["literal"] = p.Literal.Value
	}
	if h := p.Heading; h != nil {
		props["level"] = h.Level
		props["style"] = h.Style
	}
	if l := p.List; l != nil {
		props["ordered"] = l.Ordered
		if l.Start.Present {
			props["start"] = l.Start.Value
		}
		if l.Bullet.Present {
			props["bullet"] = string(l.Bullet.Value)
		}
		if l.Delimiter.Present {
			props["delimiter"] = string(l.Delimiter.Value)
		}
		if l.Tight.Present {
			props["tight"] = l.Tight.Value
		}
		if l.MarkerWidth.Present {
			props["marker_width"] = l.MarkerWidth.Value
		}
		if l.Padding.Present {
			props["padding"] = l.Padding.Value
		}
		if l.ContentIndent.Present {
			props["content_indent"] = l.ContentIndent.Value
		}
		if l.Prefix != nil {
			props["prefix"] = value(*l.Prefix)
		}
	}
	if c := p.Code; c != nil {
		props["style"] = c.Style
		if c.Fence.Present {
			props["fence"] = string(c.Fence.Value)
		}
		if c.Width.Present {
			props["width"] = c.Width.Value
		}
		if c.Info != nil {
			props["info"] = value(*c.Info)
		}
	}
	if h := p.HTML; h != nil {
		if h.BlockForm.Present {
			props["block_form"] = h.BlockForm.Value
		}
		if b.profile == "gfm" {
			tags := make([]FilteredTag, 0, len(h.FilteredTags))
			for _, tag := range h.FilteredTags {
				tags = append(tags, FilteredTag{tag.Name, tag.Closing, value(tag.Prefix), bytesOf(tag.Affected), valuePointer(tag.Tag)})
			}
			props["filtered_tags"] = tags
		}
	}
	if t := p.Table; t != nil {
		props["width"] = t.Width
		alignments := make([]string, 0, len(t.Alignments))
		for _, a := range t.Alignments {
			alignments = append(alignments, string(a))
		}
		props["alignments"] = alignments
	}
	if row := p.Row; row != nil {
		props["role"] = row.Role
		props["raw_cells"] = row.RawCells
		props["padding"] = row.Padding
		props["separators"] = byteRanges(row.Separators)
		if r := optionalSpan(row.LeadingPipe); r != nil {
			props["leading_pipe"] = *r
		}
		if r := optionalSpan(row.TrailingPipe); r != nil {
			props["trailing_pipe"] = *r
		}
	}
	if cell := p.Cell; cell != nil {
		props["escaped_pipes"] = byteRanges(cell.EscapedPipes)
		if cell.Alignment.Present {
			props["alignment"] = string(cell.Alignment.Value)
		}
	}
	if task := p.Task; task != nil {
		props["task"] = Task{bytesOf(task.Paragraph), bytesOf(task.Marker), bytesOf(task.Opening), bytesOf(task.Check), bytesOf(task.Closing), bytesOf(task.Separator), byteRanges(task.Segments), task.Checked, task.CheckSpelling}
	}
	if d := p.Delimiter; d != nil {
		props["marker"] = string(d.Marker)
		props["width"] = d.Width
	}
	if br := p.Break; br != nil {
		props["origin"] = br.Origin
	}
	if l := p.Link; l != nil {
		props["origin"] = l.Origin
		props["label"] = value(l.Label)
		props["destination"] = value(l.Destination)
		props["resolution"] = string(l.Resolution)
		if l.ReferenceLabel != nil {
			props["reference_label"] = value(*l.ReferenceLabel)
		}
		if l.Title != nil {
			props["title"] = value(*l.Title)
		}
		if l.NormalizedLabel.Present {
			props["normalized_label"] = l.NormalizedLabel.Value
		}
	}
	for _, child := range n.Children() {
		v, err := b.node(child)
		if err != nil {
			return Node{}, err
		}
		out.Children = append(out.Children, v)
	}
	return out, nil
}
func (b *builder) ast(d *parser.Document, snap result.Snapshot) (AST, error) {
	root, err := b.node(d.Root())
	if err != nil {
		return AST{}, err
	}
	out := AST{ProjectionVersion: 1, Profile: b.profile, Snapshot: snap, Root: root, References: []Reference{}}
	if err := b.charge(128 * int64(b.source.Len()+1)); err != nil {
		return AST{}, err
	}
	if header, present := d.Frontmatter(); present {
		projected, err := b.frontmatter(header)
		if err != nil {
			return AST{}, err
		}
		out.Frontmatter = projected
	}
	refs := d.References()
	for i, r := range refs.Definitions {
		p := r.Properties()
		effective := false
		if p.NormalizedLabel.Present {
			v, ok := refs.Lookup[p.NormalizedLabel.Value]
			effective = ok && v == i
		}
		out.References = append(out.References, Reference{bytesOf(p.Envelope), markers(p.Markers), value(p.Label), value(p.Destination), valuePointer(p.Title), optional(p.NormalizedLabel), effective, false})
	}
	return out, nil
}
func (b *builder) parsingNotes(parsed parser.ParseResult, path string, snap result.Snapshot) ([]result.Note, error) {
	notes := []result.Note{}
	for i, uncertainty := range parsed.Uncertainty {
		if err := b.charge(1); err != nil {
			return nil, err
		}
		if err := b.budget.ChargeNodes(1); err != nil {
			return nil, err
		}
		span, err := b.rangeOf(uncertainty.Region.Start(), uncertainty.Region.End())
		if err != nil {
			return nil, err
		}
		br := bytesOf(uncertainty.Region)
		notes = append(notes, result.Note{ID: "parsing-note-" + strconv.Itoa(i+1), Code: string(uncertainty.Reason), Message: "Parser interpretation is incomplete in this original source region.", Path: path, Range: &span, ByteRange: &br, Snapshot: &snap})
	}
	return notes, nil
}

func (b *builder) frontmatter(header parser.Frontmatter) (*Frontmatter, error) {
	if err := b.charge(16); err != nil {
		return nil, err
	}
	if err := b.budget.ChargeNodes(12); err != nil {
		return nil, err
	}
	region := func(span parser.Span) (SourceRegion, error) {
		r, err := b.rangeOf(span.Start(), span.End())
		return SourceRegion{Range: r, ByteRange: bytesOf(span)}, err
	}
	whole, err := region(header.Span())
	if err != nil {
		return nil, err
	}
	opener, err := region(header.Opener())
	if err != nil {
		return nil, err
	}
	content, err := region(header.Content())
	if err != nil {
		return nil, err
	}
	out := &Frontmatter{Format: header.Format(), Range: whole.Range, ByteRange: whole.ByteRange, Opener: opener, Content: content, BodyOffset: header.BodyOffset()}
	if header.Closer().Present() {
		closer, err := region(header.Closer())
		if err != nil {
			return nil, err
		}
		out.Closer = &closer
	}
	return out, nil
}

func directiveID(index int) string { return "directive-" + strconv.Itoa(index+1) }
func (b *builder) directives(state engine.DirectiveResult) ([]Directive, error) {
	if err := b.charge(64 * int64(b.source.Len()+1)); err != nil {
		return nil, err
	}
	out := []Directive{}
	candidates := state.Candidates()
	type ownerRule struct {
		owner int
		rule  string
	}
	ownedRanges := map[ownerRule][]result.Range{}
	for id := range b.knownRules {
		for _, interval := range state.Intervals(id) {
			if interval.Owner < 0 {
				continue
			}
			r, err := b.rangeOf(interval.Start, interval.End)
			if err != nil {
				return nil, err
			}
			key := ownerRule{interval.Owner, id}
			ownedRanges[key] = append(ownedRanges[key], r)
		}
	}
	for i, c := range candidates {
		r, err := b.rangeOf(c.Span.Start(), c.Span.End())
		if err != nil {
			return nil, err
		}
		d := Directive{ID: directiveID(i), Raw: c.Raw, Range: r, ByteRange: bytesOf(c.Span), ClosingOffset: c.Span.End(), Operation: string(c.Operation), State: string(c.Disposition), Effects: []DirectiveEffect{}}
		for _, token := range c.Named {
			d.NamedTokens = append(d.NamedTokens, NamedToken{token.Name, bytesOf(token.Span)})
		}
		for _, id := range c.IDs {
			if _, ok := b.knownRules[id]; ok {
				d.ResolvedRules = append(d.ResolvedRules, id)
			}
		}
		if c.Closing.Present() {
			d.ClosingOffset = c.Closing.End()
		}
		switch c.Disposition {
		case engine.DirectiveApplied:
			d.State = "effective"
		case engine.DirectiveBlocked, engine.DirectiveInapplicable, engine.DirectiveInactive, engine.DirectiveInvalid:
		case engine.DirectiveUncertain:
			d.State = "uncertain"
			for _, n := range state.Notes() {
				if n.Code == "directive-literal" && n.Span == c.Span {
					d.State = "literal"
				}
			}
		}
		dispositions := map[string]bool{}
		for _, e := range c.Effects {
			effect := DirectiveEffect{Rule: e.Rule, Permission: "allowed", Effect: string(c.Operation), Ranges: []result.Range{}}
			switch e.Disposition {
			case engine.DirectiveApplied, engine.DirectiveInactive, engine.DirectiveInvalid, engine.DirectiveUncertain:
			case engine.DirectiveBlocked:
				effect.Permission = "denied"
				effect.Effect = "blocked"
			case engine.DirectiveInapplicable:
				effect.Permission = "inapplicable"
				effect.Effect = "inapplicable"
			}
			for _, origin := range e.Restrictions {
				pointer := "/" + strings.ReplaceAll(origin.PropertyPath, ".", "/")
				p := result.Provenance{Pointer: pointer, SourceKind: "selected-config", Path: origin.Path}
				if origin.Path != b.selectedPath {
					p.SourceKind = "ancestor-config"
					p.Inherited = true
				}
				if origin.Position != nil {
					p.Range = &result.Range{Start: *origin.Position, End: *origin.Position}
				}
				effect.Provenance = append(effect.Provenance, p)
			}
			if ranges := ownedRanges[ownerRule{i, e.Rule}]; ranges != nil {
				effect.Ranges = ranges
			}
			dispositions[effect.Effect] = true
			d.Effects = append(d.Effects, effect)
		}
		if len(dispositions) > 1 {
			d.State = "mixed"
		} else if dispositions["blocked"] {
			d.State = "blocked"
		} else if dispositions["inapplicable"] {
			d.State = "inapplicable"
		}
		out = append(out, d)
	}
	return out, nil
}
func (b *builder) rules(trace *engine.InspectionTrace, settings map[string]engine.ResolvedRule) ([]Rule, error) {
	out := []Rule{}
	state := trace.Directives()
	for _, evaluation := range trace.Evaluations() {
		if err := b.charge(1024); err != nil {
			return nil, err
		}
		s := settings[evaluation.Rule]
		r := Rule{Rule: evaluation.Rule, Implementation: "unavailable", ConfiguredEnabled: s.Enabled, AllowDirectives: s.AllowDirectives, Options: s.Options, EffectiveState: "unresolved", Intervals: []Interval{}, ActiveEvaluation: evaluation.Active, ObservationalEvaluation: evaluation.Observational, Reasons: slices.Clone(evaluation.Reasons)}
		if s.Available {
			r.Implementation = "available"
		}
		if state.Complete() {
			r.EffectiveState = "inactive"
			if state.EnabledSomewhere(evaluation.Rule) {
				r.EffectiveState = "active"
			}
			for _, v := range state.Intervals(evaluation.Rule) {
				span, err := b.rangeOf(v.Start, v.End)
				if err != nil {
					return nil, err
				}
				in := Interval{Range: span, ByteRange: result.ByteRange{Start: v.Start, End: v.End}, Enabled: v.Enabled, Origin: "configuration"}
				if v.Owner >= 0 {
					in.Origin = "directive"
					in.DirectiveID = directiveID(v.Owner)
				}
				r.Intervals = append(r.Intervals, in)
			}
		}
		coverage := func(spans []parser.Span, status string) (EvaluationCoverage, error) {
			c := EvaluationCoverage{Availability: "unknown", Ranges: []result.Range{}, ByteRanges: []result.ByteRange{}}
			if status == "not-requested" || status == "skipped" {
				c.Availability = "not-requested"
			}
			if len(spans) > 0 {
				c.Availability = "complete"
			}
			for _, s := range spans {
				r, err := b.rangeOf(s.Start(), s.End())
				if err != nil {
					return c, err
				}
				c.Ranges = append(c.Ranges, r)
				c.ByteRanges = append(c.ByteRanges, bytesOf(s))
			}
			return c, nil
		}
		var err error
		r.SourceCoverage.Active, err = coverage(evaluation.ActiveCoverage, evaluation.Active)
		if err != nil {
			return nil, err
		}
		r.SourceCoverage.Observational, err = coverage(evaluation.ObservationalCoverage, evaluation.Observational)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}
func (b *builder) inventory() ([]RawCandidate, error) {
	if err := b.charge(8 * int64(b.source.Len()+1)); err != nil {
		return nil, err
	}
	span, _ := b.source.Span(0, b.source.Len())
	text, _ := b.source.Text(span)
	out := []RawCandidate{}
	nextClose := -1
	searchedEnd := false
	for at := 0; at < len(text); {
		relative := strings.Index(text[at:], "<!--")
		if relative < 0 {
			break
		}
		start := at + relative
		at = start + 4
		if !searchedEnd && nextClose < at {
			relativeClose := strings.Index(text[at:], "-->")
			if relativeClose < 0 {
				searchedEnd = true
				nextClose = -1
			} else {
				nextClose = at + relativeClose
			}
		}
		end := len(text)
		bodyEnd := end
		if nextClose >= at {
			end = nextClose + 3
			bodyEnd = nextClose
		}
		body := strings.TrimLeftFunc(text[at:bodyEnd], unicode.IsSpace)
		if len(body) < 5 || !strings.EqualFold(body[:5], "mdchk") {
			continue
		}
		if len(body) > 5 && body[5] != ':' {
			r, _ := utf8.DecodeRuneInString(body[5:])
			if !unicode.IsSpace(r) {
				continue
			}
		}
		if err := b.charge(8 * int64(end-start+1)); err != nil {
			return out, err
		}
		out = append(out, RawCandidate{ByteRange: result.ByteRange{Start: start, End: end}, RawBase64: base64.StdEncoding.EncodeToString([]byte(text[start:end])), Context: "unconfirmed"})
	}
	return out, nil
}

// authority reserves construction before encoding and observes context and the
// same elapsed meter again before its image can be published as successful.
func (b *builder) authority(analysis *Analysis, work int64) error {
	if err := b.charge(work); err != nil {
		return err
	}
	wireView := *analysis
	wireView.Result.Inspection = nil // projection appears once in authority
	wire, err := json.Marshal(wireView)
	if err != nil {
		return err
	}
	nodes := jsonStructures(wire)
	if err := b.charge(0); err != nil {
		return err
	}
	analysis.wire = wire
	analysis.filterNodes = nodes
	return nil
}

// slideContext encodes only known annotation, independently of parser confidence.
// Exempt rule rows remain partial when unrelated source/directive data is unknown.
func (b *builder) slideContext(c engine.SlideContext, frontmatter string) (json.RawMessage, error) {
	if err := b.charge(8*int64(len(c.Pattern)+len(c.OriginPath)+len(c.OriginProperty)) + 1024); err != nil {
		return nil, err
	}
	if err := b.budget.ChargeNodes(12); err != nil {
		return nil, err
	}
	data := DocumentContextData{Slides: c.Slides, MatchedSlidePatterns: []string{}, Frontmatter: frontmatter}
	if c.Slides {
		m := &SlideMatch{Pattern: c.Pattern, Index: c.PatternIndex, Origin: SlideOrigin{Path: c.OriginPath, PropertyPath: c.OriginProperty}}
		if c.OriginPosition != (parser.Position{}) {
			m.Origin.Position = &SlideOriginPosition{Line: c.OriginPosition.Line, Column: c.OriginPosition.Column}
		}
		data.SlideMatch = m
		data.MatchedSlidePatterns = append(data.MatchedSlidePatterns, c.Pattern)
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	if err := b.charge(0); err != nil {
		return nil, err
	}
	return raw, nil
}
