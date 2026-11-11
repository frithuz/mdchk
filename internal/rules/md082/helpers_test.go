package md082_test

import (
	"context"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/rules/md082"
	"github.com/frithuz/mdchk/internal/rules/ruletest"
)

func registry(t testing.TB, registrations []engine.Registration) *engine.Registry {
	t.Helper()
	return ruletest.Registry(t, registrations)
}

// settings enables only MD047 and disables every other catalog rule.
func settings() engine.Settings { return ruletest.Settings("MD047") }

func analyzeHeadings(t *testing.T, r *engine.Registry, source string, s engine.Settings, dc engine.DocumentContext, opts engine.RunOptions) result.Result {
	t.Helper()
	return ruletest.AnalyzeDocument(t, r, source, s, dc, opts)
}

// The callback gets the engine's real immutable context and residual budget.
// Interrupt after it has entered the suffix traversal, rather than cancelling
// before acquisition and accidentally testing only the engine's early gate.
type traversalContext struct {
	context.Context
	calls int
	err   error
}

func (c *traversalContext) Err() error {
	c.calls++
	if c.calls >= 4 {
		return c.err
	}
	return nil
}

var sectionsWithoutContent = md082.SectionsWithoutContent
