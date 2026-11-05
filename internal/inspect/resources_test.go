package inspect

import (
	"context"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/resource"
)

// nativeResources is a minimal native provider over a fixture root, so inactive
// MD057 observation can resolve link targets the fixture creates.
type nativeResources struct {
	resolver *resource.FragmentResolver
	view     resource.View
}

func (r *nativeResources) Fragments(context.Context) (*resource.FragmentResolver, resource.View, error) {
	return r.resolver, r.view, nil
}

func newNativeResources(t *testing.T, root string, snapshot *Snapshot) *nativeResources {
	t.Helper()
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	ctx := context.Background()
	native, err := resource.OpenFilesystemRoot(ctx, budget, root, resource.DefaultFilesystemLimits())
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := native.BindTarget(snapshot.File().Path, snapshot.File().Path, snapshot.Source())
	if err != nil {
		t.Fatal(err)
	}
	settings, _ := resource.NewSettings(parser.ParseOptions{}, "index.md", "github", "test-1")
	operation, err := resource.NewOperation(ctx, budget, native.Root(), settings, []resource.Target{receipt.Target()})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := native.Reader(operation, receipt)
	if err != nil {
		t.Fatal(err)
	}
	service, err := resource.NewFilesystemService(reader, resource.DefaultServiceLimits())
	if err != nil {
		t.Fatal(err)
	}
	view, err := operation.NewView(resource.Original, nil)
	if err != nil {
		t.Fatal(err)
	}
	normalizer, err := resource.NewNormalizer(view, resource.DefaultPathLimits(), resource.NativeNamePolicy)
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := resource.NewFragmentResolver(service, normalizer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close(); _ = native.Close() })
	return &nativeResources{resolver, view}
}
