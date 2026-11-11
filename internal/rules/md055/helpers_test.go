package md055_test

import (
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/rules/md055"
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

type (
	gateStopContext  = ruletest.GateStopContext
	gateQuotaContext = ruletest.GateQuotaContext
)

func onlyMD055Settings() engine.Settings { return ruletest.Settings("MD055") }

var (
	compareTableOuterPipes = md055.CompareTableOuterPipes
	outerTablePipes        = md055.OuterTablePipes
	tableOuterPipeOutcome  = md055.TableOuterPipeOutcome
)
