package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/resource"
)

func (c *DocumentContext) resolveAnchorStyle() error {
	if c.AnchorStyle == "" {
		c.AnchorStyle = "github"
	}
	if c.AnchorStyle != "github" && c.AnchorStyle != "explicit" {
		return fmt.Errorf("invalid anchor style %q", c.AnchorStyle)
	}
	return nil
}

// HeadingAnchors obtains shared same-document heading bases and original labels.
// Availability distinguishes a complete empty result from unknown anchor syntax;
// this grants no resource read, service evidence, cache or cross-file authority.
func (c RuleContext) HeadingAnchors(ctx context.Context) (out []resource.HeadingAnchor, availability resource.Availability, err error) {
	if err := c.HeadingAnchorCheckpoint(ctx, 1, 0); err != nil {
		if _, interrupted := resource.InterruptionProblem(err); interrupted {
			return nil, resource.Interrupted, err
		}
		return nil, resource.Unavailable, err
	}
	defer func() {
		err = errors.Join(err, c.HeadingAnchorCheckpoint(ctx, 0, 0))
		if err != nil {
			out = nil
			if _, interrupted := resource.InterruptionProblem(err); interrupted {
				availability = resource.Interrupted
			} else if availability == resource.Available {
				availability = resource.Failed
			}
		}
	}()
	return resource.OriginalHeadingAnchors(ruleOperation{ctx, c.operation}, c.parsed, c.budget, c.index, c.Context().ParseOptions(), c.Context().AnchorStyle)
}

// HeadingAnchorCheckpoint reserves local comparison work/records under the
// actual parse and policy witness and both caller and originating controls.
func (c RuleContext) HeadingAnchorCheckpoint(ctx context.Context, work, nodes int64) error {
	if ctx == nil || c.operation == nil || c.document == nil || c.budget == nil || !c.syntaxReliable || c.document.Source().Token() != c.Source().Token() {
		return parser.ErrModel
	}
	return c.document.HeadingAnchorCheckpoint(ruleOperation{ctx, c.operation}, c.budget, c.index, c.Context().ParseOptions(), work, nodes)
}
