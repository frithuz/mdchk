package md077

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

// listContinuationFacts indexes the round's space-only MD077 facts by span start.
// A document with any leading front matter block has none: its edits are withheld.
func listContinuationFacts(ctx context.Context, c fix.RoundContext) (map[int]parser.ListContinuationFact, error) {
	document := c.Parsed().Document
	if _, header := document.Frontmatter(); header || document.UnrecognizedBlock() {
		return nil, nil
	}
	facts, err := c.ListContinuationIndentation(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.Reserve(ctx, int64(len(facts))+1, int64(len(facts))); err != nil {
		return nil, err
	}
	out := make(map[int]parser.ListContinuationFact, len(facts))
	for _, f := range facts {
		if f.SpaceOnly {
			out[f.Span.Start()] = f
		}
	}
	return out, nil
}

// listContinuationEdit derives one fact's edit: a surplus deletes its span and a
// deficit inserts the missing ASCII spaces at its point.
func listContinuationEdit(ctx context.Context, c fix.RoundContext, f parser.ListContinuationFact) (fix.Edit, error) {
	if f.Actual > f.Expected {
		if err := c.Reserve(ctx, int64(f.Span.End()-f.Span.Start())+1, 1); err != nil {
			return fix.Edit{}, err
		}
		text, err := c.Current().Text(f.Span)
		if err != nil {
			return fix.Edit{}, err
		}
		return fix.Edit{Span: f.Span, Expected: []byte(text)}, nil
	}
	if err := c.Reserve(ctx, int64(f.Expected-f.Actual)+1, 1); err != nil {
		return fix.Edit{}, err
	}
	return fix.Edit{Span: f.Span, Replacement: []byte(strings.Repeat(" ", f.Expected-f.Actual))}, nil
}

// Correction regenerates one group for each eligible active MD077
// finding from this round's own parse.
func Correction(ctx context.Context, c fix.RoundContext, findings []result.Diagnostic) ([]fix.Group, error) {
	if len(findings) == 0 {
		return nil, nil
	}
	facts, err := listContinuationFacts(ctx, c)
	if err != nil {
		return nil, err
	}
	groups := make([]fix.Group, 0, len(findings))
	for _, d := range findings {
		if err := c.Reserve(ctx, 128, 12); err != nil {
			return nil, err
		}
		f, ok := facts[d.ByteRange.Start]
		if !ok || f.Span.End() != d.ByteRange.End {
			continue
		}
		edit, err := listContinuationEdit(ctx, c, f)
		if err != nil {
			return nil, err
		}
		groups = append(groups, fix.Group{ID: fmt.Sprintf("MD077-indentation-%d", f.Span.Start()), Rule: "MD077", Provenance: "MD077 list continuation indentation", Anchor: f.Span, Edits: []fix.Edit{edit}, Requirements: []fix.Requirement{{Kind: fix.Meaning}, {Kind: fix.ProtectedSyntax}, {Kind: fix.DirectiveStructure}}})
	}
	return groups, c.Checkpoint(ctx, 0)
}

// listContinuationRound holds the facts every MD077 group of a round shares.
type listContinuationRound struct {
	facts     map[int]parser.ListContinuationFact
	preserved bool
}

// Checker returns the MD077 candidate checker.
func Checker() fix.CheckerRegistration {
	return fix.CheckerRegistration{Rule: "MD077", Check: checkListContinuation}
}

// checkListContinuation authorizes one group against the before parse, trusting
// neither producer data nor fix flags. Whole-candidate proof runs once a round.
// Changing leading spaces outside comments cannot create a directive finding, so
// no ForbidNewFindings is registered.
func checkListContinuation(ctx context.Context, c fix.CheckContext) (fix.Proof, error) {
	shared, err := c.Once("list-continuation", func() (any, error) { return listContinuationProof(ctx, c) })
	if err != nil {
		return fix.Unknown, err
	}
	round, ok := shared.(listContinuationRound)
	if !ok || !round.preserved {
		return fix.Unsafe, nil
	}
	g := c.Group()
	if g.Rule != "MD077" || len(g.Edits) != 1 {
		return fix.Unsafe, nil
	}
	e := g.Edits[0]
	f, ok := round.facts[e.Span.Start()]
	if !ok || f.Span != e.Span || g.Anchor != f.Span {
		return fix.Unsafe, nil
	}
	expected, err := listContinuationEdit(ctx, c.Before, f)
	if err != nil {
		return fix.Unknown, err
	}
	if !bytes.Equal(e.Expected, expected.Expected) || !bytes.Equal(e.Replacement, expected.Replacement) {
		return fix.Unsafe, nil
	}
	return fix.Preserved, nil
}

// listContinuationProof recomputes the expected facts from the before parse,
// proves the combined candidate byte for byte and compares both parses: node
// kinds, order and properties, text values and every block envelope mapped
// around the edits, so each line keeps its owning item and block kind.
func listContinuationProof(ctx context.Context, c fix.CheckContext) (listContinuationRound, error) {
	failed := listContinuationRound{}
	facts, err := listContinuationFacts(ctx, c.Before)
	if err != nil || facts == nil {
		return failed, err
	}
	if unchanged, err := c.OnlyEditsChanged(ctx); err != nil || !unchanged {
		return failed, err
	}
	if same, err := c.MeaningPreservedAcrossLineEndWhitespace(ctx); err != nil || !same {
		return failed, err
	}
	if same, err := sameBlockEnvelopes(ctx, c); err != nil || !same {
		return failed, err
	}
	return listContinuationRound{facts, true}, c.Before.Checkpoint(ctx, 0)
}

// sameBlockEnvelopes walks both trees in step and maps every block envelope of
// the before parse into the candidate. Inline nodes are compared by meaning only.
func sameBlockEnvelopes(ctx context.Context, c fix.CheckContext) (bool, error) {
	type pair struct{ a, b *parser.Node }
	stack := []pair{{c.Before.Parsed().Document.Root(), c.After.Parsed().Document.Root()}}
	for len(stack) > 0 {
		p := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		ac, bc := p.a.Children(), p.b.Children()
		if err := c.Before.Reserve(ctx, int64(len(ac)+len(bc))+1, int64(len(ac)+len(bc))); err != nil {
			return false, err
		}
		if p.a.Kind() != p.b.Kind() {
			return false, nil
		}
		switch p.a.Kind() {
		case parser.DocumentKind, parser.BlockQuote, parser.List, parser.ListItem, parser.Paragraph, parser.Heading, parser.ThematicBreak, parser.CodeBlock, parser.HTMLBlock, parser.Table, parser.TableRow:
			mapped, ok, err := c.MapEndpoints(p.a.Span())
			if err != nil || !ok || mapped != p.b.Span() {
				return false, err
			}
			if len(ac) != len(bc) {
				return false, nil
			}
			for i := range ac {
				stack = append(stack, pair{ac[i], bc[i]})
			}
		case parser.TableCell, parser.Text, parser.SoftBreak, parser.HardBreak, parser.CodeSpan, parser.InlineHTML, parser.Emphasis, parser.Strong, parser.Strike, parser.Link, parser.Image:
			// Inline syntax and cells are compared by meaning, never by envelope.
		}
	}
	return true, nil
}
