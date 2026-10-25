//go:build linux || darwin || windows

package resource

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestFilesystemAnchorIndexResourceViewsShareFirstBytes(t *testing.T) {
	f := newFilesystemFixture(t)
	f.write(t, "asset.md", []byte("# Foo\r\n\r\n# Foo\r\n\r\n<span id='foo-1'></span>\r\n"))
	service := filesystemTestService(t, f)
	reads := 0
	read := f.filesystem.read
	f.filesystem.read = func(file *os.File, p []byte) (int, error) {
		reads++
		return read(file, p)
	}
	var firstSource parser.Source
	firstReads := 0
	for _, phase := range []Phase{Original, Candidate, Saved} {
		var replacements []Replacement
		if phase != Original {
			replacements = []Replacement{{TargetID: "report.md", Source: parser.NewSource([]byte("# Requester " + string(phase) + "\r\n"))}}
		}
		view, err := f.operation.NewView(phase, replacements)
		if err != nil {
			t.Fatal(err)
		}
		q := f.request(t, "asset.md", ParsedAnchors, RegularFile, view)
		spec, err := service.Read(context.Background(), q)
		if err != nil || spec.Anchors != Available || spec.Index.state == nil {
			t.Fatal(phase, spec, err)
		}
		if firstSource == (parser.Source{}) {
			firstSource, firstReads = spec.Source, reads
			f.write(t, "asset.md", []byte("# Changed after first resource observation\n"))
		} else if spec.Source != firstSource || reads != firstReads {
			t.Fatal("resource-only view reopened first bytes", phase, reads, firstReads)
		}
		headings, err := spec.Index.Headings(context.Background())
		if err != nil || len(headings) != 2 || headings[0].ID != parser.Some("foo") || headings[1].ID != parser.Some("foo-2") {
			t.Fatal(phase, headings, err)
		}
		if _, err := spec.Source.Text(headings[1].Envelope); err != nil {
			t.Fatal("native heading source ownership", err)
		}
		observation, err := f.operation.Record(q, spec)
		if err != nil || observation.Treatment() != Complete {
			t.Fatal(phase, observation, err)
		}
	}
	if firstReads == 0 {
		t.Fatal("no actual native acquisition")
	}
}

func TestFilesystemAnchorSelectedNoKeyAndMetadataStaySelfOnly(t *testing.T) {
	for _, metadata := range []bool{false, true} {
		t.Run(map[bool]string{false: "outside", true: "metadata"}[metadata], func(t *testing.T) {
			dir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			outside, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(outside, "outside.md")
			if metadata {
				path = filepath.Join(dir, ".git", "selected.md")
				if err := os.Mkdir(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
			}
			source := parser.NewSource([]byte("# Original\r\n[other](secret.md)\r\n"))
			if err := os.WriteFile(path, source.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "secret.md"), []byte("# In root\n"), 0600); err != nil {
				t.Fatal(err)
			}
			budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
			native, err := OpenFilesystemRoot(context.Background(), budget, dir, DefaultFilesystemLimits())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := native.Close(); err != nil {
					t.Error(err)
				}
			})
			proof, err := native.BindTarget(path, "selected-report.md", source)
			if err != nil || proof.Target().ResourceKey != (Key{}) {
				t.Fatal("unproved/metadata target acquired a resource key", proof.Target(), err)
			}
			settings, _ := NewSettings(parser.ParseOptions{}, "index.md", "github", "selected-anchor-v1")
			op, err := NewOperation(context.Background(), budget, native.Root(), settings, []Target{proof.Target()})
			if err != nil {
				t.Fatal(err)
			}
			reader, err := native.Reader(op, proof)
			if err != nil {
				t.Fatal(err)
			}
			service, err := NewFilesystemService(reader, DefaultServiceLimits())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := service.Close(); err != nil {
					t.Error(err)
				}
			})
			original, _ := op.NewView(Original, nil)
			result, err := service.TargetAnchors(context.Background(), original, "selected-report.md")
			index, ok := result.Index()
			if err != nil || !ok || index.Source() != source {
				t.Fatal("NoKey target could not analyze its own snapshot", result.Availability(), ok, err)
			}
			content, evidence, valid := result.Content()
			if !valid || content != source || evidence.Source() != source {
				t.Fatal("selected content/witness lost")
			}
			if targets, err := index.Lookup(context.Background(), "original"); err != nil || len(targets) != 1 {
				t.Fatal(targets, err)
			}
			candidateSource := parser.NewSource([]byte("# Candidate\n"))
			candidate, _ := op.NewView(Candidate, []Replacement{{TargetID: "selected-report.md", Source: candidateSource}})
			if _, err := service.Anchors(context.Background(), candidate, evidence); !errors.Is(err, ErrContract) {
				t.Fatal("old selected source borrowed a candidate view", err)
			}
			candidateResult, err := service.TargetAnchors(context.Background(), candidate, "selected-report.md")
			candidateIndex, ok := candidateResult.Index()
			if err != nil || !ok || candidateIndex.Source() != candidateSource || candidateIndex == index {
				t.Fatal("candidate index fell back", ok, err)
			}
			unavailable, _ := op.NewView(Saved, []Replacement{{TargetID: "selected-report.md", Unavailable: true}})
			missing, err := service.TargetAnchors(context.Background(), unavailable, "selected-report.md")
			if err != nil || missing.Availability() != Unavailable {
				t.Fatal("unavailable Saved source fell back", missing.Availability(), err)
			}
			if _, exists := missing.Index(); exists {
				t.Fatal("unavailable Saved index available")
			}
			span, _ := source.Span(0, 1)
			link, _ := NewLink("selected-report.md", "MD051", "secret.md", source, span)
			key, _ := NewKey("secret.md")
			q, err := original.Request(RequestSpec{Key: key, Link: link, Class: Local, Capability: ParsedAnchors, Required: RegularFile})
			if err != nil {
				t.Fatal(err)
			}
			cross, err := service.Read(context.Background(), q)
			if err != nil || cross.Existence != Unknown || cross.Problem.Kind != Blocked || cross.Index.state != nil || op.Targets()[0].ResourceKey != (Key{}) || op.Root() != native.Root() {
				t.Fatal("self index widened resource authority", cross, err)
			}
			if _, err := service.TargetAnchors(context.Background(), original, "foreign-id"); !errors.Is(err, ErrContract) {
				t.Fatal("foreign report ID gained target authority", err)
			}
		})
	}
}
