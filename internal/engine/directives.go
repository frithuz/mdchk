package engine

import (
	"context"
	"errors"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

type DirectiveOperation string

const (
	DirectiveEnable  DirectiveOperation = "enable"
	DirectiveDisable DirectiveOperation = "disable"
)

type DirectiveDisposition string

const (
	DirectiveApplied      DirectiveDisposition = "applied"
	DirectiveBlocked      DirectiveDisposition = "blocked"
	DirectiveInapplicable DirectiveDisposition = "inapplicable"
	DirectiveInactive     DirectiveDisposition = "inactive"
	DirectiveInvalid      DirectiveDisposition = "invalid"
	DirectiveUncertain    DirectiveDisposition = "uncertain"
)

// DirectiveToken retains the first original token anchor, including repeated names.
type DirectiveToken struct {
	Name string
	Span parser.Span
}
type DirectiveEffect struct {
	Rule         string
	Token        parser.Span
	Disposition  DirectiveDisposition
	Restrictions []result.ConfigurationOrigin
}
type DirectiveCandidate struct {
	Raw, Payload  string
	Span, Closing parser.Span
	Operation     DirectiveOperation
	Named         []DirectiveToken
	IDs           []string
	Disposition   DirectiveDisposition
	Effects       []DirectiveEffect
}

// DirectiveNote is a processing explanation, never a configurable rule violation.
type DirectiveNote struct {
	Code, Message, Rule string
	Span                parser.Span
	Restrictions        []result.ConfigurationOrigin
}

// DirectiveInterval includes its start and excludes its end, except that the final
// interval also owns the EOF anchor. Owner is a candidate index, or -1 for
// configured state. Repeated instructions retain separate ownership.
type DirectiveInterval struct {
	Start, End int
	Enabled    bool
	Owner      int
	Token      parser.Span
}
type DirectiveState struct {
	Enabled bool
	Owner   int
	Token   parser.Span
	Options Options
}

// DirectiveResult owns its records and never exposes writable aliases. Incomplete results
// withhold intervals entirely: unprocessed comments cannot imply stable state.
type DirectiveResult struct {
	operation   parser.OperationToken
	catalogs    map[string]*registryIdentity
	source      parser.Source
	complete    bool
	candidates  []DirectiveCandidate
	diagnostics []result.Diagnostic
	notes       []DirectiveNote
	intervals   map[string][]DirectiveInterval
	rules       map[string]ResolvedRule
	err         error
}

func (r DirectiveResult) Complete() bool { return r.complete }
func (r DirectiveResult) Err() error {
	var limit *parser.LimitExceeded
	if errors.As(r.err, &limit) {
		copy := *limit
		return &copy
	}
	return r.err
}
func (r DirectiveResult) Source() parser.Source { return r.source }
func (r DirectiveResult) Candidates() []DirectiveCandidate {
	out := slices.Clone(r.candidates)
	for i := range out {
		out[i].Named = slices.Clone(out[i].Named)
		out[i].IDs = slices.Clone(out[i].IDs)
		out[i].Effects = slices.Clone(out[i].Effects)
		for j := range out[i].Effects {
			out[i].Effects[j].Restrictions = cloneDirectiveOrigins(out[i].Effects[j].Restrictions)
		}
	}
	return out
}
func (r DirectiveResult) Diagnostics() []result.Diagnostic { return slices.Clone(r.diagnostics) }
func (r DirectiveResult) Notes() []DirectiveNote {
	out := slices.Clone(r.notes)
	for i := range out {
		out[i].Restrictions = cloneDirectiveOrigins(out[i].Restrictions)
	}
	return out
}
func (r DirectiveResult) Intervals(id string) []DirectiveInterval {
	return slices.Clone(r.intervals[id])
}
func (r DirectiveResult) StateAt(id string, offset int) (DirectiveState, error) {
	if !r.complete || offset < 0 || offset > r.source.Len() {
		return DirectiveState{}, errors.New("unresolved directive state or source offset")
	}
	intervals, known := r.intervals[id]
	if !known {
		return DirectiveState{}, errors.New("unknown directive rule")
	}
	i, _ := slices.BinarySearchFunc(intervals, offset, func(v DirectiveInterval, at int) int {
		if v.Start < at {
			return -1
		}
		if v.Start > at {
			return 1
		}
		return 0
	})
	// Several transitions may share EOF; the last instruction owns the anchor.
	for i < len(intervals) && intervals[i].Start <= offset {
		i++
	}
	v := intervals[i-1]
	return DirectiveState{v.Enabled, v.Owner, v.Token, cloneDirectiveOptions(r.rules[id].Options)}, nil
}
func cloneDirectiveOptions(in Options) Options {
	out := make(Options, len(in))
	for k, v := range in {
		if s, ok := v.([]string); ok {
			v = slices.Clone(s)
		}
		out[k] = v
	}
	return out
}
func cloneDirectiveOrigins(in []result.ConfigurationOrigin) []result.ConfigurationOrigin {
	out := slices.Clone(in)
	for i := range out {
		if out[i].Position != nil {
			p := *out[i].Position
			out[i].Position = &p
		}
	}
	return out
}

// ProcessDirectives requires the same operation budget used for parsing. No budget is
// created/reset here. Configuration authority comes from private validated state,
// independently of mutable inspection views and executable rule availability.
func ProcessDirectives(ctx context.Context, parsed parser.ParseResult, configuration DirectivePolicy, budget *parser.OperationBudget) DirectiveResult {
	r := DirectiveResult{operation: budget.Token(), source: parsed.Source, intervals: map[string][]DirectiveInterval{}}
	stop := func(err error, code string) DirectiveResult {
		r.complete = false
		r.err = err
		r.intervals = nil
		r.notes = append(r.notes, DirectiveNote{Code: code, Message: err.Error()})
		return r
	}
	if ctx == nil || budget == nil {
		return stop(errors.New("nil directive context or operation budget"), "directive-incomplete")
	}
	check := func(units int64) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return budget.ChargeWork(units)
	}
	if err := check(0); err != nil {
		return stop(err, "directive-incomplete")
	}
	if configuration == nil {
		return stop(errors.New("unresolved directive configuration"), "directive-policy-unresolved")
	}
	global, rules, err := configuration.DirectiveConfiguration()
	if err != nil {
		return stop(err, "directive-policy-unresolved")
	}
	r.rules = rules
	if err := check(int64(len(rules))); err != nil {
		return stop(err, "directive-incomplete")
	}
	if err := budget.ChargeNodes(int64(len(rules))); err != nil {
		return stop(err, "directive-incomplete")
	}
	r.catalogs = make(map[string]*registryIdentity, len(rules))
	for id, rule := range rules {
		if err := check(1); err != nil {
			return stop(err, "directive-incomplete")
		}
		r.catalogs[id] = rule.registry
	}
	if parsed.Status != parser.Complete || parsed.AST != parser.Available || len(parsed.Uncertainty) != 0 || parsed.Document == nil || parsed.Document.Source() != parsed.Source {
		return stop(errors.New("directive comment context is uncertain"), "parsing")
	}
	facts, err := parsed.Document.Comments(ctx, budget)
	if err != nil {
		return stop(err, "parsing")
	}
	for id, rule := range rules {
		if err := check(1); err != nil {
			return stop(err, "directive-incomplete")
		}
		r.intervals[id] = []DirectiveInterval{{Start: 0, End: r.source.Len(), Enabled: rule.Enabled, Owner: -1}}
	}
	var index parser.LineIndex
	indexed := false
	for _, fact := range facts {
		if err := check(int64(8 * (fact.Envelope.End() - fact.Envelope.Start() + len(fact.Payload) + 1))); err != nil {
			return stop(err, "directive-incomplete")
		}
		var payload strings.Builder
		for _, part := range fact.Payload {
			text, _ := r.source.Text(part.Source)
			payload.WriteString(text)
		}
		body := payload.String()
		trimmed := strings.TrimSpace(body)
		if !candidateLike(trimmed) {
			continue
		}
		raw, _ := r.source.Text(fact.Envelope)
		c := DirectiveCandidate{Raw: raw, Payload: body, Span: fact.Envelope, Closing: fact.Close, Disposition: DirectiveInactive}
		if !global {
			units, err := configuration.DirectivePermissionWork("")
			if err != nil {
				return stop(err, "directive-policy-unresolved")
			}
			if err := check(2 * units); err != nil {
				return stop(err, "directive-incomplete")
			}
			r.candidates = append(r.candidates, c)
			r.notes = append(r.notes, DirectiveNote{Code: "directives-disabled", Message: "directive candidate is inactive because global directives are disabled", Span: c.Span, Restrictions: configuration.GlobalDirectivePermission().Restrictions})
			continue
		}
		if fact.Protected || !fact.Close.Present() {
			c.Disposition = DirectiveUncertain
			r.candidates = append(r.candidates, c)
			code, message := "parsing", "unclosed directive comment has no transition"
			if fact.Protected {
				code, message = "directive-literal", "directive candidate is in a literal HTML interior"
			}
			r.notes = append(r.notes, DirectiveNote{Code: code, Message: message, Span: c.Span})
			continue
		}
		tokens := tokenize(body, fact.Payload, r.source)
		problem := ""
		switch tokens[0].Name {
		case "mdchk:enable":
			c.Operation = DirectiveEnable
		case "mdchk:disable":
			c.Operation = DirectiveDisable
		default:
			problem = "unknown or malformed directive command"
		}
		c.Named = slices.Clone(tokens[1:])
		if len(c.Named) == 0 {
			problem = "directive requires at least one canonical rule ID"
		}
		seen := map[string]bool{}
		for _, token := range c.Named {
			if err := check(int64(len(token.Name) + 1)); err != nil {
				return stop(err, "directive-incomplete")
			}
			if _, known := rules[token.Name]; !known {
				problem = "unknown or malformed canonical rule ID: " + token.Name
			}
			if !seen[token.Name] {
				seen[token.Name] = true
				c.IDs = append(c.IDs, token.Name)
			}
		}
		if problem != "" {
			c.Disposition = DirectiveInvalid
			r.candidates = append(r.candidates, c)
			if !indexed {
				if err := check(int64(3*r.source.Len() + 1)); err != nil {
					return stop(err, "directive-incomplete")
				}
				index = parser.NewLineIndex(r.source)
				indexed = true
			}
			// Position performs Unicode prefix decoding. Reserve both full-source
			// prefixes conservatively before the unmetered public index calls.
			if err := check(int64(2*r.source.Len() + 1)); err != nil {
				return stop(err, "directive-incomplete")
			}
			start, _ := index.Position(c.Span.Start())
			end, _ := index.Position(c.Span.End())
			r.diagnostics = append(r.diagnostics, result.Diagnostic{Rule: "directive", Severity: "error", Message: problem, Range: result.Range{Start: start, End: end}, ByteRange: result.ByteRange{Start: c.Span.Start(), End: c.Span.End()}})
			continue
		}
		// Reserve all effects before publishing any transition from this directive.
		if err := check(int64(4*len(c.IDs) + 1)); err != nil {
			return stop(err, "directive-incomplete")
		}
		c.Disposition = DirectiveApplied
		for _, id := range c.IDs {
			var token parser.Span
			for _, named := range c.Named {
				if named.Name == id {
					token = named.Span
					break
				}
			}
			units, err := configuration.DirectivePermissionWork(id)
			if err != nil {
				return stop(err, "directive-policy-unresolved")
			}
			// Permission queries and blocked-effect notes retain separate owned
			// views. Reserve all possible origin copies before either allocation.
			if err := check(4 * units); err != nil {
				return stop(err, "directive-incomplete")
			}
			permission := configuration.DirectivePermission(id)
			effect := DirectiveEffect{Rule: id, Token: token, Disposition: DirectiveApplied, Restrictions: permission.Restrictions}
			switch {
			case permission.Err != nil:
				return stop(permission.Err, "directive-policy-unresolved")
			case !permission.Allowed:
				effect.Disposition = DirectiveBlocked
				r.notes = append(r.notes, DirectiveNote{Code: "directive-blocked", Message: "directive effect blocked by configuration", Rule: id, Span: token, Restrictions: cloneDirectiveOrigins(permission.Restrictions)})
			case id == "MD074" || id == "MD094":
				effect.Disposition = DirectiveInapplicable
				r.notes = append(r.notes, DirectiveNote{Code: "directive-inapplicable", Message: "document directives do not control this rule", Rule: id, Span: token})
			default:
				intervals := r.intervals[id]
				at := fact.Close.End()
				intervals[len(intervals)-1].End = at
				r.intervals[id] = append(intervals, DirectiveInterval{Start: at, End: r.source.Len(), Enabled: c.Operation == DirectiveEnable, Owner: len(r.candidates), Token: token})
			}
			c.Effects = append(c.Effects, effect)
		}
		r.candidates = append(r.candidates, c)
	}
	if err := check(0); err != nil {
		return stop(err, "directive-incomplete")
	}
	r.complete = true
	return r
}

// DirectiveCandidate classification is case insensitive only for the mdchk introducer;
// validation remains exact. Prefix collisions such as mdchecker are ordinary HTML.
func candidateLike(s string) bool {
	if len(s) < 5 || !strings.EqualFold(s[:5], "mdchk") {
		return false
	}
	if len(s) == 5 {
		return true
	}
	next, _ := utf8.DecodeRuneInString(s[5:])
	return next == ':' || unicode.IsSpace(next)
}

// Whitespace is Unicode White_Space. Tokens retain original byte anchors across
// mapped container gaps; whitespace/newlines are never inferred from node ends.
func tokenize(body string, parts []parser.Segment, source parser.Source) []DirectiveToken {
	var tokens []DirectiveToken
	logical := 0
	partIndex := 0
	anchor := func(offset int) int {
		for partIndex < len(parts) {
			width := parts[partIndex].Source.End() - parts[partIndex].Source.Start()
			if offset < logical+width {
				return parts[partIndex].Source.Start() + offset - logical
			}
			logical += width
			partIndex++
		}
		return parts[len(parts)-1].Source.End()
	}
	for i := 0; i < len(body); {
		r, width := utf8.DecodeRuneInString(body[i:])
		if unicode.IsSpace(r) {
			i += width
			continue
		}
		start := i
		for i < len(body) {
			r, width = utf8.DecodeRuneInString(body[i:])
			if unicode.IsSpace(r) {
				break
			}
			i += width
		}
		a := anchor(start)
		b := anchor(i-1) + 1
		span, _ := source.Span(a, b)
		tokens = append(tokens, DirectiveToken{body[start:i], span})
	}
	return tokens
}

// EnabledSomewhere includes the final EOF anchor, even for a zero-width interval.
// Earlier empty transition intervals have no anchor and cannot require a check.
func (r DirectiveResult) EnabledSomewhere(id string) bool {
	if !r.complete {
		return false
	}
	intervals := r.intervals[id]
	for i, v := range intervals {
		if v.Enabled && (v.Start < v.End || i == len(intervals)-1) {
			return true
		}
	}
	return false
}
