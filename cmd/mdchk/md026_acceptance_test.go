package main

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/result"
)

func TestCompiledPlatformMD026ProductionAnalysis(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	for _, tc := range []struct {
		name, source, option string
		points               []result.ByteRange
		positions            []result.Position
	}{
		{"published", "## Installation!", "", []result.ByteRange{{Start: 15, End: 16}}, []result.Position{{Line: 1, Column: 16}}},
		{"disabled", "# A!", "off", nil, nil},
		{"empty", "# A!", "empty", nil, nil},
		{"unicode", "# 雪！", "unicode", []result.ByteRange{{Start: 5, End: 8}}, []result.Position{{Line: 1, Column: 4}}},
		{"entity", "# A&#33;", "", []result.ByteRange{{Start: 3, End: 8}}, []result.Position{{Line: 1, Column: 4}}},
		{"code", "# A! `?`", "", nil, nil},
		{"escape", "# A!\\?", "", nil, nil},
		{"Setext", "A!\n===", "", []result.ByteRange{{Start: 1, End: 2}}, []result.Position{{Line: 1, Column: 2}}},
		{"containers", "> # A!", "", []result.ByteRange{{Start: 5, End: 6}}, []result.Position{{Line: 1, Column: 6}}},
		{"valid", "# A", "", nil, nil},
		{"raw-nested-closer", "# `x` <script>*payload</script>*!", "", []result.ByteRange{{Start: 32, End: 33}}, []result.Position{{Line: 1, Column: 33}}},
		{"raw-letter-tail", "# A!<script>*payload</script>* B", "", nil, nil},
		{"raw-image-alt-opaque", "# `x`<script>![payload</script>](u)!", "", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configuration := strings.Replace(cleanConfiguration(), "MD026: {enabled: false}", "MD026: {enabled: true}", 1)
			switch tc.option {
			case "off":
				configuration = strings.Replace(cleanConfiguration(), "MD026: {enabled: false}", "", 1)
			case "empty":
				configuration = strings.Replace(configuration, "MD026: {enabled: true}", "MD026: {enabled: true, punctuation: ''}", 1)
			case "unicode":
				configuration = strings.Replace(configuration, "MD026: {enabled: true}", "MD026: {enabled: true, punctuation: '！'}", 1)
			}
			putConfigurationFile(t, root, ".mdchk.yaml", configuration)
			putConfigurationFile(t, root, "doc.md", tc.source)
			before := configurationBytes(t, root)
			for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}} {
				got := runConfigurationProcess(t, binary, root, args...)
				out := validateConfigurationOperationJSON(t, []byte(got.Stdout), args[0])
				status := 0
				if len(tc.points) > 0 {
					status = 10
				}

				if got.Status != status || !out.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != len(tc.points) || len(out.Changes) != 0 || len(out.Documents) != 0 {
					t.Fatal(got)
				}
				for i, d := range out.Diagnostics {
					if d.Rule != "MD026" || d.FixAvailable || d.ByteRange != tc.points[i] || d.Range.Start != tc.positions[i] || d.Range.End != md026ExpectedEnd(tc.name, tc.positions[i], tc.points[i]) || d.Message != "Heading must not end with configured punctuation." {
						t.Fatal(d)
					}
				}
			}
			seen, p := runInspection(t, binary, root, root, "inspect", "doc.md", "--format=json")
			if seen.Status != 0 {
				t.Fatal(seen)
			}
			found := false
			for _, rule := range *p.Rules.Data {
				if rule.Rule == "MD026" {
					found = true
					if rule.Implementation != "available" || (tc.option != "off" && rule.ActiveEvaluation != "complete") {
						t.Fatal(rule)
					}
				}
			}
			if !found || !reflect.DeepEqual(before, configurationBytes(t, root)) {
				t.Fatal("missing admission or changed source/config")
			}
		})
	}
	for _, option := range []string{"{punctuation: null}", "{punctuation: 1}", "{punctuation: true}", "{punctuation: []}", "{enabled: false, punctuation: {}}", "{enabled: false, unknown: true}", "{enabled: null}", "{enabled: false, allow_directives: null}", "{enabled: false, allow_directives: 'false'}", "null"} {
		configuration := strings.Replace(cleanConfiguration(), "MD026: {enabled: false}", "MD026: "+option, 1)
		putConfigurationFile(t, root, ".mdchk.yaml", configuration)
		got := runConfigurationProcess(t, binary, root, "lint", "missing.md", "--format=json")
		out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "lint")
		if got.Status != 12 || len(out.Errors) == 0 || len(out.CheckedFiles) != 0 {
			t.Fatal(got)
		}
	}
}

func md026ExpectedEnd(name string, start result.Position, span result.ByteRange) result.Position {
	width := span.End - span.Start
	if name == "unicode" {
		width = 1
	}
	return result.Position{Line: start.Line, Column: start.Column + width}
}

// The newline proposal must leave the original manual heading finding visible
// against the validated candidate, and a second preview must be stable.
func TestCompiledPlatformMD026RawCloserNewlineComposition(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"commonmark", "gfm"} {
		for _, ending := range []struct{ name, text string }{{"LF", "\n"}, {"CRLF", "\r\n"}, {"CR", "\r"}} {
			t.Run(flavor+"/"+ending.name, func(t *testing.T) {
				root := configurationProject(t)
				config := strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1)
				config = strings.Replace(config, "MD026: {enabled: false}", "MD026: {enabled: true}", 1)
				config = strings.Replace(config, "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
				putConfigurationFile(t, root, ".mdchk.yaml", config)
				original := "# `x` <script>*payload</script>*!" + ending.text + "Body"
				candidate := original + ending.text
				digest := fmt.Sprintf("%x", sha256.Sum256([]byte(candidate)))
				for pass, source := range []string{original, candidate} {
					putConfigurationFile(t, root, "doc.md", source)
					before := configurationBytes(t, root)
					got := runConfigurationProcess(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
					out := validateConfigurationOperationJSON(t, []byte(got.Stdout), "fix")
					if got.Status != 10 || !out.Complete || len(out.Errors) != 0 || len(out.Diagnostics) != 1 || len(out.Changes) != 1-pass || len(out.Documents) != 0 || len(out.Files) != 1 {
						t.Fatal(got)
					}
					d := out.Diagnostics[0]
					if d.Rule != "MD026" || d.FixAvailable || d.ByteRange != (result.ByteRange{Start: 32, End: 33}) || d.Range.Start != (result.Position{Line: 1, Column: 33}) || d.Range.End != (result.Position{Line: 1, Column: 34}) || d.Snapshot.SHA256 != digest || d.Snapshot.SizeBytes != len(candidate) || d.Snapshot != *out.Files[0].ResultSnapshot {
						t.Fatal(d)
					}
					if pass == 0 {
						c := out.Changes[0]
						if !reflect.DeepEqual(c.Rules, []string{"MD047"}) || c.EditCount != 1 || c.Candidate.SHA256 != digest || c.Candidate.SizeBytes != len(candidate) || c.Before != *out.Files[0].InputSnapshot || c.Candidate != *out.Files[0].ResultSnapshot || c.Disposition != "proposed" || c.Diff == nil || len(c.Diff.Hunks) != 1 {
							t.Fatal(c)
						}
						wantLines := []result.DiffLine{{Kind: "context", Text: "# `x` <script>*payload</script>*!", LineEnding: strings.ToLower(ending.name)}, {Kind: "remove", Text: "Body", LineEnding: "none"}, {Kind: "add", Text: "Body", LineEnding: strings.ToLower(ending.name)}}
						if !reflect.DeepEqual(c.Diff.Hunks[0].Lines, wantLines) {
							t.Fatal(c.Diff)
						}
					}
					if !reflect.DeepEqual(before, configurationBytes(t, root)) {
						t.Fatal("preview changed source/config")
					}
				}
			})
		}
	}
}
