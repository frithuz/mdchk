package resource

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestProjectSourceExactViewAndForeignAuthority(t *testing.T) {
	f := newServiceFixture(t, context.Background(), `{"complete":true,"entries":[{"path":"mkdocs.yml","kind":"file","content":"nav: []"},{"path":"docs/page.md","kind":"file"}]}`, DefaultServiceLimits())
	key, _ := NewKey("source.md")
	original, spec, e := f.service.AcquireSource(context.Background(), f.view, key)
	if e != nil || spec.Source.Token() != f.source.Token() {
		t.Fatal(spec, e)
	}
	replacement := parser.NewSource([]byte("nav: [candidate.md]"))
	candidate, e := f.op.NewView(Candidate, []Replacement{{TargetID: "source.md", Source: replacement}})
	if e != nil {
		t.Fatal(e)
	}
	changed, spec, e := f.service.AcquireSource(context.Background(), candidate, key)
	if e != nil || changed.Source().Token() != replacement.Token() || spec.Source.Token() == original.Source().Token() {
		t.Fatal("candidate borrowed original", spec, e)
	}
	saved, e := f.op.NewView(Saved, []Replacement{{TargetID: "source.md", Unavailable: true}})
	if e != nil {
		t.Fatal(e)
	}
	unavailable, spec, e := f.service.AcquireSource(context.Background(), saved, key)
	if e != nil || unavailable.Source().Token() != (parser.SourceToken{}) || spec.Content == Available {
		t.Fatal("unavailable Saved borrowed original", spec, e)
	}
	configKey, _ := NewKey("mkdocs.yml")
	config, _, e := f.service.AcquireSource(context.Background(), saved, configKey)
	if e != nil {
		t.Fatal(e)
	}
	root, e := config.DirectoryLocation(context.Background(), "")
	if e != nil {
		t.Fatal(e)
	}
	inventory, e := config.Enumerate(context.Background(), root)
	if e != nil || inventory.Complete() {
		t.Fatal("unavailable saved minted complete membership", e)
	}
	for name := range projectNames(inventory) {
		if name == "source.md" {
			t.Fatal("unavailable saved borrowed name membership")
		}
	}
	other := newServiceFixture(t, context.Background(), `{"complete":true,"entries":[]}`, DefaultServiceLimits())
	if _, _, e := f.service.AcquireSource(context.Background(), other.view, key); !errors.Is(e, ErrContract) {
		t.Fatal("foreign view accepted", e)
	}
	foreign, _, e := other.service.AcquireSource(context.Background(), other.view, key)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := inventory.Facts(context.Background(), foreign); !errors.Is(e, ErrContract) {
		t.Fatal("foreign source borrowed inventory", e)
	}
	span, _ := parser.NewSource(config.Source().Bytes()).Span(0, 1)
	if _, e := config.Resolve(context.Background(), span, root, "docs/page.md", false); !errors.Is(e, ErrContract) {
		t.Fatal("equal bytes forged source span", e)
	}
}
func projectNames(inventory NameInventory) map[string]Existence {
	out := map[string]Existence{}
	for n := 0; n < inventory.KnownCount(); n++ {
		fact, _ := inventory.KnownFact(n)
		out[fact.Key().String()] = fact.Existence()
	}
	return out
}

func TestProjectSourceSuppliedNoFallbackAndRootInventory(t *testing.T) {
	for _, complete := range []bool{false, true} {
		raw := `{"complete":false,"entries":[{"path":"mkdocs.yml","kind":"file","content":"nav: []"},{"path":"docs/page.md","kind":"file"},{"path":"docs/unknown","kind":"unavailable"}]}`
		if complete {
			raw = `{"complete":true,"entries":[{"path":"mkdocs.yml","kind":"file","content":"nav: []"},{"path":"docs/page.md","kind":"file"}]}`
		}
		f := newServiceFixture(t, context.Background(), raw, DefaultServiceLimits())
		key, _ := NewKey("mkdocs.yml")
		config, _, e := f.service.AcquireSource(context.Background(), f.view, key)
		if e != nil {
			t.Fatal(e)
		}
		directory, e := config.DirectoryLocation(context.Background(), "docs")
		if e != nil {
			t.Fatal(e)
		}
		inventory, e := config.Enumerate(context.Background(), directory)
		if e != nil {
			t.Fatal(e)
		}
		if inventory.Complete() != complete {
			t.Fatal("supplied completeness", complete, inventory.Complete())
		}
		expected := map[string]Existence{"docs/page.md": File}
		if !reflect.DeepEqual(projectNames(inventory), expected) {
			t.Fatal(projectNames(inventory))
		}
		// Names are exact and independent of real host files or working directory.
		location, e := config.DirectoryLocation(context.Background(), "go.mod")
		if e != nil {
			t.Fatal(e)
		}
		spec, e := config.Inspect(context.Background(), location, RegularFile)
		if e != nil || complete && spec.Existence != Missing || !complete && spec.Existence != Unknown {
			t.Fatal("host fallback", spec, e)
		}
	}
}
