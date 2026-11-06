package rules

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/confusables"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/idna"
	"github.com/frithuz/mdchk/internal/parser"
)

// md095Authority is the result of splitting one semantic destination. Offsets
// are bytes in that destination. Problem names unreliable input; otherwise a
// host with end > start is checked and anything else is outside MD095.
type md095Authority struct {
	authorityStart, authorityEnd int
	hostStart, hostEnd           int
	problem                      string
}

const (
	md095InvalidAuthority = "invalid URL authority"
	md095InvalidPercent   = "invalid percent-encoded UTF-8"
	md095Unmapped         = "unmapped source"
)

// md095SplitAuthority finds the host of a hierarchical absolute or
// protocol-relative destination. User information is everything up to the
// last "@"; the port follows the last ":". Bracketed and IPv4 literals, empty
// hosts and every other destination have no host to check.
//
// Special schemes (http, https, ws, wss, ftp) and protocol-relative
// destinations follow WHATWG URL parsing, as browsers do: any run of "/" and
// "\" after the scheme, or a leading run of two or more, is skipped, and "\"
// also ends the authority. Otherwise "https:///host" would hide its host.
func md095SplitAuthority(destination string) (out md095Authority) {
	slashes := func(i int) int {
		for i < len(destination) && (destination[i] == '/' || destination[i] == '\\') {
			i++
		}
		return i
	}
	start, special := 0, true
	if n := slashes(0); n >= 2 {
		start = n
	} else {
		i := 0
		for i < len(destination) && (asciiAlpha(destination[i]) || i > 0 && (destination[i] >= '0' && destination[i] <= '9' || destination[i] == '+' || destination[i] == '-' || destination[i] == '.')) {
			i++
		}
		if i == 0 || i >= len(destination) || destination[i] != ':' {
			return out
		}
		switch strings.ToLower(destination[:i]) {
		case "http", "https", "ws", "wss", "ftp":
			start = slashes(i + 1)
		default:
			if !strings.HasPrefix(destination[i:], "://") {
				return out
			}
			start, special = i+3, false
		}
	}
	end := start
	for end < len(destination) && destination[end] != '/' && destination[end] != '?' && destination[end] != '#' && (!special || destination[end] != '\\') {
		end++
	}
	out.authorityStart, out.authorityEnd = start, end
	host := start
	if at := strings.LastIndexByte(destination[start:end], '@'); at >= 0 {
		host = start + at + 1
	}
	if host < end && destination[host] == '[' {
		if strings.IndexByte(destination[host:end], ']') < 0 {
			out.problem = md095InvalidAuthority
		}
		return out
	}
	hostEnd := end
	if colon := strings.LastIndexByte(destination[host:end], ':'); colon >= 0 {
		hostEnd = host + colon
		for _, c := range []byte(destination[hostEnd+1 : end]) {
			if c < '0' || c > '9' {
				out.problem = md095InvalidAuthority
				return out
			}
		}
	}
	if hostEnd == host || md095IPv4(destination[host:hostEnd]) {
		return out
	}
	out.hostStart, out.hostEnd = host, hostEnd
	return out
}

func asciiAlpha(c byte) bool { return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' }

// md095IPv4 reports whether the final label, after one root dot, is decimal
// digits or 0x/0X followed by hex digits.
func md095IPv4(host string) bool {
	host = strings.TrimSuffix(host, ".")
	label := host[strings.LastIndexByte(host, '.')+1:]
	digits := "0123456789"
	if len(label) > 2 && label[0] == '0' && (label[1] == 'x' || label[1] == 'X') {
		label, digits = label[2:], "0123456789abcdefABCDEF"
	}
	if label == "" {
		return false
	}
	for _, c := range []byte(label) {
		if strings.IndexByte(digits, c) < 0 {
			return false
		}
	}
	return true
}

// md095Decode percent-decodes a host once. Each escape must be two hex digits
// and the result must be valid UTF-8.
func md095Decode(host string) (string, bool) {
	if strings.IndexByte(host, '%') < 0 {
		return host, utf8.ValidString(host)
	}
	var b strings.Builder
	b.Grow(len(host))
	for i := 0; i < len(host); i++ {
		if host[i] != '%' {
			b.WriteByte(host[i])
			continue
		}
		if i+2 >= len(host) || !md095Hex(host[i+1]) || !md095Hex(host[i+2]) {
			return "", false
		}
		b.WriteByte(md095HexValue(host[i+1])<<4 | md095HexValue(host[i+2]))
		i += 2
	}
	decoded := b.String()
	return decoded, utf8.ValidString(decoded)
}

func md095Hex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func md095HexValue(c byte) byte {
	switch {
	case c >= 'a':
		return c - 'a' + 10
	case c >= 'A':
		return c - 'A' + 10
	}
	return c - '0'
}

// md095FastPath reports a host that cannot hold a non-ASCII code point after
// IDNA processing: ASCII only, with no A-label.
func md095FastPath(host string) bool {
	for i := 0; i < len(host); i++ {
		if host[i] >= utf8.RuneSelf {
			return false
		}
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) >= 4 && strings.EqualFold(label[:4], "xn--") {
			return false
		}
	}
	return true
}

// md095Flagged lists each distinct non-ASCII code point of a canonical
// Unicode host that has an ASCII-letter confusable, in order of first
// appearance, with the byte range from the first to the last flagged one.
func md095Flagged(unicode string) (items []string, first, last int) {
	seen := map[rune]bool{}
	first, last = -1, -1
	for i, r := range unicode {
		if r < utf8.RuneSelf {
			continue
		}
		mapping, ok := confusables.ASCIILetters(r)
		if !ok {
			continue
		}
		if first < 0 {
			first = i
		}
		last = i + utf8.RuneLen(r)
		if !seen[r] {
			seen[r] = true
			items = append(items, fmt.Sprintf("U+%04X looks like \"%s\"", r, mapping))
		}
	}
	return items, first, last
}

func md095Message(source string, items []string) string {
	return fmt.Sprintf("Hostname \"%s\" contains lookalike characters: %s.", source, strings.Join(items, ", "))
}

// md095SourceRange maps the semantic bytes [start, end) of a destination to
// original source. Exact reports that every covering segment is Exact; ok is
// false when a covering segment has no source.
func md095SourceRange(segments []parser.Segment, start, end int) (from, to int, exact, ok bool) {
	from, to, exact, offset := -1, -1, true, 0
	for _, s := range segments {
		n := len(s.Semantic.Value)
		a, b := offset, offset+n
		offset = b
		if n == 0 || b <= start || a >= end {
			continue
		}
		if !s.Source.Present() || s.Transformation == parser.Synthetic || s.Transformation == parser.MappingUnavailable {
			return 0, 0, false, false
		}
		x, y := s.Source.Start(), s.Source.End()
		if s.Transformation == parser.Exact {
			x, y = x+max(start, a)-a, x+min(end, b)-a
		} else {
			exact = false
		}
		if from < 0 {
			from = x
		}
		to = y
	}
	if from < 0 {
		return 0, 0, false, false
	}
	return from, to, exact, true
}

// md095Outcome classifies one destination. A problem carries its reason; a
// finding carries the flagged items and the whole original host range besides
// its own. A zero outcome reports nothing.
type md095Outcome struct {
	from, to         int
	hostFrom, hostTo int
	items            []string
	problem          string
}

func (o md095Outcome) reported() bool { return o.problem != "" || len(o.items) != 0 }

// md095Check classifies one destination against canonical allowed A-labels.
// The fallback locates problems whose own range has no source.
func md095Check(destination string, segments []parser.Segment, fallback parser.Span, allowed map[string]bool) (md095Outcome, error) {
	a := md095SplitAuthority(destination)
	problem := func(reason string, start, end int) (md095Outcome, error) {
		from, to, _, ok := md095SourceRange(segments, start, end)
		if !ok || end <= start {
			from, to = fallback.Start(), fallback.End()
		}
		return md095Outcome{from: from, to: to, problem: reason}, nil
	}
	if a.problem != "" {
		return problem(a.problem, a.authorityStart, a.authorityEnd)
	}
	if a.hostEnd <= a.hostStart {
		return md095Outcome{}, nil
	}
	raw := destination[a.hostStart:a.hostEnd]
	decoded, valid := md095Decode(raw)
	if !valid {
		return problem(md095InvalidPercent, a.hostStart, a.hostEnd)
	}
	if md095FastPath(decoded) {
		return md095Outcome{}, nil
	}
	from, to, exact, mapped := md095SourceRange(segments, a.hostStart, a.hostEnd)
	if !mapped {
		return problem(md095Unmapped, a.hostStart, a.hostEnd)
	}
	host, err := idna.Canonicalize(decoded)
	if err != nil {
		var e *idna.Error
		if !errors.As(err, &e) {
			return md095Outcome{}, err
		}
		return problem(e.Error(), a.hostStart, a.hostEnd)
	}
	if allowed[host.ASCII] {
		return md095Outcome{}, nil
	}
	items, first, last := md095Flagged(host.Unicode)
	if len(items) == 0 {
		return md095Outcome{}, nil
	}
	out := md095Outcome{from: from, to: to, hostFrom: from, hostTo: to, items: items}
	if exact && decoded == raw && strings.TrimSuffix(decoded, ".") == host.Unicode {
		out.from, out.to = from+first, from+last
	}
	return out, nil
}

func lookalikeHostnames(ctx context.Context, c engine.RuleContext) (out engine.RuleOutcome) {
	defer func() {
		out.Err = errors.Join(out.Err, c.SectionCheckpoint(ctx, 1, 0))
		if out.Err != nil {
			out = engine.RuleOutcome{Err: out.Err}
		}
	}()
	items, err := c.LinkDestinations(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	switch items.Availability {
	case parser.Available:
	case parser.Unavailable:
		return engine.RuleOutcome{IncompleteReason: "Link destination context is unavailable; the complete MD095 inventory is withheld."}
	default:
		return engine.RuleOutcome{Err: parser.ErrModel}
	}
	entries, err := c.AllowedHosts(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	allowed := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if out.Err = c.SectionCheckpoint(ctx, 16+int64(len(entry)), 1); out.Err != nil {
			return out
		}
		host, err := idna.Canonicalize(entry)
		if err != nil {
			// Configuration validation admits only valid entries.
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		allowed[host.ASCII] = true
	}
	previous := 0
	for _, l := range items.Destinations {
		// Hostname processing is proportional to the destination, which bounds
		// its host.
		if out.Err = c.SectionCheckpoint(ctx, 18+16*int64(len(l.Destination))+int64(len(l.Segments)), 0); out.Err != nil {
			return out
		}
		s := l.Span
		if !s.Present() || s.SourceToken() != c.Source().Token() || s.Start() < previous || s.End() < s.Start() || s.End() > c.Source().Len() {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		previous = s.Start()
		result, err := md095Check(l.Destination, l.Segments, s, allowed)
		if err != nil {
			return engine.RuleOutcome{Err: err}
		}
		if !result.reported() {
			continue
		}
		if out.Err = c.SectionCheckpoint(ctx, 2, 1); out.Err != nil {
			return out
		}
		span, err := c.Source().Span(result.from, result.to)
		if err != nil {
			return engine.RuleOutcome{Err: parser.ErrModel}
		}
		if result.problem != "" {
			finding := engine.Finding{Span: span, Message: "Hostname cannot be checked for lookalike characters: " + result.problem + "."}
			out.ParsingProblems, out.Err = imageAlternativeAppend(ctx, c, out.ParsingProblems, finding)
		} else {
			host, err := c.Source().Span(result.hostFrom, result.hostTo)
			if err != nil {
				return engine.RuleOutcome{Err: parser.ErrModel}
			}
			text, err := c.Source().Text(host)
			if err != nil {
				return engine.RuleOutcome{Err: parser.ErrModel}
			}
			out.Findings, out.Err = imageAlternativeAppend(ctx, c, out.Findings, engine.Finding{Span: span, Message: md095Message(text, result.items)})
		}
		if out.Err != nil {
			return out
		}
	}
	return out
}
