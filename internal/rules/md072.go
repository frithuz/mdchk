package rules

import (
	"context"
	"fmt"
	"sort"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/metadata"
	"github.com/frithuz/mdchk/internal/parser"
)

// metadataOrderingWithheld explains incomplete coverage by the first decoder
// problem, located where it was written. Without a problem the cause is a key
// with no authored location, which has nothing to point at.
func metadataOrderingWithheld(first metadata.Problem) (string, parser.Span) {
	switch first.Code {
	case metadata.Malformed:
		return "Metadata is malformed; the affected key ordering checks were withheld.", first.Location
	case metadata.Unsupported:
		return "Metadata uses an unsupported structure such as a merge key, custom tag or cycle; the affected key ordering checks were withheld.", first.Location
	case metadata.DuplicateKey:
		return "Metadata contains a duplicate key; the affected key ordering checks were withheld.", first.Location
	}
	return "A metadata key has no authored source location; its key ordering check was withheld.", parser.Span{}
}

// metadataKeyOrdering reports each key that sorts strictly before the key just
// before it. Withheld mappings and keys without an authored location reduce
// coverage instead of producing a guessed finding.
func metadataKeyOrdering(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	orders, complete, first, err := c.MetadataOrdering(ctx)
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	var findings []engine.Finding
	for _, o := range orders {
		if !o.Violation() {
			continue
		}
		if err := c.HeadingComparisonCheckpoint(ctx, int64(o.Count())+1, int64(o.Count())); err != nil {
			return engine.RuleOutcome{Err: err}
		}
		for _, offending := range o.OffendingKeys() {
			if !offending.Entry.KeySpan().Present() {
				complete = false
				continue
			}
			findings = append(findings, engine.Finding{Span: offending.Entry.KeySpan(), Message: fmt.Sprintf("Metadata key %q should come before %q.", offending.Entry.Key(), offending.Previous.Key())})
		}
	}
	sort.SliceStable(findings, func(i, j int) bool { return findings[i].Span.Start() < findings[j].Span.Start() })
	var unique []engine.Finding
	for _, f := range findings {
		if len(unique) == 0 || f.Span != unique[len(unique)-1].Span {
			unique = append(unique, f)
		}
	}
	out := engine.RuleOutcome{Findings: unique}
	if !complete {
		out.IncompleteReason, out.IncompleteSpan = metadataOrderingWithheld(first)
	}
	return out
}
