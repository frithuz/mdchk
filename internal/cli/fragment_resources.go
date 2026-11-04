package cli

import (
	"context"
	"errors"
	"sync"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/resource"
)

// parserRevision names the parser/index behavior retained by resource cache keys.
const parserRevision = "mdchk-fragments-1"

type fragmentTarget struct {
	id, path string
	source   parser.Source
}

// fragmentResources lazily builds the read-only resource context for one lint
// invocation. Nothing is opened or bound until a rule needs a fragment decision,
// so documents without cross-document fragment links pay no filesystem cost.
// Selected targets are exactly the acquired inputs; the native root is selected
// from the working directory and the already selected configuration, and every
// read uses the strict no-follow, bounded adapter in internal/resource.
type fragmentResources struct {
	ctx     context.Context
	budget  *parser.OperationBudget
	context engine.DocumentContext
	cwd     string
	config  string
	targets []fragmentTarget

	mu       sync.Mutex
	started  bool
	err      error
	root     *resource.FilesystemRoot
	service  *resource.Service
	resolver *resource.FragmentResolver
	view     resource.View
}

func (f *fragmentResources) Fragments(ctx context.Context) (*resource.FragmentResolver, resource.View, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.started {
		f.started = true
		f.err = f.open()
	}
	if f.err != nil {
		return nil, resource.View{}, f.err
	}
	return f.resolver, f.view, nil
}

func (f *fragmentResources) open() error {
	path, err := resource.SelectFilesystemRoot(f.ctx, f.budget, resource.RootSelectionOptions{Mode: resource.RootCLI, WorkingDirectory: f.cwd, SelectedConfig: f.config})
	if err != nil {
		return err
	}
	root, err := resource.OpenFilesystemRoot(f.ctx, f.budget, path, resource.DefaultFilesystemLimits())
	if err != nil {
		return err
	}
	f.root = root
	bound := make([]resource.FilesystemTarget, 0, len(f.targets))
	targets := make([]resource.Target, 0, len(f.targets))
	for _, target := range f.targets {
		receipt, err := root.BindTarget(target.path, target.id, target.source)
		if err != nil && (receipt.Target().ID == "" || interrupted(err)) {
			return err
		}
		bound = append(bound, receipt)
		targets = append(targets, receipt.Target())
	}
	settings, err := resource.NewSettings(f.context.ParseOptions(), f.context.IndexFile, f.context.AnchorStyle, parserRevision)
	if err != nil {
		return err
	}
	operation, err := resource.NewOperation(f.ctx, f.budget, root.Root(), settings, targets)
	if err != nil {
		return err
	}
	reader, err := root.Reader(operation, bound...)
	if err != nil {
		return err
	}
	f.service, err = resource.NewFilesystemService(reader, resource.DefaultServiceLimits())
	if err != nil {
		return err
	}
	f.view, err = operation.NewView(resource.Original, nil)
	if err != nil {
		return err
	}
	normalizer, err := resource.NewNormalizer(f.view, resource.DefaultPathLimits(), resource.NativeNamePolicy)
	if err != nil {
		return err
	}
	f.resolver, err = resource.NewFragmentResolver(f.service, normalizer)
	return err
}

// Close joins the service worker before releasing the native root.
func (f *fragmentResources) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	var err error
	if f.service != nil {
		err = f.service.Close()
	}
	if f.root != nil {
		err = errors.Join(err, f.root.Close())
	}
	f.service, f.root = nil, nil
	return err
}

func interrupted(err error) bool {
	var limit *parser.LimitExceeded
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.As(err, &limit)
}
