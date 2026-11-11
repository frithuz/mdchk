// Package md003 checks and corrects MD003, which requires one heading syntax style.
package md003

import (
	"context"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/rules/internal/headingstyle"
)

// Registration returns the MD003 rule ID and its check.
func Registration() engine.Registration {
	return engine.Registration{ID: "MD003", Check: check}
}

func check(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	headings, err := c.Headings(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	expected, _ := c.Options()["style"].(string)
	out := engine.RuleOutcome{}
	edits := c.InteriorEdits()
	for _, heading := range headings {
		if err := c.Checkpoint(ctx, 1); err != nil {
			out.Err = err
			return out
		}
		actual := heading.Style
		if heading.Closed {
			actual = "atx_closed"
		}
		// The first actual heading fixes one document-wide baseline, including
		// headings whose observations the engine will subsequently suppress.
		if expected == "consistent" {
			expected = actual
		}
		if actual != expected {
			finding := engine.Finding{Span: heading.Envelope, Message: "Heading syntax must be " + expected + "."}
			rewrite, delimiter, ok, err := headingstyle.Rewrite(ctx, c.Source(), heading, expected, c.Checkpoint)
			if err != nil {
				out.Err = err
				return out
			}
			// Every changed span and the delimiter itself must be editable here.
			finding.FixEligible = ok
			for _, span := range append([]parser.Span{delimiter}, spans(rewrite)...) {
				if !finding.FixEligible || span.Start() == span.End() {
					continue
				}
				finding.FixEligible, err = edits.Eligible(ctx, "MD003", heading.Envelope, span)
				if err != nil {
					out.Err = err
					return out
				}
			}
			out.Findings = append(out.Findings, finding)
		}
	}
	return out
}

func spans(edits []headingstyle.Edit) []parser.Span {
	out := make([]parser.Span, len(edits))
	for i, e := range edits {
		out[i] = e.Span
	}
	return out
}
