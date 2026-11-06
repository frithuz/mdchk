package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/result"
)

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
	conflict := []string{"conflict", "conflict"}
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
		// Criterion 16: MD047 conflict.
		{name: "conflict", source: "foo   ", want: "foo   ", md047: true, manual: []int{3}, deferred: conflict},
		{name: "conflict blank line", source: "foo\n  ", want: "foo\n  ", md047: true, manual: []int{4}, deferred: conflict},
		{name: "conflict beside correction", source: "a \nfoo ", want: "a\nfoo ", md047: true, manual: []int{5}, deferred: conflict, rules: []string{"MD009"}},
		{name: "conflict strict", source: "foo  ", want: "foo  ", md047: true, option: "{strict: true}", manual: []int{3}, deferred: conflict},
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
	binary := sharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range md009FixCases() {
			if tc.flavor != "" && tc.flavor != flavor {
				continue
			}
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				root := configurationProject(t)
				putConfigurationFile(t, root, ".mdchk.yaml", tc.configuration(flavor))
				putConfigurationFile(t, root, "doc.md", tc.source)
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

				preview := runFixConfiguration(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
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

				first := runAppliedFix(t, binary, root, "fix", "doc.md", "--format=json")
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
				second := runAppliedFix(t, binary, root, "fix", "doc.md", "--format=json")
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

// Criterion 16: the MD009 removal and MD047's insertion touch at EOF, so neither wins
// and both findings stay.
func TestCompiledMD009MD047ConflictPinned(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, source := range []string{"foo   ", "foo\n  "} {
			root := configurationProject(t)
			conf := strings.Replace(md009Configuration(flavor), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", source)
			got := runAppliedFix(t, binary, root, "fix", "doc.md", "--format=json")
			//nolint:gosec // Access only synthetic test fixtures.
			saved, err := os.ReadFile(filepath.Join(root, "doc.md"))
			if err != nil || string(saved) != source || got.Status != 10 || len(got.Result.Changes) != 0 {
				t.Fatalf("saved %q: %v %s", saved, err, got.Stdout)
			}
			findings, deferred := map[string]bool{}, map[string]string{}
			for _, d := range got.Result.Diagnostics {
				findings[d.Rule] = d.FixAvailable
			}
			for _, r := range got.Result.DeferredFixes {
				deferred[r.Rule] = r.Reason
			}
			md009Available, md009Found := findings["MD009"]
			if _, md047Found := findings["MD047"]; !md009Found || !md047Found || md009Available {
				t.Fatal("findings", got.Stdout)
			}
			if !reflect.DeepEqual(deferred, map[string]string{"MD009": "conflict", "MD047": "conflict"}) {
				t.Fatal("deferred", got.Stdout)
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
	binary := sharedExecutable(t)
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
				root := configurationProject(t)
				putConfigurationFile(t, root, ".mdchk.yaml", strings.Replace(md009Configuration(flavor), "MD047: {enabled: false}", "MD047: {enabled: true}", 1))
				putConfigurationFile(t, root, "doc.md", tc.source)
				for run := range 2 {
					got := runAppliedFix(t, binary, root, "fix", "doc.md", "--format=json")
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
	binary := sharedExecutable(t)
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
				root := configurationProject(t)
				putConfigurationFile(t, root, ".mdchk.yaml", conf)
				putConfigurationFile(t, root, "doc.md", tc.source)
				got := runAppliedFix(t, binary, root, "fix", "doc.md", "--format=json")
				//nolint:gosec // Access only synthetic test fixtures.
				saved, err := os.ReadFile(filepath.Join(root, "doc.md"))
				if err != nil || string(saved) != tc.want || len(got.Result.Changes) != 1 || !reflect.DeepEqual(got.Result.Changes[0].Rules, tc.rules) {
					t.Fatalf("saved %q: %v %s", saved, err, got.Stdout)
				}
				second := runAppliedFix(t, binary, root, "fix", "doc.md", "--format=json")
				if len(second.Result.Changes) != 0 {
					t.Fatal("second run changed", second.Stdout)
				}
			})
		}
	}
}

// A run that validated completely has the budget to describe what it wrote, so the
// exit code a preview promises holds for the saved run.
func TestCompiledFixWriteIsNotReportedIncomplete(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	source := strings.Repeat("# T\n\nbody text here\n\n", 50) + "end"
	want := source + "\n"
	conf := strings.Replace(cleanConfiguration(), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
	run := func(limit int) (configurationRun, string) {
		root := configurationProject(t)
		putConfigurationFile(t, root, ".mdchk.yaml", conf)
		putConfigurationFile(t, root, "doc.md", source)
		got := runAppliedFix(t, binary, root, "fix", "doc.md", "--format=json", "--limit", fmt.Sprintf("work-units=%d", limit))
		//nolint:gosec // Access only synthetic test fixtures.
		saved, err := os.ReadFile(filepath.Join(root, "doc.md"))
		if err != nil {
			t.Fatal(err)
		}
		return got, string(saved)
	}
	// Find the smallest budget that writes.
	low, high := 1000, 250000000
	for high-low > 1 {
		middle := (low + high) / 2
		if _, saved := run(middle); saved == want {
			high = middle
		} else {
			low = middle
		}
	}
	// A stop in the write's own last steps stays visible, so start just above them.
	for _, extra := range []int{1000, 5000, 20000, 50000} {
		got, saved := run(high + extra)
		if saved != want || got.Status != 0 || !got.Result.Complete {
			t.Fatalf("limit %d: status %d saved %q: %s", high+extra, got.Status, saved, got.Stdout)
		}
	}
}

// Criterion 22: lint advertises exactly what fix then corrects. A finding's line is
// stable across MD009 edits, so it identifies the same finding before and after.
func TestCompiledMD009LintParity(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, tc := range md009FixCases() {
			if tc.flavor != "" && tc.flavor != flavor {
				continue
			}
			t.Run(flavor+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				root := configurationProject(t)
				putConfigurationFile(t, root, ".mdchk.yaml", tc.configuration(flavor))
				putConfigurationFile(t, root, "doc.md", tc.source)
				lint := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
				findings := validateConfigurationOperationJSON(t, []byte(lint.Stdout), "lint")
				fixed := runAppliedFix(t, binary, root, "fix", "doc.md", "--format=json")
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
	binary := sharedExecutable(t)
	for _, flavor := range []string{"commonmark", "gfm"} {
		root := configurationProject(t)
		putConfigurationFile(t, root, ".mdchk.yaml", strings.Replace(md009Configuration(flavor), "directives: {enabled: false}", "directives: {enabled: true}", 1))
		putConfigurationFile(t, root, "doc.md", "<!-- mdchk:disable MD009 -->\na \n")
		got, _ := runInspection(t, binary, root, root, "inspect", "doc.md", "--format=json")
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
	binary := sharedExecutable(t)
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", md009Configuration("commonmark"))
	putConfigurationFile(t, root, "doc.md", "雪 \n")
	got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
	out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
	if len(out.Diagnostics) != 1 {
		t.Fatal(got)
	}
	d := out.Diagnostics[0]
	if d.ByteRange != (result.ByteRange{Start: 3, End: 4}) || d.Range.Start != (result.Position{Line: 1, Column: 2}) || d.Range.End != (result.Position{Line: 1, Column: 3}) || !d.FixAvailable {
		t.Fatal(d)
	}
}

// Criterion 19: rules outside this slice stay report-only through fix.
func TestCompiledReportOnlyRulesProduceNoEdits(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	for _, tc := range []struct{ rule, setting, source string }{
		{"MD013", "{enabled: true, length: 5}", "abcdefghij klmn\n"},
		{"MD028", "{enabled: true}", "> a\n\n> b\n"},
		{"MD037", "{enabled: true}", "a * b * c\n"},
		{"MD081", "{enabled: true}", "**aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa**\n"},
		{"MD010", "{enabled: true}", "a\tb\n"},
		{"MD012", "{enabled: true}", "a\n\n\nb\n"},
		{"MD027", "{enabled: true}", ">  a\n"},
		{"MD064", "{enabled: true}", "a  b\n"},
		{"MD085", "{enabled: true}", "a\n  b\n"},
		{"MD089", "{enabled: true}", "中a\n"},
		{"MD035", "{enabled: true}", "---\n\n***\n"},
		{"MD038", "{enabled: true}", "` a `\n"},
		{"MD044", "{enabled: true, names: [JavaScript]}", "javascript\n"},
		{"MD049", "{enabled: true}", "*a* _b_\n"},
		{"MD050", "{enabled: true}", "**a** __b__\n"},
		{"MD065", "{enabled: true}", "a\n\n---\nb\n"},
		{"MD088", "{enabled: true}", "a — b\n"},
	} {
		for _, flavor := range []string{"commonmark", "gfm"} {
			t.Run(tc.rule+"/"+flavor, func(t *testing.T) {
				t.Parallel()
				root := configurationProject(t)
				conf := strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1)
				putConfigurationFile(t, root, ".mdchk.yaml", strings.Replace(conf, tc.rule+": {enabled: false}", tc.rule+": "+tc.setting, 1))
				putConfigurationFile(t, root, "doc.md", tc.source)
				before := configurationBytes(t, root)
				got := runAppliedFix(t, binary, root, "fix", "doc.md", "--format=json")
				if got.Status != 10 || len(got.Result.Diagnostics) == 0 || len(got.Result.Changes) != 0 || len(got.Result.DeferredFixes) != 0 {
					t.Fatal(got)
				}
				for _, d := range got.Result.Diagnostics {
					if d.Rule != tc.rule || d.FixAvailable {
						t.Fatal(d)
					}
				}
				if !reflect.DeepEqual(before, configurationBytes(t, root)) {
					t.Fatal("report-only rule changed bytes")
				}
			})
		}
	}
}

// Criterion 24: default limits complete a preview with thousands of edits.
func TestCompiledMD009FixScale(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", md009Configuration("commonmark"))
	putConfigurationFile(t, root, "doc.md", strings.Repeat("`x` \n", 5000))
	got := runFixConfiguration(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
	if got.Status != 0 || !got.Result.Complete || len(got.Result.Diagnostics) != 0 || len(got.Result.Changes) != 1 || got.Result.Changes[0].EditCount != 5000 {
		t.Fatalf("status %d complete %v diagnostics %d interruptions %+v", got.Status, got.Result.Complete, len(got.Result.Diagnostics), got.Result.Interruptions)
	}
	want := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Repeat("`x`\n", 5000))))
	if snapshot := got.Result.Files[0].ResultSnapshot; snapshot == nil || snapshot.SHA256 != want {
		t.Fatal("scale candidate", got.Result.Files)
	}
}
