// Package headingcase selects MD063's required heading initials and produces and
// checks their safe capitalization corrections.
package headingcase

import (
	"context"
	"fmt"
	"unicode"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// Checkpoint charges derived work and records to the caller's operation.
type Checkpoint func(work, nodes int64) error

// Word reports whether r continues a title-style word.
func Word(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsMark(r) || unicode.IsNumber(r) || unicode.Is(unicode.Pc, r)
}

// inWord applies Word with the apostrophe-join rule: an apostrophe between two
// word scalars belongs to their word.
func inWord(scalars []parser.HeadingScalar, i int) bool {
	s := scalars[i].Scalar
	if (s == '\'' || s == '’') && i > 0 && i+1 < len(scalars) {
		return Word(scalars[i-1].Scalar) && Word(scalars[i+1].Scalar)
	}
	return Word(s)
}

// Names marks the scalars of exact proper-name matches. Name matches settle
// before initials are examined. Empty names are no-ops; every repeated scalar
// comparison and protection write uses the original work.
func Names(names []string, scalars []parser.HeadingScalar, checkpoint Checkpoint) ([]bool, error) {
	if len(names) == 0 {
		return nil, nil
	}
	if err := checkpoint(int64(len(scalars))+1, int64(len(scalars))); err != nil {
		return nil, err
	}
	protected := make([]bool, len(scalars))
	for _, name := range names {
		if err := checkpoint(1, 0); err != nil {
			return nil, err
		}
		if name == "" {
			continue
		}
		for start := range scalars {
			if err := checkpoint(8, 0); err != nil {
				return nil, err
			}
			if start > 0 && Word(scalars[start-1].Scalar) {
				continue
			}
			end, offset := start, 0
			for offset < len(name) && end < len(scalars) {
				if err := checkpoint(9, 0); err != nil {
					return nil, err
				}
				r, width := utf8.DecodeRuneInString(name[offset:])
				if r == utf8.RuneError && width == 1 || r != scalars[end].Scalar {
					break
				}
				offset += width
				end++
			}
			if offset != len(name) || (end < len(scalars) && Word(scalars[end].Scalar)) {
				continue
			}
			for i := start; i < end; i++ {
				if err := checkpoint(1, 0); err != nil {
					return nil, err
				}
				protected[i] = true
			}
		}
	}
	return protected, nil
}

// Targets calls yield with the index of each required lowercase initial in
// order: the first cased initial for sentence style and every word's for title
// style, when it is lowercase, not code and not name-protected. It stops when
// yield returns false.
func Targets(style string, scalars []parser.HeadingScalar, protected []bool, checkpoint Checkpoint, yield func(int) bool) error {
	seen := false
	for i, s := range scalars {
		if err := checkpoint(16, 0); err != nil {
			return err
		}
		if style == "title" && !inWord(scalars, i) {
			seen = false
			continue
		}
		if !unicode.IsUpper(s.Scalar) && !unicode.IsLower(s.Scalar) && !unicode.IsTitle(s.Scalar) {
			continue
		}
		if seen {
			continue
		}
		seen = true
		if !unicode.IsLower(s.Scalar) || s.Code || (len(protected) > 0 && protected[i]) {
			continue
		}
		if !s.Token.Present() {
			return parser.ErrModel
		}
		if !yield(i) {
			return nil
		}
	}
	return nil
}

// Replacement is the titlecase form of a required initial, when it is one
// different cased scalar.
func Replacement(r rune) (rune, bool) {
	up := unicode.ToTitle(r)
	return up, up != r && (unicode.IsUpper(up) || unicode.IsTitle(up))
}

// Heading is one heading's complete selection: every required initial, and
// whether all of them can be corrected together.
type Heading struct {
	Scalars []parser.HeadingScalar
	Targets []int
	Ready   bool
}

// Analyze selects every required initial of one heading and decides whether
// the heading is correctable as a whole: each initial is a literal scalar with a
// titlecase form, and none is near a proper name.
func Analyze(source parser.Source, style string, names []string, scalars []parser.HeadingScalar, checkpoint Checkpoint) (Heading, error) {
	out := Heading{Scalars: scalars}
	protected, err := Names(names, scalars, checkpoint)
	if err != nil {
		return Heading{}, err
	}
	if err := Targets(style, scalars, protected, checkpoint, func(i int) bool {
		out.Targets = append(out.Targets, i)
		return true
	}); err != nil {
		return Heading{}, err
	}
	if len(out.Targets) == 0 {
		return out, nil
	}
	if err := checkpoint(int64(len(out.Targets))+1, int64(len(out.Targets))); err != nil {
		return Heading{}, err
	}
	for _, i := range out.Targets {
		s := scalars[i]
		if _, ok := Replacement(s.Scalar); !ok {
			return out, nil
		}
		text, err := source.Text(s.Token)
		if err != nil {
			return Heading{}, err
		}
		if text != string(s.Scalar) {
			return out, nil
		}
	}
	if len(names) > 0 {
		folded, err := foldedNames(names, scalars, checkpoint)
		if err != nil {
			return Heading{}, err
		}
		for _, i := range out.Targets {
			if err := checkpoint(int64(len(scalars)), 0); err != nil {
				return Heading{}, err
			}
			lo, hi := i, i+1
			for lo > 0 && inWord(scalars, lo-1) {
				lo--
			}
			for hi < len(scalars) && inWord(scalars, hi) {
				hi++
			}
			for j := lo; j < hi; j++ {
				if protected[j] || folded[j] {
					return out, nil
				}
			}
			before := lo - 1
			for before >= 0 && !inWord(scalars, before) {
				before--
			}
			after := hi
			for after < len(scalars) && !inWord(scalars, after) {
				after++
			}
			if before >= 0 && protected[before] || after < len(scalars) && protected[after] {
				return out, nil
			}
		}
	}
	out.Ready = true
	return out, nil
}

// foldedNames marks every scalar inside a span matching a configured name under
// simple case folding, scalar by scalar, with no word-boundary condition.
func foldedNames(names []string, scalars []parser.HeadingScalar, checkpoint Checkpoint) ([]bool, error) {
	if err := checkpoint(int64(len(scalars))+1, int64(len(scalars))); err != nil {
		return nil, err
	}
	folded := make([]bool, len(scalars))
	for _, name := range names {
		if name == "" {
			continue
		}
		for start := range scalars {
			if err := checkpoint(8, 0); err != nil {
				return nil, err
			}
			end, offset := start, 0
			for offset < len(name) && end < len(scalars) {
				if err := checkpoint(9, 0); err != nil {
					return nil, err
				}
				r, width := utf8.DecodeRuneInString(name[offset:])
				if r == utf8.RuneError && width == 1 || !foldEqual(r, scalars[end].Scalar) {
					break
				}
				offset += width
				end++
			}
			if offset != len(name) {
				continue
			}
			for i := start; i < end; i++ {
				folded[i] = true
			}
		}
	}
	return folded, nil
}

func foldEqual(a, b rune) bool {
	if a == b {
		return true
	}
	for r := unicode.SimpleFold(a); r != a; r = unicode.SimpleFold(r) {
		if r == b {
			return true
		}
	}
	return false
}

// edits is the heading's one edit per required initial.
func (h Heading) edits(source parser.Source) ([]fix.Edit, error) {
	out := make([]fix.Edit, 0, len(h.Targets))
	for _, i := range h.Targets {
		s := h.Scalars[i]
		text, err := source.Text(s.Token)
		if err != nil {
			return nil, err
		}
		up, _ := Replacement(s.Scalar)
		out = append(out, fix.Edit{Span: s.Token, Expected: []byte(text), Replacement: []byte(string(up))})
	}
	return out, nil
}

// headings analyzes every heading of one round, indexed by the token start of
// its first required initial: the span of its MD063 finding.
func headings(ctx context.Context, c fix.RoundContext) (map[int]Heading, error) {
	style, names, err := c.HeadingCapitalization()
	if err != nil {
		return nil, err
	}
	all, err := c.HeadingScalars(ctx)
	if err != nil {
		return nil, err
	}
	checkpoint := func(work, nodes int64) error { return c.Reserve(ctx, work, nodes) }
	out := make(map[int]Heading, len(all))
	for _, scalars := range all {
		h, err := Analyze(c.Current(), style, names, scalars, checkpoint)
		if err != nil {
			return nil, err
		}
		if len(h.Targets) > 0 {
			out[scalars[h.Targets[0]].Token.Start()] = h
		}
	}
	return out, nil
}

// Groups regenerates one group for each eligible active finding from this
// round's heading scalars and options, never from saved coordinates.
func Groups(ctx context.Context, c fix.RoundContext, findings []result.Diagnostic) ([]fix.Group, error) {
	if len(findings) == 0 {
		return nil, nil
	}
	index, err := headings(ctx, c)
	if err != nil {
		return nil, err
	}
	groups := make([]fix.Group, 0, len(findings))
	for _, d := range findings {
		if err := c.Reserve(ctx, 128, 12); err != nil {
			return nil, err
		}
		h, ok := index[d.ByteRange.Start]
		if !ok || !h.Ready {
			continue
		}
		anchor := h.Scalars[h.Targets[0]].Token
		if anchor.End() != d.ByteRange.End {
			continue
		}
		edits, err := h.edits(c.Current())
		if err != nil {
			return nil, err
		}
		if err := c.Reserve(ctx, int64(len(edits))*8, int64(len(edits))*3); err != nil {
			return nil, err
		}
		groups = append(groups, fix.Group{ID: fmt.Sprintf("MD063-capitalization-%d", anchor.Start()), Rule: "MD063", Provenance: "MD063 heading capitalization", Anchor: anchor, Edits: edits, Requirements: []fix.Requirement{{Kind: fix.Meaning}, {Kind: fix.ProtectedSyntax}, {Kind: fix.DirectiveStructure}}})
	}
	return groups, nil
}

// caseRound holds the facts every MD063 group of a round shares: the before
// headings by finding start, and which of them the candidate leaves clean.
type caseRound struct {
	headings  map[int]Heading
	clean     map[int]bool
	preserved bool
}

// Check authorizes one group against the before parse, never against producer
// data. Whole-candidate proof runs once for the round.
func Check(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
	shared, err := c.Once("heading-case", func() (any, error) { return caseProof(ctx, c) })
	if err != nil {
		return fix.Unknown, err
	}
	round, ok := shared.(caseRound)
	if !ok || !round.preserved {
		return fix.Unsafe, nil
	}
	g := c.Group()
	if g.Rule != "MD063" {
		return fix.Unsafe, nil
	}
	h, ok := round.headings[g.Anchor.Start()]
	if !ok || !h.Ready || !round.clean[g.Anchor.Start()] || g.Anchor != h.Scalars[h.Targets[0]].Token {
		return fix.Unsafe, nil
	}
	want, err := h.edits(c.Before.Current())
	if err != nil {
		return fix.Unknown, err
	}
	if err := c.Before.Checkpoint(ctx, int64(len(want))*8+1); err != nil {
		return fix.Unknown, err
	}
	if len(g.Edits) != len(want) {
		return fix.Unsafe, nil
	}
	for i, e := range g.Edits {
		if e.Span != want[i].Span || string(e.Expected) != string(want[i].Expected) || string(e.Replacement) != string(want[i].Replacement) {
			return fix.Unsafe, nil
		}
	}
	return fix.Preserved, nil
}

// caseProof compares the whole candidate with the before parse: only the round's
// edits changed bytes, every heading keeps its level, style, closed state, raw
// label ends and containers, and its visible scalars differ only where a
// correctable required initial became its titlecase form.
func caseProof(ctx context.Context, c fix.CheckContext) (caseRound, error) {
	failed := caseRound{}
	unchanged, err := c.OnlyEditsChanged(ctx)
	if err != nil || !unchanged {
		return failed, err
	}
	before, err := c.Before.Headings(ctx)
	if err != nil {
		return failed, err
	}
	after, err := c.After.Headings(ctx)
	if err != nil {
		return failed, err
	}
	if len(before) != len(after) {
		return failed, nil
	}
	for i, b := range before {
		a := after[i]
		if err := c.Before.Checkpoint(ctx, int64(len(b.Containers))+1); err != nil {
			return failed, err
		}
		if b.Level != a.Level || b.Style != a.Style || b.Closed != a.Closed || b.HasRawContent != a.HasRawContent || len(b.Containers) != len(a.Containers) {
			return failed, nil
		}
		label, ok, err := c.MapEndpoints(b.Label)
		if err != nil || !ok || label != a.Label {
			return failed, err
		}
		envelope, ok, err := c.MapEndpoints(b.Envelope)
		if err != nil || !ok || envelope != a.Envelope {
			return failed, err
		}
		for j, container := range b.Containers {
			mapped, ok, err := c.MapEndpoints(container.Envelope)
			if err != nil || !ok || container.Kind != a.Containers[j].Kind || mapped != a.Containers[j].Envelope {
				return failed, err
			}
		}
	}
	style, names, err := c.Before.HeadingCapitalization()
	if err != nil {
		return failed, err
	}
	old, err := c.Before.HeadingScalars(ctx)
	if err != nil {
		return failed, err
	}
	current, err := c.After.HeadingScalars(ctx)
	if err != nil {
		return failed, err
	}
	if len(old) != len(before) || len(current) != len(old) {
		return failed, nil
	}
	round := caseRound{headings: map[int]Heading{}, clean: map[int]bool{}, preserved: true}
	reserveBefore := func(work, nodes int64) error { return c.Before.Reserve(ctx, work, nodes) }
	reserveAfter := func(work, nodes int64) error { return c.After.Reserve(ctx, work, nodes) }
	for i, scalars := range old {
		h, err := Analyze(c.Before.Current(), style, names, scalars, reserveBefore)
		if err != nil {
			return failed, err
		}
		next := current[i]
		if err := c.Before.Checkpoint(ctx, int64(len(scalars)+len(h.Targets))+1); err != nil {
			return failed, err
		}
		if len(next) != len(scalars) {
			return failed, nil
		}
		target := make(map[int]bool, len(h.Targets))
		for _, t := range h.Targets {
			target[t] = h.Ready
		}
		changed := false
		for j, s := range scalars {
			if s.Code != next[j].Code {
				return failed, nil
			}
			if s.Scalar == next[j].Scalar {
				continue
			}
			up, ok := Replacement(s.Scalar)
			if !target[j] || !ok || next[j].Scalar != up {
				return failed, nil
			}
			changed = true
		}
		if len(h.Targets) == 0 {
			continue
		}
		start := scalars[h.Targets[0]].Token.Start()
		round.headings[start] = h
		if changed {
			a, err := Analyze(c.After.Current(), style, names, next, reserveAfter)
			if err != nil {
				return failed, err
			}
			round.clean[start] = len(a.Targets) == 0
		}
	}
	return round, nil
}
