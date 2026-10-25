package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/result"
)

func TestCompiledMD001ProductionAnalysis(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	for _, tc := range []struct {
		name, source             string
		title                    bool
		status, start, end, line int
	}{
		{"body", "# Guide\n### Install\n", false, 10, 8, 20, 2},
		{"YAML virtual H1", "---\ntitle: Guide\n---\n### Install\n", true, 10, 21, 33, 4},
		{"TOML virtual H1", "+++\ntitle = \"Guide\"\n+++\n### Install\n", true, 10, 24, 36, 4},
		{"title opt-out", "---\ntitle: Guide\n---\n### Install\n", false, 0, 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			title := "false"
			if tc.title {
				title = "true"
			}
			configuration := strings.Replace(cleanConfiguration(), "MD001: {enabled: false}", "MD001: {enabled: true, frontmatter_title: "+title+"}", 1) + "frontmatter: {enabled: true}\n"
			putConfigurationFile(t, root, ".mdchk.yaml", configuration)
			putConfigurationFile(t, root, "doc.md", tc.source)
			before := configurationBytes(t, root)
			got := runConfigurationProcess(t, binary, root, "lint", "doc.md", "--format=json")
			out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
			want := 0
			if tc.status == 10 {
				want = 1
			}
			if got.Status != tc.status || !out.Complete || len(out.Errors) != 0 || len(out.Coverage) != 0 || len(out.Diagnostics) != want || len(out.Changes) != 0 || len(out.Documents) != 0 {
				t.Fatal(got)
			}
			if want > 0 {
				d := out.Diagnostics[0]
				if d.Rule != "MD001" || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: tc.start, End: tc.end}) || d.Range.Start != (result.Position{Line: tc.line, Column: 1}) || d.Range.End != (result.Position{Line: tc.line + 1, Column: 1}) {
					t.Fatal(d)
				}
			}
			if !reflect.DeepEqual(before, configurationBytes(t, root)) {
				t.Fatal("compiled analysis changed files")
			}
		})
	}
}
