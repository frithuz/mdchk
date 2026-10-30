package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/result"
)

type md087Literal struct {
	Name, Source string
	Settings     json.RawMessage
	Complete     *bool
	Findings     []struct {
		Target string
		Bytes  [2]int
		Range  [4]int
	}
}

func md087Literals(t *testing.T) map[string]md087Literal {
	t.Helper()
	raw, err := os.ReadFile("../../internal/rules/testdata/md087.json")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(raw)) != "f6184989ac6ad5f5e80a884baf5853c843ede3b3ada28c0517c38106088e2d13" {
		t.Fatal("root oracle identity")
	}
	var oracle struct{ Cases []md087Literal }
	if err = json.Unmarshal(raw, &oracle); err != nil {
		t.Fatal(err)
	}
	cases := map[string]md087Literal{}
	for _, tc := range oracle.Cases {
		cases[tc.Name] = tc
	}
	return cases
}
func md087Configuration(flavor, source string) string {
	conf := strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1)
	conf = strings.Replace(conf, "directives: {enabled: false}", "directives: {enabled: true}", 1)
	ids := regexp.MustCompile(`MD[0-9]{3}`).FindAllString(source, -1)
	ids = append(ids, "MD087")
	for _, id := range ids {
		conf = strings.Replace(conf, id+": {enabled: false}", id+": {enabled: true}", 1)
	}
	return conf
}
func TestCompiledPlatformMD087LiteralLint(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	cases := md087Literals(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, name := range []string{
			"single-clean-heading", "one-hidden-jump", "repeated-disable-owns-new-interval", "mixed-order-and-duplicate",
			"eof-terminal-finding-owned", "terminal-clean-lf", "eof-no-heading-finding", "astral-crlf", "blockquote-original-source",
			"self-turns-off-only-after-close", "configured-off-target-incapable", "production-md086-clean", "known-unavailable-mixed",
		} {
			t.Run(flavor+"/"+name, func(t *testing.T) {
				tc, ok := cases[name]
				if !ok {
					t.Fatal("missing literal", name)
				}
				conf := md087Configuration(flavor, tc.Source)
				if name == "configured-off-target-incapable" {
					conf = strings.Replace(conf, "MD001: {enabled: true}", "MD001: {enabled: false}", 1)
				}
				if name == "known-unavailable-mixed" {
					conf = strings.Replace(conf, "  MD011: {enabled: true}\n", "", 1)
				}
				root := configurationProject(t)
				putConfigurationFile(t, root, ".mdchk.yaml", conf)
				putConfigurationFile(t, root, "doc.md", tc.Source)
				before := readinessState(t, root)
				got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
				after := readinessState(t, root)
				readinessAssertEqual(t, "MD087 read-only lint", before, after)
				out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
				complete := true
				if tc.Complete != nil {
					complete = *tc.Complete
				}
				exit := 0
				if !complete {
					exit = 15
				} else if len(tc.Findings) > 0 {
					exit = 10
				}
				if got.Status != exit || out.Complete != complete || len(out.Errors) != 0 || len(out.Diagnostics) != len(tc.Findings) || len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 {
					t.Fatal(got, out)
				}
				for i, d := range out.Diagnostics {
					w := tc.Findings[i]
					if d.Rule != "MD087" || d.Message != "Suppression for "+w.Target+" hides no findings." || d.FixAvailable || [2]int{d.ByteRange.Start, d.ByteRange.End} != w.Bytes || [4]int{d.Range.Start.Line, d.Range.Start.Column, d.Range.End.Line, d.Range.End.Column} != w.Range || d.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Source))) || d.Snapshot.SizeBytes != len(tc.Source) {
						t.Fatal(d, w)
					}
				}
			})
		}
	}
}
func TestCompiledPlatformMD087InspectionOwnersAndPermissions(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	cases := md087Literals(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct{ name, focus string }{
			{"repeated-disable-owns-new-interval", ""}, {"repeated-disable-owns-new-interval", "4-4"}, {"mixed-order-and-duplicate", ""}, {"blocked-disable", ""},
		} {
			t.Run(flavor+"/"+tc.name+"/"+tc.focus, func(t *testing.T) {
				source := cases[tc.name].Source
				conf := md087Configuration(flavor, source)
				root := configurationProject(t)
				if tc.name == "blocked-disable" {
					parent := strings.Replace(conf, "MD001: {enabled: true}", "MD001: {enabled: true, allow_directives: false}", 1)
					putConfigurationFile(t, root, ".mdchk.yaml", parent)
					putConfigurationFile(t, root, "nested/.mdchk.yaml", conf)
					putConfigurationFile(t, root, "nested/doc.md", source)
				} else {
					putConfigurationFile(t, root, ".mdchk.yaml", conf)
					putConfigurationFile(t, root, "doc.md", source)
				}
				before := readinessState(t, root)
				cwd := root
				if tc.name == "blocked-disable" {
					cwd = filepath.Join(root, "nested")
				}
				args := []string{"inspect", "doc.md", "--format=json"}
				if tc.focus != "" {
					args = append(args, "--lines="+tc.focus)
				}
				got, p := runInspection(t, binary, cwd, root, args...)
				readinessAssertEqual(t, "MD087 inspection", before, readinessState(t, root))
				if got.Status != 0 || !got.Result.Complete || p.Directives.Data == nil {
					t.Fatal(got, p)
				}
				ds := *p.Directives.Data
				if tc.name == "blocked-disable" {
					d := ds[0]
					if d.Effects[0].Permission != "denied" || d.Effects[0].Effect != "blocked" || d.Effects[0].Reason != "" || len(d.Effects[0].Provenance) == 0 {
						t.Fatal(d)
					}
				} else if tc.name == "mixed-order-and-duplicate" {
					d := ds[0]
					if len(d.NamedTokens) != 3 || d.Effects[0].Reason != "suppression-unused" || d.Effects[1].Reason != "suppression-used" {
						t.Fatal(d)
					}
				} else {
					if len(ds) != 2 || ds[0].Effects[0].Reason != "suppression-unused" || ds[1].Effects[0].Reason != "suppression-used" {
						t.Fatal(ds)
					}
					if tc.focus != "" && (!ds[0].ContextOnly || !ds[1].ContextOnly) {
						t.Fatal(ds)
					}
				}
			})
		}
	}
}
func TestCompiledPlatformMD087MD047Composition(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	const source = "<!-- mdchk:disable MD001 -->\ntext"
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, dry := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/dry-%v", flavor, dry), func(t *testing.T) {
				root := configurationProject(t)
				conf := strings.Replace(md087Configuration(flavor, source), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
				putConfigurationFile(t, root, ".mdchk.yaml", conf)
				putConfigurationFile(t, root, "doc.md", source)
				for pass := 0; pass < 2; pass++ {
					before := readinessState(t, root)
					args := []string{"fix", "doc.md", "--format=json"}
					if dry {
						args = append(args, "--dry-run")
					}
					got := runConfigurationProcess(t, binary, root, args...)
					after := readinessState(t, root)
					out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "fix")
					if !dry && md038NativeRefusal(t, got, out) {
						readinessAssertEqual(t, "native refusal", before, after)
						return
					}
					changes := 1
					if !dry && pass == 1 {
						changes = 0
					}
					if got.Status != 10 || !out.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != 1 || len(out.Changes) != changes || len(out.Documents)+len(out.DeferredFixes) != 0 {
						t.Fatal(got, out)
					}
					d := out.Diagnostics[0]
					view := "saved"
					if dry {
						view = "candidate"
					}
					if d.Rule != "MD087" || d.FixAvailable || d.Message != "Suppression for MD001 hides no findings." || d.ByteRange != (result.ByteRange{Start: 19, End: 24}) || d.Snapshot.View != view || d.Snapshot.SizeBytes != len(source)+1 || d.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(source+"\n"))) {
						t.Fatal(d)
					}
					for _, change := range out.Changes {
						if len(change.Rules) != 1 || change.Rules[0] != "MD047" {
							t.Fatal(change)
						}
					}
					//nolint:gosec // Fixed test-owned document.
					actual, err := os.ReadFile(filepath.Join(root, "doc.md"))
					want := source
					if !dry {
						want += "\n"
					}
					if err != nil || string(actual) != want {
						t.Fatal(string(actual), err)
					}
					if dry || pass == 1 {
						readinessAssertEqual(t, "stable source/config", before, after)
					}
				}
			})
		}
	}
}
func TestCompiledPlatformMD087NoCGORefusal(t *testing.T) {
	t.Setenv("CGO_ENABLED", "0")
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	source := "<!-- mdchk:disable MD001 -->\ntext"
	conf := strings.Replace(md087Configuration("gfm", source), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
	putConfigurationFile(t, root, ".mdchk.yaml", conf)
	putConfigurationFile(t, root, "doc.md", source)
	before := readinessState(t, root)
	got := runConfigurationProcess(t, binary, root, "fix", "doc.md", "--format=json")
	after := readinessState(t, root)
	out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "fix")
	if !md038NativeRefusal(t, got, out) || !out.Complete || len(out.Errors) != 1 || len(out.Diagnostics)+len(out.CheckedFiles)+len(out.Changes)+len(out.Documents)+len(out.DeferredFixes) != 0 {
		t.Fatal(got, out)
	}
	readinessAssertEqual(t, "actual native early refusal", before, after)
}
func TestCompiledPlatformMD087DefaultsAndAdmission(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	source := "<!-- mdchk:disable MD001 -->\n# A\n"
	for _, setting := range []string{"OMITTED", "{}", "{enabled: true}", "{enabled: false}"} {
		t.Run(setting, func(t *testing.T) {
			root := configurationProject(t)
			conf := md087Configuration("gfm", source)
			if setting == "OMITTED" {
				conf = strings.Replace(conf, "  MD087: {enabled: true}\n", "", 1)
			} else {
				conf = strings.Replace(conf, "MD087: {enabled: true}", "MD087: "+setting, 1)
			}
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", source)
			before := readinessState(t, root)
			got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
			out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
			want, exit := 0, 0
			if setting == "{enabled: true}" {
				want, exit = 1, 10
			}
			if got.Status != exit || !out.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != want {
				t.Fatal(got, out)
			}
			readinessAssertEqual(t, "defaults", before, readinessState(t, root))
		})
	}
	for _, setting := range []string{"null", "[]", "true", "{enabled: null}", "{enabled: 'false'}", "{allow_directives: null}", "{enabled: false, unknown: true}", "{enabled: false, options: {}}"} {
		t.Run("invalid/"+setting, func(t *testing.T) {
			root := configurationProject(t)
			conf := strings.Replace(md087Configuration("gfm", source), "MD087: {enabled: true}", "MD087: "+setting, 1)
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			before := readinessState(t, root)
			got := runConfigurationProcess(t, binary, root, "lint", "missing.md", "--format=json")
			if got.Status != 12 {
				t.Fatal(got)
			}
			readinessAssertEqual(t, "admission precedes acquisition", before, readinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD087FourReporters(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	const source = "<!-- mdchk:disable MD001 -->\n# A\n"
	putConfigurationFile(t, root, ".mdchk.yaml", md087Configuration("gfm", source))
	putConfigurationFile(t, root, "doc.md", source)
	before := readinessState(t, root)
	for _, format := range []string{"text", "json", "vscode", "github"} {
		t.Run(format, func(t *testing.T) {
			got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format="+format)
			if got.Status != 10 || !strings.Contains(got.Stdout, "MD087") || !strings.Contains(got.Stdout, "Suppression for MD001 hides no findings.") {
				t.Fatal(got)
			}
			if format == "json" {
				out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
				if !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].FixAvailable || out.Diagnostics[0].ByteRange != (result.ByteRange{Start: 19, End: 24}) {
					t.Fatal(out)
				}
			}
		})
	}
	readinessAssertEqual(t, "all MD087 reporters read-only", before, readinessState(t, root))
}
