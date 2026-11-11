package md012_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/frithuz/mdchk/internal/acceptance"
	"github.com/frithuz/mdchk/internal/result"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestMain removes this process's private builds once every test has finished
// with them.
func TestMain(m *testing.M) {
	m.Run()
	acceptance.Cleanup()
}

func md012Configuration(flavor string) string {
	return strings.Replace(strings.Replace(acceptance.CleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1), "MD012: {enabled: false}", "MD012: {enabled: true}", 1)
}
func TestCompiledPlatformMD012ProductionAnalysis(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	data, err := os.ReadFile("testdata/md012.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source, Flavor  string
		Recognize, Incomplete bool
		Fixed                 string
		Maximum               int
		SHA256                string
		SizeBytes             int

		Expected []struct {
			Range   []int
			Source  string
			Message string
		}
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			for _, f := range fixtures {
				if f.Flavor != "" && f.Flavor != flavor {
					continue
				}
				t.Run(f.Name, func(t *testing.T) {
					root := acceptance.Project(t)
					conf := strings.Replace(md012Configuration(flavor), "MD012: {enabled: true}", fmt.Sprintf("MD012: {maximum: %d}", f.Maximum), 1)
					if f.Recognize {
						conf += "frontmatter: {enabled: true}\n"
					}
					acceptance.PutFile(t, root, ".mdchk.yaml", conf)
					acceptance.PutFile(t, root, "doc.md", f.Source)
					if f.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(f.Source))) || f.SizeBytes != len(f.Source) {
						t.Fatal("literal source provenance", f.Name)
					}
					before := acceptance.ConfigurationBytes(t, root)
					for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}} {
						got := acceptance.RunProcess(t, binary, root, args...)
						out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), args[0])
						// A preview that corrects the runs reports the candidate, which
						// every fixture leaves with no finding.
						expected, changes := f.Expected, 0
						if args[0] == "fix" && f.Fixed != f.Source {
							expected, changes = nil, 1
						}
						status := 0
						if len(expected) > 0 {
							status = 10
						}
						if f.Incomplete {
							status = 15
						}
						if got.Status != status || out.Complete == f.Incomplete || len(out.Errors) != 0 || len(out.Diagnostics) != len(expected) || len(out.Changes) != changes || len(out.Documents) != 0 {
							t.Fatal(got)
						}
						if changes == 1 {
							c := out.Changes[0]
							if !reflect.DeepEqual(c.Rules, []string{"MD012"}) || c.Candidate.SHA256 != acceptance.Digest(f.Fixed) || c.Candidate.SizeBytes != len(f.Fixed) {
								t.Fatal(c)
							}
						}
						for i, w := range expected {
							d := out.Diagnostics[i]
							actual := []int{d.ByteRange.Start, d.ByteRange.End, d.Range.Start.Line, d.Range.Start.Column, d.Range.End.Line, d.Range.End.Column}
							if !reflect.DeepEqual(actual, w.Range) || d.Rule != "MD012" || d.FixAvailable || d.Message != w.Message || f.Source[d.ByteRange.Start:d.ByteRange.End] != w.Source {
								t.Fatal(d, w)
							}
						}
					}
					got, p := acceptance.RunInspection(t, binary, root, root, "inspect", "doc.md", "--format=json")
					if got.Status != 0 {
						t.Fatal(got)
					}
					if !f.Incomplete {
						found := false
						if p.Rules.Data != nil {
							for _, r := range *p.Rules.Data {
								if r.Rule == "MD012" {
									found = r.Implementation == "available" && r.ActiveEvaluation == "complete"
								}
							}
						}
						if !found {
							t.Fatal(p.Rules)
						}
					}
					if !reflect.DeepEqual(before, acceptance.ConfigurationBytes(t, root)) {
						t.Fatal("read-only analysis changed bytes")
					}
				})
			}
		})
	}
}

func TestCompiledPlatformMD012DefaultsAndConfiguration(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, setting := range []string{"OMITTED", "{}", "{enabled: true}", "{maximum: 0}", "{maximum: 9223372036854775807}"} {
			t.Run(flavor+"/"+setting, func(t *testing.T) {
				root := acceptance.Project(t)
				conf := md012Configuration(flavor)
				if setting == "OMITTED" {
					conf = strings.Replace(conf, "  MD012: {enabled: true}\n", "", 1)
				} else {
					conf = strings.Replace(conf, "MD012: {enabled: true}", "MD012: "+setting, 1)
				}
				acceptance.PutFile(t, root, ".mdchk.yaml", conf)
				acceptance.PutFile(t, root, "doc.md", "a\n\n\n")
				validated := acceptance.RunProcessEnv(t, binary, root, acceptance.ValidationEnvironment(t), "config", "validate", "--format=json")
				if validated.Status != 0 {
					t.Fatal(validated)
				}
				got := acceptance.RunProcess(t, binary, root, "lint", "doc.md", "--format=json")
				out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "lint")
				want := 1
				if strings.Contains(setting, "9223372036854775807") {
					want = 0
				}
				status := 0
				if want > 0 {
					status = 10
				}
				if got.Status != status || !out.Complete || len(out.Diagnostics) != want || (want > 0 && (out.Diagnostics[0].Rule != "MD012" || out.Diagnostics[0].FixAvailable)) {
					t.Fatal(got)
				}
			})
		}
		for _, option := range []string{"null", "{enabled: false, unknown: true}", "{enabled: null}", "{enabled: false, allow_directives: null}", "{maximum: -2}", "{enabled: false, maximum: null}", "{maximum: '2'}", "{maximum: 2.0}", "{maximum: 2e0}", "{maximum: true}", "{maximum: []}", "{maximum: 9223372036854775808}", "{maximum: 1, maximum: 0}"} {
			t.Run(flavor+"/invalid_"+option, func(t *testing.T) {
				root := acceptance.Project(t)
				acceptance.PutFile(t, root, ".mdchk.yaml", strings.Replace(md012Configuration(flavor), "MD012: {enabled: true}", "MD012: "+option, 1))
				before := acceptance.ConfigurationBytes(t, root)
				got := acceptance.RunProcess(t, binary, root, "lint", "missing.md", "--format=json")
				out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "lint")
				if got.Status != 12 || len(out.Errors) == 0 || len(out.CheckedFiles) != 0 || !reflect.DeepEqual(before, acceptance.ConfigurationBytes(t, root)) {
					t.Fatal(got)
				}
			})
		}
	}
}

func TestCompiledPlatformMD012ActiveControls(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			name, source, setting     string
			active, hidden, directive int
		}{
			{"before", "<!-- mdchk:disable MD012 -->\na\n\n\nb\n", "{}", 0, 1, 0},
			{"late", "a\n\n\n<!-- mdchk:disable MD012 -->\n", "{}", 1, 0, 0},
			{"later-enable", "<!-- mdchk:disable MD012 -->\na\n\n\n<!-- mdchk:enable MD012 -->\nb\n", "{}", 0, 1, 0},
			{"configured-off-enable", "<!-- mdchk:enable MD012 -->\na\n\n\nb\n", "{enabled: false}", 1, 0, 0},
			{"denied-disable", "<!-- mdchk:disable MD012 -->\na\n\n\nb\n", "{allow_directives: false}", 1, 0, 0},
			{"denied-enable", "<!-- mdchk:enable MD012 -->\na\n\n\nb\n", "{enabled: false, allow_directives: false}", 0, 0, 0},
			{"code-span", "`<!-- mdchk:disable MD012 -->`\na\n\n\nb\n", "{}", 1, 0, 0},
			{"code-block", "```\n<!-- mdchk:disable MD012 -->\n```\na\n\n\nb\n", "{}", 1, 0, 0},
			{"raw", "<script>\n<!-- mdchk:disable MD012 -->\n</script>\na\n\n\nb\n", "{}", 1, 0, 0},
			{"malformed", "<!-- mdchk:disable MD012 extra -->\na\n\n\nb\n", "{}", 1, 0, 1},
			{"comment-break", "a\n\n<!-- mdchk:disable MD012\n\n\n-->\n\nb\n", "{}", 0, 0, 0},
		} {
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				root := acceptance.Project(t)
				conf := strings.Replace(md012Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
				conf = strings.Replace(conf, "MD012: {enabled: true}", "MD012: "+tc.setting, 1)
				acceptance.PutFile(t, root, ".mdchk.yaml", conf)
				acceptance.PutFile(t, root, "doc.md", tc.source)
				before := acceptance.ConfigurationBytes(t, root)
				for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}, {"inspect", "doc.md", "--format=json"}} {
					got := acceptance.RunProcess(t, binary, root, args...)
					out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), args[0])
					// A preview corrects every active run and keeps hidden ones hidden.
					active, changes := tc.active, 0
					if args[0] == "fix" && active > 0 {
						active, changes = 0, 1
					}
					status := 0
					if args[0] != "inspect" && active+tc.directive > 0 {
						status = 10
					}
					hidden := tc.hidden
					if got.Status != status || !out.Complete || len(out.Diagnostics) != active+tc.directive || len(out.SuppressedDiagnostics) != hidden || len(out.Changes) != changes || len(out.Documents) != 0 {
						t.Fatal(got)
					}
					for _, d := range out.Diagnostics {
						if d.Rule == "MD012" && (d.FixAvailable || d.ByteRange.Start != d.ByteRange.End || d.Message != "Too many consecutive blank body lines.") {
							t.Fatal(d)
						}
					}
				}
				if !reflect.DeepEqual(before, acceptance.ConfigurationBytes(t, root)) {
					t.Fatal("analysis changed files")
				}
			})
		}
	}
}
func TestCompiledPlatformMD012ActiveMetadata(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, header := range []struct{ name, delimiter string }{{"yaml", "---"}, {"toml", "+++"}} {
			for _, recognize := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/%v", flavor, header.name, recognize), func(t *testing.T) {
					root := acceptance.Project(t)
					conf := strings.Replace(md012Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1) + fmt.Sprintf("frontmatter: {enabled: %v}\n", recognize)
					source := header.delimiter + "\nx: [broken lexical metadata\n<!-- mdchk:disable MD012 -->\n" + header.delimiter + "\na\n\n\nb\n"
					acceptance.PutFile(t, root, ".mdchk.yaml", conf)
					acceptance.PutFile(t, root, "doc.md", source)
					before := acceptance.ConfigurationBytes(t, root)
					for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}, {"inspect", "doc.md", "--format=json"}} {
						got := acceptance.RunProcess(t, binary, root, args...)
						out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), args[0])
						want, changes := 0, 0
						if recognize {
							want = 1
						}
						suppressed := 1 - want
						// A preview corrects the active run after recognized front matter.
						if args[0] == "fix" && want > 0 {
							want, changes = 0, 1
						}
						status := 0
						if args[0] != "inspect" && want > 0 {
							status = 10
						}
						if got.Status != status || !out.Complete || len(out.Diagnostics) != want || len(out.SuppressedDiagnostics) != suppressed || len(out.Changes) != changes || len(out.Documents) != 0 {
							t.Fatal(got)
						}
					}
					if !reflect.DeepEqual(before, acceptance.ConfigurationBytes(t, root)) {
						t.Fatal("changed source")
					}
				})
			}
		}
	}
}
func TestCompiledPlatformMD012InheritedPermissionsAndSlicing(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			name         string
			global, rule bool
			hidden       int
		}{{"allowed", true, true, 1}, {"global", false, true, 0}, {"rule", true, false, 0}} {
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				root := acceptance.Project(t)
				parent := strings.Replace(md012Configuration(flavor), "directives: {enabled: false}", fmt.Sprintf("directives: {enabled: %v}", tc.global), 1)
				parent = strings.Replace(parent, "MD012: {enabled: true}", fmt.Sprintf("MD012: {enabled: true, allow_directives: %v}", tc.rule), 1)
				child := strings.Replace(md012Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
				acceptance.PutFile(t, root, ".mdchk.yaml", parent)
				acceptance.PutFile(t, root, "nested/.mdchk.yaml", child)
				acceptance.PutFile(t, root, "nested/doc.md", "<!-- mdchk:disable MD012 -->\na\n\n\n\nb\n")
				before := acceptance.ConfigurationBytes(t, root)
				got, p := acceptance.RunInspection(t, binary, filepath.Join(root, "nested"), root, "inspect", "doc.md", "--lines=4", "--format=json")
				if got.Status != 0 || len(got.Result.Diagnostics) != 1-tc.hidden || len(got.Result.SuppressedDiagnostics) != tc.hidden || p.File.SelectedLines.Start != 4 {
					t.Fatal(got, p)
				}
				// Hiding the original anchor does not invent another in the selected suffix.
				got, _ = acceptance.RunInspection(t, binary, filepath.Join(root, "nested"), root, "inspect", "doc.md", "--lines=5", "--format=json")
				if got.Status != 0 || len(got.Result.Diagnostics) != 0 || len(got.Result.SuppressedDiagnostics) != 0 {
					t.Fatal(got)
				}
				if !reflect.DeepEqual(before, acceptance.ConfigurationBytes(t, root)) {
					t.Fatal("changed nested source/config")
				}
			})
		}
	}
}
func TestCompiledPlatformMD012ReportersAndReset(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := acceptance.Project(t)
			conf := strings.Replace(md012Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			acceptance.PutFile(t, root, ".mdchk.yaml", conf)
			acceptance.PutFile(t, root, "first.md", "<!-- mdchk:disable MD012 -->\na\n\n\nb\n")
			acceptance.PutFile(t, root, "second.md", "b\n\n\nc\n")
			before := acceptance.ConfigurationBytes(t, root)
			for _, format := range []string{"text", "json", "vscode", "github"} {
				got := acceptance.RunProcess(t, binary, root, "lint", "first.md", "second.md", "--format="+format)
				if got.Status != 10 || !strings.Contains(got.Stdout, "MD012") {
					t.Fatal(got)
				}
				if format == "json" {
					out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "lint")
					if len(out.Diagnostics) != 1 || len(out.SuppressedDiagnostics) != 1 || out.Diagnostics[0].Path != "second.md" {
						t.Fatal(got)
					}
				}
			}
			if !reflect.DeepEqual(before, acceptance.ConfigurationBytes(t, root)) {
				t.Fatal("changed source/config")
			}
		})
	}
}
func TestCompiledPlatformMD012Composition(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, mode := range []string{"ordinary", "container", "code-tail"} {
			for _, ending := range []struct{ name, text string }{{"LF", "\n"}, {"CRLF", "\r\n"}, {"CR", "\r"}} {
				t.Run(flavor+"/"+mode+"/"+ending.name, func(t *testing.T) {
					root := acceptance.Project(t)
					conf := strings.Replace(md012Configuration(flavor), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
					e := ending.text
					original := "a" + e + e + e + "tail"
					start := 1 + 2*len(e)
					column := 1
					if mode == "container" {
						original = "> a" + e + ">" + e + ">" + e + "> tail"
						start = 5 + 2*len(e)
						column = 2
					}
					if mode == "code-tail" {
						original = "    a" + e + e + "    b" + e + e + e + "tail"
						start = 10 + 4*len(e)
					}
					// One preview removes the excess line, prefix included, and adds
					// the final newline; the candidate is then clean.
					first := start - (column - 1)
					candidate := original[:first] + original[start+len(e):] + e
					digest := fmt.Sprintf("%x", sha256.Sum256([]byte(candidate)))
					acceptance.PutFile(t, root, ".mdchk.yaml", conf)
					for pass, source := range []string{original, candidate} {
						acceptance.PutFile(t, root, "doc.md", source)
						before := acceptance.ConfigurationBytes(t, root)
						got := acceptance.RunProcess(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
						out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "fix")
						if got.Status != 0 || !out.Complete || len(out.Diagnostics) != 0 || len(out.Changes) != 1-pass || len(out.Documents) != 0 {
							t.Fatal(got)
						}
						snapshot := out.Files[0].ResultSnapshot
						if snapshot == nil || snapshot.SHA256 != digest || snapshot.SizeBytes != len(candidate) {
							t.Fatal(out.Files)
						}
						if pass == 0 {
							c := out.Changes[0]
							if !reflect.DeepEqual(c.Rules, []string{"MD012", "MD047"}) || c.EditCount != 2 || c.Candidate != *snapshot {
								t.Fatal(c)
							}
						}
						if !reflect.DeepEqual(before, acceptance.ConfigurationBytes(t, root)) {
							t.Fatal("preview changed source")
						}
					}
				})
			}
		}
	}
}

func TestCompiledPlatformMD012Fix(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			name, source, want string
			maximum, remaining int
			hidden             int
			rules              []string
			frontmatter, dirs  bool
		}{
			{name: "maximum-one", source: "One\n\n\n\nTwo\n", want: "One\n\nTwo\n", maximum: 1},
			{name: "maximum-two", source: "a\n\n\n\n\nb\n", want: "a\n\n\nb\n", maximum: 2},
			{name: "maximum-zero-heading", source: "# H\n\n\npara\n", want: "# H\npara\n"},
			{name: "maximum-zero-merge", source: "One\n\nTwo\n", want: "One\n\nTwo\n", remaining: 1},
			{name: "maximum-large", source: "a\n\n\n\n\nb\n", want: "a\n\n\n\n\nb\n", maximum: 1000},
			{name: "crlf", source: "One\r\n\r\n\r\nTwo\r\n", want: "One\r\n\r\nTwo\r\n", maximum: 1},
			{name: "cr", source: "One\r\r\r\rTwo\r", want: "One\r\rTwo\r", maximum: 1},
			{name: "mixed", source: "a\r\n\n\r\r\nb\r\n\r\n\n\nc\n", want: "a\r\n\nb\r\n\r\nc\n", maximum: 1},
			{name: "non-ascii", source: "héllo 日本\n\n\n\nwörld ✓\n", want: "héllo 日本\n\nwörld ✓\n", maximum: 1},
			{name: "whitespace-only", source: "a\n \n\t\n  \nb\n", want: "a\n \nb\n", maximum: 1},
			{name: "quote", source: "> a\n>\n>\n> b\n", want: "> a\n>\n> b\n", maximum: 1},
			{name: "quote-prefix-mismatch", source: "> a\n>\n>\n\n\nb\n", want: "> a\n>\n>\n\n\nb\n", maximum: 1, remaining: 1},
			{name: "list-loose", source: "- a\n\n\n\n- b\n", want: "- a\n\n- b\n", maximum: 1},
			{name: "list-tightness", source: "- a\n\n\n- b\n", want: "- a\n\n\n- b\n", remaining: 1},
			{name: "list-boundary", source: "- a\n\n\n  b\n", want: "- a\n\n\n  b\n", remaining: 1},
			{name: "lazy", source: "> a\nb\n\n\n\nc\n", want: "> a\nb\n\nc\n", maximum: 1},
			{name: "fenced", source: "```\n\n\n\n```\n\n\n\nend\n", want: "```\n\n\n\n```\n\nend\n", maximum: 1},
			{name: "indented", source: "    a\n\n\n\n    b\n\n\nend\n", want: "    a\n\n\n\n    b\n\nend\n", maximum: 1},
			{name: "html", source: "<script>\n\n\n\n</script>\n\n\n\nend\n", want: "<script>\n\n\n\n</script>\n\nend\n", maximum: 1},
			{name: "comment", source: "<!--\n\n\n\n-->\n\n\n\nend\n", want: "<!--\n\n\n\n-->\n\nend\n", maximum: 1},
			{name: "front-matter", source: "---\nx: 1\n\n\n\n---\n\n\n\nend\n", want: "---\nx: 1\n\n\n\n---\n\nend\n", maximum: 1, frontmatter: true},
			{name: "directive-disabled", source: "<!-- mdchk:disable MD012 -->\na\n\n\nb\n", want: "<!-- mdchk:disable MD012 -->\na\n\n\nb\n", maximum: 1, hidden: 1, dirs: true},
			{name: "directive-enabled-after", source: "<!-- mdchk:disable MD012 -->\na\n\n\n<!-- mdchk:enable MD012 -->\nb\n\n\nc\n", want: "<!-- mdchk:disable MD012 -->\na\n\n\n<!-- mdchk:enable MD012 -->\nb\n\nc\n", maximum: 1, hidden: 1, dirs: true},
			{name: "new-md022", source: "# H\n\n\npara\n", want: "# H\n\n\npara\n", remaining: 1, rules: []string{"MD022"}},
			{name: "new-md031", source: "x\n\n\n```\ncode\n```\n", want: "x\n\n\n```\ncode\n```\n", remaining: 1, rules: []string{"MD031"}},
			{name: "new-md032", source: "para\n\n\n- a\n", want: "para\n\n\n- a\n", remaining: 1, rules: []string{"MD032"}},
			{name: "md009-md047", source: "a \n\n  \n\n\nb", want: "a\n\nb\n", maximum: 1, rules: []string{"MD009", "MD047"}},
		} {
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				root := acceptance.Project(t)
				conf := strings.Replace(md012Configuration(flavor), "MD012: {enabled: true}", fmt.Sprintf("MD012: {maximum: %d}", tc.maximum), 1)
				for _, rule := range tc.rules {
					conf = strings.Replace(conf, rule+": {enabled: false}", rule+": {enabled: true}", 1)
				}
				if tc.frontmatter {
					conf += "frontmatter: {enabled: true}\n"
				}
				if tc.dirs {
					conf = strings.Replace(conf, "directives: {enabled: false}", "directives: {enabled: true}", 1)
				}
				acceptance.PutFile(t, root, ".mdchk.yaml", conf)
				acceptance.PutFile(t, root, "doc.md", tc.source)
				status, changes := 0, 0
				if tc.remaining > 0 {
					status = 10
				}
				if tc.want != tc.source {
					changes = 1
				}
				check := func(args ...string) result.Result {
					got := acceptance.RunProcess(t, binary, root, args...)
					out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "fix")
					if got.Status != status || !out.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != tc.remaining || len(out.SuppressedDiagnostics) != tc.hidden || len(out.Changes) != changes {
						t.Fatal(got)
					}
					for _, d := range out.Diagnostics {
						if d.Rule != "MD012" || d.FixAvailable {
							t.Fatal(d)
						}
					}
					for _, raw := range out.SuppressedDiagnostics {
						var hidden struct{ Diagnostic result.Diagnostic }
						if err := json.Unmarshal(raw, &hidden); err != nil || hidden.Diagnostic.Rule != "MD012" || hidden.Diagnostic.FixAvailable {
							t.Fatal(string(raw))
						}
					}
					if changes == 1 && (out.Changes[0].Candidate.SHA256 != acceptance.Digest(tc.want) || out.Changes[0].Candidate.SizeBytes != len(tc.want)) {
						t.Fatal(out.Changes[0])
					}
					return out
				}
				check("fix", "--dry-run", "doc.md", "--format=json")
				if acceptance.Saved(t, root) != tc.source {
					t.Fatal("preview changed source")
				}
				check("fix", "doc.md", "--format=json")
				if saved := acceptance.Saved(t, root); saved != tc.want {
					t.Fatalf("saved %q, want %q", saved, tc.want)
				}
				// A second run on the output changes nothing.
				changes = 0
				check("fix", "doc.md", "--format=json")
				if acceptance.Saved(t, root) != tc.want {
					t.Fatal("second run changed source")
				}
			})
		}
	}
}

func TestCompiledPlatformMD012FixPreview(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	root := acceptance.Project(t)
	acceptance.PutFile(t, root, ".mdchk.yaml", strings.Replace(md012Configuration("commonmark"), "MD012: {enabled: true}", "MD012: {maximum: 1}", 1))
	source := "One\n\n\n\nTwo\n"
	acceptance.PutFile(t, root, "doc.md", source)
	got := acceptance.RunProcess(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
	out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "fix")
	if got.Status != 0 || len(out.Changes) != 1 || acceptance.Saved(t, root) != source {
		t.Fatal(got)
	}
	// Lines 3 and 4 are removed whole, each with its own LF; nothing is added.
	c := out.Changes[0]
	want := []result.DiffLine{{Kind: "context", Text: "One", LineEnding: "lf"}, {Kind: "context", Text: "", LineEnding: "lf"}, {Kind: "remove", Text: "", LineEnding: "lf"}, {Kind: "remove", Text: "", LineEnding: "lf"}, {Kind: "context", Text: "Two", LineEnding: "lf"}}
	if !reflect.DeepEqual(c.Rules, []string{"MD012"}) || c.EditCount != 2 || c.Diff == nil || len(c.Diff.Hunks) != 1 || c.Diff.Hunks[0].OldStart != 1 || c.Diff.Hunks[0].OldLines != 5 || c.Diff.Hunks[0].NewLines != 3 || !reflect.DeepEqual(c.Diff.Hunks[0].Lines, want) {
		t.Fatal(c)
	}
}
