package engine_test

import (
	"context"
	"errors"
	"math"
	"slices"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
)

func TestCatalogOptionDomains(t *testing.T) {
	r, err := engine.NewCatalogRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		id, option     string
		valid, invalid []any
	}{
		{"MD009", "hard_break_spaces", []any{2, 3}, []any{0, 1, -1, 2.0, "2", nil}},
		{"MD012", "maximum", []any{0, 1}, []any{-1, 1.0}},
		{"MD025", "level", []any{1, 6}, []any{0, 7, "1"}},
		{"MD041", "level", []any{1, 6}, []any{0, 7}},
		{"MD054", "allowed_styles", []any{[]string{"inline", "full_reference", "collapsed_reference", "shortcut_reference", "autolink"}}, []any{[]string{}, []string{"reference"}, []string{"Inline"}, []any{"inline"}, nil}},
		{"MD073", "start_marker", []any{"<!-- -->", "<!---->", "<!-- CUSTOM -->"}, []any{"", "<!-->", "<!-- open", "close -->", "<!-- a\nb -->", " <!-- toc -->", "<!-- x -->suffix", "<!--><!-->", "<!-- <!-- -->", "<!-- a --!> b -->"}},
		{"MD073", "min_level", []any{1, 6}, []any{0, 7}},
		{"MD073", "max_level", []any{1, 6}, []any{0, 7}},
		{"MD081", "maximum_ratio", []any{0, 1, 0.0, 0.5, 1.0}, []any{-0.01, 1.01, math.NaN(), math.Inf(1), math.Inf(-1), "0.5", true, nil}},
		{"MD081", "minimum_characters", []any{1, 20}, []any{0, -1, 1.0}},
		{"MD084", "allowed_codepoints", []any{[]string{}, []string{"0000", "D7FF", "E000", "10FFFF", "feff"}}, []any{[]string{""}, []string{"D800"}, []string{"DFFF"}, []string{"110000"}, []string{"U+FEFF"}, []string{"0xFEFF"}, []string{"-1"}, []string{" FEFF"}}},
		{"MD088", "style", []any{"ascii"}, []any{"unicode", "ASCII", "", nil}},
		{"MD093", "allowed_constructs", []any{[]string{}, []string{"emphasis", "strong", "code", "link", "image"}}, []any{[]string{"html"}, []string{"Code"}, []string{""}}},
	}
	for _, tt := range tests {
		t.Run(tt.id+"/"+tt.option, func(t *testing.T) {
			for _, value := range tt.valid {
				// Disable every rule under test. Invalid option checks must not depend on
				// rule enablement or executable availability.
				_, err := r.ResolveSettings(engine.Settings{tt.id: {Enabled: boolptr(false), Options: engine.Options{tt.option: value}}}, engine.ValidationProviders{})
				if err != nil {
					t.Errorf("valid %v: %v", value, err)
				}
			}
			for _, value := range tt.invalid {
				if _, err := r.ResolveSettings(engine.Settings{tt.id: {Enabled: boolptr(false), Options: engine.Options{tt.option: value}}}, engine.ValidationProviders{}); err == nil {
					t.Errorf("accepted invalid %v (%T)", value, value)
				}
			}
		})
	}
	for _, options := range []engine.Options{
		{"start_marker": "<!-- same -->", "end_marker": "<!-- same -->"},
		{"min_level": 5, "max_level": 4},
	} {
		if _, err := r.ResolveSettings(engine.Settings{"MD073": {Options: options}}, engine.ValidationProviders{}); err == nil {
			t.Errorf("accepted invalid TOC relationship: %v", options)
		}
	}
	for id, option := range map[string]string{"MD051": "anchor_style", "MD057": "index_file", "MD073": "anchor_style", "MD080": "anchor_style", "MD088": "replacements"} {
		if _, err := r.ResolveSettings(engine.Settings{id: {Enabled: boolptr(false), Options: engine.Options{option: "anything"}}}, engine.ValidationProviders{}); err == nil {
			t.Errorf("accepted obsolete option %s.%s", id, option)
		}
	}
}

func TestHostnameSemanticValidationBoundary(t *testing.T) {
	r, err := engine.NewCatalogRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, settings := range []engine.Settings{nil, {"MD095": {}}, {"MD095": {Options: engine.Options{"allowed_hosts": []string{}}}}} {
		if _, err := r.ResolveSettings(settings, engine.ValidationProviders{}); err != nil {
			t.Fatalf("empty/default hosts require no semantic provider: %v", err)
		}
	}
	// Every nonempty list needs full validation, even ASCII input, disabled rules,
	// malformed A-labels, joiners, bidi, and ambiguous delimiter spellings. A local
	// structural approximation must not silently admit any of these as validated.
	hosts := []string{"example.org", "exаmple.org", "xn--exmple-4nf.org", "xn--.org", "ab\u200dcd.org", "aא.org", "example..org", ".example.org", "https://example.org", "*.example.org", " example.org", "example.org:443"}
	for _, host := range hosts {
		_, err := r.ResolveSettings(engine.Settings{"MD095": {Enabled: boolptr(false), Options: engine.Options{"allowed_hosts": []string{host}}}}, engine.ValidationProviders{})
		if !errors.Is(err, engine.ErrHostnameValidationUnavailable) {
			t.Errorf("%q: expected unavailable semantic validation, got %v", host, err)
		}
	}
	rejected := errors.New("provider rejected host")
	var seen []string
	services := engine.ValidationProviders{ValidateHostname: func(host string) error {
		seen = append(seen, host)
		if host == "bad" {
			return rejected
		}
		return nil
	}}
	original := []string{"EXAMPLE.ORG.", "exаmple.org", "xn--exmple-4nf.org"}
	resolved, err := r.ResolveSettings(engine.Settings{"MD095": {Enabled: boolptr(false), Options: engine.Options{"allowed_hosts": original}}}, services)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(seen, original) || !slices.Equal(resolved["MD095"].Options["allowed_hosts"].([]string), original) {
		t.Fatal("catalog rewrote hostname input or bypassed supplied semantic provider")
	}
	if _, err := r.ResolveSettings(engine.Settings{"MD095": {Options: engine.Options{"allowed_hosts": []string{"bad"}}}}, services); !errors.Is(err, rejected) {
		t.Fatalf("lost provider error: %v", err)
	}
	// Admission happens before any analysis callback, regardless of enablement.
	calls := 0
	withCheck, err := engine.NewCatalogRegistry([]engine.Registration{{ID: "MD001", Check: func(context.Context, engine.RuleContext) engine.RuleOutcome { calls++; return engine.RuleOutcome{} }}})
	if err != nil {
		t.Fatal(err)
	}
	in := input(t, "hosts.md", []byte("# heading\n"), engine.Settings{"MD095": {Enabled: boolptr(false), Options: engine.Options{"allowed_hosts": []string{"example.org"}}}})
	if _, err := withCheck.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{}); !errors.Is(err, engine.ErrHostnameValidationUnavailable) || calls != 0 {
		t.Fatalf("hostname admission did not precede callback: calls=%d err=%v", calls, err)
	}
}

func TestEveryCatalogOptionChecksItsDeclaredDomain(t *testing.T) {
	r, err := engine.NewCatalogRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	for id, schema := range documentedOptions() {
		for name, spec := range schema {
			bad := []any{nil, map[string]any{}}
			switch spec.Kind {
			case engine.Boolean:
				bad = append(bad, "true", 1)
			case engine.Integer:
				bad = append(bad, 1.0, "1")
				if spec.Minimum != nil {
					bad = append(bad, *spec.Minimum-1)
				}
				if spec.Maximum != nil {
					bad = append(bad, *spec.Maximum+1)
				}
			case engine.Number:
				bad = append(bad, "0.5", math.NaN())
			case engine.String:
				bad = append(bad, 1, true)
				if len(spec.Choices) > 0 {
					bad = append(bad, "unknown-choice")
				}
			case engine.Strings:
				bad = append(bad, "text", []any{"text"})
				if len(spec.Choices) > 0 {
					bad = append(bad, []string{"unknown-choice"})
				}
			}
			for _, value := range bad {
				if _, err := r.ResolveSettings(engine.Settings{id: {Enabled: boolptr(false), Options: engine.Options{name: value}}}, engine.ValidationProviders{}); err == nil {
					t.Errorf("accepted invalid %s.%s = %v (%T)", id, name, value, value)
				}
			}
			if spec.Kind == engine.String {
				for _, value := range spec.Choices {
					if _, err := r.ResolveSettings(engine.Settings{id: {Enabled: boolptr(false), Options: engine.Options{name: value}}}, engine.ValidationProviders{}); err != nil {
						t.Errorf("rejected documented choice %s.%s=%q: %v", id, name, value, err)
					}
				}
			}
		}
	}
}

func TestCompleteCatalogExecutesOnlyAvailableEnabledApplicableChecks(t *testing.T) {
	catalog := engine.Catalog()
	for i := range catalog {
		if catalog[i].ID == "MD003" {
			catalog[i].AppliesTo = func(engine.DocumentContext) bool { return false }
		}
	}
	calls := map[string]int{}
	var regs []engine.Registration
	for _, id := range []string{"MD001", "MD003", "MD013"} {
		regs = append(regs, engine.Registration{ID: id, Check: func(context.Context, engine.RuleContext) engine.RuleOutcome { calls[id]++; return engine.RuleOutcome{} }})
	}
	r, err := engine.NewRegistry(catalog, regs)
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.Lint(context.Background(), []engine.Input{input(t, "catalog.md", []byte("# Heading\n"), nil)}, engine.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Metadata()) != 87 || calls["MD001"] != 1 || calls["MD003"] != 0 || calls["MD013"] != 0 || out.Complete {
		t.Fatalf("wrong catalog execution: calls=%v complete=%t", calls, out.Complete)
	}
	for _, coverage := range out.Coverage {
		if coverage.Rule == "MD001" || coverage.Rule == "MD003" || coverage.Rule == "MD013" {
			t.Fatalf("coverage for executed/disabled/inapplicable check: %+v", coverage)
		}
	}
}
