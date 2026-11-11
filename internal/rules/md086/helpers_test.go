package md086_test

import (
	"context"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/rules/md086"
	"github.com/frithuz/mdchk/internal/rules/ruletest"
)

func registry(t testing.TB, registrations []engine.Registration) *engine.Registry {
	t.Helper()
	return ruletest.Registry(t, registrations)
}

func analyzeHeadings(t *testing.T, r *engine.Registry, source string, s engine.Settings, dc engine.DocumentContext, opts engine.RunOptions) result.Result {
	t.Helper()
	return ruletest.AnalyzeDocument(t, r, source, s, dc, opts)
}

type stopAtContext struct {
	context.Context
	calls, stop int
	reason      error
}

func (c *stopAtContext) Err() error {
	c.calls++
	if c.calls >= c.stop {
		return c.reason
	}
	return c.Context.Err()
}

var (
	compareUnclosedHTMLComments = md086.CompareUnclosedHTMLComments
	unclosedHTMLComments        = md086.UnclosedHTMLComments
)
