package md009_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/acceptance"
	"github.com/frithuz/mdchk/internal/result"
)

// TestMain removes this process's private builds once every test has finished
// with them.
func TestMain(m *testing.M) {
	m.Run()
	acceptance.Cleanup()
}

func md009Configuration(flavor string) string {
	return strings.Replace(strings.Replace(acceptance.CleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1), "MD009: {enabled: false}", "MD009: {enabled: true}", 1)
}
func TestCompiledPlatformMD009ProductionAnalysis(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	data, err := os.ReadFile("testdata/md009.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Source, Flavor, Style               string
		Recognize, Incomplete, Strict, CodeBlocks bool
		Spaces                                    int
		SHA256                                    string
		SizeBytes                                 int

		Expected []struct {
			Range           []int
			Source          string
			Message         string
			FixAvailable    bool
			FixAvailableGFM *bool
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
					conf := strings.Replace(md009Configuration(flavor), "MD009: {enabled: true}", fmt.Sprintf("MD009: {hard_break_spaces: %d, strict: %v, code_blocks: %v}", f.Spaces, f.Strict, f.CodeBlocks), 1)
					if f.Recognize {
						conf += "frontmatter: {enabled: true}\n"
					}
					acceptance.PutFile(t, root, ".mdchk.yaml", conf)
					acceptance.PutFile(t, root, "doc.md", f.Source)
					if f.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(f.Source))) || f.SizeBytes != len(f.Source) {
						t.Fatal("literal source provenance", f.Name)
					}
					before := acceptance.ConfigurationBytes(t, root)
					got := acceptance.RunProcess(t, binary, root, "lint", "doc.md", "--format=json")
					out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "lint")
					status := 0
					if len(f.Expected) > 0 {
						status = 10
					}
					if f.Incomplete {
						status = 15
					}
					if got.Status != status || out.Complete == f.Incomplete || len(out.Errors) != 0 || len(out.Diagnostics) != len(f.Expected) || len(out.Changes) != 0 || len(out.Documents) != 0 {
						t.Fatal(got)
					}
					manual := 0
					for i, w := range f.Expected {
						d := out.Diagnostics[i]
						actual := []int{d.ByteRange.Start, d.ByteRange.End, d.Range.Start.Line, d.Range.Start.Column, d.Range.End.Line, d.Range.End.Column}
						fixAvailable := w.FixAvailable
						if flavor == "gfm" && w.FixAvailableGFM != nil {
							fixAvailable = *w.FixAvailableGFM
						}
						if !fixAvailable {
							manual++
						}
						if !reflect.DeepEqual(actual, w.Range) || d.Rule != "MD009" || d.FixAvailable != fixAvailable || d.Message != w.Message || f.Source[d.ByteRange.Start:d.ByteRange.End] != w.Source {
							t.Fatal(d, w)
						}
					}
					// A preview leaves only the findings lint reported as manual.
					got = acceptance.RunProcess(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
					out = acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "fix")
					status = 0
					if manual > 0 {
						status = 10
					}
					if f.Incomplete {
						status = 15
					}
					changes := 0
					if len(f.Expected) > manual {
						changes = 1
					}
					if got.Status != status || out.Complete == f.Incomplete || len(out.Errors) != 0 || len(out.Diagnostics) != manual || len(out.Changes) != changes || len(out.Documents) != 0 {
						t.Fatal(got)
					}
					got, p := acceptance.RunInspection(t, binary, root, root, "inspect", "doc.md", "--format=json")
					if got.Status != 0 {
						t.Fatal(got)
					}
					if !f.Incomplete {
						found := false
						if p.Rules.Data != nil {
							for _, r := range *p.Rules.Data {
								if r.Rule == "MD009" {
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

func TestCompiledPlatformMD009DefaultsAndConfiguration(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, setting := range []string{"OMITTED", "{}", "{enabled: true}", "{hard_break_spaces: 4, strict: true, code_blocks: true}", "{hard_break_spaces: 9223372036854775807}"} {
			t.Run(flavor+"/"+setting, func(t *testing.T) {
				root := acceptance.Project(t)
				conf := md009Configuration(flavor)
				if setting == "OMITTED" {
					conf = strings.Replace(conf, "  MD009: {enabled: true}\n", "", 1)
				} else {
					conf = strings.Replace(conf, "MD009: {enabled: true}", "MD009: "+setting, 1)
				}
				acceptance.PutFile(t, root, ".mdchk.yaml", conf)
				acceptance.PutFile(t, root, "doc.md", "a \n")
				validated := acceptance.RunProcessEnv(t, binary, root, acceptance.ValidationEnvironment(t), "config", "validate", "--format=json")
				if validated.Status != 0 {
					t.Fatal(validated)
				}
				got := acceptance.RunProcess(t, binary, root, "lint", "doc.md", "--format=json")
				out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "lint")
				if got.Status != 10 || !out.Complete || len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD009" || !out.Diagnostics[0].FixAvailable {
					t.Fatal(got)
				}
			})
		}
		for _, option := range []string{"null", "{enabled: false, unknown: true}", "{enabled: null}", "{enabled: false, allow_directives: null}", "{hard_break_spaces: 0}", "{hard_break_spaces: 1}", "{hard_break_spaces: -2}", "{enabled: false, hard_break_spaces: null}", "{hard_break_spaces: '2'}", "{hard_break_spaces: 2.0}", "{hard_break_spaces: 2e0}", "{hard_break_spaces: true}", "{hard_break_spaces: []}", "{strict: null}", "{strict: 'true'}", "{strict: 1}", "{code_blocks: null}", "{code_blocks: 'true'}", "{code_blocks: 1}"} {
			t.Run(flavor+"/invalid_"+option, func(t *testing.T) {
				root := acceptance.Project(t)
				acceptance.PutFile(t, root, ".mdchk.yaml", strings.Replace(md009Configuration(flavor), "MD009: {enabled: true}", "MD009: "+option, 1))
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

func TestCompiledPlatformMD009DirectivesAndValidation(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	source := "<!-- mdchk:disable MD009 -->\na \nb <!-- mdchk:enable MD009 --> \nz"
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			name, setting  string
			active, hidden int
		}{{"hidden before", "{enabled: true}", 1, 1}, {"configured off", "{enabled: false}", 1, 1}, {"denied disable", "{enabled: true, allow_directives: false}", 2, 0}, {"denied enable", "{enabled: false, allow_directives: false}", 0, 0}, {"empty mapping", "{}", 1, 1}} {
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				root := acceptance.Project(t)
				conf := strings.Replace(md009Configuration(flavor), "MD009: {enabled: true}", "MD009: "+tc.setting, 1)
				conf = strings.Replace(conf, "directives: {enabled: false}", "directives: {enabled: true}", 1)
				acceptance.PutFile(t, root, ".mdchk.yaml", conf)
				acceptance.PutFile(t, root, "doc.md", source)
				before := acceptance.ConfigurationBytes(t, root)
				validated := acceptance.RunProcessEnv(t, binary, root, acceptance.ValidationEnvironment(t), "config", "validate", "--format=json")
				if validated.Status != 0 {
					t.Fatal(validated)
				}
				got := acceptance.RunProcess(t, binary, root, "lint", "doc.md", "--format=json")
				out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "lint")
				status := 0
				if tc.active > 0 {
					status = 10
				}
				if got.Status != status || !out.Complete || len(out.Diagnostics) != tc.active || len(out.SuppressedDiagnostics) != tc.hidden || len(out.Changes) != 0 || len(out.Documents) != 0 {
					t.Fatal(got)
				}
				if tc.active == 1 && out.Diagnostics[0].ByteRange != (result.ByteRange{Start: len(source) - 3, End: len(source) - 2}) {
					t.Fatal(out.Diagnostics)
				}
				// A preview corrects every active finding and keeps hidden ones hidden.
				changes := 0
				if tc.active > 0 {
					changes = 1
				}
				got = acceptance.RunProcess(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
				out = acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "fix")
				if got.Status != 0 || !out.Complete || len(out.Diagnostics) != 0 || len(out.SuppressedDiagnostics) != tc.hidden || len(out.Changes) != changes || len(out.Documents) != 0 {
					t.Fatal(got)
				}
				if !reflect.DeepEqual(before, acceptance.ConfigurationBytes(t, root)) {
					t.Fatal("analysis changed bytes")
				}
			})
		}
	}
}

func TestCompiledPlatformMD009InheritedDirectivePermissions(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range []struct {
			name         string
			global, rule bool
			hidden       int
		}{{"allowed", true, true, 2}, {"global denial", false, true, 0}, {"rule denial", true, false, 0}} {
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				root := acceptance.Project(t)
				parent := strings.Replace(md009Configuration(flavor), "directives: {enabled: false}", fmt.Sprintf("directives: {enabled: %v}", tc.global), 1)
				parent = strings.Replace(parent, "MD009: {enabled: true}", fmt.Sprintf("MD009: {enabled: true, allow_directives: %v}", tc.rule), 1)
				child := strings.Replace(md009Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
				child = strings.Replace(child, "MD009: {enabled: true}", "MD009: {enabled: true, allow_directives: true}", 1)
				acceptance.PutFile(t, root, ".mdchk.yaml", parent)
				acceptance.PutFile(t, root, "nested/.mdchk.yaml", child)
				acceptance.PutFile(t, root, "nested/doc.md", "<!-- mdchk:disable MD009 -->\na \nb \nz")
				before := acceptance.ConfigurationBytes(t, root)
				got, p := acceptance.RunInspection(t, binary, filepath.Join(root, "nested"), root, "inspect", "doc.md", "--format=json")
				if got.Status != 0 || len(got.Result.SuppressedDiagnostics) != tc.hidden || len(got.Result.Diagnostics) != 2-tc.hidden {
					t.Fatal(got, p)
				}
				if !reflect.DeepEqual(before, acceptance.ConfigurationBytes(t, root)) {
					t.Fatal("analysis changed files")
				}
			})
		}
	}
}

func TestCompiledPlatformMD009InspectionRetainsHiddenParent(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		t.Run(flavor, func(t *testing.T) {
			root := acceptance.Project(t)
			conf := strings.Replace(md009Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1)
			source := "<!-- mdchk:disable MD009 -->\na \nb <!-- mdchk:enable MD009 --> \nz \n"
			acceptance.PutFile(t, root, ".mdchk.yaml", conf)
			acceptance.PutFile(t, root, "doc.md", source)
			before := acceptance.ConfigurationBytes(t, root)
			got, p := acceptance.RunInspection(t, binary, root, root, "inspect", "doc.md", "--lines=4", "--format=json")
			if got.Status != 0 || !got.Result.Complete || len(got.Result.Diagnostics) != 1 || len(got.Result.SuppressedDiagnostics) != 0 || p.File.SelectedLines.Start != 4 || p.AST.Data.Root.ByteRange.Start != 0 || p.AST.Data.Root.ByteRange.End != len(source) {
				t.Fatal(got, p)
			}
			d := got.Result.Diagnostics[0]
			if d.Rule != "MD009" || d.Message != "Whitespace at line end." || d.ByteRange.Start != len(source)-2 || d.ByteRange.End != len(source)-1 || !d.FixAvailable {
				t.Fatal(d)
			}
			if !reflect.DeepEqual(before, acceptance.ConfigurationBytes(t, root)) {
				t.Fatal("inspection changed source")
			}
		})
	}
}

func TestCompiledPlatformMD009Composition(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, mode := range []string{"ordinary", "code", "structural"} {
			for _, ending := range []struct{ name, text string }{{"LF", "\n"}, {"CRLF", "\r\n"}, {"CR", "\r"}} {
				t.Run(flavor+"/"+mode+"/"+ending.name, func(t *testing.T) {
					root := acceptance.Project(t)
					conf := strings.Replace(md009Configuration(flavor), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
					original := "雪 " + ending.text + "tail"
					candidate := "雪" + ending.text + "tail" + ending.text
					rules := []string{"MD009", "MD047"}
					remaining, edits, status := 0, 2, 0
					start, end, line, column := 0, 0, 0, 2
					if mode == "code" {
						// Literal code stays manual, so MD047 is the only fixer here.
						original = "```" + ending.text + "雪 " + ending.text + "```" + ending.text + "tail"
						candidate = original + ending.text
						rules = []string{"MD047"}
						remaining, edits, status = 1, 1, 10
						start = 6 + len(ending.text)
						end = start + 1
						line = 2
						conf = strings.Replace(conf, "MD009: {enabled: true}", "MD009: {code_blocks: true}", 1)
					}
					if mode == "structural" {
						original = "- 雪" + ending.text + "  " + ending.text + "  tail"
						candidate = "- 雪" + ending.text + ending.text + "  tail" + ending.text
					}
					digest := fmt.Sprintf("%x", sha256.Sum256([]byte(candidate)))
					acceptance.PutFile(t, root, ".mdchk.yaml", conf)
					for pass, source := range []string{original, candidate} {
						acceptance.PutFile(t, root, "doc.md", source)
						before := acceptance.ConfigurationBytes(t, root)
						got := acceptance.RunProcess(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
						out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "fix")
						if got.Status != status || !out.Complete || len(out.Diagnostics) != remaining || len(out.Changes) != 1-pass || len(out.Documents) != 0 {
							t.Fatal(got)
						}
						snapshot := out.Files[0].ResultSnapshot
						if snapshot == nil || snapshot.SHA256 != digest || snapshot.SizeBytes != len(candidate) {
							t.Fatal(out.Files)
						}
						if remaining == 1 {
							d := out.Diagnostics[0]
							if d.Rule != "MD009" || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: start, End: end}) || d.Range.Start != (result.Position{Line: line, Column: column}) || d.Range.End != (result.Position{Line: line, Column: column + end - start}) || d.Snapshot != *snapshot {
								t.Fatal(d, start, end)
							}
						}
						if pass == 0 {
							c := out.Changes[0]
							if !reflect.DeepEqual(c.Rules, rules) || c.EditCount != edits || c.Candidate != *snapshot {
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

// md009FixCase pins one source through lint, preview, saved mode and a second run.
// Manual lists the bytes that select findings left reported after fix, and
// Deferred their expected reasons in source order.
type md009FixCase struct {
	name, source, want string
	option             string // replaces the default MD009 setting
	md047, directives  bool
	frontmatter        bool
	flavor             string // restricts the case to one flavor
	manual             []int
	deferred           []string
	rules              []string
}

func (tc md009FixCase) configuration(flavor string) string {
	conf := md009Configuration(flavor)
	if tc.option != "" {
		conf = strings.Replace(conf, "MD009: {enabled: true}", "MD009: "+tc.option, 1)
	}
	if tc.md047 {
		conf = strings.Replace(conf, "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
	}
	if tc.directives {
		conf = strings.Replace(conf, "directives: {enabled: false}", "directives: {enabled: true}", 1)
	}
	if tc.frontmatter {
		conf += "frontmatter: {enabled: true}\n"
	}
	return conf
}

func md009FixCases() []md009FixCase {
	unproven := []string{"meaning-not-proven"}
	both := []string{"MD009", "MD047"}
	return []md009FixCase{
		// Criterion 1: removal.
		{name: "removal", source: "a \n\nb\n", want: "a\n\nb\n"},
		// Criterion 2: non-break suffixes.
		{name: "paragraph end", source: "a   \n\nb\n", want: "a\n\nb\n"},
		{name: "soft break", source: "a  \t\nb\n", want: "a\nb\n"},
		{name: "tab after delimiter", source: "*a*\t\nb\n", want: "*a*\nb\n"},
		// Criterion 3: hard-break normalization.
		{name: "long hard break", source: "a   \nb\n", want: "a  \nb\n"},
		{name: "mixed hard break", source: "a\t  \nb\n", want: "a  \nb\n"},
		// Criterion 4: hard_break_spaces.
		{name: "hard three grows", source: "a  \nb\n", want: "a   \nb\n", option: "{hard_break_spaces: 3}"},
		{name: "hard three shrinks", source: "a    \nb\n", want: "a   \nb\n", option: "{hard_break_spaces: 3}"},
		// Expansion limit: a hard break can't grow past 8 spaces, and can always shrink.
		{name: "grow to limit", source: "a  \nb\n", want: "a        \nb\n", option: "{hard_break_spaces: 8}"},
		{name: "grow past limit", source: "c \n\na  \nb\n", want: "c\n\na  \nb\n", option: "{hard_break_spaces: 9}", manual: []int{4}, deferred: []string{"expansion-limit"}},
		{name: "shrink past limit", source: "a          \nb\n", want: "a         \nb\n", option: "{hard_break_spaces: 9}"},
		{name: "huge option", source: "a  \nb\n", want: "a  \nb\n", option: "{hard_break_spaces: 300000000}", manual: []int{1}, deferred: []string{"expansion-limit"}},
		// Criterion 5: strict.
		{name: "strict removes", source: "a  \n\nb\n", want: "a\n\nb\n", option: "{strict: true}"},
		{name: "strict keeps break", source: "a  \nb\n", want: "a  \nb\n", option: "{strict: true}"},
		{name: "default keeps exact", source: "a  \n\nb\n", want: "a  \n\nb\n"},
		// Criterion 6: code_blocks.
		{name: "code opt-in", source: "``` \ncode \n``` \n", want: "```\ncode \n```\n", option: "{code_blocks: true}", manual: []int{8}},
		{name: "code default", source: "``` \ncode \n``` \n", want: "```\ncode \n```\n"},
		// Criterion 7: line endings.
		{name: "endings", source: "a \r\nb \rc \n", want: "a\r\nb\rc\n"},
		{name: "crlf break", source: "a   \r\nb\r\n", want: "a  \r\nb\r\n"},
		// Criterion 8: Unicode.
		{name: "scalar", source: "雪 \n", want: "雪\n"},
		{name: "no-break space", source: "a  \n", want: "a \n"},
		{name: "ideographic space", source: "a　\n", want: "a　\n"},
		// Criterion 9: containers.
		{name: "bare quote", source: "> \n", want: ">\n"},
		{name: "quote break", source: "> a   \n> b\n", want: "> a  \n> b\n"},
		{name: "item blank", source: "- a\n  \n  b\n", want: "- a\n\n  b\n"},
		{name: "quoted item blank", source: "> - a\n>   \n>   b\n", want: "> - a\n>\n>   b\n"},
		// Criterion 10: protected content.
		{name: "frontmatter", source: "---\nt: x \n---\n\nb\n", want: "---\nt: x \n---\n\nb\n", frontmatter: true},
		{name: "html block", source: "<div>\nx \n</div>\n", want: "<div>\nx \n</div>\n"},
		{name: "inline html", source: "a <span \nb>c</span>\n", want: "a <span \nb>c</span>\n"},
		{name: "raw tag", source: "a <script>x  \ny</script> \nz\n", want: "a <script>x  \ny</script>\nz\n"},
		// Criterion 11: directives.
		{name: "directive", source: "<!-- mdchk:disable MD009 -->\na \n<!-- mdchk:enable MD009 -->\nb \n", want: "<!-- mdchk:disable MD009 -->\na \n<!-- mdchk:enable MD009 -->\nb\n", directives: true},
		// Criterion 12: titles.
		{name: "inline title", source: "[a](b \"t \nu\")\n", want: "[a](b \"t \nu\")\n", manual: []int{8}, deferred: unproven},
		{name: "definition title", source: "[a]: b \"t \nu\"\n", want: "[a]: b \"t \nu\"\n", manual: []int{9}, deferred: unproven},
		{name: "image title", source: "![a](b \"t \nu\")\n", want: "![a](b \"t \nu\")\n", manual: []int{9}, deferred: unproven},
		{name: "title beside correction", source: "c \n\n[a](b \"t \nu\")\n", want: "c\n\n[a](b \"t \nu\")\n", manual: []int{11}, deferred: unproven},
		// Criterion 13: other link positions. Two spaces are permitted unless strict.
		{name: "link text", source: "[a \nb](c)\n", want: "[a\nb](c)\n"},
		{name: "before title", source: "[a](b  \n\"t\")\n", want: "[a](b\n\"t\")\n", option: "{strict: true}"},
		{name: "before title one space", source: "[a](b \n\"t\")\n", want: "[a](b\n\"t\")\n"},
		{name: "reference label", source: "[a \nb]: c\n\n[a b]\n", want: "[a\nb]: c\n\n[a b]\n"},
		// Criterion 14: backslashes.
		{name: "odd backslash", source: "a\\ \nb\n", want: "a\\ \nb\n", manual: []int{2}, deferred: unproven},
		{name: "even backslash", source: "a\\\\ \nb\n", want: "a\\\\\nb\n"},
		{name: "backslash break", source: "a\\   \nb\n", want: "a\\  \nb\n"},
		// Criterion 15: GFM tasks, MD009 only.
		{name: "task separator", source: "- [ ] \n- x\n", want: "- [ ]\n- x\n"},
		{name: "eof task separator", flavor: "gfm", source: "- [x] ", want: "- [x] ", manual: []int{5}, deferred: unproven},
		{name: "eof literal task", flavor: "commonmark", source: "- [x] ", want: "- [x]"},
		{name: "task continuation", flavor: "gfm", source: "b \n\n- [ ] \n  a\n", want: "b\n\n- [ ] \n  a\n", manual: []int{8}, deferred: unproven},
		{name: "literal task continuation", flavor: "commonmark", source: "- [ ] \n  a\n", want: "- [ ]\n  a\n"},
		// Criterion 16: MD009 wins its EOF conflict with MD047, whose newline follows.
		{name: "conflict", source: "foo   ", want: "foo\n", md047: true, rules: both},
		{name: "conflict blank line", source: "foo\n  ", want: "foo\n", md047: true},
		{name: "conflict beside correction", source: "a \nfoo ", want: "a\nfoo\n", md047: true, rules: both},
		{name: "conflict strict", source: "foo  ", want: "foo\n", md047: true, option: "{strict: true}", rules: both},
		{name: "no conflict exact", source: "foo  ", want: "foo  \n", md047: true, rules: []string{"MD047"}},
		// Criterion 17: both fixers together.
		{name: "both", source: "a \nb", want: "a\nb\n", md047: true, rules: both},
		{name: "exposed newline", source: "a\n \n", want: "a\n", md047: true, rules: both},
		{name: "both tasks", source: "- [ ] a \n- [x] b", want: "- [ ] a\n- [x] b\n", md047: true, rules: both},
		{name: "both task separator", source: "- [ ] \n- [x] b", want: "- [ ]\n- [x] b\n", md047: true, rules: both},
		{name: "task continuation both", source: "- [x] a \nb", want: "- [x] a\nb\n", md047: true, rules: both},
		{name: "task indented continuation both", source: "- [x] a \n  b", want: "- [x] a\n  b\n", md047: true, rules: both},
		{name: "task break both", source: "- [x] a   \n  b", want: "- [x] a  \n  b\n", md047: true, rules: both},
		// Criterion 18: MD047 off or suppressed.
		{name: "md047 off", source: "foo ", want: "foo"},
		{name: "md047 suppressed", source: "<!-- mdchk:disable MD047 -->\nfoo ", want: "<!-- mdchk:disable MD047 -->\nfoo", md047: true, directives: true},
	}
}

type md009Deferred struct {
	Rule, Reason, Group string
	Line, Column        int
}

func md009DeferredRecords(records []result.DeferredFix) []md009Deferred {
	out := []md009Deferred{}
	for _, r := range records {
		_, group, _ := strings.Cut(r.GroupID, ":")
		out = append(out, md009Deferred{r.Rule, r.Reason, group, r.Range.Start.Line, r.Range.Start.Column})
	}
	return out
}

func md009Located(diagnostics []result.Diagnostic) []string {
	out := []string{}
	for _, d := range diagnostics {
		out = append(out, fmt.Sprint(d.Rule, d.ByteRange, d.FixAvailable))
	}
	return out
}

func TestCompiledMD009FixCorrections(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range md009FixCases() {
			if tc.flavor != "" && tc.flavor != flavor {
				continue
			}
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				root := acceptance.Project(t)
				acceptance.PutFile(t, root, ".mdchk.yaml", tc.configuration(flavor))
				acceptance.PutFile(t, root, "doc.md", tc.source)
				path := filepath.Join(root, "doc.md")
				status := 0
				if len(tc.manual) > 0 {
					status = 10
				}
				rules := tc.rules
				if rules == nil && tc.source != tc.want {
					rules = []string{"MD009"}
				}
				digest := fmt.Sprintf("%x", sha256.Sum256([]byte(tc.want)))

				preview := acceptance.RunFixDryRun(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
				if preview.Status != status || !preview.Result.Complete {
					t.Fatalf("preview status %d want %d: %s", preview.Status, status, preview.Stdout)
				}
				if got := preview.Result.Files[0].ResultSnapshot; got == nil || got.SHA256 != digest {
					t.Fatalf("preview candidate differs from %q: %s", tc.want, preview.Stdout)
				}
				//nolint:gosec // Access only synthetic test fixtures.
				if kept, err := os.ReadFile(path); err != nil || string(kept) != tc.source {
					t.Fatalf("preview changed the file to %q: %v", kept, err)
				}

				first := acceptance.RunFix(t, binary, root, "fix", "doc.md", "--format=json")
				//nolint:gosec // Access only synthetic test fixtures.
				saved, err := os.ReadFile(path)
				if err != nil || string(saved) != tc.want {
					t.Fatalf("saved %q want %q: %v %s", saved, tc.want, err, first.Stdout)
				}
				if first.Status != status || !first.Result.Complete {
					t.Fatalf("status %d want %d: %s", first.Status, status, first.Stdout)
				}
				if tc.source == tc.want {
					if len(first.Result.Changes) != 0 {
						t.Fatal("unexpected change", first.Stdout)
					}
				} else if len(first.Result.Changes) != 1 || !reflect.DeepEqual(first.Result.Changes[0].Rules, rules) {
					t.Fatalf("changes want %v: %s", rules, first.Stdout)
				}

				manual := map[int]bool{}
				for _, start := range tc.manual {
					manual[start] = true
				}
				if len(first.Result.Diagnostics) < len(tc.manual) {
					t.Fatal("reported findings", first.Stdout)
				}
				for _, d := range first.Result.Diagnostics {
					if d.Rule == "MD009" && (!manual[d.ByteRange.Start] || d.FixAvailable) {
						t.Fatalf("unexpected remaining finding %+v: %s", d, first.Stdout)
					}
				}
				if len(first.Result.DeferredFixes) != len(tc.deferred) {
					t.Fatalf("deferred want %v: %s", tc.deferred, first.Stdout)
				}
				for i, reason := range tc.deferred {
					if first.Result.DeferredFixes[i].Reason != reason {
						t.Fatalf("deferred want %v: %s", tc.deferred, first.Stdout)
					}
				}

				// Criterion 21: a second run has nothing left to do and reports the same.
				second := acceptance.RunFix(t, binary, root, "fix", "doc.md", "--format=json")
				//nolint:gosec // Access only synthetic test fixtures.
				again, err := os.ReadFile(path)
				if err != nil || string(again) != tc.want || second.Status != status || len(second.Result.Changes) != 0 {
					t.Fatalf("second run %q: %v %s", again, err, second.Stdout)
				}
				if !reflect.DeepEqual(md009Located(first.Result.Diagnostics), md009Located(second.Result.Diagnostics)) || !reflect.DeepEqual(md009DeferredRecords(first.Result.DeferredFixes), md009DeferredRecords(second.Result.DeferredFixes)) {
					t.Fatalf("second run reports differently:\n%s\n%s", first.Stdout, second.Stdout)
				}
			})
		}
	}
}

// Criterion 16: the MD009 removal and MD047's insertion touch at EOF. MD009 wins the
// conflict and MD047's final newline follows in the next round, so nothing is left.
func TestCompiledMD009MD047ConflictPinned(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, source := range []string{"foo   ", "foo\n  "} {
			root := acceptance.Project(t)
			conf := strings.Replace(md009Configuration(flavor), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
			acceptance.PutFile(t, root, ".mdchk.yaml", conf)
			acceptance.PutFile(t, root, "doc.md", source)
			got := acceptance.RunFix(t, binary, root, "fix", "doc.md", "--format=json")
			//nolint:gosec // Access only synthetic test fixtures.
			saved, err := os.ReadFile(filepath.Join(root, "doc.md"))
			if err != nil || string(saved) != "foo\n" || got.Status != 0 || len(got.Result.Changes) != 1 {
				t.Fatalf("saved %q: %v %s", saved, err, got.Stdout)
			}
			if len(got.Result.Diagnostics) != 0 || len(got.Result.DeferredFixes) != 0 {
				t.Fatal("left", got.Stdout)
			}
		}
	}
}

// A manual MD047 finding fails its proof, and a failed proof defers a whole round. It
// must not take MD009's corrections with it, and its own outcome stays what it is in a
// file with nothing else to fix. A withheld suffix that MD047's ending makes safe is
// corrected in a later round and leaves no record.
func TestCompiledMD009BesideManualMD047(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	const failed = "candidate-preservation-failed"
	for _, tc := range []struct {
		name, flavor, source, want string
		rules                      []string
		deferred                   map[string]string
		left                       map[string]bool
	}{
		{name: "final comment", source: "# T\n\nend \n<!-- x -->", want: "# T\n\nend\n<!-- x -->", rules: []string{"MD009"}, deferred: map[string]string{"MD047": failed}, left: map[string]bool{"MD047": false}},
		{name: "final html block", source: "a \n<div>x</div>", want: "a\n<div>x</div>", rules: []string{"MD009"}, deferred: map[string]string{"MD047": failed}, left: map[string]bool{"MD047": false}},
		{name: "unclosed fence", source: "a \n```\ncode", want: "a\n```\ncode", rules: []string{"MD009"}, deferred: map[string]string{"MD047": failed}, left: map[string]bool{"MD047": false}},
		{name: "literal task", flavor: "gfm", source: "a \n- [x]", want: "a\n- [x]", rules: []string{"MD009"}, deferred: map[string]string{"MD047": "meaning-not-preserved"}, left: map[string]bool{"MD047": false}},
		{name: "comment alone", source: "<!-- x -->", want: "<!-- x -->", deferred: map[string]string{"MD047": failed}, left: map[string]bool{"MD047": false}},
		{name: "withheld then resolved", flavor: "gfm", source: "- [x] ", want: "- [x]\n", rules: []string{"MD009", "MD047"}, deferred: map[string]string{}, left: map[string]bool{}},
	} {
		for _, flavor := range []string{"commonmark", "gfm"} {
			if tc.flavor != "" && tc.flavor != flavor {
				continue
			}
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				root := acceptance.Project(t)
				acceptance.PutFile(t, root, ".mdchk.yaml", strings.Replace(md009Configuration(flavor), "MD047: {enabled: false}", "MD047: {enabled: true}", 1))
				acceptance.PutFile(t, root, "doc.md", tc.source)
				for run := range 2 {
					got := acceptance.RunFix(t, binary, root, "fix", "doc.md", "--format=json")
					//nolint:gosec // Access only synthetic test fixtures.
					saved, err := os.ReadFile(filepath.Join(root, "doc.md"))
					if err != nil || string(saved) != tc.want || !got.Result.Complete {
						t.Fatalf("run %d saved %q: %v %s", run, saved, err, got.Stdout)
					}
					if run == 0 && tc.rules != nil && (len(got.Result.Changes) != 1 || !reflect.DeepEqual(got.Result.Changes[0].Rules, tc.rules)) {
						t.Fatalf("changes want %v: %s", tc.rules, got.Stdout)
					}
					if (run == 1 || tc.rules == nil) && len(got.Result.Changes) != 0 {
						t.Fatalf("run %d changed: %s", run, got.Stdout)
					}
					deferred, left := map[string]string{}, map[string]bool{}
					for _, r := range got.Result.DeferredFixes {
						deferred[r.Rule] = r.Reason
					}
					for _, d := range got.Result.Diagnostics {
						left[d.Rule] = d.FixAvailable
					}
					if !reflect.DeepEqual(deferred, tc.deferred) || !reflect.DeepEqual(left, tc.left) {
						t.Fatalf("run %d deferred %v left %v: %s", run, deferred, left, got.Stdout)
					}
				}
			})
		}
	}
}

// MD009 shares its rounds with MD019, MD021 and MD047, in one change.
func TestCompiledMD009BesideHeadingSpacing(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, tc := range []struct {
		source, want string
		rules        []string
	}{
		{"#  a \n", "# a\n", []string{"MD009", "MD019"}},
		{"#  a  # \n", "# a #\n", []string{"MD009", "MD019", "MD021"}},
		{"#   a\t\nb", "# a\nb\n", []string{"MD009", "MD019", "MD047"}},
		{"> #  a \nb", "> # a\nb\n", []string{"MD009", "MD019", "MD047"}},
		{"#  a \n- [x] b \n  c", "# a\n- [x] b\n  c\n", []string{"MD009", "MD019", "MD047"}},
		{"#  a \n\n<!-- x -->", "# a\n\n<!-- x -->", []string{"MD009", "MD019"}},
	} {
		for _, flavor := range []string{"commonmark", "gfm"} {
			t.Run(flavor+"/"+tc.source, func(t *testing.T) {
				t.Parallel()
				conf := md009Configuration(flavor)
				for _, rule := range []string{"MD019", "MD021", "MD047"} {
					conf = strings.Replace(conf, rule+": {enabled: false}", rule+": {enabled: true}", 1)
				}
				root := acceptance.Project(t)
				acceptance.PutFile(t, root, ".mdchk.yaml", conf)
				acceptance.PutFile(t, root, "doc.md", tc.source)
				got := acceptance.RunFix(t, binary, root, "fix", "doc.md", "--format=json")
				//nolint:gosec // Access only synthetic test fixtures.
				saved, err := os.ReadFile(filepath.Join(root, "doc.md"))
				if err != nil || string(saved) != tc.want || len(got.Result.Changes) != 1 || !reflect.DeepEqual(got.Result.Changes[0].Rules, tc.rules) {
					t.Fatalf("saved %q: %v %s", saved, err, got.Stdout)
				}
				second := acceptance.RunFix(t, binary, root, "fix", "doc.md", "--format=json")
				if len(second.Result.Changes) != 0 {
					t.Fatal("second run changed", second.Stdout)
				}
			})
		}
	}
}

// Criterion 22: lint advertises exactly what fix then corrects. A finding's line is
// stable across MD009 edits, so it identifies the same finding before and after.
func TestCompiledMD009LintParity(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range md009FixCases() {
			if tc.flavor != "" && tc.flavor != flavor {
				continue
			}
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				root := acceptance.Project(t)
				acceptance.PutFile(t, root, ".mdchk.yaml", tc.configuration(flavor))
				acceptance.PutFile(t, root, "doc.md", tc.source)
				lint := acceptance.RunProcess(t, binary, root, "lint", "doc.md", "--format=json")
				findings := acceptance.ValidateOperationJSON(t, []byte(lint.Stdout), "lint")
				fixed := acceptance.RunFix(t, binary, root, "fix", "doc.md", "--format=json")
				left := map[int]bool{}
				for _, d := range fixed.Result.Diagnostics {
					if d.Rule == "MD009" {
						left[d.Range.Start.Line] = true
					}
				}
				for _, d := range findings.Diagnostics {
					if d.Rule == "MD009" && d.FixAvailable == left[d.Range.Start.Line] {
						t.Fatalf("fix_available %v with remaining %v: %s", d.FixAvailable, left, lint.Stdout)
					}
				}
			})
		}
	}
}

func TestCompiledMD009HiddenFindingHasNoFix(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		root := acceptance.Project(t)
		acceptance.PutFile(t, root, ".mdchk.yaml", strings.Replace(md009Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1))
		acceptance.PutFile(t, root, "doc.md", "<!-- mdchk:disable MD009 -->\na \n")
		got, _ := acceptance.RunInspection(t, binary, root, root, "inspect", "doc.md", "--format=json")
		if got.Status != 0 || len(got.Result.SuppressedDiagnostics) != 1 {
			t.Fatal(got)
		}
		var hidden result.SuppressedDiagnostic
		if err := json.Unmarshal(got.Result.SuppressedDiagnostics[0], &hidden); err != nil || hidden.Diagnostic.Rule != "MD009" || hidden.Diagnostic.FixAvailable {
			t.Fatal(hidden, err)
		}
	}
}

func TestCompiledMD009DiagnosticPositions(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	root := acceptance.Project(t)
	acceptance.PutFile(t, root, ".mdchk.yaml", md009Configuration("commonmark"))
	acceptance.PutFile(t, root, "doc.md", "雪 \n")
	got := acceptance.RunProcess(t, binary, root, "lint", "doc.md", "--format=json")
	out := acceptance.ValidateOperationJSON(t, []byte(got.Stdout), "lint")
	if len(out.Diagnostics) != 1 {
		t.Fatal(got)
	}
	d := out.Diagnostics[0]
	if d.ByteRange != (result.ByteRange{Start: 3, End: 4}) || d.Range.Start != (result.Position{Line: 1, Column: 2}) || d.Range.End != (result.Position{Line: 1, Column: 3}) || !d.FixAvailable {
		t.Fatal(d)
	}
}

// Criterion 24: default limits complete a preview with thousands of edits.
func TestCompiledMD009FixScale(t *testing.T) {
	t.Parallel()
	binary := acceptance.SharedExecutable(t)
	root := acceptance.Project(t)
	acceptance.PutFile(t, root, ".mdchk.yaml", md009Configuration("commonmark"))
	acceptance.PutFile(t, root, "doc.md", strings.Repeat("`x` \n", 5000))
	got := acceptance.RunFixDryRun(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
	if got.Status != 0 || !got.Result.Complete || len(got.Result.Diagnostics) != 0 || len(got.Result.Changes) != 1 || got.Result.Changes[0].EditCount != 5000 {
		t.Fatalf("status %d complete %v diagnostics %d interruptions %+v", got.Status, got.Result.Complete, len(got.Result.Diagnostics), got.Result.Interruptions)
	}
	want := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Repeat("`x`\n", 5000))))
	if snapshot := got.Result.Files[0].ResultSnapshot; snapshot == nil || snapshot.SHA256 != want {
		t.Fatal("scale candidate", got.Result.Files)
	}
}
