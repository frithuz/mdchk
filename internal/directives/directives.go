// Package directives recognizes parser-confirmed comments and retains document-
// local rule-state intervals. It does not execute rules or grant capabilities.
package directives

import (
	"context"
	"errors"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

type Operation string

const (
	Enable  Operation = "enable"
	Disable Operation = "disable"
)

type Disposition string

const (
	Applied      Disposition = "applied"
	Blocked      Disposition = "blocked"
	Inapplicable Disposition = "inapplicable"
	Inactive     Disposition = "inactive"
	Invalid      Disposition = "invalid"
	Uncertain    Disposition = "uncertain"
)

// Token retains the first original token anchor, including repeated names.
type Token struct {
	Name string
	Span parser.Span
}
type Effect struct {
	Rule         string
	Token        parser.Span
	Disposition  Disposition
	Restrictions []config.Origin
}
type Candidate struct {
	Raw, Payload  string
	Span, Closing parser.Span
	Operation     Operation
	Named         []Token
	IDs           []string
	Disposition   Disposition
	Effects       []Effect
}

// Note is a processing explanation, never a configurable rule violation.
type Note struct {
	Code, Message, Rule string
	Span                parser.Span
	Restrictions        []config.Origin
}

// Interval includes its start and excludes its end, except that the final
// interval also owns the EOF anchor. Owner is a candidate index, or -1 for
// configured state. Repeated instructions retain separate ownership.
type Interval struct {
	Start, End int
	Enabled    bool
	Owner      int
	Token      parser.Span
}
type State struct {
	Enabled bool
	Owner   int
	Token   parser.Span
	Options engine.Options
}

// Result owns its records and never exposes writable aliases. Incomplete results
// withhold intervals entirely: unprocessed comments cannot imply stable state.
type Result struct {
	source      parser.Source
	complete    bool
	candidates  []Candidate
	diagnostics []result.Diagnostic
	notes       []Note
	intervals   map[string][]Interval
	rules       map[string]engine.ResolvedRule
	err         error
}

func (r Result) Complete() bool        { return r.complete }
func (r Result) Err() error            { return r.err }
func (r Result) Source() parser.Source { return r.source }
func (r Result) Candidates() []Candidate {
	out := slices.Clone(r.candidates)
	for i := range out {
		out[i].Named = slices.Clone(out[i].Named)
		out[i].IDs = slices.Clone(out[i].IDs)
		out[i].Effects = slices.Clone(out[i].Effects)
		for j := range out[i].Effects {
			out[i].Effects[j].Restrictions = cloneOrigins(out[i].Effects[j].Restrictions)
		}
	}
	return out
}
func (r Result) Diagnostics() []result.Diagnostic { return slices.Clone(r.diagnostics) }
func (r Result) Notes() []Note {
	out := slices.Clone(r.notes)
	for i := range out {
		out[i].Restrictions = cloneOrigins(out[i].Restrictions)
	}
	return out
}
func (r Result) Intervals(id string) []Interval { return slices.Clone(r.intervals[id]) }
func (r Result) StateAt(id string, offset int) (State, error) {
	if !r.complete || offset < 0 || offset > r.source.Len() {
		return State{}, errors.New("unresolved directive state or source offset")
	}
	intervals, known := r.intervals[id]
	if !known {
		return State{}, errors.New("unknown directive rule")
	}
	i, _ := slices.BinarySearchFunc(intervals, offset, func(v Interval, at int) int {
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
	return State{v.Enabled, v.Owner, v.Token, cloneOptions(r.rules[id].Options)}, nil
}
func cloneOptions(in engine.Options) engine.Options {
	out := make(engine.Options, len(in))
	for k, v := range in {
		if s, ok := v.([]string); ok {
			v = slices.Clone(s)
		}
		out[k] = v
	}
	return out
}
func cloneOrigins(in []config.Origin) []config.Origin {
	out := slices.Clone(in)
	for i := range out {
		if out[i].Position != nil {
			p := *out[i].Position
			out[i].Position = &p
		}
	}
	return out
}

// Process requires the same operation budget used for parsing. No budget is
// created/reset here. Configuration authority comes from private validated state,
// independently of mutable inspection views and executable rule availability.
func Process(ctx context.Context, parsed parser.ParseResult, configuration *config.Configuration, budget *parser.OperationBudget) Result {
	r := Result{source: parsed.Source, intervals: map[string][]Interval{}}
	stop := func(err error, code string) Result {
		r.complete = false
		r.err = err
		r.intervals = nil
		r.notes = append(r.notes, Note{Code: code, Message: err.Error()})
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
	global, rules, err := configuration.DirectiveConfiguration()
	if err != nil {
		return stop(err, "directive-policy-unresolved")
	}
	r.rules = rules
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
		r.intervals[id] = []Interval{{Start: 0, End: r.source.Len(), Enabled: rule.Enabled, Owner: -1}}
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
		c := Candidate{Raw: raw, Payload: body, Span: fact.Envelope, Closing: fact.Close, Disposition: Inactive}
		if !global {
			units, err := configuration.DirectivePermissionWork("")
			if err != nil {
				return stop(err, "directive-policy-unresolved")
			}
			if err := check(2 * units); err != nil {
				return stop(err, "directive-incomplete")
			}
			r.candidates = append(r.candidates, c)
			r.notes = append(r.notes, Note{Code: "directives-disabled", Message: "directive candidate is inactive because global directives are disabled", Span: c.Span, Restrictions: configuration.GlobalDirectivePermission().Restrictions})
			continue
		}
		if fact.Protected || !fact.Close.Present() {
			c.Disposition = Uncertain
			r.candidates = append(r.candidates, c)
			code, message := "parsing", "unclosed directive comment has no transition"
			if fact.Protected {
				code, message = "directive-literal", "directive candidate is in a literal HTML interior"
			}
			r.notes = append(r.notes, Note{Code: code, Message: message, Span: c.Span})
			continue
		}
		tokens := tokenize(body, fact.Payload, r.source)
		problem := ""
		switch tokens[0].Name {
		case "mdchk:enable":
			c.Operation = Enable
		case "mdchk:disable":
			c.Operation = Disable
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
			c.Disposition = Invalid
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
		c.Disposition = Applied
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
			effect := Effect{Rule: id, Token: token, Disposition: Applied, Restrictions: permission.Restrictions}
			switch {
			case permission.Err != nil:
				return stop(permission.Err, "directive-policy-unresolved")
			case !permission.Allowed:
				effect.Disposition = Blocked
				r.notes = append(r.notes, Note{Code: "directive-blocked", Message: "directive effect blocked by configuration", Rule: id, Span: token, Restrictions: cloneOrigins(permission.Restrictions)})
			case id == "MD074" || id == "MD094":
				effect.Disposition = Inapplicable
				r.notes = append(r.notes, Note{Code: "directive-inapplicable", Message: "document directives do not control this rule", Rule: id, Span: token})
			default:
				intervals := r.intervals[id]
				at := fact.Close.End()
				intervals[len(intervals)-1].End = at
				r.intervals[id] = append(intervals, Interval{Start: at, End: r.source.Len(), Enabled: c.Operation == Enable, Owner: len(r.candidates), Token: token})
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

// Candidate classification is case insensitive only for the mdchk introducer;
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
func tokenize(body string, parts []parser.Segment, source parser.Source) []Token {
	var tokens []Token
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
		tokens = append(tokens, Token{body[start:i], span})
	}
	return tokens
}
