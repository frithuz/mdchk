// Package headingblanks plans, produces and checks the MD022 insertion of missing
// blank lines around headings.
package headingblanks

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// Side is one heading side's blank-line obligation. At is the physical line
// boundary that receives the insertion, never the diagnostic point. Neighbour is
// the whole physical line on the far side of At, whose protection defers the
// insertion. Missing and Line already merge every deficient side sharing At, and
// Known is false when no single prefix-only line can be chosen.
type Side struct {
	Anchor, At, Neighbour parser.Span
	Deficient, Known      bool
	Missing               int
	Line                  string
}

// Sides returns the before and after sides of each spacing fact, in order. The
// inserted line copies the side's heading line blockquote prefix without trailing
// whitespace. A heading inside a list item is never known: a blank line between
// item children can make a tight list loose. The neighbour must carry the same
// quote prefix, so a lazy continuation or a different quote defers. A shortfall
// larger than the whole document defers too, so a huge configured minimum cannot
// make one round allocate it.
func Sides(ctx context.Context, source parser.Source, lines parser.LineIndex, headings []parser.HeadingFact, spacing []parser.HeadingSpacingFact, before, after int, checkpoint func(context.Context, int64) error) ([][2]Side, error) {
	out := make([][2]Side, len(spacing))
	matched := len(headings) == len(spacing)
	shared := map[int][]*Side{}
	for i, h := range spacing {
		if err := checkpoint(ctx, 4); err != nil {
			return nil, err
		}
		quotes, nested := 0, false
		if matched {
			for _, container := range headings[i].Containers {
				if container.Kind == parser.BlockQuote {
					quotes++
				} else {
					nested = true
				}
			}
		}
		for s, side := range [...]struct {
			point          parser.Span
			count, minimum int
			edge           bool
		}{{h.Before, h.BeforeBlank, before, h.BeforeEdge}, {h.After, h.AfterBlank, after, h.AfterEdge}} {
			out[i][s] = Side{Anchor: side.point}
			if side.edge || side.count >= side.minimum {
				continue
			}
			out[i][s].Deficient = true
			out[i][s].Missing = side.minimum - side.count
			position, err := lines.Position(side.point.Start())
			if err != nil {
				return nil, err
			}
			own, err := lines.Line(position.Line)
			if err != nil {
				return nil, err
			}
			far := position.Line - 1
			if s == 1 {
				far = position.Line + 1
			}
			neighbour, err := lines.Line(far)
			if err != nil {
				return nil, err
			}
			at := own.Content
			if s == 1 {
				at = neighbour.Content
			}
			out[i][s].At, _ = source.Span(at.Start(), at.Start())
			out[i][s].Neighbour, _ = source.Span(neighbour.Content.Start(), neighbour.Ending.End())
			if err := checkpoint(ctx, int64(own.Content.End()-own.Content.Start()+neighbour.Content.End()-neighbour.Content.Start())+1); err != nil {
				return nil, err
			}
			ownText, _ := source.Text(own.Content)
			farText, _ := source.Text(neighbour.Content)
			prefix, ok := quotePrefix(ownText, quotes)
			_, farOK := quotePrefix(farText, quotes)
			out[i][s].Known = matched && !nested && ok && farOK && out[i][s].Missing <= source.Len()
			out[i][s].Line = strings.TrimRight(ownText[:prefix], " \t")
			shared[out[i][s].At.Start()] = append(shared[out[i][s].At.Start()], &out[i][s])
		}
	}
	// One heading's after side and the next one's before side share a boundary.
	// Both receive the larger shortfall, so their identical edits apply once.
	for _, group := range shared {
		missing, known := 0, true
		for _, side := range group {
			missing = max(missing, side.Missing)
			known = known && side.Known && side.Line == group[0].Line
		}
		for _, side := range group {
			side.Missing, side.Known = missing, known
		}
	}
	return out, checkpoint(ctx, int64(len(shared))+1)
}

// quotePrefix measures quotes blockquote markers, each after at most three spaces.
func quotePrefix(text string, quotes int) (int, bool) {
	pos := 0
	for range quotes {
		for spaces := 0; spaces < 3 && pos < len(text) && text[pos] == ' '; spaces++ {
			pos++
		}
		if pos >= len(text) || text[pos] != '>' {
			return 0, false
		}
		pos++
	}
	return pos, true
}

// Minimums reads MD022's admitted before and after options.
func Minimums(options engine.Options) (int, int) {
	before, _ := options["before"].(int)
	after, _ := options["after"].(int)
	return before, after
}

// round is one round's sides indexed by anchor start, with their insertions.
type round struct {
	sides     map[int]Side
	preserved bool
}

func roundSides(ctx context.Context, c fix.RoundContext) (map[int]Side, error) {
	options, ok := c.RuleOptions("MD022")
	if !ok {
		return nil, nil
	}
	headings, err := c.Headings(ctx)
	if err != nil {
		return nil, err
	}
	spacing, err := c.HeadingSpacing(ctx)
	if err != nil {
		return nil, err
	}
	before, after := Minimums(options)
	sides, err := Sides(ctx, c.Current(), c.Lines(), headings, spacing, before, after, c.Checkpoint)
	if err != nil {
		return nil, err
	}
	out := map[int]Side{}
	for _, pair := range sides {
		for _, side := range pair {
			if side.Deficient && side.Known {
				out[side.Anchor.Start()] = side
			}
		}
	}
	return out, nil
}

func replacement(ctx context.Context, c fix.RoundContext, side Side) ([]byte, error) {
	ending, err := fix.InsertionEnding(ctx, c.Current(), c.Lines(), side.At.Start(), c.Checkpoint)
	if err != nil {
		return nil, err
	}
	if err := c.Checkpoint(ctx, int64((len(side.Line)+len(ending))*side.Missing)+1); err != nil {
		return nil, err
	}
	return bytes.Repeat(append([]byte(side.Line), ending...), side.Missing), nil
}

// Groups regenerates one insertion group for each eligible MD022 finding. Merged
// sides propose identical edits, which Plan applies once and credits to both.
func Groups(ctx context.Context, c fix.RoundContext, findings []result.Diagnostic) ([]fix.Group, error) {
	if len(findings) == 0 {
		return nil, nil
	}
	sides, err := roundSides(ctx, c)
	if err != nil {
		return nil, err
	}
	groups := make([]fix.Group, 0, len(findings))
	for _, d := range findings {
		if err := c.Reserve(ctx, 128, 12); err != nil {
			return nil, err
		}
		side, ok := sides[d.ByteRange.Start]
		if !ok || side.Anchor.End() != d.ByteRange.End {
			continue
		}
		insert, err := replacement(ctx, c, side)
		if err != nil {
			return nil, err
		}
		groups = append(groups, fix.Group{ID: fmt.Sprintf("MD022-blank-%d", side.Anchor.Start()), Rule: "MD022", Provenance: "MD022 missing heading blank lines", Anchor: side.Anchor, Edits: []fix.Edit{{Span: side.At, Replacement: insert}}, Requirements: []fix.Requirement{{Kind: fix.Meaning}, {Kind: fix.ProtectedSyntax}, {Kind: fix.DirectiveStructure}}})
	}
	return groups, nil
}

// Check authorizes one group against the before parse, never against producer
// data. The round-wide proof runs once: only the round's edits changed bytes,
// every heading keeps its level, style, closed state, text and containers, and
// the document keeps its block and inline meaning. Each group must then be the
// exact pure insertion of prefix-only lines that Before's facts require.
func Check(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
	shared, err := c.Once("heading-blank-lines", func() (any, error) { return proof(ctx, c) })
	if err != nil {
		return fix.Unknown, err
	}
	r, ok := shared.(round)
	if !ok || !r.preserved {
		return fix.Unsafe, nil
	}
	g := c.Group()
	if g.Rule != "MD022" || len(g.Edits) != 1 || len(g.Edits[0].Expected) != 0 {
		return fix.Unsafe, nil
	}
	e := g.Edits[0]
	side, ok := r.sides[g.Anchor.Start()]
	if !ok || side.Anchor != g.Anchor || e.Span != side.At {
		return fix.Unsafe, nil
	}
	expected, err := replacement(ctx, c.Before, side)
	if err != nil {
		return fix.Unknown, err
	}
	if !bytes.Equal(e.Replacement, expected) {
		return fix.Unsafe, nil
	}
	return fix.Preserved, nil
}

func proof(ctx context.Context, c fix.CheckContext) (round, error) {
	failed := round{}
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
		if err := c.Before.Checkpoint(ctx, int64(len(b.Visible)+len(a.Visible)+len(b.Containers))+1); err != nil {
			return failed, err
		}
		if b.Level != a.Level || b.Style != a.Style || b.Closed != a.Closed || b.Visible != a.Visible || len(b.Containers) != len(a.Containers) {
			return failed, nil
		}
		for j, container := range b.Containers {
			same, err := c.SameEnvelope(container.Envelope, a.Containers[j].Envelope)
			if err != nil || !same || container.Kind != a.Containers[j].Kind {
				return failed, err
			}
		}
	}
	same, err := c.MeaningPreservedAcrossLineEndWhitespace(ctx)
	if err != nil || !same {
		return failed, err
	}
	sides, err := roundSides(ctx, c.Before)
	if err != nil {
		return failed, err
	}
	return round{sides, true}, nil
}
