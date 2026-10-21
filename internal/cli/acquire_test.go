package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/result"
)

func TestActualLintAcquisition(t *testing.T) {
	root := validProject(t)
	put(t, filepath.Join(root, "nested", "doc.markdown"), "雪\r\nvalid\rtext")
	put(t, filepath.Join(root, "custom.txt"), "literal \ufffd")
	put(t, filepath.Join(root, "--draft"), "\ufeff")
	put(t, filepath.Join(root, "ignored", "file.md"), "excluded")
	put(t, filepath.Join(root, ".mdchk.yaml"), availableConfig+"exclude: ['ignored/']\n")
	for _, tc := range []struct {
		paths  []string
		status int
		count  int
	}{
		{nil, 15, 1}, {[]string{"nested"}, 15, 1}, {[]string{"custom.txt"}, 15, 1}, {[]string{"--", "--draft"}, 15, 1},
		{[]string{"missing.md"}, 13, 0}, {[]string{"ignored/file.md"}, 11, 0}, {[]string{"nested", "missing.md"}, 13, 1},
	} {
		t.Run(strings.Join(tc.paths, ","), func(t *testing.T) {
			args := append([]string{"lint"}, tc.paths...)
			var out, stderr bytes.Buffer
			if status := Run(args, &out, &stderr, "dev"); status != tc.status || out.Len() == 0 || stderr.Len() != 0 {
				t.Fatalf("actual: status=%d out=%q stderr=%q", status, &out, &stderr)
			}
			options, err := parseLint(tc.paths)
			if err != nil {
				t.Fatal(err)
			}
			inv, err := prepareLint(options)
			if err != nil {
				t.Fatal(err)
			}
			if status := acquireLint(inv, io.Discard, io.Discard); status != tc.status || len(inv.result.CheckedFiles) != tc.count || inv.acquisition == nil || inv.targetSelection == nil {
				t.Fatalf("state: status=%d inv=%+v", status, inv)
			}
		})
	}
}

func TestActualLintInvalidUTF8AndSnapshots(t *testing.T) {
	root := validProject(t)
	raw := []byte("<!-- markdownlint-disable -->\r\n雪\t\xe2\x82")
	bad := filepath.Join(root, "bad.md")
	put(t, bad, string(raw))
	put(t, filepath.Join(root, "good.md"), "\ufffd valid")
	for _, enabled := range []bool{false, true} {
		// MD094 is a mandatory gate attribution rather than a registered callback.
		put(t, filepath.Join(root, ".mdchk.yaml"), availableConfig+fmt.Sprintf("rules: {MD094: {enabled: %t}}\n", enabled))
		inv, err := prepareLint(lintOptions{})
		if err != nil {
			t.Fatal(err)
		}
		var stderr bytes.Buffer
		if status := acquireLint(inv, io.Discard, &stderr); status != 13 {
			t.Fatalf("status=%d: %s", status, &stderr)
		}
		if len(inv.result.CheckedFiles) != 2 || len(inv.acquisition.Documents) != 2 {
			t.Fatalf("independent good target lost: %+v", inv)
		}
		var e *result.Error
		for i := range inv.result.Errors {
			if inv.result.Errors[i].Code == "invalid-utf8" {
				e = &inv.result.Errors[i]
				break
			}
		}
		wantRule := ""
		if enabled {
			wantRule = "MD094"
		}
		if e == nil || e.Rule != wantRule || e.Code != "invalid-utf8" || e.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256(raw)) || e.Snapshot.SizeBytes != len(raw) || *e.ByteOffset != len(raw)-2 || e.Range.Start.Line != 2 || e.Range.Start.Column != 3 || len(inv.result.Errors) != 1 {
			t.Fatalf("wrong validation: %+v", e)
		}
		if string(inv.acquisition.Documents[0].Source.Bytes()) != string(raw) {
			t.Fatal("source changed")
		}
	}
	var stderr bytes.Buffer
	if status := Run([]string{"lint"}, &stderr, io.Discard, "dev"); status != 13 || !strings.Contains(stderr.String(), "Invalid UTF-8") {
		t.Fatalf("actual invalid source: status=%d %s", status, &stderr)
	}
	if status := Run([]string{"lint"}, io.Discard, failingWriter{}, "dev"); status != 13 {
		t.Fatal(status)
	}
	//nolint:gosec // Read only the test-owned malformed fixture to verify lint is read-only.
	if data, err := os.ReadFile(bad); err != nil || string(data) != string(raw) {
		t.Fatal("lint wrote source")
	}
}

func TestLintAcquisitionLimitsAndCancellation(t *testing.T) {
	root := validProject(t)
	put(t, filepath.Join(root, "doc.md"), "12345")
	for _, tc := range []struct {
		limits map[string]int64
		status int
		stops  bool
	}{
		{map[string]int64{"document-bytes": 4}, 11, false},
		{map[string]int64{"work-units": 1}, 15, true},
	} {
		inv, err := prepareLint(lintOptions{limits: tc.limits})
		if err != nil {
			t.Fatal(err)
		}
		if status := acquireLint(inv, io.Discard, io.Discard); status != tc.status {
			t.Fatalf("status=%d", status)
		}
		if tc.stops && (len(inv.acquisition.Interruptions) == 0 || len(inv.result.CheckedFiles) != 0) {
			t.Fatalf("interruption lost: %+v", inv)
		}
	}
	inv, err := prepareLint(lintOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if status := acquireLintContext(ctx, inv, io.Discard); status != 15 || len(inv.acquisition.Interruptions) == 0 || len(inv.result.CheckedFiles) != 0 {
		t.Fatalf("cancelled=%d %+v", status, inv)
	}
	// IO remains stronger than a simultaneous interruption.
	inv, err = prepareLint(lintOptions{paths: []string{"doc.md", "missing.md"}})
	if err != nil {
		t.Fatal(err)
	}
	if status := acquireLintContext(ctx, inv, io.Discard); status != 13 {
		t.Fatalf("IO precedence=%d", status)
	}
}

func TestMandatoryGateWithAllOrdinaryRulesDisabled(t *testing.T) {
	root := validProject(t)
	put(t, filepath.Join(root, "bad.md"), "<!-- markdownlint-disable -->\n雪\xff")
	for _, enabled := range []bool{false, true} {
		var body strings.Builder
		body.WriteString(availableConfig + "rules:\n")
		registry, err := engine.NewCatalogRegistry(nil)
		if err != nil {
			t.Fatal(err)
		}
		// Catalog membership is independent of executable rule registration.
		for _, metadata := range registry.Metadata() {
			fmt.Fprintf(&body, "  %s: {enabled: %t}\n", metadata.ID, metadata.ID == "MD094" && enabled)
		}
		put(t, filepath.Join(root, ".mdchk.yaml"), body.String())
		inv, err := prepareLint(lintOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if status := acquireLint(inv, io.Discard, io.Discard); status != 13 || len(inv.result.CheckedFiles) != 1 || len(inv.result.Errors) != 1 || inv.result.Errors[0].Code != "invalid-utf8" {
			t.Fatalf("gate bypassed: %d %+v", status, inv)
		}
	}
}

func TestActualLintCleanDisabledRulesAndFormats(t *testing.T) {
	root := validProject(t)
	put(t, filepath.Join(root, "doc.md"), "literal \ufffd\r\n")
	var body strings.Builder
	body.WriteString(availableConfig + "rules:\n")
	for _, metadata := range engine.Catalog() {
		fmt.Fprintf(&body, "  %s: {enabled: false}\n", metadata.ID)
	}
	put(t, filepath.Join(root, ".mdchk.yaml"), body.String())
	for _, format := range []string{"text", "json"} {
		var stderr bytes.Buffer
		if status := Run([]string{"lint", "--format", format, "doc.md"}, &stderr, io.Discard, "dev"); status != 0 || !strings.Contains(stderr.String(), "clean") {
			t.Fatalf("format=%s status=%d stderr=%s", format, status, &stderr)
		}
	}
}

func TestLintOversizedPreflightStartsNoCallbacks(t *testing.T) {
	root := validProject(t)
	put(t, filepath.Join(root, "a.md"), "ok")
	put(t, filepath.Join(root, "z.md"), "oversized")
	calls := 0
	registry := executionRegistry(t, map[string]engine.Check{"MD001": func(context.Context, engine.RuleContext) engine.RuleOutcome { calls++; return engine.RuleOutcome{} }}, false)
	status, inv, stderr := executeFixture(t, context.Background(), registry, config.Capabilities{CommonMark: true}, "--limit", "document-bytes=4")
	if status != 11 || calls != 0 || len(inv.result.CheckedFiles) != 0 || len(inv.result.Files) != 2 {
		t.Fatalf("preflight status=%d calls=%d stderr=%s result=%+v", status, calls, stderr, inv.result)
	}
	assertReadOnlyResult(t, inv.result)
}

func TestLintExecutionPreservesAncestorAndSelectedConfig(t *testing.T) {
	root := validProject(t)
	ancestor := filepath.Join(root, config.Filename)
	ancestorBody := "version: 1\ndirectives: {enabled: false}\nrules: {MD013: {length: 99}}\n"
	put(t, ancestor, ancestorBody)
	child := filepath.Join(root, "child")
	selected := filepath.Join(child, config.Filename)
	put(t, selected, "version: 1\nflavor: commonmark\n")
	target := filepath.Join(child, "doc.md")
	put(t, target, "# unchanged\r\n")
	t.Chdir(child)
	registry := executionRegistry(t, nil, false)
	status, inv, stderr := executeFixture(t, context.Background(), registry, config.Capabilities{CommonMark: true}, "doc.md")
	if status != 0 || len(inv.selection.AncestorPaths) != 1 {
		t.Fatalf("status=%d stderr=%s selection=%+v", status, stderr, inv.selection)
	}
	assertReadOnlyResult(t, inv.result)
	for path, want := range map[string]string{ancestor: ancestorBody, selected: "version: 1\nflavor: commonmark\n", target: "# unchanged\r\n"} {
		//nolint:gosec // Read only test-owned original bytes to verify lint immutability.
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("changed %s: %q %v", path, got, err)
		}
	}
}
