package acceptance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/frithuz/mdchk/internal/result"
	"github.com/frithuz/mdchk/internal/testenv"
)

// MD014NativeEnvironment returns the inherited environment with fixture-owned
// absent Git configuration under cwd.
//
// Only CLI children receive fixture-owned absent Git inputs. Build processes,
// HOME, authentication and every other inherited environment entry are retained.
func MD014NativeEnvironment(t *testing.T, cwd string) []string {
	t.Helper()
	root, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatal(err)
	}
	absent := filepath.Join(root, "absent-gitconfig")
	if _, err := os.Lstat(absent); !os.IsNotExist(err) {
		t.Fatal("fixture Git input must be absent", err)
	}
	env := testenv.Environ()
	env = slices.DeleteFunc(env, func(entry string) bool {
		key, _, _ := strings.Cut(entry, "=")
		return key == "GIT_CONFIG_NOSYSTEM" || key == "GIT_CONFIG_GLOBAL"
	})
	return append(env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+absent)
}

// MD014NativeRun runs binary in cwd with MD014NativeEnvironment and logs the run.
func MD014NativeRun(t *testing.T, binary, cwd string, args ...string) Run {
	t.Helper()
	return MD014NativeRunEnv(t, binary, cwd, MD014NativeEnvironment(t, cwd), args...)
}

// MD014NativeRunEnv runs binary in cwd with env and logs the run.
func MD014NativeRunEnv(t *testing.T, binary, cwd string, env []string, args ...string) Run {
	t.Helper()
	got := RunProcessEnv(t, binary, cwd, env, args...)
	var controls []string
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if key == "GIT_CONFIG_NOSYSTEM" || key == "GIT_CONFIG_GLOBAL" || key == "CGO_ENABLED" {
			controls = append(controls, entry)
		}
	}
	// Native go-test JSON retains actual argv, child-specific controls and raw
	// stdout/status. The shared launcher asserts actual stderr is empty.
	t.Logf("MD014_NATIVE binary=%q cwd=%q argv=%q inherited_env=preserved child_controls=%q status=%d stdout=%q stderr=%q", binary, cwd, args, controls, got.Status, got.Stdout, "")
	return got
}

// MD014SchemaCheck validates data against schema, resolving references from root.
//
// Evaluate the exact checked-in closed schema without introducing a hosted
// Python/jsonschema dependency. These are the assertion keywords present in the
// normative schema; unsupported future keywords fail closed.
func MD014SchemaCheck(schema, root, data any) error {
	if b, ok := schema.(bool); ok {
		if b {
			return nil
		}
		return fmt.Errorf("false schema")
	}
	s, ok := schema.(map[string]any)
	if !ok {
		return fmt.Errorf("invalid schema node")
	}
	for key := range s {
		switch key {
		case "$defs", "$schema", "$ref", "title", "description", "contentEncoding", "type", "const", "enum", "required", "properties", "additionalProperties", "items", "minimum", "maximum", "minItems", "maxItems", "minLength", "pattern", "uniqueItems", "allOf", "anyOf", "if", "then", "else", "not":
		default:
			return fmt.Errorf("unsupported schema keyword %q", key)
		}
	}
	if ref, ok := s["$ref"].(string); ok {
		if !strings.HasPrefix(ref, "#/") {
			return fmt.Errorf("nonlocal schema ref")
		}
		resolved := root
		for _, key := range strings.Split(ref[2:], "/") {
			key = strings.ReplaceAll(strings.ReplaceAll(key, "~1", "/"), "~0", "~")
			object, ok := resolved.(map[string]any)
			if !ok {
				return fmt.Errorf("bad schema reference")
			}
			resolved, ok = object[key]
			if !ok {
				return fmt.Errorf("unknown schema ref")
			}
		}
		if err := MD014SchemaCheck(resolved, root, data); err != nil {
			return err
		}
	}
	if expected, exists := s["const"]; exists && !reflect.DeepEqual(expected, data) {
		return fmt.Errorf("const mismatch")
	}
	if choices, ok := s["enum"].([]any); ok {
		found := false
		for _, choice := range choices {
			found = found || reflect.DeepEqual(choice, data)
		}
		if !found {
			return fmt.Errorf("enum mismatch")
		}
	}
	if typ, exists := s["type"]; exists {
		match := func(kind any) bool {
			switch kind {
			case "object":
				_, ok := data.(map[string]any)
				return ok
			case "array":
				_, ok := data.([]any)
				return ok
			case "string":
				_, ok := data.(string)
				return ok
			case "boolean":
				_, ok := data.(bool)
				return ok
			case "integer":
				n, ok := data.(float64)
				return ok && math.Trunc(n) == n
			case "number":
				_, ok := data.(float64)
				return ok
			case "null":
				return data == nil
			}
			return false
		}
		valid := match(typ)
		if list, ok := typ.([]any); ok {
			valid = false
			for _, kind := range list {
				valid = valid || match(kind)
			}
		}
		if !valid {
			return fmt.Errorf("type mismatch")
		}
	}
	if object, ok := data.(map[string]any); ok {
		if required, ok := s["required"].([]any); ok {
			for _, key := range required {
				if _, exists := object[key.(string)]; !exists {
					return fmt.Errorf("missing %v", key)
				}
			}
		}
		properties, _ := s["properties"].(map[string]any)
		for key, value := range object {
			if child, exists := properties[key]; exists {
				if err := MD014SchemaCheck(child, root, value); err != nil {
					return fmt.Errorf("%s: %w", key, err)
				}
			} else if additional, exists := s["additionalProperties"]; exists {
				if err := MD014SchemaCheck(additional, root, value); err != nil {
					return fmt.Errorf("additional %s: %w", key, err)
				}
			}
		}
	}
	if array, ok := data.([]any); ok {
		if n, ok := s["minItems"].(float64); ok && float64(len(array)) < n {
			return fmt.Errorf("minItems")
		}
		if n, ok := s["maxItems"].(float64); ok && float64(len(array)) > n {
			return fmt.Errorf("maxItems")
		}
		seen := map[string]bool{}
		for _, value := range array {
			if child, exists := s["items"]; exists {
				if err := MD014SchemaCheck(child, root, value); err != nil {
					return err
				}
			}
			if s["uniqueItems"] == true {
				b, _ := json.Marshal(value)
				if seen[string(b)] {
					return fmt.Errorf("uniqueItems")
				}
				seen[string(b)] = true
			}
		}
	}
	if text, ok := data.(string); ok {
		if n, ok := s["minLength"].(float64); ok && float64(utf8.RuneCountInString(text)) < n {
			return fmt.Errorf("minLength")
		}
		if pattern, ok := s["pattern"].(string); ok {
			expression, err := regexp.Compile(pattern)
			if err != nil {
				return err
			}
			if !expression.MatchString(text) {
				return fmt.Errorf("pattern")
			}
		}
	}
	if n, ok := data.(float64); ok {
		if minimum, ok := s["minimum"].(float64); ok && n < minimum {
			return fmt.Errorf("minimum")
		}
		if maximum, ok := s["maximum"].(float64); ok && n > maximum {
			return fmt.Errorf("maximum")
		}
	}
	if children, ok := s["allOf"].([]any); ok {
		for _, child := range children {
			if err := MD014SchemaCheck(child, root, data); err != nil {
				return err
			}
		}
	}
	if children, ok := s["anyOf"].([]any); ok {
		found := false
		for _, child := range children {
			found = found || MD014SchemaCheck(child, root, data) == nil
		}
		if !found {
			return fmt.Errorf("anyOf")
		}
	}
	if child, exists := s["not"]; exists && MD014SchemaCheck(child, root, data) == nil {
		return fmt.Errorf("not")
	}
	if condition, exists := s["if"]; exists {
		branch := "else"
		if MD014SchemaCheck(condition, root, data) == nil {
			branch = "then"
		}
		if child, exists := s[branch]; exists {
			if err := MD014SchemaCheck(child, root, data); err != nil {
				return err
			}
		}
	}
	return nil
}

// MD014NativeJSON validates a CLI report for operation, then decodes it with
// MD014ResultJSON.
func MD014NativeJSON(t *testing.T, data []byte, operation string) result.Result {
	t.Helper()
	// CLI reports retain portable-path and newline-framing validation.
	ValidateOperationJSON(t, data, operation)
	return MD014ResultJSON(t, data, operation)
}

// MD014ResultJSON checks data against the result schema and its cross-record
// invariants and decodes it.
func MD014ResultJSON(t *testing.T, data []byte, operation string) result.Result {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(ModuleRoot(t), "docs", "contracts", "results", "result.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema, wire any
	if err = json.Unmarshal(b, &schema); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if err = MD014SchemaCheck(schema, schema, wire); err != nil {
		t.Fatal("closed normative result schema", err, string(data))
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var out result.Result
	if err = decoder.Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.SchemaVersion != 1 || out.Operation != operation {
		t.Fatal("invalid result envelope", out)
	}
	// Cross-record semantic invariants independent of the schema's shape checks.
	object := wire.(map[string]any)
	ids := map[string]bool{}
	recordSets := []string{"diagnostics", "coverage", "changes", "deferred_fixes", "errors", "notes", "interruptions"}
	for _, key := range recordSets {
		for _, raw := range object[key].([]any) {
			record := raw.(map[string]any)
			id := record["id"].(string)
			if ids[id] {
				t.Fatal("duplicate record id", id)
			}
			ids[id] = true
		}
	}
	files := map[string]result.File{}
	for _, f := range out.Files {
		if _, exists := files[f.Path]; exists {
			t.Fatal("duplicate target", f.Path)
		}
		files[f.Path] = f
	}
	for _, path := range out.CheckedFiles {
		if _, exists := files[path]; !exists {
			t.Fatal("checked file missing outcome", path)
		}
	}
	check := func(d result.Diagnostic) {
		f, exists := files[d.Path]
		if !exists {
			t.Fatal("diagnostic without target", d)
		}
		want := f.InputSnapshot
		if d.Snapshot.View != "original" {
			want = f.ResultSnapshot
		}
		if want == nil || d.Snapshot != *want {
			t.Fatal("diagnostic snapshot/target disagreement", d, f)
		}
		if d.ByteRange.Start < 0 || d.ByteRange.End < d.ByteRange.Start || d.ByteRange.End > d.Snapshot.SizeBytes {
			t.Fatal("diagnostic byte bounds", d)
		}
	}
	for _, d := range out.Diagnostics {
		check(d)
	}
	for _, raw := range out.SuppressedDiagnostics {
		var h result.SuppressedDiagnostic
		if err = json.Unmarshal(raw, &h); err != nil {
			t.Fatal(err)
		}
		if ids[h.Diagnostic.ID] {
			t.Fatal("duplicate hidden id")
		}
		ids[h.Diagnostic.ID] = true
		check(h.Diagnostic)
	}
	for _, f := range out.Files {
		for _, id := range append(append(append([]string{}, f.ErrorIDs...), f.CoverageIDs...), f.ChangeIDs...) {
			if !ids[id] {
				t.Fatal("dangling file record", id)
			}
		}
	}
	for _, c := range out.Coverage {
		for _, id := range c.CauseIDs {
			if !ids[id] {
				t.Fatal("dangling cause", id)
			}
		}
	}
	return out
}
