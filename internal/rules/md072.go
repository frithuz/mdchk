package rules

import (
	"context"
	"fmt"
	"sort"

	"github.com/frithuz/mdchk/internal/engine"
)

const metadataOrderingWithheld = "Metadata is unavailable or unsupported; the affected key ordering checks were withheld."

// metadataKeyOrdering reports each key that sorts strictly before the key just
// before it. Withheld mappings and keys without an authored location reduce
// coverage instead of producing a guessed finding.
func metadataKeyOrdering(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	orders, complete, err := c.MetadataOrdering(ctx)
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
		pos := make([]int, o.Count())
		for s := range pos {
			original, _ := o.KnownIndex(s)
			pos[original] = s
		}
		mapping := o.Mapping()
		for i := 1; i < len(pos); i++ {
			if pos[i] >= pos[i-1] {
				continue
			}
			previous, _ := mapping.KnownEntry(i - 1)
			current, _ := mapping.KnownEntry(i)
			if !current.KeySpan().Present() {
				complete = false
				continue
			}
			// A projected alias use has no authored key; the anchored definition
			// is checked where it is written.
			if current.KeySpan() == mapping.Span() {
				continue
			}
			findings = append(findings, engine.Finding{Span: current.KeySpan(), Message: fmt.Sprintf("Metadata key %q should come before %q.", current.Key(), previous.Key())})
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
		out.IncompleteReason = metadataOrderingWithheld
	}
	return out
}
