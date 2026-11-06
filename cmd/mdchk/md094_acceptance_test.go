package main

import (
	"crypto/sha1" //nolint:gosec // Inspection reports SHA-1 as a file identity, not for security.
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	inspection "github.com/frithuz/mdchk/internal/inspect"
	"github.com/frithuz/mdchk/internal/result"
)

type md094Case struct {
	Name     string
	Source   []byte
	Expected []md094Expected
}

type md094Expected struct {
	ByteOffset int    `json:"byte_offset"`
	ByteEnd    int    `json:"byte_end"`
	Line       int    `json:"line"`
	Column     int    `json:"column"`
	BytesHex   string `json:"bytes_hex"`
}

// md094Message spells the fixture's bytes the way the message must name them.
func md094Message(want md094Expected) string {
	raw, _ := hex.DecodeString(want.BytesHex)
	var b strings.Builder
	b.WriteString("Invalid UTF-8 byte sequence")
	for _, c := range raw {
		fmt.Fprintf(&b, " 0x%02X", c)
	}
	fmt.Fprintf(&b, " at byte offset %d.", want.ByteOffset)
	return b.String()
}

// md094Cases reads the shared hex fixture, pinned by its SHA-256.
func md094Cases(t *testing.T) map[string]md094Case {
	t.Helper()
	b, err := os.ReadFile("../../internal/rules/testdata/md094.json")
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != "651f3353d42bbf33707fc1d227ecc7a1aba34375d60cf85fee3e924be07ae810" {
		t.Fatal("root fixture identity", err)
	}
	var fixture struct {
		Cases []struct {
			Name      string `json:"name"`
			SourceHex string `json:"source_hex"`
			Expected  json.RawMessage
		}
	}
	if err = json.Unmarshal(b, &fixture); err != nil || len(fixture.Cases) != 42 {
		t.Fatal(err, len(fixture.Cases))
	}
	out := map[string]md094Case{}
	for _, raw := range fixture.Cases {
		tc := md094Case{Name: raw.Name}
		if tc.Source, err = hex.DecodeString(raw.SourceHex); err != nil {
			t.Fatal(raw.Name, err)
		}
		if err = json.Unmarshal(raw.Expected, &tc.Expected); err != nil {
			t.Fatal(raw.Name, err)
		}
		if _, exists := out[tc.Name]; exists {
			t.Fatal("duplicate fixture", tc.Name)
		}
		out[tc.Name] = tc
	}
	return out
}

// md094Configuration replaces the clean MD094 entry; an empty rule removes it
// so the default configuration applies.
func md094Configuration(rule string) string {
	if rule == "" {
		return strings.Replace(cleanConfiguration(), "  MD094: {enabled: false}\n", "", 1)
	}
	return strings.Replace(cleanConfiguration(), "MD094: {enabled: false}", "MD094: "+rule, 1)
}

func md094Project(t *testing.T, conf string, files map[string][]byte) string {
	t.Helper()
	root := configurationProject(t)
	putConfigurationFile(t, root, ".mdchk.yaml", conf)
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(root, name), contents, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// md094FixJSON checks the closed result schema for fix. md094ResultJSON also checks
// that every snapshot is the original view, but fix reports the unchanged
// bytes of a failed file as its candidate view.
func md094FixJSON(t *testing.T, run configurationRun) result.Result {
	t.Helper()
	out := validateConfigurationOperationJSON(t, []byte(run.Stdout), "fix")
	b, err := os.ReadFile("../../docs/contracts/results/result.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema, wire any
	if err = json.Unmarshal(b, &schema); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal([]byte(run.Stdout), &wire); err != nil {
		t.Fatal(err)
	}
	if err = md014SchemaCheck(schema, schema, wire); err != nil {
		t.Fatal("closed normative result schema", err, run.Stdout)
	}
	return out
}

func md094Snapshot(view string, source []byte) result.Snapshot {
	return result.Snapshot{View: view, SHA256: fmt.Sprintf("%x", sha256.Sum256(source)), SizeBytes: len(source)}
}

// md094CheckError checks every invalid-utf8 error against the fixture. Fix
// validates the candidate view, which for a failed file is the original bytes.
func md094CheckError(t *testing.T, errors []result.Error, path, rule, scope, view string, tc md094Case) {
	t.Helper()
	if len(errors) != len(tc.Expected) {
		t.Fatal("errors", errors)
	}
	for i, want := range tc.Expected {
		e := errors[i]
		point := result.Position{Line: want.Line, Column: want.Column}
		if e.Category != result.IO || e.Code != "invalid-utf8" || e.Scope != scope || e.Path != path || e.Rule != rule || e.Message != md094Message(want) {
			t.Fatalf("error=%+v", e)
		}
		if e.ByteOffset == nil || *e.ByteOffset != want.ByteOffset || e.ByteRange == nil || *e.ByteRange != (result.ByteRange{Start: want.ByteOffset, End: want.ByteEnd}) || e.Range == nil || e.Range.Start != point || e.Range.End != point {
			t.Fatalf("location offset=%v span=%+v range=%+v", e.ByteOffset, e.ByteRange, e.Range)
		}
		if e.Snapshot == nil || *e.Snapshot != md094Snapshot(view, tc.Source) {
			t.Fatalf("snapshot=%+v", e.Snapshot)
		}
	}
}

// md094CheckFailedFile checks one failed file: no findings, one decoding-failed
// coverage entry caused by all of its errors, and incomplete error status.
func md094CheckFailedFile(t *testing.T, out result.Result, path string) {
	t.Helper()
	var errorIDs []string
	for _, e := range out.Errors {
		if e.Path == path {
			errorIDs = append(errorIDs, e.ID)
		}
	}
	for _, d := range out.Diagnostics {
		if d.Path == path {
			t.Fatal("diagnostic from an undecoded file", d)
		}
	}
	var decoding []result.Coverage
	for _, c := range out.Coverage {
		if c.Path == path && c.Reason == "decoding-failed" {
			decoding = append(decoding, c)
		}
	}
	if len(decoding) != 1 || !md094SameIDs(decoding[0].CauseIDs, errorIDs) {
		t.Fatal("coverage", out.Coverage)
	}
	analysed := false
	for _, f := range out.Files {
		if f.Path == path {
			analysed = true
			if f.Analysis != "failed" || !reflect.DeepEqual(f.ErrorIDs, errorIDs) || len(f.ChangeIDs) != 0 {
				t.Fatal("file", f)
			}
		}
	}
	if !analysed || out.Status != "error" || out.Complete || len(out.SuppressedDiagnostics) != 0 || len(out.Notes) != 0 {
		t.Fatal("result", out.Status, out.Complete, out.Files, out.Notes)
	}
}

// Coverage cause IDs are in canonical text order, not numeric record order.
func md094SameIDs(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

// Every fixture runs through lint in JSON and text under default, enabled and
// disabled attribution and both allow_directives values. Only rule differs.
func TestCompiledMD094FixtureLint(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	for _, tc := range md094Cases(t) {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			gates := map[string][]result.Error{}
			for _, row := range []struct{ name, rule, attribution string }{
				{"default", "", "MD094"},
				{"enabled", "{enabled: true}", "MD094"},
				{"disabled", "{enabled: false}", ""},
				{"allow-directives-false", "{enabled: true, allow_directives: false}", "MD094"},
				{"allow-directives-true", "{enabled: true, allow_directives: true}", "MD094"},
			} {
				root := md094Project(t, md094Configuration(row.rule), map[string][]byte{"doc.md": tc.Source})
				before := configurationBytes(t, root)
				got := md014NativeRun(t, binary, root, "lint", "--format=json", "doc.md")
				out := md094ResultJSON(t, got, root, "lint")
				if len(tc.Expected) == 0 {
					if got.Status != 0 || len(out.Errors) != 0 || !out.Complete {
						t.Fatal(row.name, got.Status, got.Stdout)
					}
				} else {
					if got.Status != 13 {
						t.Fatal(row.name, got.Status, got.Stdout)
					}
					md094CheckError(t, out.Errors, "doc.md", row.attribution, "operation", "original", tc)
					md094CheckFailedFile(t, out, "doc.md")
					if len(out.Diagnostics) != 0 {
						t.Fatal(row.name, out.Diagnostics)
					}
				}
				gates[row.name] = out.Errors
				if row.name == "default" {
					text := md014NativeRun(t, binary, root, "lint", "doc.md")
					if len(tc.Expected) == 0 {
						if text.Status != 0 || strings.Contains(text.Stdout, "invalid-utf8") {
							t.Fatal("text", text.Status, text.Stdout)
						}
					} else {
						if text.Status != 13 || strings.Count(text.Stdout, "invalid-utf8") != len(tc.Expected) {
							t.Fatal("text", text.Status, text.Stdout)
						}
						for _, expected := range tc.Expected {
							want := fmt.Sprintf("doc.md:%d:%d error invalid-utf8 (io): %s\n", expected.Line, expected.Column, md094Message(expected))
							if !strings.Contains(text.Stdout, want) {
								t.Fatal("text", want, text.Stdout)
							}
						}
					}
				}
				if after := configurationBytes(t, root); !reflect.DeepEqual(before, after) {
					t.Fatal("lint changed the fixture tree")
				}
			}
			if !reflect.DeepEqual(gates["allow-directives-false"], gates["allow-directives-true"]) || !reflect.DeepEqual(gates["default"], gates["enabled"]) {
				t.Fatal("gate results differ", gates)
			}
		})
	}
}

// MD094 has no options, and enabled must be a boolean.
func TestCompiledMD094RejectsOptions(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	bad := md094Cases(t)["documented-violating-example"].Source
	for _, rule := range []string{"{foo: 1}", "{enabled: 1}", "{enabled: yes-please}"} {
		root := md094Project(t, md094Configuration(rule), map[string][]byte{"doc.md": bad})
		got := md014NativeRun(t, binary, root, "lint", "--format=json", "doc.md")
		out := md094ResultJSON(t, got, root, "lint")
		if got.Status != 12 || len(out.Errors) != 1 || out.Errors[0].Code != "config-definition" {
			t.Fatal(rule, got.Status, got.Stdout)
		}
	}
}

func md094Notes(t *testing.T, out result.Result) []result.Note {
	t.Helper()
	notes := make([]result.Note, len(out.Notes))
	for i, raw := range out.Notes {
		if err := json.Unmarshal(raw, &notes[i]); err != nil {
			t.Fatal(err)
		}
	}
	return notes
}

func md094Inspection(t *testing.T, run configurationRun, root string) (result.Result, inspection.Projection) {
	t.Helper()
	out := md094ResultJSON(t, run, root, "inspect")
	var p inspection.Projection
	if err := json.Unmarshal(out.Inspection, &p); err != nil {
		t.Fatal(err)
	}
	return out, p
}

// A valid directive naming MD094 is inapplicable; MD009 in the same comment
// still takes effect and MD087 does not assess the MD094 target.
func TestCompiledMD094Directives(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	source := []byte("a \n\n<!-- mdchk:disable MD094 MD009 -->\n\nb \n")
	conf := md094Configuration("{enabled: true}")
	conf = strings.Replace(conf, "MD009: {enabled: false}", "MD009: {enabled: true}", 1)
	conf = strings.Replace(conf, "MD087: {enabled: false}", "MD087: {enabled: true}", 1)
	enabled := strings.Replace(conf, "directives: {enabled: false}", "directives: {enabled: true}", 1)

	root := md094Project(t, enabled, map[string][]byte{"doc.md": source})
	got := md014NativeRun(t, binary, root, "lint", "--format=json", "doc.md")
	out := md094ResultJSON(t, got, root, "lint")
	if got.Status != 10 || len(out.Errors) != 0 || !out.Complete {
		t.Fatal(got.Status, got.Stdout)
	}
	notes := md094Notes(t, out)
	if len(notes) != 1 || notes[0].Code != "directive-inapplicable" || notes[0].Rule != "MD094" || notes[0].ByteRange == nil || *notes[0].ByteRange != (result.ByteRange{Start: 23, End: 28}) || notes[0].Range == nil || *notes[0].Range != (result.Range{Start: result.Position{Line: 3, Column: 20}, End: result.Position{Line: 3, Column: 25}}) {
		t.Fatal("notes", notes)
	}
	if len(out.Diagnostics) != 1 || out.Diagnostics[0].Rule != "MD009" || out.Diagnostics[0].Range.Start.Line != 1 {
		t.Fatal("diagnostics", out.Diagnostics)
	}
	if len(out.SuppressedDiagnostics) != 1 {
		t.Fatal("suppressed", out.SuppressedDiagnostics)
	}
	var suppressed result.SuppressedDiagnostic
	if err := json.Unmarshal(out.SuppressedDiagnostics[0], &suppressed); err != nil || suppressed.Diagnostic.Rule != "MD009" || suppressed.Diagnostic.Range.Start.Line != 5 {
		t.Fatal("suppressed", err, suppressed)
	}

	root = md094Project(t, conf, map[string][]byte{"doc.md": source})
	_, p := md094Inspection(t, md014NativeRun(t, binary, root, "inspect", "--format=json", "doc.md"), root)
	if p.Directives.Availability != "available" || p.Directives.Data == nil || len(*p.Directives.Data) != 1 {
		t.Fatal("directives", p.Directives)
	}
	if d := (*p.Directives.Data)[0]; d.State != "inactive" || len(d.Effects) != 0 {
		t.Fatal("directive", d)
	}

	tc := md094Cases(t)["directive-before-bad-byte"]
	root = md094Project(t, enabled, map[string][]byte{"doc.md": tc.Source})
	got = md014NativeRun(t, binary, root, "lint", "--format=json", "doc.md")
	out = md094ResultJSON(t, got, root, "lint")
	if got.Status != 13 {
		t.Fatal(got.Status, got.Stdout)
	}
	md094CheckError(t, out.Errors, "doc.md", "MD094", "operation", "original", tc)
	md094CheckFailedFile(t, out, "doc.md")
	seen := md014NativeRun(t, binary, root, "inspect", "--format=json", "doc.md")
	_, p = md094Inspection(t, seen, root)
	if seen.Status != 0 || p.Directives.Availability != "unavailable" || !reflect.DeepEqual(p.Directives.Reasons, []string{"decoding-failed"}) || p.Directives.Data != nil {
		t.Fatal("inspect directives", seen.Status, p.Directives)
	}
}

// An invalid file fails alone: the valid file is still analysed and its MD047
// finding or proposal is reported, and no file changes on disk.
func TestCompiledMD094MultiFile(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	tc := md094Cases(t)["documented-violating-example"]
	conf := strings.Replace(md094Configuration(""), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
	root := md094Project(t, conf, map[string][]byte{"bad.md": tc.Source, "ok.md": []byte("ok")})
	before := configurationBytes(t, root)

	got := md014NativeRun(t, binary, root, "lint", "--format=json", "bad.md", "ok.md")
	out := md094ResultJSON(t, got, root, "lint")
	if got.Status != 13 {
		t.Fatal(got.Status, got.Stdout)
	}
	md094CheckError(t, out.Errors, "bad.md", "MD094", "operation", "original", tc)
	md094CheckFailedFile(t, out, "bad.md")
	if len(out.Diagnostics) != 1 || out.Diagnostics[0].Path != "ok.md" || out.Diagnostics[0].Rule != "MD047" {
		t.Fatal("diagnostics", out.Diagnostics)
	}
	if len(out.Files) != 2 || out.Files[1].Path != "ok.md" || out.Files[1].Analysis != "complete" {
		t.Fatal("files", out.Files)
	}

	got = md014NativeRun(t, binary, root, "fix", "--dry-run", "--format=json", "bad.md", "ok.md")
	out = md094FixJSON(t, got)
	if got.Status != 13 || out.Status != "error" || out.Complete {
		t.Fatal(got.Status, got.Stdout)
	}
	md094CheckError(t, out.Errors, "bad.md", "MD094", "operation", "candidate", tc)
	if len(out.Changes) != 1 || out.Changes[0].Path != "ok.md" || !reflect.DeepEqual(out.Changes[0].Rules, []string{"MD047"}) {
		t.Fatal("changes", out.Changes)
	}
	for _, f := range out.Files {
		if f.Path == "bad.md" && (f.Analysis != "failed" || len(f.ChangeIDs) != 0) || f.Path == "ok.md" && f.Analysis != "complete" {
			t.Fatal("file", f)
		}
	}
	if after := configurationBytes(t, root); !reflect.DeepEqual(before, after) {
		t.Fatal("a run changed the fixture tree")
	}
}

// fix --dry-run on an invalid file proposes nothing and previews nothing.
func TestCompiledMD094FixDryRun(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	tc := md094Cases(t)["after-crlf-line"]
	conf := strings.Replace(md094Configuration(""), "MD047: {enabled: false}", "MD047: {enabled: true}", 1)
	root := md094Project(t, conf, map[string][]byte{"bad.md": tc.Source})
	before := configurationBytes(t, root)
	got := md014NativeRun(t, binary, root, "fix", "--dry-run", "--format=json", "bad.md")
	out := md094FixJSON(t, got)
	if got.Status != 13 || out.Status != "error" || out.Complete || len(out.Changes)+len(out.Documents)+len(out.DeferredFixes)+len(out.Diagnostics) != 0 {
		t.Fatal(got.Status, got.Stdout)
	}
	md094CheckError(t, out.Errors, "bad.md", "MD094", "operation", "candidate", tc)
	if len(out.Files) != 1 || out.Files[0].Analysis != "failed" || len(out.Files[0].ChangeIDs) != 0 {
		t.Fatal("files", out.Files)
	}
	text := md014NativeRun(t, binary, root, "fix", "--dry-run", "bad.md")
	if text.Status != 13 || strings.Contains(text.Stdout, "--- bad.md") || strings.Contains(text.Stdout, "proposed") || strings.Count(text.Stdout, "invalid-utf8") != 1 {
		t.Fatal("text", text.Status, text.Stdout)
	}
	if after := configurationBytes(t, root); !reflect.DeepEqual(before, after) {
		t.Fatal("fix --dry-run changed the fixture tree")
	}
}

// Inspect reports the raw identity and the error, keeps configuration and
// flavor, and marks decoding-dependent sections unavailable.
func TestCompiledMD094InspectInvalid(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	tc := md094Cases(t)["mid-line-after-multibyte"]
	root := md094Project(t, md094Configuration(""), map[string][]byte{"bad.md": tc.Source})
	got := md014NativeRun(t, binary, root, "inspect", "--format=json", "bad.md")
	out, p := md094Inspection(t, got, root)
	if got.Status != 0 || out.Status != "inspected" || out.Complete {
		t.Fatal(got.Status, got.Stdout)
	}
	md094CheckError(t, out.Errors, "bad.md", "MD094", "observation", "original", tc)
	if p.File.SizeBytes != int64(len(tc.Source)) || p.File.SHA1 != fmt.Sprintf("%x", sha1.Sum(tc.Source)) || p.File.SHA256 != fmt.Sprintf("%x", sha256.Sum256(tc.Source)) { //nolint:gosec // Identity only.
		t.Fatal("file", p.File)
	}
	for name, section := range map[string]struct {
		availability string
		reasons      []string
	}{
		"ast":           {p.AST.Availability, p.AST.Reasons},
		"rules":         {p.Rules.Availability, p.Rules.Reasons},
		"directives":    {p.Directives.Availability, p.Directives.Reasons},
		"parsing_notes": {p.ParsingNotes.Availability, p.ParsingNotes.Reasons},
	} {
		if section.availability != "unavailable" || !reflect.DeepEqual(section.reasons, []string{"decoding-failed"}) {
			t.Fatal(name, section)
		}
	}
	if p.Configuration.Availability != "available" || p.Flavor.Availability != "available" {
		t.Fatal("configuration", p.Configuration.Availability, "flavor", p.Flavor.Availability)
	}
}

// With attribution off, the MD094 record still shows complete active
// evaluation over the whole snapshot: validation ran.
func TestCompiledMD094InspectDisabledStillValidates(t *testing.T) {
	t.Parallel()
	binary := sharedExecutable(t)
	source := []byte("# T\r\nUTF-8 encoded text.\n")
	root := md094Project(t, md094Configuration("{enabled: false}"), map[string][]byte{"ok.md": source})
	got := md014NativeRun(t, binary, root, "inspect", "--format=json", "ok.md")
	out, p := md094Inspection(t, got, root)
	if got.Status != 0 || out.Status != "inspected" || !out.Complete || len(out.Errors) != 0 || p.Rules.Data == nil {
		t.Fatal(got.Status, got.Stdout)
	}
	var record *inspection.Rule
	for i := range *p.Rules.Data {
		if (*p.Rules.Data)[i].Rule == "MD094" {
			record = &(*p.Rules.Data)[i]
		}
	}
	if record == nil || record.ConfiguredEnabled || record.ActiveEvaluation != "complete" {
		t.Fatal("record", record)
	}
	active := record.SourceCoverage.Active
	if active.Availability != "complete" || !reflect.DeepEqual(active.ByteRanges, []result.ByteRange{{Start: 0, End: len(source)}}) {
		t.Fatal("coverage", active)
	}
}

// md094ResultJSON validates a report against the closed schema and the shared
// semantic checks.
func md094ResultJSON(t *testing.T, run configurationRun, root, operation string) result.Result {
	t.Helper()
	run.Result = validateConfigurationOperationJSON(t, []byte(run.Stdout), operation)
	b, err := os.ReadFile("../../docs/contracts/results/result.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema, wire any
	if err = json.Unmarshal(b, &schema); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal([]byte(run.Stdout), &wire); err != nil {
		t.Fatal(err)
	}
	if err = md014SchemaCheck(schema, schema, wire); err != nil {
		t.Fatal("closed normative result schema", err, run.Stdout)
	}
	if operation != "inspect" {
		checkConfigurationLinks(t, root, run)
	}
	return run.Result
}
