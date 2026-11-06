package rules

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/idna"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/result"
)

type md095Located struct {
	Bytes, Start, End [2]int
	Message           string
}

type md095Literal struct {
	Name, Source string
	Bytes        int    `json:"source_bytes"`
	SHA          string `json:"source_sha256"`
	Flavors      []string
	Options      struct {
		Enabled      *bool
		AllowedHosts []string `json:"allowed_hosts"`
		Frontmatter  string
	}
	Findings   []md095Located
	Problems   []md095Located
	Incomplete string
}

func md095Literals(t *testing.T) []md095Literal {
	t.Helper()
	b, e := os.ReadFile("testdata/md095.json")
	if e != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "20677564569fb578707dfca8c2d4b09a5ad5654acf42ef516892f5180b8d540f" {
		t.Fatal("oracle identity", e)
	}
	var v struct{ Cases []md095Literal }
	if e = json.Unmarshal(b, &v); e != nil || len(v.Cases) != 90 {
		t.Fatal(e, len(v.Cases))
	}
	return v.Cases
}

func md095Settings(enabled *bool, allowed []string) engine.Settings {
	s := onlyDefaultMD038Settings()
	off, on := false, true
	s["MD038"] = engine.RuleSetting{Enabled: &off}
	if enabled == nil {
		enabled = &on
	}
	setting := engine.RuleSetting{Enabled: enabled}
	if allowed != nil {
		setting.Options = engine.Options{"allowed_hosts": allowed}
	}
	s["MD095"] = setting
	return s
}

func md095Matches(t *testing.T, r result.Range, b *result.ByteRange, w md095Located) bool {
	t.Helper()
	return b != nil && b.Start == w.Bytes[0] && b.End == w.Bytes[1] && r.Start == (result.Position{Line: w.Start[0], Column: w.Start[1]}) && r.End == (result.Position{Line: w.End[0], Column: w.End[1]})
}

func TestMD095FrozenProductionOracle(t *testing.T) {
	providers := engine.ValidationProviders{ValidateHostname: idna.ValidateHostname}
	for _, tc := range md095Literals(t) {
		for _, flavor := range tc.Flavors {
			for _, inspect := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/inspect-%v", tc.Name, flavor, inspect), func(t *testing.T) {
					if len(tc.Source) != tc.Bytes || fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) != tc.SHA {
						t.Fatal("source identity")
					}
					dc := engine.DocumentContext{Flavor: flavor}
					if tc.Options.Frontmatter != "" {
						var e error
						dc.Frontmatter, e = parser.NewFrontmatterOptions(true, []string{tc.Options.Frontmatter})
						if e != nil {
							t.Fatal(e)
						}
					}
					selection, e := registry(t, Registrations()).SelectRules(md095Settings(tc.Options.Enabled, tc.Options.AllowedHosts), providers)
					if e != nil {
						t.Fatal(e)
					}
					plan, e := selection.Plan(dc, nil)
					if e != nil {
						t.Fatal(e)
					}
					original := []byte(tc.Source)
					in, e := plan.NewInput("literal.md", original)
					if e != nil {
						t.Fatal(e)
					}
					token := in.Source().Token()
					var out result.Result
					var trace *engine.InspectionTrace
					if inspect {
						out, trace, e = plan.Inspect(context.Background(), in, engine.RunOptions{}, engine.InspectionOptions{})
					} else {
						out, e = plan.Lint(context.Background(), []engine.Input{in}, engine.RunOptions{})
					}
					complete := len(tc.Problems) == 0 && tc.Incomplete == ""
					if e != nil || out.Complete != complete || len(out.Errors)+len(out.Changes)+len(out.Documents)+len(out.DeferredFixes)+len(out.Interruptions)+len(out.Omissions)+len(out.SuppressedDiagnostics) != 0 || string(original) != tc.Source || string(in.Source().Bytes()) != tc.Source || in.Source().Token() != token {
						t.Fatal(out, e)
					}
					if len(out.Files) != 1 || out.Files[0].WriteStatus != "not-requested" || len(out.Files[0].ChangeIDs) != 0 || !reflect.DeepEqual(out.CheckedFiles, []string{"literal.md"}) {
						t.Fatal(out.Files)
					}
					if len(out.Diagnostics) != len(tc.Findings) {
						t.Fatal(out.Diagnostics, tc.Findings)
					}
					for i, d := range out.Diagnostics {
						w := tc.Findings[i]
						if d.Rule != "MD095" || d.Message != w.Message || d.Path != "literal.md" || d.Snapshot != (result.Snapshot{View: "original", SizeBytes: tc.Bytes, SHA256: tc.SHA}) || d.FixAvailable || !md095Matches(t, d.Range, &d.ByteRange, w) {
							t.Fatal(d, w)
						}
					}
					// Each problem is one note and one parser-uncertain coverage
					// entry; unavailable context is one unlocated coverage entry.
					if len(out.Notes) != len(tc.Problems) {
						t.Fatal(out.Notes, tc.Problems)
					}
					for i, raw := range out.Notes {
						var n result.Note
						if e := json.Unmarshal(raw, &n); e != nil {
							t.Fatal(e)
						}
						w := tc.Problems[i]
						if n.Rule != "MD095" || n.Code != "rule-parser-uncertain" || n.Message != w.Message || n.Range == nil || !md095Matches(t, *n.Range, n.ByteRange, w) {
							t.Fatal(n, w)
						}
					}
					wantCoverage := len(tc.Problems)
					if tc.Incomplete != "" {
						wantCoverage = 1
					}
					if len(out.Coverage) != wantCoverage {
						t.Fatal(out.Coverage)
					}
					for i, c := range out.Coverage {
						if c.Rule != "MD095" || tc.Incomplete != "" && (c.Reason != result.ContextUnavailable || c.Message != tc.Incomplete) || tc.Incomplete == "" && (c.Reason != "parser-uncertain" || c.Message != tc.Problems[i].Message) {
							t.Fatal(c)
						}
					}
					status := "clean"
					switch {
					case !complete:
						status = "incomplete"
					case len(tc.Findings) > 0:
						status = "violations"
					}
					if out.Status != status {
						t.Fatal(out.Status)
					}
					if inspect {
						found := false
						for _, ev := range trace.Evaluations() {
							if ev.Rule != "MD095" {
								continue
							}
							found = true
							want := "complete"
							if tc.Options.Enabled != nil && !*tc.Options.Enabled {
								want = "skipped"
							} else if !complete {
								want = "partial"
							}
							if ev.Active != want {
								t.Fatal(ev)
							}
						}
						if !found {
							t.Fatal("evaluation missing")
						}
					}
				})
			}
		}
	}
}

func TestMD095TwinsShareResults(t *testing.T) {
	cases := map[string]md095Literal{}
	for _, tc := range md095Literals(t) {
		cases[tc.Name] = tc
	}
	// Raw Unicode and its A-label flag the same code points.
	flagged := func(name string) string {
		_, items, _ := strings.Cut(cases[name].Findings[0].Message, " contains ")
		return items
	}
	if flagged("punycode") != flagged("punycode-unicode-twin") || flagged("punycode") != flagged("percent-encoded") {
		t.Fatal("A-label, percent-encoded and Unicode twins differ")
	}
	lf, crlf := cases["lf"], cases["crlf"]
	if strings.ReplaceAll(crlf.Source, "\r\n", "\n") != lf.Source || len(lf.Findings) != 2 || len(crlf.Findings) != 2 {
		t.Fatal("line-ending twins")
	}
	for i := range lf.Findings {
		if lf.Findings[i].Start != crlf.Findings[i].Start || lf.Findings[i].End != crlf.Findings[i].End || lf.Findings[i].Message != crlf.Findings[i].Message {
			t.Fatal(lf.Findings[i], crlf.Findings[i])
		}
	}
}
