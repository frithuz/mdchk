package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/inspect"
)

const gfmConsumerSource = "> - [X]\té ~~ok~~ www.a.b\r\n\r\n| a | b |\n| :- | -: |\n| short |\n| long | `code` | excess |\n\n<textarea>\n<!-- mdchk:disable MD047 -->\n</textarea>\n\nlast"

func assertCompiledGFM(t *testing.T, p inspect.Projection, flavor string) {
	t.Helper()
	if p.AST.Data == nil || p.AST.Data.Profile != flavor || p.Flavor.Data == nil || *p.Flavor.Data != flavor {
		t.Fatal("wrong selected parser", p)
	}
	counts := map[string]int{}
	tasks, bare, tags := 0, 0, 0
	var walk func(inspect.Node)
	walk = func(n inspect.Node) {
		counts[n.Kind]++
		if _, ok := n.Properties["task"]; ok {
			tasks++
		}
		if n.Properties["origin"] == "bare-gfm" {
			bare++
		}
		if v, ok := n.Properties["filtered_tags"].([]any); ok {
			tags += len(v)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(p.AST.Data.Root)
	if flavor == "gfm" {
		if counts["table"] != 1 || counts["strike"] != 1 || tasks != 2 || bare != 1 || tags != 2 {
			t.Fatal("real selected GFM facts", counts, tasks, bare, tags)
		}
	} else if counts["table"]+counts["strike"]+tasks+bare+tags != 0 {
		t.Fatal("extensions leaked into core")
	}
}

func TestCompiledGFMSelectionAndConsumers(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"omitted", "gfm", "commonmark"} {
		t.Run(flavor, func(t *testing.T) {
			root := configurationProject(t)
			definition := md047Configuration(true, true)
			if flavor == "omitted" {
				definition = strings.ReplaceAll(definition, "flavor: commonmark\n", "")
			} else {
				definition = strings.ReplaceAll(definition, "flavor: commonmark", "flavor: "+flavor)
			}
			if flavor == "gfm" {
				definition += "frontmatter: {enabled: false}\n"
			}
			putConfigurationFile(t, root, ".mdchk.yaml", definition)
			putConfigurationFile(t, root, "doc.md", gfmConsumerSource)
			lint := runConfiguration(t, binary, root, root, "lint", "doc.md", "--format=json")
			if lint.Status != 10 || !lint.Result.Complete || len(lint.Result.Diagnostics) != 1 || lint.Result.Diagnostics[0].Rule != "MD047" || !lint.Result.Diagnostics[0].FixAvailable {
				t.Fatal(lint)
			}
			observed, _ := runInspection(t, binary, root, root, "inspect", "doc.md", "--format=json")
			if observed.Status != 0 || !observed.Result.Complete {
				t.Fatal(observed)
			}
			var p inspect.Projection
			if err := json.Unmarshal(observed.Result.Inspection, &p); err != nil {
				t.Fatal(err)
			}
			selected := flavor
			if selected == "omitted" {
				selected = "gfm"
			}
			assertCompiledGFM(t, p, selected)
			textReport, _ := runInspection(t, binary, root, root, "inspect", "doc.md")
			if textReport.Status != 0 {
				t.Fatal("text inspection", textReport.Status)
			}
			if selected == "gfm" {
				for _, fact := range []string{`"raw_cells":3`, `"check_spelling":"X"`, `"origin":"bare-gfm"`, `"filtered_tags":`, `"marker":"~"`} {
					if !strings.Contains(textReport.Stdout, fact) {
						t.Fatal("text projection omitted", fact)
					}
				}
			}

			foundOrigin := false
			for _, origin := range p.Configuration.Data.Provenance {
				if origin.Pointer == "/flavor" {
					want := "selected-config"
					if flavor == "omitted" {
						want = "default"
					}
					if origin.SourceKind != want {
						t.Fatal("flavor provenance", origin)
					}
					foundOrigin = true
				}
			}
			if !foundOrigin {
				t.Fatal("flavor provenance absent")
			}

			if len(observed.Result.Diagnostics) != 1 || observed.Result.Diagnostics[0].ByteRange != lint.Result.Diagnostics[0].ByteRange || observed.Result.Diagnostics[0].Snapshot != lint.Result.Diagnostics[0].Snapshot {
				t.Fatal("lint/inspect source mismatch")
			}
			preview := runFixConfiguration(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
			want := gfmConsumerSource + "\n"
			if preview.Status != 0 || !preview.Result.Complete || len(preview.Result.Diagnostics) != 0 || len(preview.Result.Changes) != 1 || preview.Result.Changes[0].Candidate.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(want))) || preview.Result.Files[0].WriteStatus != "proposed" {
				t.Fatal(preview)
			}
			// Candidate truth and idempotence: feed the independently specified candidate
			// to a second ordinary compiled run, without accepting rendered diff bytes.
			putConfigurationFile(t, root, "doc.md", want)
			again := runFixConfiguration(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
			if again.Status != 0 || len(again.Result.Changes) != 0 {
				t.Fatal(again)
			}
		})
	}
	for _, definition := range []string{"", "version: 1\n", "version: 1\nflavor: gfm\n"} {
		root := configurationProject(t)
		if definition != "" {
			putConfigurationFile(t, root, ".mdchk.yaml", definition)
		}
		putConfigurationFile(t, root, "doc.md", gfmConsumerSource)
		if definition != "" {
			scan := runConfigurationProcessEnv(t, binary, root, validationEnvironment(t), "config", "validate", ".", "--format=json")
			scan.Result = validateConfigurationOperationJSON(t, []byte(scan.Stdout), "config validate")
			if scan.Status != 0 || !scan.Result.Complete || scan.Result.ConfigValidation.ValidDefinitions != 1 {
				t.Fatal("GFM definition not admitted", scan)
			}
		}
		for _, args := range [][]string{{"lint", "doc.md", "--format=json"}, {"fix", "--dry-run", "doc.md", "--format=json"}, {"inspect", "doc.md", "--format=json"}} {
			var got configurationRun
			switch args[0] {
			case "inspect":
				got, _ = runInspection(t, binary, root, root, args...)
			case "fix":
				got = runFixConfiguration(t, binary, root, args...)
			default:
				got = runConfiguration(t, binary, root, root, args...)
			}
			want := 15
			if args[0] == "inspect" {
				want = 0
				var p inspect.Projection
				_ = json.Unmarshal(got.Result.Inspection, &p)
				assertCompiledGFM(t, p, "gfm")
			}
			if got.Status != want || got.Result.Complete || len(got.Result.Coverage) == 0 {
				t.Fatal("default catalog coverage", got)
			}
		}
	}
	for _, extra := range []string{"extensions: {tables: true}\n", "unknown: true\n", "rules: {MD055: {enabled: true}}\n"} {
		root := configurationProject(t)
		putConfigurationFile(t, root, ".mdchk.yaml", "version: 1\nflavor: gfm\n"+extra)
		putConfigurationFile(t, root, "doc.md", gfmConsumerSource)
		got := runConfiguration(t, binary, root, root, "lint", "doc.md", "--format=json")
		if got.Status != 12 || len(got.Result.CheckedFiles) != 0 {
			t.Fatal("unavailable independent capability admitted", got)
		}
	}
	for _, tail := range []string{"<textarea>\nprotected", "```\nprotected"} {
		root := configurationProject(t)
		putConfigurationFile(t, root, ".mdchk.yaml", strings.ReplaceAll(md047Configuration(true, true), "flavor: commonmark", "flavor: gfm"))
		putConfigurationFile(t, root, "doc.md", gfmConsumerSource+"\n\n"+tail)
		lint := runConfiguration(t, binary, root, root, "lint", "doc.md", "--format=json")
		if lint.Status != 10 || len(lint.Result.Diagnostics) != 1 || lint.Result.Diagnostics[0].FixAvailable {
			t.Fatal("protected EOF eligible", lint)
		}
		preview := runFixConfiguration(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
		if preview.Status != 10 || len(preview.Result.Changes) != 0 || len(preview.Result.DeferredFixes) == 0 {
			t.Fatal("protected EOF changed", preview)
		}
	}

}
