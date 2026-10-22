//go:build darwin && arm64 && cgo

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
)

// Ordinary production binaries exercise the real APFS replacement boundary.
func TestCompiledProductionMD047ApplySavedWorkflow(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	data, err := os.ReadFile("testdata/md047_corrections.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Source, Candidate, Config string
		Status                          int
	}
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			for _, format := range []string{"json", "text"} {
				root := configurationProject(t)
				putConfigurationFile(t, root, ".mdchk.yaml", md047Configuration(tc.Config != "off", tc.Config != "blocked"))
				putConfigurationFile(t, root, "doc.md", tc.Source)
				path := filepath.Join(root, "doc.md")
				//nolint:gosec // Access only synthetic test fixtures or the fixed repository source overlay.
				if err := os.Chmod(path, 0640); err != nil {
					t.Fatal(err)
				}
				before, _ := os.Stat(path)
				inventory := configurationBytes(t, root)
				preview := runFixConfiguration(t, binary, root, "fix", "--dry-run", "doc.md", "--format=json")
				got := runAppliedFix(t, binary, root, "fix", "doc.md", "--format="+format)
				if got.Status != tc.Status {
					t.Fatalf("status %d want %d: %s", got.Status, tc.Status, got.Stdout)
				}
				//nolint:gosec // Access only synthetic test fixtures or the fixed repository source overlay.
				saved, err := os.ReadFile(path)
				if err != nil || string(saved) != tc.Candidate {
					t.Fatalf("saved %q want %q: %v %s", saved, tc.Candidate, err, got.Stdout)
				}
				after, _ := os.Stat(path)
				changed := tc.Source != tc.Candidate
				if os.SameFile(before, after) == changed || before.Mode() != after.Mode() {
					t.Fatal("publication identity/mode", before, after)
				}
				inventory[path] = sha256.Sum256([]byte(tc.Candidate))
				if !reflect.DeepEqual(inventory, configurationBytes(t, root)) {
					t.Fatal("unexpected artifacts or changed sibling")
				}
				if format == "json" {
					f := got.Result.Files[0]
					if f.ResultSnapshot == nil || f.ResultSnapshot.View != "saved" || f.ResultSnapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256(saved)) {
						t.Fatal(f)
					}
					if changed && (f.WriteStatus != "written" || len(got.Result.Changes) != 1 || got.Result.Changes[0].Disposition != "written" || got.Result.Changes[0].Candidate != preview.Result.Changes[0].Candidate) {
						t.Fatal(got)
					}
					for _, d := range got.Result.Diagnostics {
						if d.Snapshot != *f.ResultSnapshot {
							t.Fatal("not saved diagnostic", d)
						}
					}
				} else if strings.Contains(got.Stdout, "written doc.md") != changed {
					t.Fatal(got.Stdout)
				}
				second := runAppliedFix(t, binary, root, "fix", "doc.md", "--format=json")
				twice, _ := os.Stat(path)
				if second.Status != tc.Status || len(second.Result.Changes) != 0 || !os.SameFile(after, twice) {
					t.Fatal("non-idempotent", second)
				}
			}
		})
	}
}

func TestCompiledApplyAdmissionAndReporting(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, limit := range []string{"reporting-time=1ns", "reporting-time=1us", "response-bytes=1024", "work-units=1", "document-bytes=1"} {
		t.Run(limit, func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md047Configuration(true, true))
			putConfigurationFile(t, root, "doc.md", "hello")
			got := runAppliedFix(t, binary, root, "fix", "doc.md", "--format=json", "--limit="+limit)
			//nolint:gosec // Access only synthetic test fixtures or the fixed repository source overlay.
			saved, _ := os.ReadFile(filepath.Join(root, "doc.md"))
			if strings.HasPrefix(limit, "reporting-time=") {
				if string(saved) != "hello\n" || len(got.Result.Changes) != 1 || got.Result.Files[0].WriteStatus != "written" || (got.Status != 0 && got.Status != 15) {
					t.Fatal(got, string(saved))
				}
			} else if string(saved) != "hello" || got.Status == 0 || limit == "document-bytes=1" && got.Status != 11 {
				t.Fatal("admission mutated", got, string(saved))
			}
		})
	}
	for _, bad := range [][]string{{"--bad"}, {"--format=bogus"}, {"--config=missing.yaml"}} {
		root := configurationProject(t)
		putConfigurationFile(t, root, ".mdchk.yaml", md047Configuration(true, true))
		putConfigurationFile(t, root, "a.md", "a")
		putConfigurationFile(t, root, "b.md", "b")
		before := configurationBytes(t, root)
		args := append([]string{"fix", "a.md", "b.md"}, bad...)
		got := runConfigurationProcess(t, binary, root, args...)
		if got.Status == 0 || !reflect.DeepEqual(before, configurationBytes(t, root)) {
			t.Fatal(got)
		}
	}
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", "version: 1\nflavor: commonmark\n")
	putConfigurationFile(t, root, "doc.md", "hello")
	got := runAppliedFix(t, binary, root, "fix", "doc.md", "--format=json")
	//nolint:gosec // Access only synthetic test fixtures or the fixed repository source overlay.
	saved, _ := os.ReadFile(filepath.Join(root, "doc.md"))
	if got.Status != 15 || string(saved) != "hello\n" || got.Result.Files[0].WriteStatus != "written" {
		t.Fatal(got, string(saved))
	}
}

func runAppliedFix(t *testing.T, binary, root string, args ...string) configurationRun {
	t.Helper()
	got := runConfigurationProcess(t, binary, root, args...)
	for _, arg := range args {
		if arg == "--format=json" {
			got.Result = validateConfigurationOperationJSON(t, []byte(got.Stdout), "fix")
		}
	}
	return got
}

func buildApplyBoundaryFixture(t *testing.T, body string) string {
	t.Helper()
	return buildApplyBoundaryOriginFixture(t, body, false)
}

func buildApplyBoundaryOriginFixture(t *testing.T, body string, cancellation bool) string {
	t.Helper()
	cwd, e := os.Getwd()
	if e != nil {
		t.Fatal(e)
	}
	sourcePath := filepath.Join(cwd, "..", "..", "internal", "cli", "fix_apply.go")
	//nolint:gosec // Access only synthetic test fixtures or the fixed repository source overlay.
	data, e := os.ReadFile(sourcePath)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Count(string(data), "return validated.PublishMapped(ctx, native, path, names)") != 1 {
		t.Fatal("publication seam changed")
	}
	source := strings.Replace(string(data), "return validated.PublishMapped(ctx, native, path, names)", body, 1)
	if strings.Contains(body, "os.") {
		source = strings.Replace(source, "import (", "import (\n\"os\"", 1)
	}
	replacement := filepath.Join(t.TempDir(), "fix_apply.go")
	//nolint:gosec // Access only synthetic test fixtures or the fixed repository source overlay.
	if e = os.WriteFile(replacement, []byte(source), 0600); e != nil {
		t.Fatal(e)
	}
	replacements := map[string]string{sourcePath: replacement}
	if cancellation {
		fixPath := filepath.Join(cwd, "..", "..", "internal", "cli", "fix.go")
		//nolint:gosec // Read only fixed repository source for this build-time control.
		fixData, err := os.ReadFile(fixPath)
		if err != nil {
			t.Fatal(err)
		}
		needle := "ctx, cancel := context.WithTimeout(ctx, profile.OperationTime)"
		if strings.Count(string(fixData), needle) != 1 {
			t.Fatal("origin cancellation seam changed")
		}
		patched := strings.Replace(string(fixData), needle, needle+"\nctx = context.WithValue(ctx, struct{ issue149 bool }{true}, cancel)", 1)
		destination := filepath.Join(t.TempDir(), "fix.go")
		//nolint:gosec // Write only this test-owned source overlay.
		if err := os.WriteFile(destination, []byte(patched), 0600); err != nil {
			t.Fatal(err)
		}
		replacements[fixPath] = destination
	}
	overlayData, e := json.Marshal(struct{ Replace map[string]string }{replacements})
	if e != nil {
		t.Fatal(e)
	}
	overlay := filepath.Join(t.TempDir(), "overlay.json")
	if e = os.WriteFile(overlay, overlayData, 0600); e != nil {
		t.Fatal(e)
	}
	return buildConfigurationExecutable(t, overlay)
}

func TestCompiledApplyPartialFailuresAndDeadline(t *testing.T) {
	for _, mode := range []string{"refused", "published-error"} {
		t.Run(mode, func(t *testing.T) {
			body := `if path=="b.md" { return files.Publication{Path:path,Err:errors.New("injected native publication failure")} }; return validated.PublishMapped(ctx,native,path,names)`
			if mode == "published-error" {
				body = `pub:=validated.PublishMapped(ctx,native,path,names); if path=="b.md" { pub.Err=errors.New("injected post-publication close failure") }; return pub`
			}
			binary := buildApplyBoundaryFixture(t, body)
			for _, format := range []string{"json", "text"} {
				for _, deadline := range []string{"reporting-time=2s", "reporting-time=1ns"} {
					root := configurationProject(t)
					putConfigurationFile(t, root, ".mdchk.yaml", md047Configuration(true, true))
					for _, p := range []string{"a.md", "b.md", "c.md"} {
						putConfigurationFile(t, root, p, p)
					}
					got := runAppliedFix(t, binary, root, "fix", "a.md", "b.md", "c.md", "--format="+format, "--limit="+deadline)
					if got.Status != 13 {
						t.Fatal(got)
					}
					for i, p := range []string{"a.md", "b.md", "c.md"} {
						//nolint:gosec // Access only synthetic test fixtures or the fixed repository source overlay.
						content, e := os.ReadFile(filepath.Join(root, p))
						want := p + "\n"
						status := "written"
						if p == "b.md" && mode == "refused" {
							want = p
							status = "refused"
						}
						if e != nil || string(content) != want {
							t.Fatal(p, string(content), e)
						}
						if format == "json" {
							f := got.Result.Files[i]
							if f.WriteStatus != status || f.ResultSnapshot == nil || f.ResultSnapshot.View != "saved" || f.ResultSnapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256(content)) {
								t.Fatal(f)
							}
						}
					}
				}
			}
		})
	}
}

func TestCompiledApplyHardLinkConsequences(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", md047Configuration(true, true))
	putConfigurationFile(t, root, "doc.md", "hello")
	target, alias := filepath.Join(root, "doc.md"), filepath.Join(root, "unselected.md")
	if e := os.Link(target, alias); e != nil {
		t.Fatal(e)
	}
	original, _ := os.Stat(alias)
	got := runAppliedFix(t, binary, root, "fix", "doc.md", "--format=json")
	//nolint:gosec // Read only the unselected alias of the synthetic target.
	old, e := os.ReadFile(alias)
	if e != nil {
		t.Fatal(e)
	}
	current, _ := os.Stat(target)
	retained, _ := os.Stat(alias)
	if got.Status != 0 || string(old) != "hello" || !os.SameFile(original, retained) || os.SameFile(current, retained) {
		t.Fatal(got, string(old))
	}
	found := false
	for _, raw := range got.Result.Notes {
		var note struct{ Code, Path, Message string }
		if e := json.Unmarshal(raw, &note); e != nil {
			t.Fatal(e)
		}
		if note.Code == "hard-link-replacement" && note.Path == "doc.md" && strings.Contains(note.Message, "link count 2") {
			found = true
		}
	}
	if !found {
		t.Fatal("missing hard-link consequence note", got)
	}
}

func TestCompiledApplyTextManifestAdmissionBeforeWrites(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", md047Configuration(true, true))
	putConfigurationFile(t, root, "doc.md", "hello")
	path := filepath.Join(root, "doc.md")
	before, e := os.Stat(path)
	if e != nil {
		t.Fatal(e)
	}
	inventory := configurationBytes(t, root)
	got := runAppliedFix(t, binary, root, "fix", "doc.md", "--format=text", "--limit=response-bytes=1024")
	after, e := os.Stat(path)
	if e != nil {
		t.Fatal(e)
	}
	if got.Status == 0 || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) || before.Mode() != after.Mode() || !reflect.DeepEqual(inventory, configurationBytes(t, root)) {
		t.Fatal("text manifest admission allowed a write", got)
	}
}

// A real acquisition refusal must not turn independent native writes into a
// whole-operation transaction, including when all files depend on configuration.
func TestCompiledApplyContinuesAfterReadOnlyAcquisition(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, format := range []string{"json", "text"} {
		t.Run(format, func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", md047Configuration(true, true))
			before := map[string]os.FileInfo{}
			for _, name := range []string{"a.md", "b.md", "c.md"} {
				putConfigurationFile(t, root, name, name)
				path := filepath.Join(root, name)
				mode := os.FileMode(0640)
				if name == "b.md" {
					mode = 0444
				}
				if err := os.Chmod(path, mode); err != nil {
					t.Fatal(err)
				}
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				before[name] = info
			}
			inventory := configurationBytes(t, root)
			got := runAppliedFix(t, binary, root, "fix", "a.md", "b.md", "c.md", "--format="+format)
			if got.Status != 13 {
				t.Fatal(got)
			}
			for _, name := range []string{"a.md", "b.md", "c.md"} {
				path := filepath.Join(root, name)
				want := name + "\n"
				if name == "b.md" {
					want = name
				}
				//nolint:gosec // Read only independently created synthetic target files.
				data, err := os.ReadFile(path)
				if err != nil || string(data) != want {
					t.Fatal(name, string(data), err, got)
				}
				after, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if after.Mode() != before[name].Mode() || os.SameFile(before[name], after) != (name == "b.md") {
					t.Fatal("wrong identity or permissions", name)
				}
				if name == "b.md" && !before[name].ModTime().Equal(after.ModTime()) {
					t.Fatal("failed target mtime changed")
				}
				inventory[path] = sha256.Sum256([]byte(want))
				if format == "json" {
					found := false
					for _, f := range got.Result.Files {
						if f.Path != name {
							continue
						}
						found = true
						if name == "b.md" {
							if f.Analysis != "failed" || f.WriteStatus != "not-requested" || f.InputSnapshot != nil || f.ResultSnapshot != nil || len(f.ErrorIDs) == 0 || len(f.ChangeIDs) != 0 {
								t.Fatal("invented acquisition/write state", f)
							}
						} else if f.WriteStatus != "written" || f.Analysis != "complete" || f.ResultSnapshot == nil || f.ResultSnapshot.View != "saved" || f.ResultSnapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256(data)) {
							t.Fatal("missing saved authority", f)
						}
					}
					if !found {
						t.Fatal("lost target", name, got)
					}
				}
			}
			if !reflect.DeepEqual(inventory, configurationBytes(t, root)) {
				t.Fatal("unexpected artifact or sibling mutation")
			}
			if format == "json" {
				if len(got.Result.Changes) != 2 || len(got.Result.Errors) != 1 || got.Result.Errors[0].Path != "b.md" || got.Result.Errors[0].Category != "io" {
					t.Fatal(got)
				}
				for _, change := range got.Result.Changes {
					if change.Path == "b.md" || change.Disposition != "written" {
						t.Fatal(change)
					}
				}
			} else if !strings.Contains(got.Stdout, "written a.md") || !strings.Contains(got.Stdout, "written c.md") || strings.Contains(got.Stdout, "written b.md") || !strings.Contains(got.Stdout, "b.md") {
				t.Fatal(got.Stdout)
			}
		})
	}
}
