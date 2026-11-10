package md051_test

import (
	"context"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/resource"
	"github.com/frithuz/mdchk/internal/rules"
	"github.com/frithuz/mdchk/internal/rules/ruletest"
)

func TestMD051ResolveFragmentOutcomes(t *testing.T) {
	files := map[string]string{"docs/other.md": "# Intro\n", "docs/bad.md": "# Intro \xff\n", "docs/sub/index.md": "# Top\n"}
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, row := range []struct {
			destination string
			outcome     resource.FragmentOutcome
		}{
			{"other.md#intro", resource.FragmentFound},
			{"other.md#nope", resource.FragmentAbsentTarget},
			{"other.md#%ZZ", resource.FragmentMalformed},
			{"other.md#", resource.DocumentReference},
			{"gone.md#intro", resource.FragmentInapplicable},
			{"bad.md#intro", resource.FragmentContextUnavailable},
			{"sub/#top", resource.FragmentFound},
			{"sub/#nope", resource.FragmentAbsentTarget},
			{"./self.md#self", resource.FragmentFound},
			{"https://example.test/#a", resource.FragmentSkipped},
		} {
			t.Run(flavor+"/"+row.destination, func(t *testing.T) {
				source := "# Self\n\n[x](" + row.destination + ")\n"
				called := false
				r := ruletest.Registry(t, []engine.Registration{{ID: "MD051", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					items, err := c.FragmentLinks(ctx)
					if err != nil || len(items.Links) != 1 {
						t.Fatal(items, err)
					}
					resolved, err := c.ResolveFragment(ctx, items.Links[0].Destination, items.Links[0].Span)
					if err != nil || !resolved.Available || resolved.Resolution.Outcome() != row.outcome {
						t.Fatal(resolved.Resolution.Outcome(), err)
					}
					called = true
					return engine.RuleOutcome{}
				}}})
				s := md051Settings()
				on := true
				s["MD051"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{"cross_file": true}}
				in, err := engine.NewInput("docs/self.md", []byte(source), s, engine.DocumentContext{Flavor: flavor})
				if err != nil {
					t.Fatal(err)
				}
				provider := ruletest.NewNativeFragments(t, files, flavor, "docs/self.md", in.Source())
				if _, err := r.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{Fragments: provider}); err != nil || !called {
					t.Fatal(err, called)
				}
			})
		}
	}
}

func TestMD051ReferenceOriginsReportWholeLinkThroughResolver(t *testing.T) {
	files := map[string]string{"docs/other.md": "# Intro\n"}
	for _, flavor := range []string{"commonmark", "gfm"} {
		source := "# Self\n\n[z][r] [c][] [s]\n\n[r]: other.md#nope\n[c]: other.md#intro\n[s]: other.md#nope\n"
		s := md051Settings()
		on := true
		s["MD051"] = engine.RuleSetting{Enabled: &on, Options: engine.Options{"cross_file": true}}
		in, err := engine.NewInput("docs/self.md", []byte(source), s, engine.DocumentContext{Flavor: flavor})
		if err != nil {
			t.Fatal(err)
		}
		provider := ruletest.NewNativeFragments(t, files, flavor, "docs/self.md", in.Source())
		out, err := ruletest.Registry(t, rules.Registrations()).Lint(context.Background(), []engine.Input{in}, engine.RunOptions{Fragments: provider})
		if err != nil || len(out.Errors)+len(out.Coverage) != 0 {
			t.Fatal(out, err)
		}
		var spans []string
		for _, d := range out.Diagnostics {
			if d.Rule == "MD051" {
				spans = append(spans, source[d.ByteRange.Start:d.ByteRange.End])
			}
		}
		if len(spans) != 2 || spans[0] != "[z][r]" || spans[1] != "[s]" {
			t.Fatal(spans)
		}
	}
}
