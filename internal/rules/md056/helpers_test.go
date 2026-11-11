package md056_test

import (
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/rules/md056"
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

func onlyMD056Settings() engine.Settings { return ruletest.Settings("MD056") }

var (
	compareTableCellCounts = md056.CompareTableCellCounts
	mismatchedTableCells   = md056.MismatchedTableCells
	tableCellCountOutcome  = md056.TableCellCountOutcome
)
