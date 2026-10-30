package rules

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

func md087Settings(targets ...string) engine.Settings {
	s := engine.Settings{}
	off, on := false, true
	for _, m := range engine.Catalog() {
		s[m.ID] = engine.RuleSetting{Enabled: &off}
	}
	s["MD087"] = engine.RuleSetting{Enabled: &on}
	for _, id := range targets {
		s[id] = engine.RuleSetting{Enabled: &on}
	}
	return s
}

type md087GlobalOff struct {
	rules map[string]engine.ResolvedRule
}

func (p md087GlobalOff) DirectiveConfiguration() (bool, map[string]engine.ResolvedRule, error) {
	return false, p.rules, nil
}
func (p md087GlobalOff) DirectivePermission(string) engine.DirectiveDecision {
	return engine.DirectiveDecision{Allowed: true}
}
func (p md087GlobalOff) GlobalDirectivePermission() engine.DirectiveDecision {
	return engine.DirectiveDecision{}
}
func (p md087GlobalOff) DirectivePermissionWork(string) (int64, error) { return 0, nil }

func TestMD087FrozenProductionOracle(t *testing.T) {
	raw, err := os.ReadFile("testdata/md087.json")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(raw)) != "f6184989ac6ad5f5e80a884baf5853c843ede3b3ada28c0517c38106088e2d13" {
		t.Fatal("immutable root oracle identity")
	}
	var oracle struct {
		Cases []struct {
			Name, Source string
			Settings     json.RawMessage
			Complete     *bool
			Findings     []struct {
				Target string
				Bytes  [2]int
				Range  [4]int
			}
			Decisions *[]struct {
				Owner          int
				Target, Status string
				Interval       []int
				EOF            bool
			}
		}
	}
	if err = json.Unmarshal(raw, &oracle); err != nil {
		t.Fatal(err)
	}
	ids := regexp.MustCompile(`MD[0-9]{3}`)
	for _, tc := range oracle.Cases {
		for _, flavor := range []string{"commonmark", "gfm"} {
			t.Run(tc.Name+"/"+flavor, func(t *testing.T) {
				targets := ids.FindAllString(tc.Source, -1)
				s := md087Settings()
				on, off := true, false
				for _, id := range targets {
					if id != "MD999" {
						s[id] = engine.RuleSetting{Enabled: &on}
					}
				}
				if len(tc.Settings) > 0 && tc.Settings[0] == '{' {
					var settings map[string]bool
					if e := json.Unmarshal(tc.Settings, &settings); e != nil {
						t.Fatal(e)
					}
					for id, value := range settings {
						v := value
						s[id] = engine.RuleSetting{Enabled: &v}
					}
				}
				if tc.Name == "known-unavailable-mixed" {
					delete(s, "MD011")
				}
				if tc.Name == "blocked-disable" || tc.Name == "denied-instructions-do-not-create-capability" {
					v := s["MD001"]
					v.AllowDirectives = &off
					s["MD001"] = v
				}
				dc := engine.DocumentContext{Flavor: flavor}
				if tc.Name == "metadata-literal" {
					dc.Frontmatter, err = parser.NewFrontmatterOptions(true, nil)
					if err != nil {
						t.Fatal(err)
					}
				}
				r := registry(t, Registrations())
				selection, e := r.SelectRules(s, engine.ValidationProviders{})
				if e != nil {
					t.Fatal(e)
				}
				plan, e := selection.Plan(dc, nil)
				if e != nil {
					t.Fatal(e)
				}
				if tc.Name == "global-directives-off" {
					plan = plan.WithDirectivePolicy(md087GlobalOff{plan.Rules()})
				}
				original := []byte(tc.Source)
				in, e := plan.NewInput("literal.md", original)
				if e != nil {
					t.Fatal(e)
				}
				budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
				out, e := plan.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{Budget: budget})
				complete := true
				if tc.Complete != nil {
					complete = *tc.Complete
				}
				if e != nil || len(out.Errors) != 0 || out.Complete != complete {
					t.Fatal(out, e)
				}
				if string(original) != tc.Source || string(in.Source().Bytes()) != tc.Source || len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 {
					t.Fatal("analysis changed source or produced corrections")
				}
				n := 0
				for _, d := range out.Diagnostics {
					if d.Rule != "MD087" {
						continue
					}
					if n >= len(tc.Findings) {
						t.Fatal("extra MD087", d)
					}
					w := tc.Findings[n]
					n++
					if d.Message != "Suppression for "+w.Target+" hides no findings." || d.FixAvailable || [2]int{d.ByteRange.Start, d.ByteRange.End} != w.Bytes || [4]int{d.Range.Start.Line, d.Range.Start.Column, d.Range.End.Line, d.Range.End.Column} != w.Range {
						t.Fatal(d, w)
					}
				}
				if n != len(tc.Findings) {
					t.Fatal("missing MD087", out)
				}
				var decisions []engine.SuppressionDecision
				var directiveState engine.DirectiveResult
				for _, record := range out.AnalysisRecords() {
					if a, ok := record.(engine.DirectiveAnalysis); ok {
						directiveState = a.State()
						rows, e := a.SuppressionDecisions(context.Background(), budget)
						if e != nil {
							t.Fatal(e)
						}
						decisions = append(decisions, rows...)
					}
				}
				if tc.Decisions == nil {
					// The frozen composition case specifies source/findings
					// without an inventory. Root separately authorized these
					// exact original-view decisions; omission is not empty.
					if tc.Name != "candidate-and-saved-md047" || len(decisions) != 1 || in.Source().Len() != 33 {
						t.Fatal(tc.Name, decisions)
					}
					d := decisions[0]
					if d.Owner != 0 || d.Effect != 0 || d.Rule != "MD001" || d.Reason != "suppression-unused" || d.Token.SourceToken() != in.Source().Token() || d.Token.Start() != 19 || d.Token.End() != 24 || d.Interval.Start != 28 || d.Interval.End != 33 || d.Interval.Owner != 0 || d.Interval.Enabled || d.Interval.Token != d.Token {
						t.Fatal(d)
					}
					eof, e := directiveState.StateAt("MD001", 33)
					if e != nil || eof.Owner != 0 || eof.Enabled || eof.Token != d.Token {
						t.Fatal("original EOF ownership", eof, e)
					}
					return
				}
				expected := *tc.Decisions
				if len(decisions) != len(expected) {
					t.Fatal(decisions, expected)
				}
				for i, d := range decisions {
					w := expected[i]
					if d.Owner != w.Owner || d.Rule != w.Target || d.Reason != "suppression-"+w.Status {
						t.Fatal(d, w)
					}
					if len(w.Interval) == 2 && (d.Interval.Start != w.Interval[0] || d.Interval.End != w.Interval[1]) {
						t.Fatal(d, w)
					}
				}
			})
		}
	}
}

func TestMD087DefaultsAvailabilityAndPermanentNoFix(t *testing.T) {
	r := registry(t, Registrations())
	states, err := r.ResolveSettings(nil, engine.ValidationProviders{})
	if err != nil {
		t.Fatal(err)
	}
	m := states["MD087"]
	if m.Enabled || !m.Available || !r.Available("MD087") || len(m.Options) != 0 {
		t.Fatal(m)
	}
	for _, meta := range r.Metadata() {
		if meta.ID == "MD087" && (meta.DefaultEnabled || meta.Fix != engine.NoFix || !meta.RequiresSyntax || len(meta.Options) != 0) {
			t.Fatal(meta)
		}
	}
	for _, key := range []string{"unused", "remove", "options", "unknown"} {
		s := md087Settings()
		v := s["MD087"]
		v.Options = engine.Options{key: true}
		s["MD087"] = v
		if _, e := r.ResolveSettings(s, engine.ValidationProviders{}); e == nil {
			t.Fatal(key)
		}
	}
}

func TestMD087ProductionLaterCatalogTarget(t *testing.T) {
	r := registry(t, Registrations())
	s := md087Settings("MD088")
	in, err := engine.NewInput("later.md", []byte("<!-- mdchk:disable MD088 -->\nclean\n"), s, engine.DocumentContext{Flavor: "gfm"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
	if err != nil || !out.Complete || len(out.Diagnostics) != 1 || !strings.Contains(out.Diagnostics[0].Message, "MD088") {
		t.Fatal(out, err)
	}
}

func TestMD087ProductionLaterCatalogHiddenFinding(t *testing.T) {
	regs := Registrations()
	calls := 0
	for i, registration := range regs {
		if registration.ID == "MD088" {
			check := registration.Check
			regs[i].Check = func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				calls++
				out := check(ctx, c)
				if out.Err != nil || out.IncompleteReason != "" || len(out.Findings) != 3 {
					t.Fatal(out)
				}
				return out
			}
		}
	}
	r := registry(t, regs)
	in, err := engine.NewInput("later-used.md", []byte("<!-- mdchk:disable MD088 -->\n“Guide”—example\n"), md087Settings("MD088"), engine.DocumentContext{Flavor: "gfm"})
	if err != nil {
		t.Fatal(err)
	}
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	out, err := r.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{Budget: budget})
	if err != nil || !out.Complete || calls != 1 || len(out.Diagnostics) != 0 || len(out.SuppressedDiagnostics) != 3 {
		t.Fatal(out, err, calls)
	}
	for _, record := range out.AnalysisRecords() {
		if a, ok := record.(engine.DirectiveAnalysis); ok {
			d, e := a.SuppressionDecisions(context.Background(), budget)
			if e != nil || len(d) != 1 || d[0].Reason != "suppression-used" {
				t.Fatal(d, e)
			}
		}
	}
}

func TestMD087ProductionMD001ReliablePartial(t *testing.T) {
	s := md087Settings("MD001")
	v := s["MD001"]
	v.Options = engine.Options{"frontmatter_title": true}
	s["MD001"] = v
	dc := engine.DocumentContext{Flavor: "gfm"}
	dc.Frontmatter, _ = parser.NewFrontmatterOptions(true, nil)
	source := "---\ntitle: [broken\n---\n# A\n<!-- mdchk:disable MD001 -->\n<!-- mdchk:disable MD001 -->\n### B\n"
	observed := false
	regs := Registrations()
	for i, registration := range regs {
		if registration.ID == "MD001" {
			check := registration.Check
			regs[i].Check = func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				out := check(ctx, c)
				if out.Err != nil || out.IncompleteReason == "" || len(out.Findings) != 1 {
					t.Fatal(out)
				}
				observed = true
				return out
			}
		}
	}
	r := registry(t, regs)
	in, _ := engine.NewInput("partial.md", []byte(source), s, dc)
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	out, err := r.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{Budget: budget})
	if err != nil || !observed || out.Complete || len(out.Diagnostics) != 0 || len(out.Errors) != 0 {
		t.Fatal(out, err)
	}
	for _, record := range out.AnalysisRecords() {
		if a, ok := record.(engine.DirectiveAnalysis); ok {
			d, e := a.SuppressionDecisions(context.Background(), budget)
			if e != nil || len(d) != 2 || d[0].Reason != "suppression-uncertain" || d[1].Reason != "suppression-used" {
				t.Fatal(d, e)
			}
		}
	}
}
func TestMD087ActualMD086AtomicFailure(t *testing.T) {
	regs := Registrations()
	observed := false
	for i, registration := range regs {
		if registration.ID == "MD086" {
			regs[i].Check = func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
				reliable := unclosedHTMLComments(ctx, c)
				if reliable.Err != nil || len(reliable.Findings) != 1 {
					t.Fatal(reliable)
				}
				// This valid private prefix is never returned to the engine. Fault the
				// actual production entry's facade/publication path and return its result.
				failed := unclosedHTMLComments(&md033StopAt{Context: ctx, stop: 1, reason: parser.ErrModel}, c)
				if failed.Err == nil || failed.Findings != nil {
					t.Fatal(failed)
				}
				observed = true
				return failed
			}
		}
	}
	r := registry(t, regs)
	in, _ := engine.NewInput("atomic.md", []byte("<!-- mdchk:disable MD086 -->\n<!-- open"), md087Settings("MD086"), engine.DocumentContext{})
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	out, err := r.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{Budget: budget})
	if err != nil || !observed || out.Complete || len(out.Diagnostics) != 0 || len(out.Errors) == 0 {
		t.Fatal(out, err)
	}
	for _, record := range out.AnalysisRecords() {
		if a, ok := record.(engine.DirectiveAnalysis); ok {
			d, e := a.SuppressionDecisions(context.Background(), budget)
			if e != nil || len(d) != 1 || d[0].Reason != "suppression-uncertain" {
				t.Fatal(d, e)
			}
		}
	}
}

func TestMD087ActualMD047HiddenAndInactiveEligibility(t *testing.T) {
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, inactive := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/inactive-%v", flavor, inactive), func(t *testing.T) {
				called := false
				regs := []engine.Registration{{ID: "MD087", Check: engine.CheckUnusedSuppressions}, {ID: "MD047", Check: func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
					called = true
					out := terminatingNewline(ctx, c)
					if out.Err != nil || len(out.Findings) != 1 || out.Findings[0].FixEligible {
						t.Fatal(out)
					}
					return out
				}}}
				r := registry(t, regs)
				s := md087Settings("MD047")
				source := "<!-- mdchk:disable MD047 -->"
				if inactive {
					off := false
					s["MD087"] = engine.RuleSetting{Enabled: &off}
					s["MD047"] = engine.RuleSetting{Enabled: &off}
					source = "text"
				}
				selected, err := r.SelectRules(s, engine.ValidationProviders{})
				if err != nil {
					t.Fatal(err)
				}
				plan, err := selected.Plan(engine.DocumentContext{Flavor: flavor}, nil)
				if err != nil {
					t.Fatal(err)
				}
				in, err := plan.NewInput("hidden.md", []byte(source))
				if err != nil {
					t.Fatal(err)
				}
				out, trace, err := plan.Inspect(context.Background(), in, engine.RunOptions{}, engine.InspectionOptions{ObserveInactive: inactive})
				if err != nil || !called || !out.Complete || len(out.Diagnostics) != 0 {
					t.Fatal(out, err)
				}
				for _, e := range trace.Evaluations() {
					if e.Rule == "MD047" {
						if inactive && (e.Active != "skipped" || e.Observational != "complete") {
							t.Fatal(e)
						}
						if !inactive && e.Active != "complete" {
							t.Fatal(e)
						}
					}
				}
			})
		}
	}
}

// Root authored this literal batch before the repair or any batch probe. Every
// view uses one real registry call and one residual operation allowance.
func TestMD087BatchUnusedPrefixAndCleanSuffix(t *testing.T) {
	for _, cleanSuffix := range []bool{false, true} {
		t.Run(fmt.Sprintf("clean-suffix=%v", cleanSuffix), func(t *testing.T) {
			calls := map[string]int{}
			registrations := Registrations()
			for i, reg := range registrations {
				if reg.ID == "MD001" {
					check := reg.Check
					registrations[i].Check = func(ctx context.Context, c engine.RuleContext) engine.RuleOutcome {
						calls[c.Path()]++
						return check(ctx, c)
					}
				}
			}
			r := registry(t, registrations)
			var inputs []engine.Input
			original := map[string][]byte{}
			for i := range 32 {
				path := fmt.Sprintf("a-%02d.md", i)
				content := []byte("<!-- mdchk:disable MD001 -->\n# A\n")
				in, err := engine.NewInput(path, content, md087Settings("MD001"), engine.DocumentContext{Flavor: "commonmark"})
				if err != nil {
					t.Fatal(err)
				}
				inputs = append(inputs, in)
				original[path] = bytes.Clone(content)
			}
			if cleanSuffix {
				for i := range 32 {
					path := fmt.Sprintf("z-%02d.md", i)
					content := []byte("# A\n")
					in, err := engine.NewInput(path, content, md087Settings("MD001"), engine.DocumentContext{Flavor: "commonmark"})
					if err != nil {
						t.Fatal(err)
					}
					inputs = append(inputs, in)
					original[path] = bytes.Clone(content)
				}
			}
			budget, err := parser.NewOperationBudget(parser.DefaultLimits())
			if err != nil {
				t.Fatal(err)
			}
			out, err := r.Lint(context.Background(), inputs, engine.RunOptions{Mode: "project", Budget: budget})
			if err != nil || !out.Complete || len(out.Diagnostics) != 32 || len(out.Errors)+len(out.Interruptions)+len(out.Coverage)+len(out.Omissions)+len(out.Changes)+len(out.Documents)+len(out.DeferredFixes)+len(out.SuppressedDiagnostics) != 0 || len(out.Files) != len(inputs) {
				t.Fatal(out, err)
			}
			for i, d := range out.Diagnostics {
				path := fmt.Sprintf("a-%02d.md", i)
				digest := fmt.Sprintf("%x", sha256.Sum256(original[path]))
				if d.Path != path || d.Rule != "MD087" || d.Message != "Suppression for MD001 hides no findings." || d.FixAvailable || d.ByteRange.Start != 19 || d.ByteRange.End != 24 || [4]int{d.Range.Start.Line, d.Range.Start.Column, d.Range.End.Line, d.Range.End.Column} != [4]int{1, 20, 1, 25} || d.Snapshot.SHA256 != digest || d.Snapshot.SizeBytes != 33 || d.Snapshot.View != "original" {
					t.Fatal(d)
				}
			}
			byPath := map[string]engine.Input{}
			for _, in := range inputs {
				byPath[in.Path()] = in
				if !bytes.Equal(in.Source().Bytes(), original[in.Path()]) || calls[in.Path()] != 1 {
					t.Fatal("changed source or repeated/skipped target", in.Path(), calls)
				}
			}
			if len(out.AnalysisRecords()) != len(inputs) {
				t.Fatal("missing source-local batch records")
			}
			for _, record := range out.AnalysisRecords() {
				a, ok := record.(engine.DirectiveAnalysis)
				if !ok {
					t.Fatal("unexpected analysis record")
				}
				in := byPath[a.SourcePath()]
				rows, err := a.SuppressionDecisions(context.Background(), budget)
				if err != nil {
					t.Fatal(err)
				}
				if strings.HasPrefix(a.SourcePath(), "z-") {
					if len(rows) != 0 {
						t.Fatal("clean suffix gained audit owner", rows)
					}
					continue
				}
				if len(rows) != 1 {
					t.Fatal(rows)
				}
				d := rows[0]
				if d.Owner != 0 || d.Effect != 0 || d.Rule != "MD001" || d.Reason != "suppression-unused" || d.Token.SourceToken() != in.Source().Token() || d.Token.Start() != 19 || d.Token.End() != 24 || d.Interval.Start != 28 || d.Interval.End != 33 || d.Interval.Owner != 0 || d.Interval.Enabled || d.Interval.Token != d.Token {
					t.Fatal(d)
				}
				eof, err := a.State().StateAt("MD001", 33)
				if err != nil || eof.Owner != 0 || eof.Enabled || eof.Token != d.Token {
					t.Fatal("source-local EOF ownership lost", eof, err)
				}
			}
		})
	}
}
