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

	"github.com/frithuz/mdchk/internal/engine"
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
			if status := Run(args, &out, &stderr, "dev"); status != tc.status || out.Len() != 0 {
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
			if status := acquireLint(inv, io.Discard, io.Discard); status != tc.status || len(inv.encoding) != tc.count || inv.acquisition == nil || inv.targetSelection == nil || len(inv.checkedPaths) != tc.count {
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
		if len(inv.encoding) != 2 || len(inv.checkedPaths) != 2 || len(inv.acquisition.Documents) != 2 {
			t.Fatalf("independent good target lost: %+v", inv)
		}
		e := inv.encoding[0].validation.Error
		wantRule := ""
		if enabled {
			wantRule = "MD094"
		}
		if e == nil || e.Rule != wantRule || e.Code != "invalid-utf8" || e.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256(raw)) || e.Snapshot.SizeBytes != len(raw) || *e.ByteOffset != len(raw)-2 || e.Range.Start.Line != 2 || e.Range.Start.Column != 3 || inv.encoding[1].validation.Error != nil {
			t.Fatalf("wrong validation: %+v", e)
		}
		if string(inv.acquisition.Documents[0].Source.Bytes()) != string(raw) {
			t.Fatal("source changed")
		}
	}
	var stderr bytes.Buffer
	if status := Run([]string{"lint"}, io.Discard, &stderr, "dev"); status != 13 || !strings.Contains(stderr.String(), "Invalid UTF-8") {
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
		if tc.stops && (len(inv.acquisition.Interruptions) == 0 || len(inv.checkedPaths) != 0) {
			t.Fatalf("interruption lost: %+v", inv)
		}
	}
	inv, err := prepareLint(lintOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if status := acquireLintContext(ctx, inv, io.Discard); status != 15 || len(inv.acquisition.Interruptions) == 0 || len(inv.checkedPaths) != 0 {
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
		if status := acquireLint(inv, io.Discard, io.Discard); status != 13 || len(inv.encoding) != 1 || inv.encoding[0].validation.Error == nil {
			t.Fatalf("gate bypassed: %d %+v", status, inv)
		}
	}
}
