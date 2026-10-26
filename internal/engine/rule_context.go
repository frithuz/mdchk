package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/metadata"
	"github.com/frithuz/mdchk/internal/parser"
)

// ruleOperation observes both the callback caller and the originating operation.
// It adds no deadline, worker, cancellation lifetime or allowance.
type ruleOperation struct {
	context.Context
	operation context.Context
}

func (c ruleOperation) Err() error { return errors.Join(c.Context.Err(), c.operation.Err()) }

// Headings returns the parser's source-ordered Markdown heading facts, charged
// to the original parse operation. It requires reliable complete syntax and must
// only be called within the rule callback; no budget or reparsing authority is
// exposed. Container boundaries do not partition this sequence.
func (c RuleContext) Headings(ctx context.Context) (out []parser.HeadingFact, err error) {
	if ctx == nil || c.operation == nil || c.document == nil || !c.syntaxReliable || c.document.Source().Token() != c.Source().Token() {
		return nil, parser.ErrModel
	}
	owned := ruleOperation{ctx, c.operation}
	defer func() {
		err = errors.Join(err, owned.Err(), c.budget.ChargeWork(0))
		if err != nil {
			out = nil
		}
	}()
	return c.document.Headings(owned, c.budget)
}

// FrontmatterTitle consumes only title policy for an opted-in frontmatter_title
// option. It binds, decodes and evaluates the actual original header on demand,
// sharing its document and operation controls. Reliable lexical absence means
// known absence, without decoding an unavailable root. Unknown semantic titles
// remain unknown even when later body headings can be compared reliably.
// This narrow semantic access grants no header findings or correction authority;
// UsesFrontmatter continues to govern source observations inside the header.
func (c RuleContext) FrontmatterTitle(ctx context.Context) (metadata.Title, error) {
	if ctx == nil || c.operation == nil || c.document == nil || !c.syntaxReliable || c.document.Source().Token() != c.Source().Token() || c.options["frontmatter_title"] != true {
		return metadata.Title{}, metadata.ErrContract
	}
	binding, err := metadata.Bind(ctx, c.operation, c.budget, c.document)
	if err != nil {
		return metadata.Title{}, err
	}
	if binding.State() == metadata.Absent {
		return metadata.Title{Known: true}, nil
	}
	decoded, err := metadata.Decode(ctx, binding)
	if err != nil {
		return metadata.Title{}, err
	}
	return decoded.Title(ctx, binding)
}

// OpeningHashCandidates projects parser-owned missing-separator points using
// the original operation, source and allowance. Reliable syntax is required.
func (c RuleContext) OpeningHashCandidates(ctx context.Context) (out []parser.Span, err error) {
	if ctx == nil || c.operation == nil || c.document == nil || !c.syntaxReliable {
		return nil, parser.ErrModel
	}
	owned := ruleOperation{ctx, c.operation}
	defer func() {
		err = errors.Join(err, owned.Err(), c.budget.ChargeWork(0))
		if err != nil {
			out = nil
		}
	}()
	return c.document.OpeningHashCandidates(owned, c.budget)
}

// ClosingHashCandidates projects parser-owned missing closing-separator points using
// the original operation, source and allowance. Reliable syntax is required.
func (c RuleContext) ClosingHashCandidates(ctx context.Context) (out []parser.Span, err error) {
	if ctx == nil || c.operation == nil || c.document == nil || !c.syntaxReliable {
		return nil, parser.ErrModel
	}
	owned := ruleOperation{ctx, c.operation}
	defer func() {
		err = errors.Join(err, owned.Err(), c.budget.ChargeWork(0))
		if err != nil {
			out = nil
		}
	}()
	return c.document.ClosingHashCandidates(owned, c.budget)
}

// HeadingSpacing projects physical heading endpoints and adjacent blank body
// runs from the original completed parser ownership and physical line index.
func (c RuleContext) HeadingSpacing(ctx context.Context) (out []parser.HeadingSpacingFact, err error) {
	if ctx == nil || c.operation == nil || c.document == nil || !c.syntaxReliable || c.document.Source().Token() != c.Source().Token() {
		return nil, parser.ErrModel
	}
	owned := ruleOperation{ctx, c.operation}
	defer func() {
		err = errors.Join(err, owned.Err(), c.budget.ChargeWork(0))
		if err != nil {
			out = nil
		}
	}()
	return c.document.HeadingSpacing(owned, c.budget, c.index)
}

// IndentedHeadings projects original full heading markers with parser-confirmed
// leaf indentation, using the completed original source and operation controls.
func (c RuleContext) IndentedHeadings(ctx context.Context) (out []parser.Span, err error) {
	if ctx == nil || c.operation == nil || c.document == nil || !c.syntaxReliable || c.document.Source().Token() != c.Source().Token() {
		return nil, parser.ErrModel
	}
	owned := ruleOperation{ctx, c.operation}
	defer func() {
		err = errors.Join(err, owned.Err(), c.budget.ChargeWork(0))
		if err != nil {
			out = nil
		}
	}()
	return c.document.IndentedHeadings(owned, c.budget)
}

// HeadingKey compares semantic heading text using CommonMark whitespace and
// pinned full default folding. It shares the original completed parse allowance.
func (c RuleContext) HeadingKey(ctx context.Context, h parser.HeadingFact) (string, error) {
	if !h.Label.Present() || h.Label.SourceToken() != c.Source().Token() {
		return "", parser.ErrModel
	}
	if err := c.HeadingComparisonCheckpoint(ctx, 0, 0); err != nil {
		return "", err
	}
	return c.document.HeadingKey(ruleOperation{ctx, c.operation}, c.budget, h)
}

// HeadingComparisonCheckpoint reserves comparison state and output under the
// same source, caller, origin, work, construction and elapsed controls.
func (c RuleContext) HeadingComparisonCheckpoint(ctx context.Context, work, nodes int64) error {
	if ctx == nil || c.operation == nil || c.document == nil || c.budget == nil || !c.syntaxReliable || c.document.Source().Token() != c.Source().Token() {
		return parser.ErrModel
	}
	return errors.Join(ruleOperation{ctx, c.operation}.Err(), c.budget.ChargeWork(work), c.budget.ChargeNodes(nodes))
}

// HeadingTerminals projects terminal visible scalar provenance under the actual
// completed original parse and its shared operation allowance.
func (c RuleContext) HeadingTerminals(ctx context.Context) (out []parser.HeadingTerminal, err error) {
	if ctx == nil || c.operation == nil || c.document == nil || !c.syntaxReliable || c.document.Source().Token() != c.Source().Token() {
		return nil, parser.ErrModel
	}
	owned := ruleOperation{ctx, c.operation}
	defer func() {
		err = errors.Join(err, owned.Err(), c.budget.ChargeWork(0))
		if err != nil {
			out = nil
		}
	}()
	return c.document.HeadingTerminals(owned, c.budget)
}
