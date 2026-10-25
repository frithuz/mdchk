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
	if ctx == nil || c.operation == nil || c.document == nil || !c.syntaxReliable || c.options["frontmatter_title"] != true {
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
