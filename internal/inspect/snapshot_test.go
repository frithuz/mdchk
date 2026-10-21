package inspect

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // G505: verify the required descriptive checksum, not authentication.
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/frithuz/mdchk/internal/config"
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/files"
	"github.com/frithuz/mdchk/internal/parser"
)

func fixture(t *testing.T) (string, Options) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r, err := engine.NewCatalogRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	return root, Options{WorkingDirectory: root, BoundaryDirectory: root, Registry: r, Capabilities: config.Capabilities{CommonMark: true, DirectiveProcessing: true}}
}
func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func acquire(t *testing.T, path string, o Options) *Snapshot {
	t.Helper()
	s, err := Acquire(context.Background(), path, o)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestSnapshotExactImmutableBytesAndLines(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("雪e\u0301\r\nsecond\rthird\n"), []byte("\xef\xbf\xbd"), {'x', 0xff, '\n'}} {
		t.Run(hex.EncodeToString(data), func(t *testing.T) {
			root, o := fixture(t)
			path := filepath.Join(root, "file.md")
			write(t, path, data)
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			s := acquire(t, "file.md", o)
			if s.File().Path != path || s.File().Version != "development" || s.File().SizeBytes != int64(len(data)) {
				t.Fatalf("metadata %+v", s.File())
			}
			//nolint:gosec // G401: verify the mandated descriptive checksum against original bytes.
			a, b := sha1.Sum(data), sha256.Sum256(data)
			if s.File().SHA1 != hex.EncodeToString(a[:]) || s.File().SHA256 != hex.EncodeToString(b[:]) {
				t.Fatal("checksums differ")
			}
			after, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if before.ModTime() != after.ModTime() {
				t.Fatal("inspection wrote target")
			}
			view := s.Source().Bytes()
			if len(view) > 0 {
				view[0] ^= 255
			}
			write(t, path, []byte("changed"))
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(s.Source().Bytes(), data) {
				t.Fatal("source changed")
			}
			wantLines := 0
			if len(data) > 0 {
				wantLines = 1
			}
			if strings.Contains(string(data), "second") {
				wantLines = 3
			}
			if s.Lines().LineCount() != wantLines {
				t.Fatalf("lines %d want %d", s.Lines().LineCount(), wantLines)
			}
			if len(data) > 1 && data[1] == 255 && s.Lines().EncodingFailure() == nil {
				t.Fatal("malformed bytes lost")
			}
		})
	}
}
func TestCanonicalTargetConfigurationExclusionAndOwnedViews(t *testing.T) {
	root, o := fixture(t)
	project := filepath.Join(root, "project")
	target := filepath.Join(project, "docs", "file.md")
	write(t, target, []byte("original\n"))
	ancestor := []byte("version: 1\nflavor: pandoc\nexclude: ['docs/**']\ndirectives:\n  enabled: false\n")
	write(t, filepath.Join(project, config.Filename), ancestor)
	write(t, filepath.Join(project, "docs", config.Filename), []byte("version: 1\nflavor: commonmark\nexclude: [file.md]\ndirectives:\n  enabled: true\nrules:\n  MD013:\n    length: 91\n"))
	if err := os.Mkdir(filepath.Join(project, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "explicit")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	s := acquire(t, link, o)
	if s.ConfigurationError() != nil {
		t.Fatal(s.ConfigurationError())
	}
	c := s.Configuration()
	if c.Settings.Flavor != "commonmark" || c.Settings.DirectivesEnabled || c.Rules["MD013"].Options["length"] != 91 {
		t.Fatalf("configuration %+v", c.Settings)
	}
	if !s.Exclusion().Known || !s.Exclusion().Excluded || len(s.Exclusion().Origins) != 1 || s.Exclusion().Origins[0].Path != filepath.Join(project, "docs", config.Filename) {
		t.Fatalf("exclusion %+v", s.Exclusion())
	}
	if s.File().Path != target {
		t.Fatal(s.File())
	}
	if len(s.Sources()) != 2 {
		t.Fatal(s.Sources())
	}
	c.Settings.Flavor = "pandoc"
	c.Rules["MD013"].Options["length"] = 1
	c.EffectivePermissions.Directives[0].Path = "changed"
	ex := s.Exclusion()
	ex.Origins[0].Path = "changed"
	if ex.Origins[0].Position != nil {
		ex.Origins[0].Position.Line = 99
	}
	sources := s.Sources()
	sources[0].SHA256 = "changed"
	if s.Configuration().Settings.Flavor != "commonmark" || s.Configuration().Rules["MD013"].Options["length"] != 91 || s.Exclusion().Origins[0].Path == "changed" || s.Sources()[0].SHA256 == "changed" {
		t.Fatal("mutable view aliases snapshot")
	}
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			for range 20 {
				if s.Configuration().Settings.Flavor != "commonmark" || !bytes.Equal(s.Source().Bytes(), []byte("original\n")) {
					t.Error("concurrent view changed")
				}
			}
		})
	}
	group.Wait()
}
func TestInvalidConfigurationObservedAndKnownUnsupportedFlavor(t *testing.T) {
	for _, definition := range []string{"version: [", "version: 9\n", "version: 1\nflavor: nonsense\n", "version: 1\nflavor: gfm\n"} {
		t.Run(definition, func(t *testing.T) {
			root, o := fixture(t)
			write(t, filepath.Join(root, "x.md"), []byte("metadata\n"))
			write(t, filepath.Join(root, config.Filename), []byte(definition))
			s := acquire(t, "x.md", o)
			if s.File().SizeBytes != 9 {
				t.Fatal(s.File())
			}
			if strings.Contains(definition, "gfm") {
				if s.Configuration() == nil || s.Configuration().Settings.Flavor != "gfm" || s.AdmissionError() == nil || !s.Exclusion().Known {
					t.Fatal("known unsupported config erased")
				}
			} else if s.ConfigurationError() == nil || s.Configuration() != nil || s.Exclusion().Known {
				t.Fatal("invalid configuration fabricated defaults")
			}
			if len(s.Sources()) != 1 {
				t.Fatal(s.Sources())
			}
		})
	}
}
func TestTargetRelativeExplicitConfigAndBoundaries(t *testing.T) {
	root, o := fixture(t)
	project := filepath.Join(root, "project")
	target := filepath.Join(project, "docs", "file.md")
	write(t, target, []byte("x"))
	write(t, filepath.Join(root, config.Filename), []byte("version: 9"))
	write(t, filepath.Join(project, "chosen.yaml"), []byte("version: 1\nflavor: commonmark\n"))
	if err := os.Mkdir(filepath.Join(project, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	o.ConfigPath = "project/chosen.yaml"
	s := acquire(t, target, o)
	if s.ConfigurationError() != nil || s.Configuration().Settings.Flavor != "commonmark" {
		t.Fatal(s.ConfigurationError())
	}
	o.ConfigPath = config.Filename
	s = acquire(t, target, o)
	if s.ConfigurationError() == nil || s.Configuration() != nil || len(s.Sources()) != 0 {
		t.Fatal("above repository config read")
	}
	o.ConfigPath = "project/missing.yaml"
	s = acquire(t, target, o)
	if s.ConfigurationError() == nil || len(s.Sources()) != 0 {
		t.Fatal("missing explicit config fabricated")
	}
}
func TestConfigurationByteCapsIndependentAndInterruptions(t *testing.T) {
	root, o := fixture(t)
	write(t, filepath.Join(root, "x.md"), []byte("x"))
	definition := []byte("version: 1\nflavor: commonmark\n")
	write(t, filepath.Join(root, config.Filename), definition)
	limits := parser.DefaultLimits()
	limits.DocumentBytes = 1
	o.Budget, _ = parser.NewOperationBudget(limits)
	o.ConfigBytes = int64(len(definition))
	o.TotalConfigBytes = int64(len(definition))
	if s := acquire(t, "x.md", o); s.ConfigurationError() != nil {
		t.Fatal(s.ConfigurationError())
	}
	o.Budget, _ = parser.NewOperationBudget(limits)
	o.ConfigBytes--
	s, err := Acquire(context.Background(), "x.md", o)
	var limit *parser.LimitExceeded
	if s == nil || !errors.As(err, &limit) || limit.Name != "config-bytes" {
		t.Fatalf("%+v %v", s, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = Acquire(ctx, "missing", o); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	limits.WorkUnits = 1
	o.Budget, _ = parser.NewOperationBudget(limits)
	if _, err = Acquire(context.Background(), "x.md", o); !errors.As(err, &limit) {
		t.Fatal(err)
	}
}
func TestDirectoryMissingAndVersionSize(t *testing.T) {
	root, o := fixture(t)
	if _, err := Acquire(context.Background(), root, o); err == nil {
		t.Fatal("directory accepted")
	}
	if _, err := Acquire(context.Background(), "missing", o); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte("a"), 1536)
	write(t, filepath.Join(root, "x.md"), data)
	o.Version = "v1.2.3"
	s := acquire(t, "x.md", o)
	if s.File().Version != "v1.2.3" || s.File().SizeDisplay != "1.5 KiB (1536 bytes)" {
		t.Fatal(s.File())
	}
}

func TestInvalidAncestorPolicyAndUnopenedDefinition(t *testing.T) {
	for _, mode := range []string{"invalid-ancestor", "policy-denial", "directory"} {
		t.Run(mode, func(t *testing.T) {
			root, o := fixture(t)
			target := filepath.Join(root, "child", "x.md")
			write(t, target, []byte("available"))
			write(t, filepath.Join(root, "child", config.Filename), []byte("version: 1\nflavor: commonmark\nexclude: ['x.md']\n"))
			switch mode {
			case "invalid-ancestor":
				write(t, filepath.Join(root, config.Filename), []byte("version: 9"))
			case "policy-denial":
				write(t, filepath.Join(root, config.Filename), []byte("version: 1\nallow_nested_excludes: false\n"))
			case "directory":
				if err := os.Mkdir(filepath.Join(root, config.Filename), 0700); err != nil {
					t.Fatal(err)
				}
			}
			s := acquire(t, target, o)
			if s.ConfigurationError() == nil || s.Configuration() != nil || s.Exclusion().Known || s.Source().Len() != 9 {
				t.Fatal("unresolved observation lost", s.ConfigurationError())
			}
			if mode == "directory" && len(s.Sources()) != 0 {
				t.Fatal("unread configuration hashed")
			}
		})
	}
}
func TestProviderOnceAndCancellationAfterConfigurationRead(t *testing.T) {
	root, o := fixture(t)
	write(t, filepath.Join(root, "x.md"), []byte("bytes"))
	write(t, filepath.Join(root, config.Filename), []byte("version: 1\nflavor: commonmark\nrules: {MD095: {enabled: false, allowed_hosts: [example.com]}}\n"))
	calls := 0
	o.Providers.ValidateHostname = func(string) error { calls++; return nil }
	s := acquire(t, "x.md", o)
	if s.ConfigurationError() != nil || calls != 1 {
		t.Fatalf("provider calls %d error %v", calls, s.ConfigurationError())
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	o.Providers.ValidateHostname = func(string) error { cancel(); return nil }
	s, err := Acquire(ctx, "x.md", o)
	if s == nil || !errors.Is(err, context.Canceled) || s.Configuration() != nil {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestSnapshotResolvesRawSymlinkDotDotBeforeConfiguration(t *testing.T) {
	root, o := fixture(t)
	lexical := filepath.Join(root, "target.md")
	physical := filepath.Join(root, "elsewhere", "target.md")
	write(t, lexical, []byte("lexical wrong bytes"))
	content := []byte("physical target\r\n")
	write(t, physical, content)
	if err := os.Mkdir(filepath.Join(root, "elsewhere", "dir"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "elsewhere", "dir"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, config.Filename), []byte("version: 1\nflavor: gfm\n"))
	write(t, filepath.Join(root, "elsewhere", config.Filename), []byte("version: 1\nflavor: commonmark\n"))
	for _, target := range []string{"link/../target.md", root + "/link/../target.md"} {
		t.Run(target, func(t *testing.T) {
			s := acquire(t, target, o)
			//nolint:gosec // G401: verify the required descriptive snapshot checksum.
			one := sha1.Sum(content)
			sum := sha256.Sum256(content)
			if s.File().Path != physical || !bytes.Equal(s.Source().Bytes(), content) || s.File().SHA256 != hex.EncodeToString(sum[:]) || s.File().SHA1 != hex.EncodeToString(one[:]) || s.File().SizeBytes != int64(len(content)) {
				t.Fatalf("wrong snapshot: %+v %q", s.File(), s.Source().Bytes())
			}
			if s.ConfigurationError() != nil || s.Configuration().Settings.Flavor != "commonmark" || s.Configuration().Selected.Path != filepath.Join(root, "elsewhere", config.Filename) {
				t.Fatalf("wrong target-parent configuration: %v", s.ConfigurationError())
			}
		})
	}
}
func TestSnapshotSelectedExclusionsAgreeWithNormalSelection(t *testing.T) {
	root, o := fixture(t)
	target := filepath.Join(root, "child", "x.md")
	write(t, target, []byte("available"))
	write(t, filepath.Join(root, config.Filename), []byte("version: 1\nexclude: ['child/**']\ndirectives: {enabled: false}\n"))
	selected := filepath.Join(root, "child", config.Filename)
	for _, exclude := range []string{"[]", "[x.md]"} {
		t.Run(exclude, func(t *testing.T) {
			write(t, selected, []byte("version: 1\nflavor: commonmark\nexclude: "+exclude+"\n"))
			s := acquire(t, target, o)
			if s.ConfigurationError() != nil {
				t.Fatal(s.ConfigurationError())
			}
			if s.Configuration().Settings.DirectivesEnabled {
				t.Fatal("ancestor denial lost")
			}
			patterns, err := config.CompilePatterns(s.Configuration().Settings.Exclude)
			if err != nil {
				t.Fatal(err)
			}
			normal, err := files.Excludes(filepath.Dir(selected), target, false, patterns)
			if err != nil {
				t.Fatal(err)
			}
			status := s.Exclusion()
			if !status.Known || status.Excluded != normal {
				t.Fatalf("inspect %+v normal %v", status, normal)
			}
			if normal && (len(status.Origins) != 1 || status.Origins[0].Path != selected) {
				t.Fatalf("wrong selected provenance: %+v", status)
			}
			if !normal && len(status.Origins) != 0 {
				t.Fatal("ancestor exclusion leaked")
			}
		})
	}
}
