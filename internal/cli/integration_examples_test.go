package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

type examplePattern struct {
	Regexp   string `json:"regexp"`
	File     int    `json:"file"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Severity int    `json:"severity"`
	Code     int    `json:"code"`
	Message  int    `json:"message"`
}
type exampleTask struct {
	Label   string   `json:"label"`
	Type    string   `json:"type"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Options struct {
		CWD string `json:"cwd"`
	} `json:"options"`
	ProblemMatcher struct {
		Owner        string         `json:"owner"`
		FileLocation []string       `json:"fileLocation"`
		Pattern      examplePattern `json:"pattern"`
	} `json:"problemMatcher"`
}

func integrationDocument(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(executionPackageDir, "../../docs/output-formats.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
func integrationFence(t *testing.T, doc, language string) string {
	t.Helper()
	parts := strings.Split(doc, "```"+language+"\n")
	if len(parts) != 2 {
		t.Fatalf("expected exactly one %s integration fence, got %d", language, len(parts)-1)
	}
	block, _, found := strings.Cut(parts[1], "```")
	if !found {
		t.Fatal("unclosed integration fence")
	}
	return block
}
func integrationTask(t *testing.T, doc string) exampleTask {
	t.Helper()
	var wrapper struct {
		Version string        `json:"version"`
		Tasks   []exampleTask `json:"tasks"`
	}
	decoder := json.NewDecoder(strings.NewReader(integrationFence(t, doc, "json")))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wrapper); err != nil {
		t.Fatal(err)
	}
	if decoder.Decode(new(any)) != io.EOF || wrapper.Version != "2.0.0" || len(wrapper.Tasks) != 1 {
		t.Fatal("unexpected task document")
	}
	task := wrapper.Tasks[0]
	if task.Type != "process" || task.Command != "mdchk" || task.Options.CWD != "${workspaceFolder}" || task.ProblemMatcher.Owner != "mdchk" || !reflect.DeepEqual(task.ProblemMatcher.FileLocation, []string{"relative", "${workspaceFolder}"}) {
		t.Fatalf("unsupported integration task: %+v", task)
	}
	return task
}
func integrationWords(t *testing.T, command string) []string {
	t.Helper()
	// Deliberately support only the published plain argv grammar. Shell operators,
	// quotes, assignments and status masks must fail rather than be discarded.
	if !regexp.MustCompile(`^mdchk(?: [A-Za-z0-9./=-]+)+$`).MatchString(command) {
		t.Fatalf("unsupported documented command %q", command)
	}
	return strings.Split(command, " ")[1:]
}
func integrationWorkflow(t *testing.T, doc string) []string {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(integrationFence(t, doc, "yaml"), "\n"), "\n")
	if len(lines) != 2 || lines[0] != "- name: Check Markdown" || !strings.HasPrefix(lines[1], "  run: ") {
		t.Fatalf("unsupported workflow structure: %q", lines)
	}
	return integrationWords(t, strings.TrimPrefix(lines[1], "  run: "))
}
func integrationAsset(t *testing.T, doc, label string) []byte {
	t.Helper()
	pattern := regexp.MustCompile(`\[` + regexp.QuoteMeta(label) + `\]\((examples/editor-ci/[^)]+)\)`)
	matches := pattern.FindAllStringSubmatch(doc, -1)
	if len(matches) != 1 {
		t.Fatalf("missing/ambiguous published asset %s", label)
	}
	data, err := os.ReadFile(filepath.Join(executionPackageDir, "../../docs", filepath.FromSlash(matches[0][1])))
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func integrationRoot(t *testing.T, doc string) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(root, ".mdchk.yaml"), string(integrationAsset(t, doc, "example configuration")))
	put(t, filepath.Join(root, "docs/guide.md"), string(integrationAsset(t, doc, "example document")))
	return root
}
func integrationRecords(t *testing.T, task exampleTask, format string, got editorExecution) []workflowCommand {
	t.Helper()
	if format == "github" {
		records := editorObservations(t, format, got.stdout)
		for _, line := range strings.Split(got.stderr, "\n") {
			if parseWorkflow(line) != nil {
				t.Fatalf("executable recovery prose %q", line)
			}
		}
		return records
	}
	pattern := task.ProblemMatcher.Pattern
	re, err := regexp.Compile(pattern.Regexp)
	if err != nil {
		t.Fatal(err)
	}
	var records []workflowCommand
	for _, line := range strings.Split(strings.TrimSuffix(got.stdout, "\n"), "\n") {
		if line == "" {
			continue
		}
		captures := re.FindStringSubmatch(line)
		fields := []int{pattern.File, pattern.Line, pattern.Column, pattern.Severity, pattern.Code, pattern.Message}
		for _, index := range fields {
			if index <= 0 || index >= len(captures) {
				t.Fatalf("invalid matcher capture %d: %q", index, captures)
			}
		}
		if captures[pattern.Severity] != "error" {
			t.Fatalf("wrong severity %q", captures)
		}
		records = append(records, workflowCommand{name: "error", data: captures[pattern.Message], properties: map[string]string{"file": captures[pattern.File], "line": captures[pattern.Line], "column": captures[pattern.Column], "title": captures[pattern.Code]}})
	}
	return records
}
func assertIntegrationFinding(t *testing.T, task exampleTask, format string, got editorExecution, status int, path, line, column string) {
	t.Helper()
	if got.status != status || got.stderr != "" {
		t.Fatalf("documented command: %+v", got)
	}
	records := integrationRecords(t, task, format, got)
	if len(records) != 1 {
		t.Fatalf("expected one finding: %+v", got)
	}
	want := map[string]string{"file": path, "line": line, "title": "MD047"}
	if format == "vscode" {
		want["column"] = column
	}
	if !reflect.DeepEqual(records[0].properties, want) || records[0].data != "File must end with a line-ending sequence." {
		t.Fatalf("literal observation mismatch: %+v want %v", records[0], want)
	}
}
func TestDocumentedIntegrationExamples(t *testing.T) {
	doc := integrationDocument(t)
	task := integrationTask(t, doc)
	binary := editorExecutable(t)
	for _, format := range []string{"vscode", "github"} {
		t.Run(format, func(t *testing.T) {
			args := task.Args
			if format == "github" {
				args = integrationWorkflow(t, doc)
			}
			root := integrationRoot(t, doc)
			got := editorExecute(t, binary, root, args...)
			assertIntegrationFinding(t, task, format, got, 10, "docs/guide.md", "1", "5")
			// Actual JSON production provides complementary canonical snapshot truth;
			// the main oracle above remains independent literal fields and exit status.
			truth := editorExecute(t, binary, root, "lint", ".", "--format=json")
			r := validateEmitted(t, t.TempDir(), []byte(truth.stdout))
			if truth.status != 10 || len(r.Diagnostics) != 1 || r.Diagnostics[0].Range.Start.Column != 5 || r.Diagnostics[0].Rule != "MD047" {
				t.Fatal(truth, r)
			}
			put(t, filepath.Join(root, "docs/guide.md"), "雪\t😀x\n")
			if clean := editorExecute(t, binary, root, args...); clean.status != 0 || clean.stdout+clean.stderr != "" {
				t.Fatal("clean example", clean)
			}
			// Same extracted command encounters a real located invalid definition.
			put(t, filepath.Join(root, ".mdchk.yaml"), "version: 1\nflavor: commonmark\nrules:\n  MD013: {length: -1}\n")
			invalid := editorExecute(t, binary, root, args...)
			records := integrationRecords(t, task, format, invalid)
			if invalid.status != 12 || len(records) != 1 || records[0].properties["title"] != "config-definition" || records[0].properties["file"] != ".mdchk.yaml" || records[0].properties["line"] != "4" || invalid.stderr != "" {
				t.Fatal("definition example", invalid, records)
			}
			// The extracted directory command retains available findings on a real file
			// UTF-8 decoding failure (no permission-bit assumptions on root CI).
			put(t, filepath.Join(root, ".mdchk.yaml"), string(integrationAsset(t, doc, "example configuration")))
			put(t, filepath.Join(root, "docs/guide.md"), "雪\t😀x")
			put(t, filepath.Join(root, "large.md"), "\xff")
			partial := editorExecute(t, binary, root, args...)
			records = integrationRecords(t, task, format, partial)
			if partial.status != 13 {
				t.Fatal("partial example", partial)
			}
			found := false
			for _, record := range records {
				if record.properties["title"] == "MD047" && record.properties["file"] == "docs/guide.md" {
					found = true
				}
			}
			if !found || !strings.Contains(partial.stdout+partial.stderr, "large.md") {
				t.Fatal("lost partial evidence", partial)
			}
			put(t, filepath.Join(root, "large.md"), "clean\n")
			put(t, filepath.Join(root, ".mdchk.yaml"), "version: 1\nflavor: commonmark\n")
			incomplete := editorExecute(t, binary, root, args...)
			integrationRecords(t, task, format, incomplete)
			if incomplete.status != 15 || !strings.Contains(incomplete.stdout+incomplete.stderr, "rule-unavailable") {
				t.Fatal("incomplete example", incomplete)
			}
			// Real OS output-delivery failure through the extracted integration argv.
			put(t, filepath.Join(root, ".mdchk.yaml"), string(integrationAsset(t, doc, "example configuration")))
			//nolint:gosec // Test-owned document opened read-only intentionally.
			readonly, err := os.Open(filepath.Join(root, "docs/guide.md"))
			if err != nil {
				t.Fatal(err)
			}
			command := exec.Command(binary, args...) //nolint:gosec // Test-owned binary and strictly parsed published argv.
			command.Dir = root
			command.Stdout = readonly
			command.Stderr = io.Discard
			err = command.Run()
			closeErr := readonly.Close()
			var failure *exec.ExitError
			if closeErr != nil || !errors.As(err, &failure) || failure.ExitCode() != 13 {
				t.Fatalf("descriptor failure %v %v", err, closeErr)
			}
		})
	}
}

func TestDocumentedConfigValidationExamples(t *testing.T) {
	doc := integrationDocument(t)
	task := integrationTask(t, doc)
	binary := editorExecutable(t)
	for _, format := range []string{"vscode", "github"} {
		prefix := "mdchk config validate . --format " + format
		if strings.Count(doc, prefix+"\n") != 1 {
			t.Fatalf("missing/ambiguous config command %s", format)
		}
		// Execute the line extracted from its actual sh fence, not a parallel argv.
		var command string
		for _, line := range strings.Split(doc, "\n") {
			if strings.HasPrefix(line, "mdchk config validate ") && strings.HasSuffix(line, " "+format) {
				if command != "" {
					t.Fatal("duplicate config command")
				}
				command = line
			}
		}
		args := integrationWords(t, command)
		root := integrationRoot(t, doc)
		if got := editorExecute(t, binary, root, args...); got.status != 0 || got.stdout+got.stderr != "" {
			t.Fatal(got)
		}
		put(t, filepath.Join(root, ".mdchk.yaml"), "version: 1\nflavor: commonmark\nrules:\n  MD013: {length: -1}\n")
		got := editorExecute(t, binary, root, args...)
		records := integrationRecords(t, task, format, got)
		if got.status != 12 || len(records) != 1 || records[0].properties["title"] != "config-definition" || records[0].properties["line"] != "4" {
			t.Fatal(got, records)
		}
		put(t, filepath.Join(root, ".mdchk.yaml"), string(integrationAsset(t, doc, "example configuration")))
		// An actual directory at the ignore-file path causes an OS read failure
		// without assuming permission bits deny this test process access.
		if err := os.Mkdir(filepath.Join(root, ".gitignore"), 0700); err != nil {
			t.Fatal(err)
		}
		got = editorExecute(t, binary, root, args...)
		records = integrationRecords(t, task, format, got)
		if got.status != 13 || !strings.Contains(got.stdout+got.stderr, ".gitignore") {
			t.Fatal(got)
		}
		if format == "vscode" && got.stdout != "" {
			t.Fatal("invented operational location", got)
		}
		if format == "github" {
			if len(records) == 0 {
				t.Fatal("missing operational annotation", got)
			}
			for _, record := range records {
				if record.properties["file"] != "" || record.properties["line"] != "" {
					t.Fatal("invented operational location", got)
				}
			}
		}
	}
}

func TestDocumentedIntegrationPreviewAndHostilePath(t *testing.T) {
	doc := integrationDocument(t)
	task := integrationTask(t, doc)
	binary := editorExecutable(t)
	for _, format := range []string{"vscode", "github"} {
		root := integrationRoot(t, doc)
		args := task.Args
		if format == "github" {
			args = integrationWorkflow(t, doc)
		}
		// Change only the documented operation to exercise its directly linked caveat.
		preview := append([]string{"fix", "--dry-run"}, args[1:]...)
		got := editorExecute(t, binary, root, preview...)
		if got.status != 0 || got.stdout+got.stderr != "" {
			t.Fatal("safe preview", got)
		}
		//nolint:gosec // Test-owned document.
		data, err := os.ReadFile(filepath.Join(root, "docs/guide.md"))
		if err != nil || !bytes.Equal(data, integrationAsset(t, doc, "example document")) {
			t.Fatal("preview wrote source", err)
		}
		put(t, filepath.Join(root, "docs/guide.md"), "\n<!-- 😀x")
		assertIntegrationFinding(t, task, format, editorExecute(t, binary, root, preview...), 10, "docs/guide.md", "2", "8")
		if os.PathSeparator == '/' {
			put(t, filepath.Join(root, "docs/guide.md"), "clean\n")
			name := "literal\\name:%0A,##[error]::group::.md"
			put(t, filepath.Join(root, name), "x")
			hostile := editorExecute(t, binary, root, args...)
			assertIntegrationFinding(t, task, format, hostile, 10, name, "1", "2")
		}
	}
}

func TestDocumentedIntegrationPathAndOperationalVariants(t *testing.T) {
	doc := integrationDocument(t)
	task := integrationTask(t, doc)
	binary := editorExecutable(t)
	for _, format := range []string{"vscode", "github"} {
		root := integrationRoot(t, doc)
		args := task.Args
		if format == "github" {
			args = integrationWorkflow(t, doc)
		}
		put(t, filepath.Join(root, "nested/.mdchk.yaml"), string(integrationAsset(t, doc, "example configuration")))
		put(t, filepath.Join(root, "nested/doc.md"), "x")
		for _, target := range []string{"nested/doc.md", filepath.Join(root, "nested/doc.md")} {
			variant := append([]string{}, args...)
			variant[1] = target
			variant = append(variant, "--config=nested/.mdchk.yaml")
			assertIntegrationFinding(t, task, format, editorExecute(t, binary, root, variant...), 10, "nested/doc.md", "1", "2")
		}
		outside := filepath.Join(filepath.Dir(root), "outside-"+filepath.Base(root)+".md")
		put(t, outside, "x")
		t.Cleanup(func() {
			if err := os.Remove(outside); err != nil {
				t.Error(err)
			}
		})
		variant := append([]string{}, args...)
		variant[1] = outside
		variant = append(variant, "--config=nested/.mdchk.yaml")
		got := editorExecute(t, binary, root, variant...)
		records := integrationRecords(t, task, format, got)
		if got.status != 10 || len(records) != 1 {
			t.Fatal(got, records)
		}
		if format == "github" && (records[0].properties["file"] != "" || records[0].properties["line"] != "" || !strings.Contains(records[0].data, "Location unavailable")) {
			t.Fatal("invented outside location", got)
		}
		// Derived missing-target variant retains the actual published format/options.
		variant[1] = "missing.md"
		missing := editorExecute(t, binary, root, variant...)
		records = integrationRecords(t, task, format, missing)
		if missing.status != 13 {
			t.Fatal(missing)
		}
		if format == "vscode" && (missing.stdout != "" || !strings.Contains(missing.stderr, "missing.md")) {
			t.Fatal(missing)
		}
		if format == "github" && (len(records) < 1 || records[0].properties["title"] != "selection-failed" || records[0].properties["file"] != "" || records[0].properties["line"] != "") {
			t.Fatal(missing, records)
		}
	}
}
