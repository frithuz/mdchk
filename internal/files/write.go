package files

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/frithuz/mdchk/internal/parser"
)

// WriteOptions observes dependencies during the same bounded acquisition as the
// analyzed targets. Resources are read-only, Absences must not exist, and Required
// declares additional configuration/permission observations used by each target.
// All declared observations are checked again while holding cooperating locks.
type WriteOptions struct {
	AcquireOptions
	Resources *Selection
	Absences  []string
	Required  map[string][]string
}

// WriteSet owns native acquisition authority. Descriptive Documents and Sources
// alone cannot manufacture it. Close releases pinned descriptors; no acquisition
// creates filesystem artifacts. This prototype is not connected to CLI apply.
type WriteSet struct {
	mu     sync.Mutex
	native *nativeWriteSet
	budget *parser.OperationBudget
	origin context.Context
}

// Publication distinguishes an unchanged refusal from publication followed by an
// error. Published bytes are never rolled back. Saved is an observed snapshot,
// not a promise against subsequent noncooperating changes. Leftover names identify
// only temporary artifacts created by this operation.
type Publication struct {
	// NoChange reports equal retained inputs without confirming current saved bytes.
	// Saved is empty and no native final validation/publication is implied.
	NoChange  bool
	Path      string
	Published bool
	Uncertain bool
	Saved     parser.Source
	HardLinks uint64
	Leftovers []string
	Err       error
}

// AcquireWrite retains native identity and security metadata from the actual
// bounded read producing each Source. Unsupported adapters refuse explicitly.
func AcquireWrite(ctx context.Context, selected *Selection, options WriteOptions) (*WriteSet, *Acquisition, *Acquisition, error) {
	if ctx == nil || selected == nil || options.Budget == nil {
		return nil, nil, nil, errors.New("write acquisition requires context, selection and residual budget")
	}
	native, targets, resources, err := acquireNativeWrite(ctx, selected, options)
	if err != nil {
		return nil, targets, resources, err
	}
	return &WriteSet{native: native, budget: options.Budget, origin: ctx}, targets, resources, nil
}

// UsesBudget prevents a fresh budget from reviving an exhausted operation.
func (s *WriteSet) UsesBudget(b *parser.OperationBudget) bool {
	return s != nil && b != nil && s.budget == b
}

func (s *WriteSet) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.native == nil {
		return nil
	}
	err := s.native.close()
	s.native = nil
	return err
}

// PublishPrototype is the native primitive, not semantic correction approval.
// The fix.Validated adapter supplies retained, privately owned approved bytes;
// callers using this low-level primitive must independently establish approval.
// Original must be the exact acquisition Source, not merely equal bytes. Every
// dependency must be an acquisition observation. Resource paths cannot be targets.
func (s *WriteSet) PublishPrototype(ctx context.Context, path string, original, candidate parser.Source, dependencies []string) Publication {
	out := Publication{Path: path}
	if s == nil || ctx == nil {
		out.Err = errors.New("nil native publication context")
		return out
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.native == nil {
		out.Err = errors.New("closed write acquisition")
		return out
	}
	if err := s.origin.Err(); err != nil {
		out.Err = err
		return out
	}
	if err := ctx.Err(); err != nil {
		out.Err = err
		return out
	}
	if err := s.budget.ChargeWork(0); err != nil {
		out.Err = err
		return out
	}
	merged, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.origin, cancel)
	defer func() { stop(); cancel() }()
	return s.native.publish(publicationContext{Context: merged, origin: s.origin}, path, original, candidate, dependencies, s.budget)
}

func writeError(path, reason string) error {
	return fmt.Errorf("write refused at %s: %s", path, reason)
}

// MatchesObservation compares opaque acquisition ownership, not descriptive hashes.
// A successfully saved dependency retains its original acquisition binding while
// native final checks use this operation's observed saved content and identity.
func (s *WriteSet) MatchesObservation(path string, source parser.Source, absent bool) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.native != nil && s.native.matches(path, source, absent)
}

// Err synchronously checks origin as well as caller cancellation. AfterFunc
// wakes waits, while this direct check closes its scheduling window at commit.
type publicationContext struct {
	context.Context
	origin context.Context
}

func (c publicationContext) Err() error {
	if err := c.origin.Err(); err != nil {
		return err
	}
	return c.Context.Err()
}

// Checkpoint retains acquisition cancellation while semantic adapters traverse
// their manifests. It confirms no bytes; native final validation remains required.
func (s *WriteSet) Checkpoint(ctx context.Context) error {
	if s == nil || ctx == nil {
		return errors.New("nil native checkpoint")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.native == nil {
		return errors.New("closed write acquisition")
	}
	if err := s.origin.Err(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.budget.ChargeWork(0)
}
