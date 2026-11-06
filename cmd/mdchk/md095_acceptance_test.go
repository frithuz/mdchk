package main

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

func md095Configuration(flavor, setting string) string {
	return strings.Replace(strings.Replace(cleanConfiguration(), "flavor: commonmark", "flavor: "+flavor, 1), "MD095: {enabled: false}", "MD095: "+setting, 1)
}

func TestCompiledPlatformMD095ProductionAnalysis(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	const flagged = ` contains lookalike characters: U+0430 looks like "a".`
	for _, tc := range []struct {
		id, source, flavor, setting string
		ranges                      [][2]int
		starts                      [][2]int
		hosts                       []string
		problems                    int
	}{
		{"rule-page", "<https://exаmple.org> where а is Cyrillic U+0430.\n", "gfm", "{enabled: true}", [][2]int{{11, 13}}, [][2]int{{1, 12}}, []string{"exаmple.org"}, 0},
		{"bare-prose", "https://exаmple.org\n", "commonmark", "{enabled: true}", nil, nil, nil, 0},
		{"xn-twin", "[x](https://xn--exmple-4nf.org)\n", "commonmark", "{enabled: true}", [][2]int{{12, 30}}, [][2]int{{1, 13}}, []string{"xn--exmple-4nf.org"}, 0},
		{"unicode-twin", "[x](https://exаmple.org)\n", "gfm", "{enabled: true}", [][2]int{{14, 16}}, [][2]int{{1, 15}}, []string{"exаmple.org"}, 0},
		{"allowlisted", "<https://exаmple.org> <https://exаmple.org:8443/>\n", "gfm", "{enabled: true, allowed_hosts: [xn--exmple-4nf.org]}", nil, nil, nil, 0},
		{"ascii-allowlist", "<https://exаmple.org>\n", "gfm", "{enabled: true, allowed_hosts: [example.org]}", [][2]int{{11, 13}}, [][2]int{{1, 12}}, []string{"exаmple.org"}, 0},
		{"crlf-definition", "intro\r\n\r\n[a]: https://exаmple.org\r\n\r\n[a] [a]\r\n", "commonmark", "{enabled: true}", [][2]int{{24, 26}}, [][2]int{{3, 16}}, []string{"exаmple.org"}, 0},
		{"fenced", "```\n[x](https://exаmple.org)\n```\n", "gfm", "{enabled: true}", nil, nil, nil, 0},
		{"problem", "[a](https://xn--a.org) [b](https://exаmple.org)\n", "gfm", "{enabled: true}", [][2]int{{37, 39}}, [][2]int{{1, 38}}, []string{"exаmple.org"}, 1},
	} {
		t.Run(tc.id, func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md095Configuration(tc.flavor, tc.setting))
			putConfigurationFile(t, root, "doc.md", tc.source)
			before := readinessState(t, root)
			for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"inspect", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}, {"fix", "doc.md", "--format=json"}} {
				got := runConfigurationProcess(t, binary, root, args...)
				out := validateConfigurationOperationJSON(t, []byte(got.Stdout), args[0])
				if args[0] == "fix" && args[1] == "doc.md" && md038NativeRefusal(t, got, out) {
					continue
				}
				status := 0
				if args[0] != "inspect" && tc.problems > 0 {
					status = 15
				} else if args[0] != "inspect" && len(tc.ranges) > 0 {
					status = 10
				}
				if got.Status != status || out.Complete != (tc.problems == 0) || len(out.Errors) != 0 || len(out.Coverage) != tc.problems || len(out.Diagnostics) != len(tc.ranges) || len(out.Changes)+len(out.DeferredFixes)+len(out.Documents) != 0 {
					t.Fatal(got)
				}
				for _, c := range out.Coverage {
					if c.Rule != "MD095" || c.Reason != "parser-uncertain" {
						t.Fatal(c)
					}
				}
				for i, d := range out.Diagnostics {
					w := tc.ranges[i]
					if args[0] != "fix" && d.Snapshot.View != "original" || args[0] == "fix" && d.Snapshot.View != "candidate" && d.Snapshot.View != "saved" {
						t.Fatal(d.Snapshot)
					}
					if d.Rule != "MD095" || d.Message != `Hostname "`+tc.hosts[i]+`"`+flagged || d.ByteRange.Start != w[0] || d.ByteRange.End != w[1] || d.Range.Start.Line != tc.starts[i][0] || d.Range.Start.Column != tc.starts[i][1] || d.FixAvailable || d.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(tc.source))) || d.Snapshot.SizeBytes != len(tc.source) {
						t.Fatal(d)
					}
				}
			}
			readinessAssertEqual(t, "report-only MD095", before, readinessState(t, root))
		})
	}
}

func TestCompiledPlatformMD095Options(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	for _, setting := range []string{"{enabled: true, allowed_hosts: ['https://example.org']}", "{enabled: true, allowed_hosts: [' example.org']}", "{enabled: true, allowed_hosts: ['*.example.org']}", "{enabled: false, allowed_hosts: ['example.org:443']}", "{enabled: false, allowed_hosts: null}", "{enabled: false, unknown: true}"} {
		t.Run(setting, func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md095Configuration("gfm", setting))
			got := runConfigurationProcess(t, binary, root, "lint", "missing.md", "--format=json")
			out := validateConfigurationJSON(t, []byte(got.Stdout))
			if got.Status != 12 || len(out.CheckedFiles) != 0 || !strings.Contains(got.Stdout, "rules.MD095") {
				t.Fatal(got)
			}
		})
	}
}
