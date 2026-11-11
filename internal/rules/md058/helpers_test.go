package md058_test

import (
	"context"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/rules/md058"
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

func onlyMD055Settings() engine.Settings { return ruletest.Settings("MD055") }

// Count the actual production callback's context checks so the stop follows a
// completed inventory and the first emission, without fixing a parser work count.
type lateStopContext struct {
	context.Context
	calls, stop int
	reason      error
}

func (c *lateStopContext) Err() error {
	c.calls++
	if c.stop > 0 && c.calls >= c.stop {
		return c.reason
	}
	return c.Context.Err()
}

var (
	compareTableBlankLines = md058.CompareTableBlankLines
	tableBlankLines        = md058.TableBlankLines
)
