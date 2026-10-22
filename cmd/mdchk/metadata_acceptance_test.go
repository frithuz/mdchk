package main

import (
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"strings"
	"testing"

	inspection "github.com/frithuz/mdchk/internal/inspect"
)

func TestVersionFromBuildInfo(t *testing.T) {
	settings := []debug.BuildSetting{{Key: "vcs.revision", Value: "abc"}, {Key: "vcs.modified", Value: "true"}}
	for _, tc := range []struct {
		name, version, want string
		ok, absent          bool
	}{
		{"release", "v1.2.3", "v1.2.3", true, false},
		{"pseudo", "v0.0.0-20261022121121-14d3d0c384a9", "v0.0.0-20261022121121-14d3d0c384a9", true, false},
		{"empty", "", "dev", true, false}, {"devel", "(devel)", "dev", true, false},
		{"unavailable", "v1.2.3", "dev", false, false}, {"nil", "", "dev", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &debug.BuildInfo{Main: debug.Module{Version: tc.version}, Settings: settings}
			if tc.absent {
				info = nil
			}
			if got := versionFromBuildInfo(info, tc.ok); got != tc.want {
				t.Fatalf("version=%q want %q", got, tc.want)
			}
		})
	}
}

func TestCompiledMetadataPolicy(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := metadataProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", "invalid: [")
	before := configurationBytes(t, root)
	version := runConfigurationProcess(t, binary, root, "--version").Stdout
	for _, command := range [][]string{nil, {"lint"}, {"fix"}, {"inspect"}, {"config"}, {"config", "validate"}} {
		for _, flag := range []string{"--help", "--version"} {
			for _, prefix := range []bool{false, true} {
				args := append([]string{}, command...)
				if prefix {
					args = append([]string{flag}, args...)
				} else {
					args = append(args, flag)
				}
				// Targets are operational requirements; they do not restrict valid metadata.
				if len(command) > 0 && command[len(command)-1] != "config" {
					args = append(args, "missing-a", "missing-b")
				}
				got := runConfigurationProcess(t, binary, root, args...)
				want := "Usage: mdchk"
				if len(command) > 0 {
					want += " " + strings.Join(command, " ")
				}
				if got.Status != 0 || flag == "--version" && got.Stdout != version || flag == "--help" && !strings.HasPrefix(got.Stdout, want) {
					t.Fatalf("%v: %+v", args, got)
				}
			}
		}
		args := append(append([]string{}, command...), "--version", "--help")
		got := runConfigurationProcess(t, binary, root, args...)
		if got.Status != 0 || !strings.HasPrefix(got.Stdout, "Usage: mdchk") {
			t.Fatalf("help precedence %v: %+v", args, got)
		}
	}
	for _, args := range [][]string{
		{"config", "--help", "validate", "a", "b"}, {"config", "--version", "validate", "a", "b"},
		{"lint", "--help", "--config=missing.yaml", "missing"}, {"fix", "--version", "--config", "missing.yaml", "missing"},
		{"inspect", "--help", "--config=missing.yaml", "missing"},
	} {
		if got := runConfigurationProcess(t, binary, root, args...); got.Status != 0 {
			t.Fatalf("metadata %v: %+v", args, got)
		}
	}
	for _, args := range [][]string{
		{"--help", "unknown"}, {"--version", "mcp"}, {"config", "--help", "unknown"},
		{"lint", "--help", "--format"}, {"fix", "--help", "--config="},
		{"lint", "--format", "--help"}, {"lint", "--help", "--dry-run"},
		{"fix", "--version", "--lines=1"}, {"inspect", "--help", "--dry-run"},
		{"inspect", "--version", "--format=github"}, {"inspect", "--help", "--format=vscode"},
		{"inspect", "--help", "--lines=0"}, {"inspect", "--version", "--lines=3-1"},
		{"config", "validate", "--help", "--config=x"}, {"config", "validate", "--help", "--dry-run"},
		{"lint", "--help", "--limit=operation-time=0s"}, {"config", "validate", "--version", "--limit=target-files=1"},
		{"inspect", "a", "b"}, {"config", "validate", "a", "b"},
	} {
		if got := runConfigurationProcess(t, binary, root, args...); got.Status != 11 || strings.HasPrefix(got.Stdout, "Usage:") {
			t.Fatalf("invalid %v: %+v", args, got)
		}
	}
	if !reflect.DeepEqual(before, configurationBytes(t, root)) {
		t.Fatal("metadata changed project bytes/inventory")
	}
}

func TestCompiledArgumentOrderingAndLiteralMetadataPaths(t *testing.T) {
	binary := buildConfigurationExecutable(t, "")
	root := metadataProject(t) // No fake Git metadata: validation must complete normally.
	putConfigurationFile(t, root, ".mdchk.yaml", md047Configuration(true, true))
	putConfigurationFile(t, root, "doc.md", "text")
	for _, pair := range [][2][]string{
		{{"lint", "doc.md", "--config=.mdchk.yaml", "--format=json"}, {"lint", "--format", "json", "--config", ".mdchk.yaml", "doc.md"}},
		{{"fix", "doc.md", "--dry-run", "--format=json"}, {"fix", "--format", "json", "--dry-run", "doc.md"}},
		{{"inspect", "doc.md", "--lines=1", "--format=json"}, {"inspect", "--format", "json", "--lines", "1", "doc.md"}},
		{{"config", "validate", ".", "--format=json"}, {"config", "validate", "--format", "json", "."}},
	} {
		a := runConfigurationProcess(t, binary, root, pair[0]...)
		b := runConfigurationProcess(t, binary, root, pair[1]...)
		var x, y any
		if json.Unmarshal([]byte(a.Stdout), &x) != nil || json.Unmarshal([]byte(b.Stdout), &y) != nil {
			t.Fatal(a, b)
		}
		if a.Status != b.Status || !reflect.DeepEqual(x, y) {
			t.Fatalf("ordering %v / %v: %+v / %+v", pair[0], pair[1], a, b)
		}
		want := 0
		if pair[0][0] == "lint" {
			want = 10
		}
		if a.Status != want {
			t.Fatalf("ordering did not actually execute: %+v", a)
		}
	}
	for _, name := range []string{"--help", "--version"} {
		raw := "literal " + name + "\n"
		putConfigurationFile(t, root, name, raw)
		hash := sha256.Sum256([]byte(raw))
		for _, command := range []string{"lint", "fix", "inspect"} {
			args := []string{command, "--format=json"}
			if command == "fix" {
				args = append(args, "--dry-run")
			}
			args = append(args, "--", name)
			before := configurationBytes(t, root)
			got := runConfigurationProcess(t, binary, root, args...)
			got.Result = validateConfigurationOperationJSON(t, []byte(got.Stdout), command)
			if !reflect.DeepEqual(before, configurationBytes(t, root)) {
				t.Fatal("literal path command changed inputs")
			}
			if got.Status != 0 || len(got.Result.Files) != 1 || got.Result.Files[0].Path != name || got.Result.Files[0].InputSnapshot.SHA256 != hex.EncodeToString(hash[:]) {
				t.Fatalf("literal %v: %+v", args, got)
			}
		}
	}
}

func TestCompiledBuildVersionPropagation(t *testing.T) {
	root := metadataProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", cleanConfiguration())
	putConfigurationFile(t, root, "doc.md", "text\n")
	normal := buildConfigurationExecutable(t, "")
	development := filepath.Join(t.TempDir(), filepath.Base(normal))
	//nolint:gosec // Fixed Go tool and a test-owned binary destination.
	command := exec.Command("go", "build", "-buildvcs=false", "-o", development, ".")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build dev: %v: %s", err, output)
	}
	// Supply metadata at the real executable's read boundary, keeping dispatch,
	// version formatting and both inspection renderers unchanged.
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	source := strings.Replace(string(data), "versionFromBuildInfo(debug.ReadBuildInfo())", `versionFromBuildInfo(&debug.BuildInfo{Main: debug.Module{Version: "v9.8.7"}, Settings: []debug.BuildSetting{{Key: "vcs.modified", Value: "true"}}}, true)`, 1)
	dir := t.TempDir()
	replacement := filepath.Join(dir, "main.go")
	//nolint:gosec // Test-owned temporary replacement; source is the fixed local main.go.
	if err := os.WriteFile(replacement, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	overlay, err := json.Marshal(struct{ Replace map[string]string }{map[string]string{filepath.Join(cwd, "main.go"): replacement}})
	if err != nil {
		t.Fatal(err)
	}
	overlayPath := filepath.Join(dir, "overlay.json")
	if err := os.WriteFile(overlayPath, overlay, 0600); err != nil {
		t.Fatal(err)
	}
	supplied := buildConfigurationExecutable(t, overlayPath)
	for _, tc := range []struct{ name, binary, want string }{{"normal", normal, ""}, {"development", development, "dev"}, {"supplied", supplied, "v9.8.7"}} {
		t.Run(tc.name, func(t *testing.T) {
			reported := runConfigurationProcess(t, tc.binary, root, "--version")
			if reported.Status != 0 || !strings.HasPrefix(reported.Stdout, "mdchk ") || strings.Count(reported.Stdout, "\n") != 1 {
				t.Fatal(reported)
			}
			if tc.name == "normal" {
				info, err := buildinfo.ReadFile(tc.binary)
				if err != nil {
					t.Fatal(err)
				}
				if reported.Stdout != "mdchk "+versionFromBuildInfo(info, true)+"\n" {
					t.Fatal("normal metadata mismatch", reported, info)
				}
			}
			version := strings.TrimSuffix(strings.TrimPrefix(reported.Stdout, "mdchk "), "\n")
			if tc.want != "" && version != tc.want {
				t.Fatalf("version=%q want %q", version, tc.want)
			}
			observed := runConfigurationProcess(t, tc.binary, root, "inspect", "doc.md", "--format=json")
			r := validateConfigurationOperationJSON(t, []byte(observed.Stdout), "inspect")
			var p inspection.Projection
			if err := json.Unmarshal(r.Inspection, &p); err != nil {
				t.Fatal(err)
			}
			text := runConfigurationProcess(t, tc.binary, root, "inspect", "doc.md")
			if observed.Status != 0 || p.File.BinaryVersion != version || text.Status != 0 || !strings.Contains(text.Stdout, "Binary version: "+version+"\n") {
				t.Fatal(version, p.File, text)
			}
		})
	}
}

// metadataProject avoids symlinked temporary ancestors without inventing Git metadata.
func metadataProject(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}
