package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/inspect"
)

func TestCompiledGFMTaskEOFManualCorrection(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, flavor := range []string{"omitted", "gfm", "commonmark"} {
		for _, ending := range []string{"\n", "\r\n", "\r"} {
			for _, tail := range []string{"- [x]", "- [ ]", "> - [X]", "- outer\n  - [x]", "- [\n  ]", "> - [\n]", "- [x] ", "ordinary"} {
				t.Run(flavor+"/"+strings.ReplaceAll(ending, "\r", "CR")+"/"+tail, func(t *testing.T) {
					root := configurationProject(t)
					definition := md047Configuration(true, true)
					if flavor == "omitted" {
						definition = strings.ReplaceAll(definition, "flavor: commonmark\n", "")
					} else {
						definition = strings.ReplaceAll(definition, "flavor: commonmark", "flavor: "+flavor)
					}
					putConfigurationFile(t, root, ".mdchk.yaml", definition)
					text := "prefix" + ending + ending + strings.ReplaceAll(tail, "\n", ending)
					putConfigurationFile(t, root, "doc.md", text)
					unsafe := flavor != "commonmark" && strings.HasSuffix(tail, "]")
					lint := runConfiguration(t, binary, root, root, "lint", "doc.md", "--format=json")
					observed, _ := runInspection(t, binary, root, root, "inspect", "doc.md", "--format=json")
					for _, got := range []configurationRun{lint, observed} {
						if !got.Result.Complete || len(got.Result.Diagnostics) != 1 || got.Result.Diagnostics[0].Rule != "MD047" || got.Result.Diagnostics[0].FixAvailable == unsafe || got.Result.Diagnostics[0].Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(text))) {
							t.Fatal("eligibility/source", got)
						}
					}
					if lint.Status != 10 || observed.Status != 0 {
						t.Fatal(lint, observed)
					}
					var p inspect.Projection
					if err := json.Unmarshal(observed.Result.Inspection, &p); err != nil {
						t.Fatal(err)
					}
					tasks := 0
					visible := false
					var walk func(inspect.Node)
					walk = func(n inspect.Node) {
						if n.Kind == "text" && n.ByteRange.Start <= strings.LastIndex(text, "[") && n.ByteRange.End > strings.LastIndex(text, "[") && strings.Contains(text[n.ByteRange.Start:n.ByteRange.End], "[") {
							visible = true
						}
						if _, ok := n.Properties["task"]; ok {
							tasks++
						}
						for _, c := range n.Children {
							walk(c)
						}
					}
					walk(p.AST.Data.Root)
					if unsafe && (tasks != 0 || !visible) {
						t.Fatal("original literal already task", p)
					}
					preview := runFixConfiguration(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
					want := text + ending
					if unsafe {
						want = text
						if preview.Status != 10 || !preview.Result.Complete || len(preview.Result.Changes) != 0 || len(preview.Result.DeferredFixes) == 0 || len(preview.Result.Diagnostics) != 1 || preview.Result.Diagnostics[0].FixAvailable || preview.Result.Diagnostics[0].Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(text))) {
							t.Fatal("unsafe candidate escaped", preview)
						}
					} else if preview.Status != 0 || !preview.Result.Complete || len(preview.Result.Changes) != 1 || preview.Result.Changes[0].Candidate.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(want))) {
						t.Fatal("safe candidate", preview)
					}
					// Repeating the manual case and accepting only the safe independently
					// specified candidate must both be idempotent in ordinary compiled runs.
					putConfigurationFile(t, root, "doc.md", want)
					again := runFixConfiguration(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
					if len(again.Result.Changes) != 0 || !again.Result.Complete || unsafe && (again.Status != 10 || len(again.Result.DeferredFixes) == 0) || !unsafe && again.Status != 0 {
						t.Fatal("second run", again)
					}
				})
			}
		}
	}
}
