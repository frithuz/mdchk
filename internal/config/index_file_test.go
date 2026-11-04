package config

import (
	"errors"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/resource"
)

// The native adapter's pure predicate admits ordinary filenames and flows the
// value through the resolution, execution context and provenance.
func TestNativeIndexFileAdmissionAndProvenance(t *testing.T) {
	caps := baseCaps()
	caps.ResourceIndex, caps.GitHubAnchors = true, true
	caps.RepresentIndexFile = resource.RepresentNativeIndexFile
	v := schemaFor(t, "version: 1\nflavor: gfm\nresources: {index_file: README.md}\n", registryFor(t))
	resolution, err := v.Admit(caps, Permissions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := resolution.Settings.IndexFile; got != "README.md" {
		t.Fatal(got)
	}
	if got := resolution.ExecutionPlan().CandidatePlan().Context().IndexFile; got != "README.md" {
		t.Fatal(got)
	}
	p := v.Configuration(caps).Provenance["resources.index_file"]
	if !p.Explicit || !p.Available || p.Origin.PropertyPath != "resources.index_file" {
		t.Fatalf("%+v", p)
	}
	defaults := schemaFor(t, "version: 1\nflavor: gfm\n", registryFor(t))
	resolution, err = defaults.Admit(caps, Permissions{})
	if err != nil || resolution.ExecutionPlan().CandidatePlan().Context().IndexFile != "index.md" || defaults.Configuration(caps).Provenance["resources.index_file"].Explicit {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		// adapter is true when only the native adapter rejects the name; false
		// when the shared filename domain already rejects it.
		adapter bool
	}{{".git", true}, {"a\x00b", false}, {strings.Repeat("a", 256), true}} {
		d := yamlFor(t, "version: 1\nflavor: gfm\nresources: {index_file: \""+strings.ReplaceAll(tc.name, "\x00", "\\u0000")+"\"}\n")
		v, e := ValidateDefinition(d, registryFor(t), engine.ValidationProviders{})
		if e == nil {
			_, e = v.Admit(caps, Permissions{})
		}
		assertPath(t, e, "resources.index_file")
		if errors.Is(e, resource.ErrIndexFileNotRepresentable) != tc.adapter {
			t.Fatal(tc.name, tc.adapter, e)
		}
	}
}
