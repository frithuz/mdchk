//go:build linux || darwin || windows

package resource_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
	r "github.com/frithuz/mdchk/internal/resource"
)

// This runs in the existing actual-host native gate on Linux/macOS/Windows.
// Expectations are independently stated logical facts, not one adapter's output
// used as the other's oracle. Partial omission is intentionally not native absence.
func TestFilesystemSuppliedInventoryParity(t *testing.T) {
	text := "雪 [file](file.md) [empty](empty.md) [dir](empty/) [missing](missing.md) [child](file.md/child) [image](image.bin) [percent](a%252Fb.md) [unicode](雪.md) [case](guide.md) [selected](b.txt) [excluded](excluded.md) [private](private/secret.md)\r\n"
	f := newInventoryFixture(t, context.Background(), `{"complete":true,"entries":[{"path":"docs/private","kind":"unavailable"},{"path":"docs/file.md","kind":"file","content":"# file 雪\r\n"},{"path":"docs/empty.md","kind":"file","content":""},{"path":"docs/empty","kind":"directory"},{"path":"docs/image.bin","kind":"file"},{"path":"docs/a%2Fb.md","kind":"file","content":"# percent\n"},{"path":"docs/雪.md","kind":"file","content":"# Unicode\r\n"},{"path":"docs/Guide.md","kind":"file","content":"# exact case\n"}]}`, text,
		r.SuppliedFile{Key: mustKey(t, "docs/b.txt"), Source: parser.NewSource([]byte("# selected original\n"))},
		r.SuppliedFile{Key: mustKey(t, "docs/excluded.md"), Source: parser.NewSource([]byte("# excluded seed\r\n"))})
	// Promote only b.txt into the selected subset. All other supplied files remain
	// resource seeds, just like unselected native entries.
	b := f.input.Files()[1]
	mapOperation, err := r.NewOperation(context.Background(), f.operation.Budget(), f.operation.Root(), f.operation.Settings(), []r.Target{{ID: "docs/a.md", Source: f.source, ResourceKey: mustKey(t, "docs/a.md")}, {ID: "b-report", Source: b.Source, ResourceKey: b.Key}})
	if err != nil {
		t.Fatal(err)
	}
	mapReader, err := r.NewSuppliedReader(context.Background(), mapOperation, f.input, r.DefaultInputLimits())
	if err != nil {
		t.Fatal(err)
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"a.md": []byte(text), "b.txt": b.Source.Bytes(), "excluded.md": []byte("# excluded seed\r\n"),
		"file.md": []byte("# file 雪\r\n"), "empty.md": {}, "image.bin": {255, 0, 254},
		"a%2Fb.md": []byte("# percent\n"), "雪.md": []byte("# Unicode\r\n"), "Guide.md": []byte("# exact case\n"),
	} {
		if err := os.WriteFile(filepath.Join(dir, "docs", name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "docs", "empty"), 0o700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.md"), []byte("must not be acquired"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "docs", "private")); err != nil {
		t.Fatal("native blocked-subtree fixture requires a real directory symlink", err)
	}
	budget, err := parser.NewOperationBudget(parser.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	native, err := r.OpenFilesystemRoot(context.Background(), budget, dir, r.DefaultFilesystemLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := native.Close(); err != nil {
			t.Error(err)
		}
	})
	aProof, err := native.BindTarget(filepath.Join(dir, "docs", "a.md"), "docs/a.md", f.source)
	if err != nil {
		t.Fatal(err)
	}
	bProof, err := native.BindTarget(filepath.Join(dir, "docs", "b.txt"), "b-report", b.Source)
	if err != nil {
		t.Fatal(err)
	}
	nativeOperation, err := r.NewOperation(context.Background(), budget, native.Root(), f.operation.Settings(), []r.Target{aProof.Target(), bProof.Target()})
	if err != nil {
		t.Fatal(err)
	}
	nativeReader, err := native.Reader(nativeOperation, aProof, bProof)
	if err != nil {
		t.Fatal(err)
	}
	replacement := parser.NewSource([]byte("# selected replacement\r\n"))
	for _, phase := range []r.Phase{r.Original, r.Candidate, r.Saved} {
		var replacements []r.Replacement
		if phase != r.Original {
			replacements = []r.Replacement{{TargetID: "b-report", Source: replacement}}
		}
		mapView, err := mapOperation.NewView(phase, replacements)
		if err != nil {
			t.Fatal(err)
		}
		nativeView, err := nativeOperation.NewView(phase, replacements)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range []struct {
			destination string
			cap         r.Capability
			required    r.RequiredType
			existence   r.Existence
			treatment   r.Treatment
			content     string
		}{
			{"file.md", r.TextContent, r.RegularFile, r.File, r.Complete, "# file 雪\r\n"},
			{"empty.md", r.TextContent, r.RegularFile, r.File, r.Complete, ""},
			{"empty/", r.ExistenceOnly, r.DirectoryType, r.Directory, r.Complete, ""},
			{"missing.md", r.ExistenceOnly, r.RegularFile, r.Missing, r.MissingTarget, ""},
			{"file.md/child", r.ExistenceOnly, r.RegularFile, r.Missing, r.MissingTarget, ""},
			{"image.bin", r.ExistenceOnly, r.RegularFile, r.File, r.Complete, ""},
			{"a%252Fb.md", r.TextContent, r.RegularFile, r.File, r.Complete, "# percent\n"},
			{"雪.md", r.TextContent, r.RegularFile, r.File, r.Complete, "# Unicode\r\n"},
			{"guide.md", r.ExistenceOnly, r.RegularFile, r.Missing, r.MissingTarget, ""},
			{"b.txt", r.TextContent, r.RegularFile, r.File, r.Complete, "# selected original\n"},
			{"excluded.md", r.TextContent, r.RegularFile, r.File, r.Complete, "# excluded seed\r\n"},
			{"private/secret.md", r.TextContent, r.RegularFile, r.Unknown, r.ContextIncomplete, ""},
		} {
			t.Run(fmt.Sprintf("%s/%s", phase, row.destination), func(t *testing.T) {
				if row.destination == "b.txt" && phase != r.Original {
					row.content = "# selected replacement\r\n"
				}
				for _, adapter := range []struct {
					name string
					op   *r.Operation
					view r.View
					read func(context.Context, r.Request) (r.ObservationSpec, error)
				}{{"supplied", mapOperation, mapView, mapReader.Read}, {"native", nativeOperation, nativeView, nativeReader.Read}} {
					q := parityInventoryRequest(t, adapter.op, adapter.view, f.source, text, row.destination, row.cap, row.required)
					spec, err := adapter.read(context.Background(), q)
					if err != nil {
						t.Fatal(adapter.name, err)
					}
					o, err := adapter.op.Record(q, spec)
					if err != nil || o.Existence() != row.existence || o.Treatment() != row.treatment {
						t.Fatal(adapter.name, o, err, row)
					}
					content, available := o.Content()
					wantContent := row.cap == r.TextContent && row.existence == r.File
					if available != wantContent || available && string(content.Bytes()) != row.content {
						t.Fatal(adapter.name, "content parity", available, content, row)
					}
				}
			})
		}
	}
	for _, adapter := range []struct {
		name string
		op   *r.Operation
		read func(context.Context, r.Request) (r.ObservationSpec, error)
	}{{"supplied", mapOperation, mapReader.Read}, {"native", nativeOperation, nativeReader.Read}} {
		view, err := adapter.op.NewView(r.Saved, []r.Replacement{{TargetID: "b-report", Unavailable: true}})
		if err != nil {
			t.Fatal(err)
		}
		q := parityInventoryRequest(t, adapter.op, view, f.source, text, "b.txt", r.TextContent, r.RegularFile)
		spec, err := adapter.read(context.Background(), q)
		if err != nil {
			t.Fatal(adapter.name, err)
		}
		o, err := adapter.op.Record(q, spec)
		if err != nil || o.Existence() != r.Unknown || o.Treatment() != r.ContextIncomplete {
			t.Fatal(adapter.name, "unavailable saved target fell back", o, err)
		}
	}
}

func parityInventoryRequest(t *testing.T, operation *r.Operation, view r.View, source parser.Source, text, destination string, capability r.Capability, required r.RequiredType) r.Request {
	t.Helper()
	policy := r.NativeNamePolicy
	if operation.Root().Kind() == r.SuppliedRoot {
		policy = nil
	}
	normalizer, err := r.NewNormalizer(view, r.DefaultPathLimits(), policy)
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(text, destination)
	if start < 0 {
		t.Fatal("semantic destination absent from source fixture", destination)
	}
	// Exact semantic span remains source-owned; no adapter projects a native path.
	span, err := source.Span(start, start+len(destination))
	if err != nil {
		t.Fatal(err)
	}
	link, err := r.NewLink("docs/a.md", "MD057", destination, source, span)
	if err != nil {
		t.Fatal(err)
	}
	n, err := normalizer.Normalize(link)
	if err != nil || n.Class() != r.Local {
		t.Fatal("shared normalization", n, err)
	}
	key, ok := n.Location().Key()
	if !ok {
		t.Fatal("parity fixture did not name a resource")
	}
	q, err := view.Request(r.RequestSpec{Key: key, Link: link, Class: n.Class(), Capability: capability, Required: required})
	if err != nil {
		t.Fatal(err)
	}
	return q
}
