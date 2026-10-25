package integration

import (
	"context"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type HeaderClass string

const (
	OrdinaryFence      HeaderClass = "ordinary"
	EscapedExample     HeaderClass = "escaped-example"
	SupportedCandidate HeaderClass = "supported-candidate"
	UnsupportedCell    HeaderClass = "unsupported-cell"
)

type LabelKind string

const (
	LabelAbsent    LabelKind = "absent"
	LabelString    LabelKind = "string"
	LabelNonString LabelKind = "non-string"
	LabelAmbiguous LabelKind = "ambiguous"
	LabelUnknown   LabelKind = "unknown"
)

// Label is a decoded policy input with original provenance. Constructing one
// does not establish that an option was present in a parser-owned cell.
type Label struct {
	kind     LabelKind
	text     string
	location Location
}

func NewLabel(kind LabelKind, text string, location Location) (Label, error) {
	if len(text) > 1<<20 || !utf8.ValidString(text) || kind != LabelString && text != "" {
		return Label{}, ErrContract
	}
	if kind == LabelAbsent {
		if location.valid() {
			return Label{}, ErrContract
		}
	} else if kind != LabelString && kind != LabelNonString && kind != LabelAmbiguous && kind != LabelUnknown || !location.valid() {
		return Label{}, ErrContract
	}
	return Label{kind, text, location}, nil
}
func (l Label) Kind() LabelKind    { return l.kind }
func (l Label) Text() string       { return l.text }
func (l Label) Location() Location { return l.location }

// Missing is meaningful only for reliably absent or string labels.
func (l Label) Missing() (bool, bool) {
	if l.kind == LabelAbsent {
		return true, true
	}
	if l.kind != LabelString {
		return false, false
	}
	return strings.TrimFunc(l.text, unicode.IsSpace) == "", true
}

type HeaderDecision struct {
	document Document
	class    HeaderClass
	language string
	label    Label
	location Location
	declared []Label
}

func (h HeaderDecision) Class() HeaderClass { return h.class }
func (h HeaderDecision) Language() string   { return h.language }
func (h HeaderDecision) Label() Label       { return h.label }
func (h HeaderDecision) Location() Location { return h.location }

// DeclaredLabels retains reliably decoded declarations even when another
// declaration/header component is ambiguous. It grants no cell authority.
func (h HeaderDecision) DeclaredLabels(ctx context.Context) ([]Label, error) {
	if h.document.scope == nil {
		return nil, ErrContract
	}
	if err := h.document.scope.checkpoint(ctx, int64(len(h.declared))+1, int64(len(h.declared))); err != nil {
		return nil, err
	}
	return append([]Label(nil), h.declared...), nil
}

// QuartoHeader evaluates a source-owned *candidate info span*. It does not find
// fences, establish protection/container ownership, or produce an executable
// Cell. The concrete parser-backed producer must do that in addition to applying
// this policy. enclosingExample is that producer's already-established fact.
func (s *Scope) QuartoHeader(ctx context.Context, document Document, location Location, enclosingExample bool) (HeaderDecision, error) {
	if !s.accepts(document) || !document.available || !location.valid() || location.source.Token() != document.source.Token() {
		return HeaderDecision{}, ErrContract
	}
	raw, _ := location.source.Text(location.span)
	if err := s.checkpoint(ctx, int64(len(raw))*8+1, int64(len(raw))+2); err != nil {
		return HeaderDecision{}, err
	}
	out := HeaderDecision{document: document, class: OrdinaryFence, label: Label{kind: LabelAbsent}, location: location}
	if !s.quarto {
		return out, nil
	}
	raw = strings.TrimSpace(raw)
	if enclosingExample {
		out.class = EscapedExample
		return out, nil
	}
	if !strings.HasPrefix(raw, "{") {
		return out, nil
	}
	out.class = UnsupportedCell
	out.label = Label{kind: LabelUnknown, location: location}
	if strings.HasPrefix(raw, "{{") {
		if strings.HasSuffix(raw, "}}") && !strings.ContainsAny(raw[2:len(raw)-2], "{}") {
			out.class = EscapedExample
			out.label = Label{kind: LabelAbsent}
		}
		return out, nil
	}
	if !strings.HasSuffix(raw, "}") {
		return out, nil
	}
	body := raw[1 : len(raw)-1]
	end := strings.IndexFunc(body, func(r rune) bool { return unicode.IsSpace(r) || r == ',' })
	if end < 0 {
		end = len(body)
	}
	language := body[:end]
	if language != "r" && language != "python" && language != "julia" {
		return out, nil
	}
	out.language = language
	rest := strings.TrimSpace(body[end:])
	if language != "r" {
		if rest == "" {
			out.class = SupportedCandidate
			out.label = Label{kind: LabelAbsent}
		}
		return out, nil
	}
	labels, ok := rHeaderLabel(rest)
	for _, text := range labels {
		out.declared = append(out.declared, Label{kind: LabelString, text: text, location: location})
	}
	if len(labels) > 1 {
		out.label = Label{kind: LabelAmbiguous, location: location}
	}
	if !ok {
		return out, nil
	}
	out.class = SupportedCandidate
	out.label = Label{kind: LabelAbsent}
	if len(labels) == 1 {
		// The complete info span is truthful provenance for a decoded header
		// declaration; fine-grained mapped extraction belongs to the producer.
		out.label = out.declared[0]
	}
	return out, nil
}

func rHeaderLabel(rest string) ([]string, bool) {
	var labels []string
	rest = strings.TrimSpace(rest)
	if strings.HasPrefix(rest, ",") {
		rest = strings.TrimSpace(rest[1:])
		if rest == "" {
			return labels, true
		}
	}
	first := true
	for rest != "" {
		if rest[0] == ',' {
			return labels, false
		}
		end := strings.IndexFunc(rest, func(r rune) bool { return unicode.IsSpace(r) || r == '=' || r == ',' })
		if end < 0 {
			end = len(rest)
		}
		name := rest[:end]
		if !literalName(name) {
			return labels, false
		}
		tail := strings.TrimLeftFunc(rest[end:], unicode.IsSpace)
		if strings.HasPrefix(tail, "=") {
			tail = strings.TrimLeftFunc(tail[1:], unicode.IsSpace)
			end = scalarEnd(tail)
			if end == 0 {
				return labels, false
			}
			value, quoted, ok := staticScalar(tail[:end])
			if !ok {
				return labels, false
			}
			if end < len(tail) {
				r, _ := utf8.DecodeRuneInString(tail[end:])
				if r != ',' && !unicode.IsSpace(r) {
					return labels, false
				}
			}
			if name == "label" {
				if !quoted {
					return labels, false
				}
				labels = append(labels, value)
			}
			rest = tail[end:]
		} else {
			if !first {
				return labels, false
			}
			labels = append(labels, name)
			rest = rest[end:]
		}
		first = false
		rest = strings.TrimLeftFunc(rest, unicode.IsSpace)
		if strings.HasPrefix(rest, ",") {
			rest = strings.TrimSpace(rest[1:])
			if rest == "" {
				return labels, false
			}
		}
	}
	return labels, len(labels) <= 1
}
func scalarEnd(text string) int {
	if text == "" {
		return 0
	}
	quote := byte(0)
	if text[0] == '\'' || text[0] == '"' {
		quote = text[0]
	}
	if quote == 0 {
		end := strings.IndexFunc(text, func(r rune) bool { return unicode.IsSpace(r) || r == ',' })
		if end < 0 {
			return len(text)
		}
		return end
	}
	escaped := false
	for i := 1; i < len(text); i++ {
		if escaped {
			escaped = false
			continue
		}
		if text[i] == '\\' {
			escaped = true
			continue
		}
		if text[i] == quote {
			return i + 1
		}
	}
	return 0
}

func literalName(text string) bool {
	if text == "" {
		return false
	}
	for _, r := range text {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' && r != '.' {
			return false
		}
	}
	return true
}
func staticScalar(text string) (string, bool, bool) {
	if len(text) >= 2 && (text[0] == '\'' || text[0] == '"') && text[len(text)-1] == text[0] {
		var out strings.Builder
		for i := 1; i < len(text)-1; i++ {
			b := text[i]
			if b == '\\' {
				i++
				if i >= len(text)-1 {
					return "", false, false
				}
				switch text[i] {
				case '\\', '\'', '"':
					b = text[i]
				case 'n':
					b = '\n'
				case 'r':
					b = '\r'
				case 't':
					b = '\t'
				default:
					return "", false, false
				}
			} else if b == text[0] || b == '\n' || b == '\r' {
				return "", false, false
			}
			out.WriteByte(b)
		}
		return out.String(), true, true
	}
	if text == "TRUE" || text == "FALSE" || text == "true" || text == "false" || text == "NULL" || text == "null" {
		return text, false, true
	}
	if _, err := strconv.ParseFloat(text, 64); err == nil {
		return text, false, true
	}
	return "", false, false
}

type LabelDecision struct {
	status                  Status
	header, body, effective Label
}

func (d LabelDecision) Status() Status   { return d.status }
func (d LabelDecision) Header() Label    { return d.header }
func (d LabelDecision) Body() Label      { return d.body }
func (d LabelDecision) Effective() Label { return d.effective }

// DeclarationsComplete describes declaration interpretation independently of
// effective-label Status. Declaration completeness is
// separate so a valid body override need not erase an uncertain header check.
func (d LabelDecision) DeclarationsComplete() bool {
	return (d.header.kind == LabelAbsent || d.header.kind == LabelString) && (d.body.kind == LabelAbsent || d.body.kind == LabelString)
}

// ResolveLabels applies precedence to decoded policy inputs. It grants no cell
// or collection completeness. A valid body string, including blank, wins; an
// uncertain body never silently falls back to the header.
func (s *Scope) ResolveLabels(ctx context.Context, document Document, header, body Label) (LabelDecision, error) {
	if !s.accepts(document) || !document.available {
		return LabelDecision{}, ErrContract
	}
	for _, label := range []Label{header, body} {
		if label.kind == "" || label.kind != LabelAbsent && (!label.location.valid() || label.location.source.Token() != document.source.Token()) {
			return LabelDecision{}, ErrContract
		}
	}
	if err := s.checkpoint(ctx, int64(len(header.text)+len(body.text))+1, 1); err != nil {
		return LabelDecision{}, err
	}
	out := LabelDecision{status: Available, header: header, body: body, effective: header}
	if !s.quarto {
		out.status = Inactive
		out.effective = Label{}
		return out, nil
	}
	if body.kind != LabelAbsent {
		out.effective = body
	}
	if out.effective.kind != LabelAbsent && out.effective.kind != LabelString {
		out.status = Unavailable
	}
	return out, nil
}

// Cell is a future parser-producer-owned source fact. There is intentionally no
// public constructor accepting raw CodeBlock nodes, header decisions or enums.
type Cell struct {
	document Document
	envelope Location
	language string
	labels   LabelDecision
	declared []Label
}

func (c Cell) Envelope() Location    { return c.envelope }
func (c Cell) Language() string      { return c.language }
func (c Cell) Labels() LabelDecision { return c.labels }
func (c Cell) DeclaredLabels(ctx context.Context) ([]Label, error) {
	if c.document.scope == nil {
		return nil, ErrContract
	}
	if err := c.document.scope.checkpoint(ctx, int64(len(c.declared))+1, int64(len(c.declared))); err != nil {
		return nil, err
	}
	return append([]Label(nil), c.declared...), nil
}

type QuartoContext struct {
	document Document
	status   Status
	cells    []Cell
}

func (c QuartoContext) Status() Status     { return c.status }
func (c QuartoContext) Document() Document { return c.document }
func (c QuartoContext) Cells(ctx context.Context) ([]Cell, error) {
	if c.document.scope == nil || c.status == "" {
		return nil, ErrContract
	}
	if err := c.document.scope.checkpoint(ctx, int64(len(c.cells))+1, int64(len(c.cells))); err != nil {
		return nil, err
	}
	return append([]Cell(nil), c.cells...), nil
}
func (s *Scope) Quarto(ctx context.Context, document Document) (QuartoContext, error) {
	if !s.accepts(document) {
		return QuartoContext{}, ErrContract
	}
	out := QuartoContext{document: document, status: Unavailable}
	if err := s.checkpoint(ctx, 1, 1); err != nil {
		return out, err
	}
	if !s.quarto {
		out.status = Inactive
	}
	// Actual parser-backed collection production is #175. Policy helpers above
	// cannot manufacture its proof or advertise that capability today.
	return out, nil
}
