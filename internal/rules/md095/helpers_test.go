package md095_test

import (
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/rules/ruletest"
)

func registry(t testing.TB, registrations []engine.Registration) *engine.Registry {
	t.Helper()
	return ruletest.Registry(t, registrations)
}

func onlyDefaultMD038Settings() engine.Settings { return ruletest.OnlyDefault("MD038") }
