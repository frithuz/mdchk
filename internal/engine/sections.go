package engine

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
)

// SectionCheckpoint reserves section analysis under both caller and originating
// controls, without granting resource, metadata or correction authority.
func (c RuleContext) SectionCheckpoint(ctx context.Context, work, nodes int64) error {
	if ctx == nil || c.operation == nil || c.document == nil || c.budget == nil || !c.syntaxReliable || c.parsed.Document != c.document || c.parsed.Source.Token() != c.Source().Token() || c.parsed.Status != parser.Complete || c.document.Source().Token() != c.Source().Token() {
		return parser.ErrModel
	}
	return c.document.SectionCheckpoint(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions(), work, nodes)
}

// EmptySections returns owned finalized records and the immutable final-section
// policy. Labels retain original source ownership; an interrupted view is withheld.
func (c RuleContext) EmptySections(ctx context.Context) (out []parser.EmptySection, allowFinal bool, err error) {
	if err := c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return nil, false, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 0, 0))
		if err != nil {
			out = nil
			allowFinal = false
		}
	}()
	allowFinal, ok := c.options["allow_final_section"].(bool)
	if !ok {
		return nil, false, parser.ErrModel
	}
	out, err = c.document.EmptySections(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
	return out, allowFinal, err
}
