package md032_test

import (
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/fix"
	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/rules"
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

func spacingCorrected(t *testing.T, text, flavor string, producer fix.Producer, ids ...string) (fix.FileView, fix.Validated) {
	t.Helper()
	return ruletest.CorrectedFlavor(t, rules.Registrations(), rules.CorrectionCheckers(), text, flavor, producer, ids...)
}
