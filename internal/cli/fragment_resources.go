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
	mode    resource.RootMode
	// canonical is the already acquired canonical target of an inspection.
	canonical string
	targets   []fragmentTarget

	mu        sync.Mutex
	started   bool
	err       error
	root      *resource.FilesystemRoot
	service   *resource.Service
	operation *resource.Operation
	original  *fragmentView
}

// fragmentView supplies the resolver for one exact view of the shared operation:
// Original, a Candidate that replaces selected targets, or a Saved overlay. It
// implements engine.FragmentResources. Views share the operation's service, so
// resource bytes are acquired once and never re-read for a different view.
type fragmentView struct {
	session      *fragmentResources
	phase        resource.Phase
	replacements []resource.Replacement

	mu       sync.Mutex
	started  bool
	err      error
	resolver *resource.FragmentResolver
	view     resource.View
}

// Candidate returns the provider for analysis of path's candidate content. Other
// selected targets keep their original acquisition; the contract's single final
// overlay is not computed per round.
func (f *fragmentResources) Candidate(path string, source parser.Source) engine.FragmentResources {
	return &fragmentView{session: f, phase: resource.Candidate, replacements: []resource.Replacement{{TargetID: path, Source: source}}}
}

// Saved returns the provider for the observed saved overlay: replacements carry
// each target's confirmed saved bytes, or Unavailable when none was established.
func (f *fragmentResources) Saved(replacements []resource.Replacement) engine.FragmentResources {
	return &fragmentView{session: f, phase: resource.Saved, replacements: replacements}
}

// newFragmentResources prepares (but does not open) the shared session. The
// plan's document context fixes the parse options, anchor style and index file.
func newFragmentResources(ctx context.Context, budget *parser.OperationBudget, dc engine.DocumentContext, mode resource.RootMode, cwd, configPath, canonical string) *fragmentResources {
	f := &fragmentResources{ctx: ctx, budget: budget, context: dc, mode: mode, cwd: cwd, config: configPath, canonical: canonical}
	f.original = &fragmentView{session: f, phase: resource.Original}
	return f
}

func (f *fragmentResources) Fragments(ctx context.Context) (*resource.FragmentResolver, resource.View, error) {
	return f.original.Fragments(ctx)
}

func (f *fragmentResources) ensure() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.started {
		f.started = true
		f.err = f.open()
	}
	return f.err
}

func (v *fragmentView) Fragments(context.Context) (*resource.FragmentResolver, resource.View, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.started {
		v.started = true
		v.err = v.build()
	}
	if v.err != nil {
		return nil, resource.View{}, v.err
	}
	return v.resolver, v.view, nil
}

func (v *fragmentView) build() error {
	if err := v.session.ensure(); err != nil {
		return err
	}
	view, err := v.session.operation.NewView(v.phase, v.replacements)
	if err != nil {
		return err
	}
	normalizer, err := resource.NewNormalizer(view, resource.DefaultPathLimits(), resource.NativeNamePolicy)
	if err != nil {
		return err
	}
	resolver, err := resource.NewFragmentResolver(v.session.service, normalizer)
	if err != nil {
		return err
	}
	v.view, v.resolver = view, resolver
	return nil
}

func (f *fragmentResources) open() error {
	path, err := resource.SelectFilesystemRoot(f.ctx, f.budget, resource.RootSelectionOptions{Mode: f.mode, WorkingDirectory: f.cwd, CanonicalTarget: f.canonical, SelectedConfig: f.config})
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
	f.operation = operation
	f.service, err = resource.NewFilesystemService(reader, resource.DefaultServiceLimits())
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

// resourceRules reports whether a rule that consults the shared read-only
// resource session is enabled and available. MD051 and MD057 share one session.
func resourceRules(rules map[string]engine.ResolvedRule) bool {
	for _, id := range []string{"MD051", "MD057"} {
		if rule := rules[id]; rule.Enabled && rule.Available {
			return true
		}
	}
	return false
}

func interrupted(err error) bool {
	var limit *parser.LimitExceeded
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.As(err, &limit)
}
