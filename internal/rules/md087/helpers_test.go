package md087_test

import (
	"context"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/rules"
	"github.com/frithuz/mdchk/internal/rules/md086"
	"github.com/frithuz/mdchk/internal/rules/ruletest"
)

func registry(t testing.TB, registrations []engine.Registration) *engine.Registry {
	t.Helper()
	return ruletest.Registry(t, registrations)
}

// registrationsWithout keeps implicit unavailable-rule coverage testable after
// the named rules gained production implementations.
func registrationsWithout(ids ...string) []engine.Registration {
	return ruletest.Without(rules.Registrations(), ids...)
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

var unclosedHTMLComments = md086.Registration().Check
