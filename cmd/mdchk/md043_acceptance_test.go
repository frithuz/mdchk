package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/result"
)

func TestCompiledPlatformMD043ProductionAnalysis(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	for _, tc := range []struct {
		name, source, mode string
		pattern            []string
		span               *result.ByteRange
		start, end         result.Position
		incomplete         bool
	}{
		{"published", "# Guide\n\n## Usage", "", []string{"# Guide", "## Install", "## Usage"}, &result.ByteRange{Start: 9, End: 9}, result.Position{Line: 3, Column: 1}, result.Position{Line: 3, Column: 1}, false},
		{"raw_closer_visible", "# A!<script>*payload</script>* B", "", []string{"# A! B"}, nil, result.Position{}, result.Position{}, false},
		{"raw_closer_mismatch", "# A!<script>*payload</script>* B", "", []string{"# A!"}, &result.ByteRange{Start: 2, End: 32}, result.Position{Line: 1, Column: 3}, result.Position{Line: 1, Column: 33}, false},
		{"valid", "# A\n# B", "", []string{"# A", "# B"}, nil, result.Position{}, result.Position{}, false},
		{"alternate_anchor", "# A\n# B\n# B\n# C", "", []string{"# A", "*", "# B", "# C"}, nil, result.Position{}, result.Position{}, false},
		{"substitution", "# Other", "", []string{"# Guide"}, &result.ByteRange{Start: 2, End: 7}, result.Position{Line: 1, Column: 3}, result.Position{Line: 1, Column: 8}, false},
		{"missing_EOF", "# Guide", "", []string{"# Guide", "## Install"}, &result.ByteRange{Start: 7, End: 7}, result.Position{Line: 1, Column: 8}, result.Position{Line: 1, Column: 8}, false},
		{"empty", "# X", "", []string{}, nil, result.Position{}, result.Position{}, false},
		{"stars", "# X\n# Y", "", []string{"*", "*"}, nil, result.Position{}, result.Position{}, false},
		{"disabled", "# X", "off", nil, nil, result.Position{}, result.Position{}, false},
		{"empty_mapping", "# X", "mapping", nil, nil, result.Position{}, result.Position{}, false},
		{"unicode", "# 雪", "", []string{"# other"}, &result.ByteRange{Start: 2, End: 5}, result.Position{Line: 1, Column: 3}, result.Position{Line: 1, Column: 4}, false},
		{"nested_missing", "# A\n\n> ## C", "", []string{"# A", "## B", "## C"}, &result.ByteRange{Start: 7, End: 7}, result.Position{Line: 3, Column: 3}, result.Position{Line: 3, Column: 3}, false},
		{"tie", "# A\n# C\n# B", "", []string{"# A", "# B", "# C"}, &result.ByteRange{Start: 6, End: 7}, result.Position{Line: 2, Column: 3}, result.Position{Line: 2, Column: 4}, false},
		{"empty_label", "#\n# A", "", []string{"# A"}, &result.ByteRange{Start: 1, End: 1}, result.Position{Line: 1, Column: 2}, result.Position{Line: 1, Column: 2}, false},
		{"unreliable", "---\n# A", "", []string{"# A"}, nil, result.Position{}, result.Position{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.pattern)
			if err != nil {
				t.Fatal(err)
			}
			conf := strings.Replace(cleanConfiguration(), "MD043: {enabled: false}", "MD043: {enabled: true, headings: "+string(raw)+"}", 1) + "frontmatter: {enabled: true}\n"
			if tc.mode == "off" {
				conf = strings.Replace(cleanConfiguration(), "MD043: {enabled: false}", "", 1)
			}
			if tc.mode == "mapping" {
				conf = strings.Replace(cleanConfiguration(), "MD043: {enabled: false}", "MD043: {}", 1)
			}
			putConfigurationFile(t, root, ".mdchk.yaml", conf)
			putConfigurationFile(t, root, "doc.md", tc.source)
			before := configurationBytes(t, root)
			for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}} {
				got := runConfigurationProcess(t, binary, root, args...)
				out := validateConfigurationOperationJSON(t, []byte(got.Stdout), args[0])
				status, want := 0, 0
				if tc.span != nil {
					status, want = 10, 1
				}
				if tc.incomplete {
					status = 15
				}
				if got.Status != status || out.Complete == tc.incomplete || len(out.Errors) != 0 || len(out.Diagnostics) != want || len(out.Changes) != 0 || len(out.Documents) != 0 {
					t.Fatal(got)
				}
				if tc.span != nil {
					d := out.Diagnostics[0]
					if d.Rule != "MD043" || d.FixAvailable || d.ByteRange != *tc.span || d.Range.Start != tc.start || d.Range.End != tc.end || d.Message != "Headings must match the configured outline." {
						t.Fatal(d)
					}
				}
			}
			seen, p := runInspection(t, binary, root, root, "inspect", "doc.md", "--format=json")
			if seen.Status != 0 {
				t.Fatal(seen)
			}
			found := false
			if tc.incomplete {
				if p.Rules.Data != nil || p.Rules.Availability != "unavailable" || len(p.Rules.Reasons) == 0 {
					t.Fatal(p.Rules)
				}
				found = true
			} else if p.Rules.Data == nil {
				t.Fatal(p.Rules)
			}
			if p.Rules.Data != nil {
				for _, rule := range *p.Rules.Data {
					if rule.Rule == "MD043" {
						found = true
						expected := "complete"
						if tc.incomplete {
							expected = "partial"
						}
						if rule.Implementation != "available" || (tc.mode == "" && rule.ActiveEvaluation != expected) {
							t.Fatal(rule)
						}
					}
				}
			}
			if !found || !reflect.DeepEqual(before, configurationBytes(t, root)) {
				t.Fatal("missing admission or mutated bytes")
			}
		})
	}
	for _, option := range []string{`{headings: null}`, `{headings: '# A'}`, `{headings: ['# A', 1]}`, `{headings: ['# A', 'bad']}`, `{enabled: false, headings: ['#']}`, `{headings: ['#\tA']}`, `{headings: ['####### A']}`, `{headings: ['* ']}`, `{enabled: null}`, `{allow_directives: null}`, `{unknown: true}`, `null`} {
		conf := strings.Replace(cleanConfiguration(), "MD043: {enabled: false}", "MD043: "+option, 1)
		putConfigurationFile(t, root, ".mdchk.yaml", conf)
		got := runConfigurationProcess(t, binary, root, "lint", "missing.md", "--format=json")
		out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
		if got.Status != 12 || len(out.Errors) == 0 || len(out.CheckedFiles) != 0 {
			t.Fatal(got)
		}
	}
}
