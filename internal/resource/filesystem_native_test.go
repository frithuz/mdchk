//go:build linux || darwin || windows

package resource

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

type filesystemFixture struct {
	dir        string
	filesystem *FilesystemRoot
	reader     *FilesystemReader
	operation  *Operation
	view       View
	source     parser.Source
	budget     *parser.OperationBudget
	proof      FilesystemTarget
}

func newFilesystemFixture(t *testing.T) *filesystemFixture {
	t.Helper()
	return newFilesystemFixtureOptions(t, context.Background(), context.Background(), parser.DefaultLimits(), DefaultFilesystemLimits())
}

func newFilesystemFixtureOptions(t *testing.T, rootContext, operationContext context.Context, parserLimits parser.Limits, limits FilesystemLimits) *filesystemFixture {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	budget, err := parser.NewOperationBudget(parserLimits)
	if err != nil {
		t.Fatal(err)
	}
	f, err := OpenFilesystemRoot(rootContext, budget, dir, limits)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			t.Error(err)
		}
	})
	source := parser.NewSource([]byte("# α\r\n[link](asset.md)\r\n"))
	if err := os.WriteFile(filepath.Join(dir, "source.md"), source.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	proof, err := f.BindTarget(filepath.Join(dir, "source.md"), "report.md", source)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := NewSettings(parser.ParseOptions{}, "index.md", "github", "native-v1")
	if err != nil {
		t.Fatal(err)
	}
	op, err := NewOperation(operationContext, budget, f.Root(), settings, []Target{proof.Target()})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := f.Reader(op, proof)
	if err != nil {
		t.Fatal(err)
	}
	view, err := op.NewView(Original, nil)
	if err != nil {
		t.Fatal(err)
	}
	return &filesystemFixture{dir, f, reader, op, view, source, budget, proof}
}

func (f *filesystemFixture) write(t *testing.T, key string, content []byte) {
	t.Helper()
	path := filepath.Join(f.dir, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
}
func (f *filesystemFixture) request(t *testing.T, key string, capability Capability, required RequiredType, views ...View) Request {
	t.Helper()
	view := f.view
	if len(views) > 0 {
		view = views[0]
	}
	target, available, exists := view.Target("report.md")
	if !available || !exists {
		t.Fatal("requesting source unavailable")
	}
	span, err := target.Source.Span(0, target.Source.Len())
	if err != nil {
		t.Fatal(err)
	}
	link, err := NewLink("report.md", "MD057", key, target.Source, span)
	if err != nil {
		t.Fatal(err)
	}
	logical, err := NewKey(key)
	if err != nil {
		t.Fatal(err)
	}
	request, err := view.Request(RequestSpec{Key: logical, Link: link, Capability: capability, Required: required, Class: Local})
	if err != nil {
		t.Fatal(err)
	}
	return request
}
func (f *filesystemFixture) observe(t *testing.T, request Request) (Observation, error) {
	t.Helper()
	spec, readErr := f.reader.Read(context.Background(), request)
	observation, err := f.operation.Record(request, spec)
	if err != nil {
		t.Fatalf("invalid native fact %+v: %v (read=%v)", spec, err, readErr)
	}
	return observation, readErr
}

func TestFilesystemNativeReadOnlyFacts(t *testing.T) {
	f := newFilesystemFixture(t)
	f.write(t, "asset.md", []byte("# resource\n[other](../../unread.md)\n"))
	f.write(t, "nested/.mdchk.yml", []byte("invalid: [config\n"))
	f.write(t, "image.bin", []byte{0xff, 0, 0xfe})
	if err := os.Mkdir(filepath.Join(f.dir, "empty"), 0700); err != nil {
		t.Fatal(err)
	}
	reads := 0
	underlying := f.filesystem.read
	f.filesystem.read = func(file *os.File, b []byte) (int, error) { reads++; return underlying(file, b) }
	for _, tc := range []struct {
		key        string
		capability Capability
		required   RequiredType
		existence  Existence
		content    Availability
		treatment  Treatment
	}{
		{"asset.md", TextContent, RegularFile, File, Available, Complete},
		{"image.bin", ExistenceOnly, RegularFile, File, NotRequested, Complete},
		{"image.bin", ParsedAnchors, RegularFile, File, NotRequested, ContextIncomplete},
		{"image.bin", ParsedAnchors, DirectoryType, File, NotRequested, MissingTarget},
		{"empty", TextContent, RegularFile, Directory, Inapplicable, MissingTarget},
		{"absent.md", TextContent, RegularFile, Missing, Inapplicable, MissingTarget},
	} {
		t.Run(tc.key+string(tc.capability)+string(tc.required), func(t *testing.T) {
			before := reads
			request := f.request(t, tc.key, tc.capability, tc.required)
			observation, err := f.observe(t, request)
			if err != nil {
				t.Fatal(err)
			}
			if observation.Existence() != tc.existence || observation.ContentAvailability() != tc.content || observation.Treatment() != tc.treatment {
				t.Fatalf("wrong fact %+v", observation)
			}
			if tc.content != Available && reads != before {
				t.Fatal("unneeded binary/type-mismatch content read")
			}
			if tc.content == Available {
				source, ok := observation.Content()
				if !ok || string(source.Bytes()) != "# resource\n[other](../../unread.md)\n" {
					t.Fatal("wrong exact resource source")
				}
			}
		})
	}
	if len(f.operation.Targets()) != 1 {
		t.Fatal("resource registered as target")
	}
	data, err := os.ReadFile(filepath.Join(f.dir, "asset.md"))
	if err != nil || string(data) != "# resource\n[other](../../unread.md)\n" {
		t.Fatal("resource changed", err)
	}
}

func TestFilesystemNativeExactNamesAndMetadata(t *testing.T) {
	f := newFilesystemFixture(t)
	for _, name := range []string{"guide.md", "a%20b.md", "é.md"} {
		f.write(t, name, []byte(name))
	}
	for _, name := range []string{"Guide.md", "a b.md", "e\u0301.md"} {
		observation, err := f.observe(t, f.request(t, name, ExistenceOnly, RegularFile))
		if err != nil || observation.Existence() != Missing {
			t.Fatal("alias accepted as exact logical name", name, observation, err)
		}
	}
	for _, name := range []string{"guide.md", "a%20b.md", "é.md"} {
		observation, err := f.observe(t, f.request(t, name, TextContent, RegularFile))
		if err != nil {
			t.Fatal(err)
		}
		source, available := observation.Content()
		if !available || string(source.Bytes()) != name {
			t.Fatal("literal spelling lost", name)
		}
	}
	f.write(t, ".git/config", []byte("metadata"))
	observation, err := f.observe(t, f.request(t, ".git/config", TextContent, RegularFile))
	if err != nil || observation.Problem().Kind != Blocked || observation.Existence() != Unknown {
		t.Fatal("metadata not blocked", observation, err)
	}
	upper := filepath.Join(f.dir, ".GIT")
	if _, err := os.Lstat(upper); errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(upper, 0700); err != nil {
			t.Fatal(err)
		}
		f.write(t, ".GIT/ordinary.md", []byte("ordinary"))
		observation, err = f.observe(t, f.request(t, ".GIT/ordinary.md", TextContent, RegularFile))
		if err != nil || observation.ContentAvailability() != Available {
			t.Fatal("distinct sensitive metadata-like name denied", observation, err)
		}
	} else {
		observation, err = f.observe(t, f.request(t, ".GIT/config", TextContent, RegularFile))
		if err != nil {
			t.Fatal(err)
		}
		// Metadata aliases are prohibited identities, independently of exact
		// logical absence. They must never expose Git bytes or become Missing.
		if observation.Existence() != Unknown || observation.Problem().Kind != Blocked {
			t.Fatal("Git alias exposed", observation)
		}
	}
}

func TestFilesystemNativeNoFollowEveryComponent(t *testing.T) {
	for _, kind := range []string{"contained-leaf", "outside-leaf", "dangling-leaf", "contained-parent", "outside-parent", "dangling-parent"} {
		t.Run(kind, func(t *testing.T) {
			f := newFilesystemFixture(t)
			f.write(t, "target.md", []byte("must not follow"))
			f.write(t, "directory/target.md", []byte("must not follow parent"))
			outside, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(outside, "target.md"), []byte("outside"), 0600); err != nil {
				t.Fatal(err)
			}
			link, target, key := filepath.Join(f.dir, "link.md"), filepath.Join(f.dir, "target.md"), "link.md"
			if strings.Contains(kind, "parent") {
				link, key = filepath.Join(f.dir, "linked"), "linked/target.md"
				target = filepath.Join(f.dir, "directory")
			}
			if strings.HasPrefix(kind, "outside") {
				target = outside
				if strings.Contains(kind, "leaf") {
					target = filepath.Join(outside, "target.md")
				}
			}
			if strings.HasPrefix(kind, "dangling") {
				target = filepath.Join(f.dir, "not-there")
			}
			if err := os.Symlink(target, link); err != nil {
				t.Fatalf("critical native symlink fixture unavailable: %v", err)
			}
			reads := 0
			f.filesystem.read = func(*os.File, []byte) (int, error) { reads++; return 0, errors.New("forbidden content read") }
			observation, err := f.observe(t, f.request(t, key, TextContent, RegularFile))
			if err != nil {
				t.Fatal(err)
			}
			if observation.Existence() != Unknown || observation.Problem().Kind != Blocked || reads != 0 {
				t.Fatal("prohibited link followed", observation, reads)
			}
		})
	}
}

func TestFilesystemNativeLinkSubstitutionAtOpen(t *testing.T) {
	for _, parent := range []bool{false, true} {
		t.Run(map[bool]string{false: "leaf", true: "parent"}[parent], func(t *testing.T) {
			f := newFilesystemFixture(t)
			key := "entry.md"
			original := filepath.Join(f.dir, key)
			parked := filepath.Join(f.dir, "parked.md")
			if parent {
				key = "entry/asset.md"
				original = filepath.Join(f.dir, "entry")
				parked = filepath.Join(f.dir, "parked")
			}
			f.write(t, key, []byte("same inode retained"))
			changed, restored := false, false
			f.filesystem.hook = func(stage, name string) error {
				match := name == key
				if parent {
					match = name == "entry"
				}
				if stage == "before-open" && match && !changed {
					changed = true
					if err := os.Rename(original, parked); err != nil {
						return err
					}
					if err := os.Symlink(parked, original); err != nil {
						return err
					}
				}
				// Unsafe follow-plus-postcheck openers would see the same inode and
				// restored entry, so only a true no-follow primitive passes this test.
				if stage == "after-open" && match && changed && !restored {
					restored = true
					if err := os.Remove(original); err != nil {
						return err
					}
					return os.Rename(parked, original)
				}
				return nil
			}
			reads := 0
			read := f.filesystem.read
			f.filesystem.read = func(file *os.File, b []byte) (int, error) { reads++; return read(file, b) }
			observation, err := f.observe(t, f.request(t, key, TextContent, RegularFile))
			if err != nil {
				t.Fatal(err)
			}
			if !changed || observation.Problem().Kind != Blocked || reads != 0 {
				t.Fatal("same-identity link substitution not denied", observation, reads)
			}
		})
	}
}

func TestFilesystemNativeDataHandleSubstitution(t *testing.T) {
	for _, directory := range []bool{false, true} {
		t.Run(map[bool]string{false: "content", true: "directory"}[directory], func(t *testing.T) {
			f := newFilesystemFixture(t)
			key := "entry.md"
			name := key
			before, after := "before-content-open", "after-content-open"
			if directory {
				key, name = "entry/asset.md", "entry"
				before, after = "before-directory-open", "after-directory-open"
			}
			f.write(t, key, []byte("same identity data"))
			original, parked := filepath.Join(f.dir, name), filepath.Join(f.dir, "parked")
			changed, restored := false, false
			f.filesystem.hook = func(stage, prefix string) error {
				if prefix != name {
					return nil
				}
				if stage == before && !changed {
					changed = true
					if err := os.Rename(original, parked); err != nil {
						return err
					}
					return os.Symlink(parked, original)
				}
				if stage == after && changed && !restored {
					restored = true
					if err := os.Remove(original); err != nil {
						return err
					}
					return os.Rename(parked, original)
				}
				return nil
			}
			reads := 0
			read := f.filesystem.read
			f.filesystem.read = func(file *os.File, b []byte) (int, error) { reads++; return read(file, b) }
			observation, err := f.observe(t, f.request(t, key, TextContent, RegularFile))
			if err != nil || !changed || observation.Problem().Kind != Blocked || reads != 0 {
				t.Fatal("native data handle followed swapped same-identity link", observation, err, reads)
			}
		})
	}
}

func TestFilesystemNativeBindingAndImmutableViews(t *testing.T) {
	f := newFilesystemFixture(t)
	// Unreceipted report spellings do not become native membership merely from
	// a plausible literal ResourceKey; the new reader requires the root receipt.
	if _, err := f.filesystem.Reader(f.operation); !errors.Is(err, ErrContract) {
		t.Fatal("unproved key accepted", err)
	}
	if err := os.WriteFile(filepath.Join(f.dir, "source.md"), []byte("changed disk"), 0600); err != nil {
		t.Fatal(err)
	}
	candidate := parser.NewSource([]byte("candidate snapshot\n"))
	saved := parser.NewSource([]byte("saved snapshot\n"))
	viewCandidate, err := f.operation.NewView(Candidate, []Replacement{{TargetID: "report.md", Source: candidate}})
	if err != nil {
		t.Fatal(err)
	}
	viewSaved, err := f.operation.NewView(Saved, []Replacement{{TargetID: "report.md", Source: saved}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		view   View
		source parser.Source
	}{{f.view, f.source}, {viewCandidate, candidate}, {viewSaved, saved}} {
		request := f.request(t, "source.md", TextContent, RegularFile, tc.view)
		observation, err := f.observe(t, request)
		if err != nil {
			t.Fatal(err)
		}
		source, ok := observation.Content()
		if !ok || source != tc.source {
			t.Fatal("selected view reopened from disk", observation)
		}
	}
	unavailable, err := f.operation.NewView(Saved, []Replacement{{TargetID: "report.md", Unavailable: true}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, available := unavailable.Target("report.md"); !available {
		t.Fatal("target identity lost")
	}
	// A second root with the same native spelling cannot grant authority to an
	// existing operation/request from the first opaque root.
	other, err := OpenFilesystemRoot(context.Background(), f.budget, f.dir, DefaultFilesystemLimits())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := other.Close(); err != nil {
			t.Error(err)
		}
	}()
	if _, err := other.Reader(f.operation); !errors.Is(err, ErrContract) {
		t.Fatal("foreign equal-spelled root accepted", err)
	}
}

func TestFilesystemNativeSelectedReplacementDocumentLimit(t *testing.T) {
	limits := parser.DefaultLimits()
	limits.DocumentBytes = 64
	f := newFilesystemFixtureOptions(t, context.Background(), context.Background(), limits, DefaultFilesystemLimits())
	for _, kind := range []Phase{Candidate, Saved} {
		t.Run(string(kind), func(t *testing.T) {
			replacement := parser.NewSource([]byte(strings.Repeat("x", 65)))
			view, err := f.operation.NewView(kind, []Replacement{{TargetID: "report.md", Source: replacement}})
			if err != nil {
				t.Fatal(err)
			}
			request := f.request(t, "source.md", TextContent, RegularFile, view)
			before := f.filesystem.bytes
			spec, err := f.reader.Read(context.Background(), request)
			var limit *parser.LimitExceeded
			if !errors.As(err, &limit) || limit.Name != "document-bytes" || spec.Existence != File || spec.Content != Interrupted || f.filesystem.bytes != before {
				t.Fatal("oversized selected replacement was scanned/returned or erased File", spec, err)
			}
		})
	}
}

func TestFilesystemNativeOtherSelectedSavedTargetNeverReopened(t *testing.T) {
	f := newFilesystemFixture(t)
	f.write(t, "asset.md", []byte("original selected asset"))
	budget := rootPolicyBudget(t, parser.DefaultLimits())
	session, err := f.filesystem.Session(context.Background(), budget, DefaultFilesystemLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := session.Close(); err != nil {
			t.Error(err)
		}
	})
	origin, err := session.BindTarget(filepath.Join(f.dir, "source.md"), "report.md", f.source)
	if err != nil {
		t.Fatal(err)
	}
	assetSource := parser.NewSource([]byte("original selected asset"))
	asset, err := session.BindTarget(filepath.Join(f.dir, "asset.md"), "other-report.md", assetSource)
	if err != nil {
		t.Fatal(err)
	}
	op, err := NewOperation(context.Background(), budget, session.Root(), f.operation.Settings(), []Target{origin.Target(), asset.Target()})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := session.Reader(op, origin, asset)
	if err != nil {
		t.Fatal(err)
	}
	f.filesystem, f.operation, f.reader = session, op, reader
	nativeCalls := 0
	open, metadata, read := session.open, session.metadata, session.read
	session.open = func(parent *os.File, name string, directory bool) (*os.File, error) {
		nativeCalls++
		return open(parent, name, directory)
	}
	session.metadata = func(parent *os.File, name string, directory bool) (*os.File, error) {
		nativeCalls++
		return metadata(parent, name, directory)
	}
	session.read = func(file *os.File, b []byte) (int, error) { nativeCalls++; return read(file, b) }
	f.write(t, "asset.md", []byte("changed disk must not replace saved snapshot"))
	for _, unavailable := range []bool{true, false} {
		saved := parser.NewSource([]byte("confirmed saved bytes"))
		replacement := Replacement{TargetID: "other-report.md", Source: saved}
		if unavailable {
			replacement.Source, replacement.Unavailable = parser.Source{}, true
		}
		view, err := op.NewView(Saved, []Replacement{replacement})
		if err != nil {
			t.Fatal(err)
		}
		spec, err := reader.Read(context.Background(), f.request(t, "asset.md", TextContent, RegularFile, view))
		if err != nil || nativeCalls != 0 {
			t.Fatal("selected target reopened", spec, err, nativeCalls)
		}
		if unavailable && (spec.Existence != Unknown || spec.Problem.Kind != Blocked) {
			t.Fatal("unavailable saved target became disk original", spec)
		}
		if !unavailable && (spec.Content != Available || spec.Source != saved) {
			t.Fatal("saved snapshot replaced", spec)
		}
	}
}

func TestFilesystemNativeOutsideBindingRetainsOnlySnapshotAuthority(t *testing.T) {
	f := newFilesystemFixture(t)
	budget := rootPolicyBudget(t, parser.DefaultLimits())
	session, err := f.filesystem.Session(context.Background(), budget, DefaultFilesystemLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := session.Close(); err != nil {
			t.Error(err)
		}
	})
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("disk is not the selected snapshot"), 0600); err != nil {
		t.Fatal(err)
	}
	proof, err := session.BindTarget(outside, "report.md", f.source)
	if err != nil || proof.Target().ID != "report.md" || proof.Target().Source != f.source || proof.Target().ResourceKey != (Key{}) {
		t.Fatal("outside target expanded root or lost original snapshot", proof, err)
	}
	op, err := NewOperation(context.Background(), budget, session.Root(), f.operation.Settings(), []Target{proof.Target()})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := session.Reader(op, proof)
	if err != nil {
		t.Fatal(err)
	}
	view, err := op.NewView(Original, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.filesystem, f.operation, f.reader, f.view = session, op, reader, view
	session.open = func(*os.File, string, bool) (*os.File, error) {
		t.Fatal("outside source obtained cross-file native authority")
		return nil, ErrContract
	}
	session.metadata = session.open
	spec, err := reader.Read(context.Background(), f.request(t, "source.md", ExistenceOnly, RegularFile))
	if err != nil || spec.Existence != Unknown || spec.Problem.Kind != Blocked {
		t.Fatal(spec, err)
	}
	span, err := f.source.Span(0, f.source.Len())
	if err != nil {
		t.Fatal(err)
	}
	link, err := NewLink("report.md", "MD051", "#heading", f.source, span)
	if err != nil {
		t.Fatal(err)
	}
	normalizer, err := NewNormalizer(view, DefaultPathLimits(), NativeNamePolicy)
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := normalizer.Normalize(link)
	if err != nil {
		t.Fatal(err)
	}
	_, hasKey := normalized.Location().Key()
	if source, same := normalized.SameSource(); !same || source != f.source || hasKey {
		t.Fatal("outside same-document snapshot lost or gained native key", normalized)
	}
}

func TestFilesystemNativeMetadataThenContentFailure(t *testing.T) {
	f := newFilesystemFixture(t)
	f.write(t, "asset.md", []byte("resource bytes"))
	open := f.filesystem.open
	f.filesystem.open = func(parent *os.File, name string, directory bool) (*os.File, error) {
		if name == "asset.md" && !directory {
			return nil, os.ErrPermission
		}
		return open(parent, name, directory)
	}
	existence, err := f.observe(t, f.request(t, "asset.md", ExistenceOnly, RegularFile))
	if err != nil || existence.Existence() != File || existence.Treatment() != Complete {
		t.Fatal("existence required content access", existence, err)
	}
	content, err := f.observe(t, f.request(t, "asset.md", TextContent, RegularFile))
	if !errors.Is(err, os.ErrPermission) || content.Existence() != File || content.ContentAvailability() != Failed || content.Problem().Kind != ReadFailure {
		t.Fatal("known existence/error lost", content, err)
	}
}
