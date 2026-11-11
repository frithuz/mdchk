// Rule fact facades for the headings rules.

package engine

import (
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/parser"
)

// SectionCheckpoint reserves section analysis under both caller and originating
// controls, without granting resource, metadata or correction authority.
// Shared by all section-checked rules.
func (c RuleContext) SectionCheckpoint(ctx context.Context, work, nodes int64) error {
	if ctx == nil || c.operation == nil || c.document == nil || c.budget == nil || !c.syntaxReliable || c.parsed.Document != c.document || c.parsed.Source.Token() != c.Source().Token() || c.parsed.Status != parser.Complete || c.document.Source().Token() != c.Source().Token() {
		return parser.ErrModel
	}
	return c.document.SectionCheckpoint(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions(), work, nodes)
}

// EmptySections returns owned finalized records and the immutable final-section
// policy. Labels retain original source ownership; an interrupted view is withheld.
// Serves MD082.
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

// DisallowedHeadingConstructs returns original syntax facts after applying the
// immutable admitted allow list. It borrows options without exposing their slice.
// Serves MD093.
func (c RuleContext) DisallowedHeadingConstructs(ctx context.Context) (out []parser.HeadingConstruct, err error) {
	if err := c.SectionCheckpoint(ctx, 1, 0); err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, c.SectionCheckpoint(ctx, 0, 0))
		if err != nil {
			out = nil
		}
	}()
	values, ok := c.options["allowed_constructs"].([]string)
	if !ok {
		return nil, parser.ErrModel
	}
	var allowed uint8
	for _, value := range values {
		if err := c.SectionCheckpoint(ctx, int64(len(value))+1, 0); err != nil {
			return nil, err
		}
		switch value {
		case "emphasis":
			allowed |= 1
		case "strong":
			allowed |= 2
		case "code":
			allowed |= 4
		case "link":
			allowed |= 8
		case "image":
			allowed |= 16
		default:
			return nil, parser.ErrModel
		}
	}
	facts, err := c.document.HeadingConstructs(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions())
	if err != nil {
		return nil, err
	}
	out = facts[:0]
	for _, fact := range facts {
		if err := c.SectionCheckpoint(ctx, 1, 0); err != nil {
			return nil, err
		}
		var bit uint8
		switch fact.Kind {
		case parser.Emphasis:
			bit = 1
		case parser.Strong:
			bit = 2
		case parser.CodeSpan:
			bit = 4
		case parser.Link:
			bit = 8
		case parser.Image:
			bit = 16
		default:
			return nil, parser.ErrModel
		}
		if allowed&bit == 0 {
			out = append(out, fact)
		}
	}
	return out, nil
}
