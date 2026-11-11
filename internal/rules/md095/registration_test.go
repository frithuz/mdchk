package md095_test

import (
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/rules"
)

func TestMD095RegistrationAndCatalog(t *testing.T) {
	registered := false
	for _, registration := range rules.Registrations() {
		registered = registered || registration.ID == "MD095"
	}
	r := registry(t, rules.Registrations())
	s, err := r.ResolveSettings(nil, engine.ValidationProviders{})
	if !registered || err != nil || !s["MD095"].Available || s["MD095"].Enabled || len(s["MD095"].Options["allowed_hosts"].([]string)) != 0 {
		t.Fatal("MD095 registration", registered, s["MD095"], err)
	}
	for _, meta := range r.Metadata() {
		if meta.ID == "MD095" && (meta.DefaultEnabled || meta.Fix != engine.NoFix || !meta.RequiresSyntax || len(meta.Options) != 1 || meta.Options["allowed_hosts"].Domain != engine.DNSHostname) {
			t.Fatal(meta)
		}
	}
}
