package main

import (
	"crypto/sha1" //nolint:gosec // Required descriptive checksum acceptance.
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	inspection "github.com/frithuz/mdchk/internal/inspect"
	"github.com/frithuz/mdchk/internal/result"
)

func runInspection(t *testing.T, binary, cwd, snapshotRoot string, args ...string) (configurationRun, inspection.Projection) {
	t.Helper()
	before := configurationBytes(t, snapshotRoot)
	beforeModes := inspectionModes(t, snapshotRoot)
	defer func() {
		if !reflect.DeepEqual(before, configurationBytes(t, snapshotRoot)) || !reflect.DeepEqual(beforeModes, inspectionModes(t, snapshotRoot)) {
			t.Error("inspect changed source/configuration bytes or inventory")
		}
	}()
	got := runConfigurationProcess(t, binary, cwd, args...)
	p := inspection.Projection{}
	for _, arg := range args {
		if arg == "--format=json" {
			got.Result = validateConfigurationOperationJSON(t, []byte(got.Stdout), "inspect")
			if len(got.Result.Inspection) > 0 {
				if err := json.Unmarshal(got.Result.Inspection, &p); err != nil {
					t.Fatal(err)
				}
			}
			break
		}
	}
	return got, p
}

func TestCompiledInspectionWorkflow(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", "version: 1\nflavor: commonmark\n")
	source := "# 雪\r\n\r\n<!-- mdchk:disable MD001 MD013 -->\r\n> one\r\n> two\r\n\r\n"
	putConfigurationFile(t, root, "README.md", source)
	putConfigurationFile(t, root, "docs/guide.md", strings.Repeat("paragraph\n\n", 20))
	for _, args := range [][]string{{"inspect", "README.md"}, {"inspect", filepath.Join(root, "docs/guide.md")}, {"inspect", "docs/guide.md", "--lines", "18-30"}} {
		text, _ := runInspection(t, binary, root, root, args...)
		if text.Status != 0 {
			t.Fatal(text)
		}
		for _, label := range []string{"File identity", "Resolved path:", "Binary version:", "Size:", "SHA-1:", "SHA-256:", "Selected lines:", "Configuration and provenance", "Markdown flavor", "Abstract syntax tree:", "Rules run:", "Rule counts", "Directives", "Raw directive candidates", "Parsing notes", "Resources", "Findings and suppressed findings", "inspected"} {
			if !strings.Contains(text.Stdout, label) {
				t.Fatalf("missing text section %s", label)
			}
		}
		jsonArgs := append(append([]string{}, args...), "--format=json")
		got, p := runInspection(t, binary, root, root, jsonArgs...)
		if got.Status != 0 || got.Result.Status != "inspected" || p.ProjectionVersion != 1 || p.RuleCounts.Availability != "available" || len(*p.Rules.Data) != 89 {
			t.Fatalf("unexpected full report: %+v", got)
		}
	}
	full, p := runInspection(t, binary, root, root, "inspect", "README.md", "--format=json")
	//nolint:gosec // Required descriptive SHA-1 metadata, not authentication.
	one := sha1.Sum([]byte(source))
	two := sha256.Sum256([]byte(source))
	if p.File.ResolvedPath != filepath.Join(root, "README.md") || p.File.SizeBytes != int64(len(source)) || p.File.SHA1 != hex.EncodeToString(one[:]) || p.File.SHA256 != hex.EncodeToString(two[:]) || p.File.BinaryVersion == "" || p.File.PhysicalLineCount != 6 || full.Result.CheckedFiles[0] != "README.md" || full.Result.Files[0].InputSnapshot.SHA256 != p.File.SHA256 {
		t.Fatal(p.File, full.Result.Files)
	}
	if full.Result.Complete || len(full.Result.Coverage) == 0 {
		t.Fatal("default unavailable implementations looked complete")
	}
	selected, q := runInspection(t, binary, root, root, "inspect", "--lines=4-5", "--format=json", "README.md")
	if selected.Status != 0 || q.File.SHA256 != p.File.SHA256 || q.File.SelectedLines.Start != 4 || q.File.SelectedRegion.ByteRange.Start != strings.Index(source, "> one") || !(*q.Directives.Data)[0].ContextOnly || q.AST.Data.Root.ByteRange.End != len(source) {
		t.Fatal(q)
	}
	for _, s := range q.Configuration.Data.Sources {
		if filepath.IsAbs(s.Path) {
			t.Fatal(s)
		}
	}
	for _, s := range q.Configuration.Data.Provenance {
		if filepath.IsAbs(s.Path) {
			t.Fatal(s)
		}
	}
	// Both option orders, literal dash path, and metadata-only global forms.
	putConfigurationFile(t, root, "--draft.md", "text\n")
	for _, args := range [][]string{{"inspect", "--format=json", "--", "--draft.md"}, {"inspect", "--help"}, {"--help", "inspect"}, {"inspect", "--version"}, {"--version", "inspect"}} {
		got := runConfigurationProcess(t, binary, root, args...)
		if got.Status != 0 {
			t.Fatal(got)
		}
	}
}

func TestCompiledInspectionObservationsBoundsAndAccess(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	for _, tc := range []struct {
		name, source, yaml string
		lines              string
		status             int
		ast                string
	}{
		{"empty", "", "version: 1\nflavor: commonmark\n", "", 0, "available"},
		{"unicode CRLF", "雪\r\nπ\r\n", "version: 1\nflavor: commonmark\n", "2", 0, "available"},
		{"lone CR", "a\rb\r", "version: 1\nflavor: commonmark\n", "2", 0, "available"},
		{"no newline", "a", "version: 1\nflavor: commonmark\n", "1", 0, "available"},
		{"invalid UTF8", "bad\xff", "version: 1\nflavor: commonmark\n", "", 0, "unavailable"},
		{"default GFM", "a\n", "version: 1\n", "", 0, "available"},
		{"unavailable Pandoc", "a\n", "version: 1\nflavor: pandoc\n", "", 0, "unavailable"},
		{"invalid config", "a\n", "unknown: [", "", 0, "unavailable"},
		{"excluded explicit", "a\n", "version: 1\nflavor: commonmark\nexclude: ['**']\n", "", 0, "available"},
		{"empty range", "", "version: 1\nflavor: commonmark\n", "1", 11, ""},
		{"phantom terminal line", "a\n", "version: 1\nflavor: commonmark\n", "2", 11, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := configurationProject(t)
			putConfigurationFile(t, root, ".mdchk.yaml", tc.yaml)
			putConfigurationFile(t, root, "doc.md", tc.source)
			args := []string{"inspect", "doc.md", "--format=json"}
			if tc.lines != "" {
				args = append(args, "--lines="+tc.lines)
			}
			got, p := runInspection(t, binary, root, root, args...)
			if got.Status != tc.status {
				t.Fatalf("status %d: %s", got.Status, got.Stdout)
			}
			if tc.status == 0 && (got.Result.Status != "inspected" || p.AST.Availability != tc.ast) {
				t.Fatal(got, p)
			}
			if tc.name == "excluded explicit" && !p.File.NormallyExcluded {
				t.Fatal(p.File)
			}
			if tc.ast == "unavailable" && (got.Result.Complete || p.RuleCounts.Availability != "unavailable") {
				t.Fatal(p, got.Result)
			}
		})
	}
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", "bad: [")
	putConfigurationFile(t, root, "doc.md", "text\n")
	for _, args := range [][]string{{"inspect"}, {"inspect", "doc.md", "doc.md"}, {"inspect", "--dry-run", "doc.md"}, {"inspect", "--lines=0", "missing.md"}, {"inspect", "--lines=01", "missing.md"}, {"inspect", "--lines=2-1", "missing.md"}, {"inspect", "--lines=1-2-3", "missing.md"}, {"inspect", "--lines=9999999999999999999999", "missing.md"}, {"inspect", "--lines=", "missing.md"}, {"inspect", "--limit=resource-lookups=1", "missing.md"}, {"inspect", "--limit=work-units=1", "--limit=work-units=2", "missing.md"}, {"inspect", "."}} {
		args = append(args, "--format=json")
		got, _ := runInspection(t, binary, root, root, args...)
		if got.Status != 11 || len(got.Result.CheckedFiles) != 0 || len(got.Result.Errors) != 1 || got.Result.Errors[0].Category != result.Usage {
			t.Fatal(args, got)
		}
	}
	for _, f := range []string{"vscode", "github", "yaml"} {
		got := runConfigurationProcess(t, binary, root, "inspect", "missing.md", "--format="+f)
		if got.Status != 11 || strings.Contains(got.Stdout, "acquisition-failed") {
			t.Fatal(got)
		}
	}
	if info, err := os.Stat(os.DevNull); err == nil && !info.Mode().IsRegular() {
		got, _ := runInspection(t, binary, root, root, "inspect", os.DevNull, "--format=json")
		if got.Status != 11 {
			t.Fatal(got)
		}
	}
	got, _ := runInspection(t, binary, root, root, "inspect", "missing.md", "--format=json")
	if got.Status != 13 {
		t.Fatal(got)
	}
	// Real permission boundaries run where the host actually denies mode 000.
	for _, name := range []string{"doc.md", ".mdchk.yaml"} {
		path := filepath.Join(root, name)
		if err := os.Chmod(path, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(path, 0600) })
		//nolint:gosec // Test-owned target/config paths exercise real permissions.
		_, readErr := os.ReadFile(path)
		if readErr == nil {
			t.Logf("permission case %s unavailable for privileged process", name)
			_ = os.Chmod(path, 0600)
			continue
		}
		got := runConfigurationProcess(t, binary, root, "inspect", "doc.md", "--format=json")
		want := 13
		if name == ".mdchk.yaml" {
			want = 0
		}
		if got.Status != want {
			t.Fatal(got)
		}
		validateConfigurationOperationJSON(t, []byte(got.Stdout), "inspect")
		if err := os.Chmod(path, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCompiledInspectionTargetConfigAndSymlinks(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", "version: 1\nflavor: commonmark\n")
	putConfigurationFile(t, root, "child/.mdchk.yaml", "version: 1\nflavor: commonmark\nexclude: ['**']\n")
	putConfigurationFile(t, root, "child/deep/doc.md", "child\n")
	putConfigurationFile(t, root, "caller/local.yaml", "version: 1\nflavor: commonmark\n")
	target := filepath.Join(root, "child/deep/doc.md")
	caller := filepath.Join(root, "caller")
	got, p := runInspection(t, binary, caller, root, "inspect", target, "--format=json")
	if got.Status != 0 || !p.File.NormallyExcluded || got.Result.CheckedFiles[0] != "../child/deep/doc.md" || p.Configuration.Data.Sources[1].Path != "../child/.mdchk.yaml" {
		t.Fatal(got, p)
	}
	got, p = runInspection(t, binary, caller, root, "inspect", target, "--config=local.yaml", "--format=json")
	if got.Status != 0 || p.Configuration.Data.Sources[1].Path != "local.yaml" || p.File.NormallyExcluded {
		t.Fatal(got, p)
	}
	if err := os.Symlink(filepath.Join(root, "child/deep"), filepath.Join(caller, "jump")); err != nil {
		t.Skip(err)
	}
	// Raw jump/.. must resolve in the target project before lexical cleaning.
	putConfigurationFile(t, root, "child/doc.md", "resolved parent\n")
	got, p = runInspection(t, binary, caller, root, "inspect", "jump/../doc.md", "--format=json")
	if got.Status != 0 || p.File.ResolvedPath != filepath.Join(root, "child/doc.md") || got.Result.CheckedFiles[0] != "../child/doc.md" {
		t.Fatal(got, p.File)
	}
	if err := os.Symlink(target, filepath.Join(caller, "link.md")); err != nil {
		t.Fatal(err)
	}
	got, p = runInspection(t, binary, caller, root, "inspect", "link.md", "--format=json")
	if got.Status != 0 || p.File.ResolvedPath != target {
		t.Fatal(got, p.File)
	}
}

func TestCompiledInspectionBoundedOutput(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", "version: 1\nflavor: commonmark\n")
	putConfigurationFile(t, root, "doc.md", strings.Repeat("paragraph <&> 雪\n\n", 180))
	for _, format := range []string{"json", "text"} {
		for _, limit := range []string{"response-bytes=64KiB", "response-bytes=96KiB", "reporting-time=1ns", "reporting-time=1us", "work-units=1", "operation-time=1ns"} {
			t.Run(format+"/"+limit, func(t *testing.T) {
				got, p := runInspection(t, binary, root, root, "inspect", "doc.md", "--format="+format, "--limit="+limit)
				if got.Status != 15 || len(got.Stdout) == 0 {
					t.Fatal(got)
				}
				if format == "json" {
					if got.Result.Status != "incomplete" || got.Result.Complete || len(got.Result.Interruptions) == 0 {
						t.Fatal(got)
					}
					if limit == "response-bytes=64KiB" && len(got.Stdout) > 65536 || limit == "response-bytes=96KiB" && len(got.Stdout) > 98304 {
						t.Fatal("oversized report")
					}
					if strings.HasPrefix(limit, "reporting") && (len(got.Result.Files) != 1 || p.File.SHA256 == "") {
						t.Fatal("reserve lost acquired identity")
					}
				}
			})
		}
	}
	for _, yaml := range []string{"unknown: [", "version: 1\nflavor: commonmark\n"} {
		putConfigurationFile(t, root, ".mdchk.yaml", yaml)
		putConfigurationFile(t, root, "doc.md", "bad\xff")
		for _, deadline := range []string{"1ns", "1us"} {
			got, p := runInspection(t, binary, root, root, "inspect", "doc.md", "--format=json", "--limit=reporting-time="+deadline)
			// A positive deadline permits a genuinely completed observation. In
			// particular, this four-byte report can finish inside 1us on Linux.
			switch got.Status {
			case 0:
				if deadline != "1us" || got.Result.Status != "inspected" || len(got.Result.Interruptions) != 0 {
					t.Fatalf("deadline %s completed outcome: %+v", deadline, got)
				}
			case 15:
				if got.Result.Status != "incomplete" || len(got.Result.Interruptions) != 1 || got.Result.Interruptions[0].Phase != "reporting" || got.Result.Interruptions[0].Reason != "deadline-exceeded" {
					t.Fatalf("deadline %s interrupted outcome: %+v", deadline, got)
				}
			default:
				t.Fatalf("deadline %s unexpected exit: %+v", deadline, got)
			}
			wantHash := sha256.Sum256([]byte("bad\xff"))
			if got.Result.Complete || len(got.Result.Files) != 1 || got.Result.Files[0].InputSnapshot == nil || got.Result.Files[0].InputSnapshot.SHA256 != hex.EncodeToString(wantHash[:]) || got.Result.Files[0].InputSnapshot.SizeBytes != 4 || p.File.SHA256 != hex.EncodeToString(wantHash[:]) || p.File.SizeBytes != 4 || p.AST.Availability != "unavailable" || p.AST.Data != nil {
				t.Fatalf("deadline %s lost identity/unavailable decoding: %+v, %+v", deadline, got, p)
			}
			categories := map[result.ErrorCategory]int{}
			for _, e := range got.Result.Errors {
				if e.Scope != "observation" {
					t.Fatalf("deadline %s invented operation error: %+v", deadline, e)
				}
				categories[e.Category]++
			}
			wantCategories := map[result.ErrorCategory]int{result.IO: 1}
			if yaml == "unknown: [" {
				wantCategories[result.Configuration] = 1
			}
			if !reflect.DeepEqual(categories, wantCategories) {
				t.Fatalf("deadline %s observation categories=%v, want %v", deadline, categories, wantCategories)
			}
			if got.Status == 0 {
				wantAvailability := "available"
				if yaml == "unknown: [" {
					wantAvailability = "unavailable"
					if got.Result.Errors[0].Code != "configuration-invalid" {
						t.Fatalf("completed observation lost configuration detail: %+v", got)
					}
				}
				if p.Configuration.Availability != wantAvailability {
					t.Fatalf("completed configuration availability=%s, want %s", p.Configuration.Availability, wantAvailability)
				}
				last := got.Result.Errors[len(got.Result.Errors)-1]
				if last.Code != "invalid-utf8" || last.ByteOffset == nil || *last.ByteOffset != 3 || last.Snapshot == nil || last.Snapshot.SHA256 != p.File.SHA256 || p.AST.Availability != "unavailable" {
					t.Fatalf("completed observation lost UTF8 detail: %+v", got)
				}
			}
		}
	}
	putConfigurationFile(t, root, ".mdchk.yaml", "version: 1\nflavor: commonmark\n")

	// Newlines in line-oriented user data cannot forge a report heading.
	putConfigurationFile(t, root, "line\nbreak.md", "```\na\rb\n```\n")
	got, _ := runInspection(t, binary, root, root, "inspect", "line\nbreak.md")
	if got.Status != 0 || strings.Contains(got.Stdout, "/line\nbreak.md") || !strings.Contains(got.Stdout, "line\\nbreak.md") {
		t.Fatalf("escaped text: %q", got.Stdout)
	}
}

func TestCompiledInspectionSyntheticLintParity(t *testing.T) {
	binary := buildConfigurationFixtureExecutable(t)
	root := configurationProject(t)
	yaml := directiveFixtureConfiguration(true, false)
	putConfigurationFile(t, root, ".mdchk.yaml", yaml)
	for _, source := range []string{"雪 fixture violation\r\n", "<!-- mdchk:disable MD001 -->fixture violation\n", "<!-- mdchk:disable MD001 --><!-- mdchk:disable MD001 -->fixture violation\n"} {
		putConfigurationFile(t, root, "doc.md", source)
		lint := runConfiguration(t, binary, root, root, "lint", "doc.md", "--format=json")
		got, p := runInspection(t, binary, root, root, "inspect", "doc.md", "--format=json")
		if got.Status != 0 || !reflect.DeepEqual(lint.Result.Diagnostics, got.Result.Diagnostics) || !reflect.DeepEqual(lint.Result.Files[0].InputSnapshot, got.Result.Files[0].InputSnapshot) || len(lint.Result.SuppressedDiagnostics) != len(got.Result.SuppressedDiagnostics) {
			t.Fatal(lint, got)
		}
		for _, raw := range got.Result.SuppressedDiagnostics {
			var h result.SuppressedDiagnostic
			_ = json.Unmarshal(raw, &h)
			if h.SuppressedBy.DirectiveID == "" || len(*p.Directives.Data) == 0 {
				t.Fatal(h)
			}
		}
	}
}

func inspectionModes(t *testing.T, root string) map[string]fs.FileMode {
	t.Helper()
	modes := map[string]fs.FileMode{}
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		modes[path] = info.Mode()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return modes
}

func TestCompiledInspectionTerminalControlsAndOptionDiagnostics(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", "version: 1\nflavor: commonmark\n")
	name := "review\x1b[2J\x1b[H\b\x7f\u009b.md"
	putConfigurationFile(t, root, name, "雪\u009b\x1b[2J\n")
	text, _ := runInspection(t, binary, root, root, "inspect", name)
	if text.Status != 0 || strings.ContainsAny(text.Stdout, "\x1b\b\x7f\u009b") || !strings.Contains(text.Stdout, `review\x1b[2J\x1b[H\b\x7f\u009b.md`) {
		t.Fatalf("unescaped terminal controls: %q", text.Stdout)
	}
	got, p := runInspection(t, binary, root, root, "inspect", name, "--format=json")
	if got.Status != 0 || got.Result.CheckedFiles[0] != name || p.File.ResolvedPath != filepath.Join(root, name) {
		t.Fatal("JSON identity changed")
	}
	for _, flag := range []string{"--dry-run", "--unknown"} {
		for _, format := range []string{"text", "json"} {
			got, _ := runInspection(t, binary, root, root, "inspect", flag, "missing.md", "--format="+format)
			if got.Status != 11 || !strings.Contains(got.Stdout, "unsupported option") || strings.Contains(got.Stdout, "unsupported lint option") {
				t.Fatal(got)
			}
		}
	}
}
