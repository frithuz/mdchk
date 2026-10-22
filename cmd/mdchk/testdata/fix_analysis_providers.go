// Compiled test-only availability fixture. Never selected by runtime input.
package cli

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/rules"
	"strings"
)

func configuredProviders() (*engine.Registry, engine.ValidationProviders, config.Capabilities, error) {
	checks := append(rules.Registrations(), engine.Registration{ID: "MD001", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
		text := string(c.Source().Bytes())
		if err := c.Checkpoint(ctx, int64(len(text))+1); err != nil {
			return engine.RuleOutcome{Err: err}
		}
		if strings.Contains(text, "cancel-analysis-fixture") {
			return engine.RuleOutcome{Err: context.Canceled}
		}
		if strings.Contains(text, "internal-fixture") && !strings.Contains(text, "late-internal-fixture") {
			return engine.RuleOutcome{Err: errors.New("synthetic internal failure")}
		}
		findings := []engine.Finding{}
		if !strings.HasPrefix(text, "\n") {
			sp, _ := c.Source().Span(0, 0)
			findings = append(findings, engine.Finding{Span: sp, Message: "synthetic leading boundary"})
		}
		if at := strings.Index(text, "雪"); at >= 0 {
			sp, _ := c.Source().Span(at, at+len("雪"))
			findings = append(findings, engine.Finding{Span: sp, Message: "remaining Unicode finding"})
		}
		return engine.RuleOutcome{Findings: findings}
	}})
	metadata := engine.Catalog()
	for i := range metadata {
		if metadata[i].ID == "MD001" {
			metadata[i].Fix = engine.PlannedFix
		}
	}
	registry, err := engine.NewRegistry(metadata, checks)
	return registry, engine.ValidationProviders{}, config.Capabilities{CommonMark: true, DirectiveProcessing: true}, err
}
