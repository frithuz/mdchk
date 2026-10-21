// This file replaces internal/cli/providers.go only in the executable acceptance
// test's Go build overlay. It is never part of a normal mdchk build.
package cli

import (
	"bytes"
	"context"
	"errors"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/engine"
)

func configuredProviders() (*engine.Registry, engine.ValidationProviders, config.Capabilities, error) {
	registry, err := engine.NewCatalogRegistry([]engine.Registration{
		{ID: "MD001", Check: configurationFixtureFinding},
		{ID: "MD003", Check: configurationFixtureFailure},
	})
	return registry, engine.ValidationProviders{}, config.Capabilities{CommonMark: true, DirectiveProcessing: true}, err
}

func configurationFixtureFinding(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	if err := c.Checkpoint(ctx, 1); err != nil {
		return engine.RuleOutcome{Err: err}
	}
	marker := []byte("fixture violation")
	start := bytes.Index(c.Source().Bytes(), marker)
	if start < 0 {
		return engine.RuleOutcome{}
	}
	span, err := c.Source().Span(start, start+len(marker))
	if err != nil {
		return engine.RuleOutcome{Err: err}
	}
	return engine.RuleOutcome{Findings: []engine.Finding{{Message: "fixture violation", Span: span}}}
}

func configurationFixtureFailure(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
	if err := c.Checkpoint(ctx, 1); err != nil {
		return engine.RuleOutcome{Err: err}
	}
	if bytes.Contains(c.Source().Bytes(), []byte("fixture failure")) {
		return engine.RuleOutcome{Err: errors.New("fixture callback failure")}
	}
	// Synthetic option probe, deliberately unrelated to MD003's future semantics.
	// It demonstrates that validated defaults and overrides reach a callback.
	marker := []byte("fixture style")
	start := bytes.Index(c.Source().Bytes(), marker)
	if start >= 0 && c.Options()["style"] == "setext" {
		span, err := c.Source().Span(start, start+len(marker))
		if err != nil {
			return engine.RuleOutcome{Err: err}
		}
		return engine.RuleOutcome{Findings: []engine.Finding{{Message: "fixture style setext", Span: span}}}
	}
	return engine.RuleOutcome{}
}
