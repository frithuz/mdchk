package config

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/parser"
)

// DiscoveryBudget binds configuration discovery to one shared operation. Source
// and aggregate byte caps are independent of the Markdown document byte cap.
// Zero byte/depth fields select the fixed application defaults.
type DiscoveryBudget struct {
	Context                 context.Context
	Budget                  *parser.OperationBudget
	SourceBytes, TotalBytes int64
	StructuredDepth         int
}

func (b *DiscoveryBudget) charge(n int64) error {
	if err := b.Context.Err(); err != nil {
		return err
	}
	return b.Budget.ChargeWork(n)
}

// ResolveValidatedBounded uses the same selection, schema and ancestor policy
// semantics as ResolveValidated. It checks effects and semantic work against one
// budget, preserving typed interruption and acquired configuration identities.
func ResolveValidatedBounded(options SelectionOptions, registry *engine.Registry, providers engine.ValidationProviders, profile DiscoveryBudget) (*Selection, error) {
	if profile.Context == nil || profile.Budget == nil {
		return nil, errors.New("nil discovery context or budget")
	}
	if profile.SourceBytes == 0 {
		profile.SourceBytes = configBytes
	}
	if profile.TotalBytes == 0 {
		profile.TotalBytes = 8388608
	}
	if profile.StructuredDepth == 0 {
		profile.StructuredDepth = structuredDepth
	}
	if profile.SourceBytes < 1 || profile.SourceBytes > 1<<30 || profile.TotalBytes < profile.SourceBytes || profile.StructuredDepth < 1 || profile.StructuredDepth > 4096 {
		return nil, errors.New("unsupported discovery limits")
	}
	if err := profile.charge(0); err != nil {
		return nil, err
	}
	return resolveValidated(options, registry, providers, &profile)
}
func validateDefinitionBounded(d *Definition, registry *engine.Registry, providers engine.ValidationProviders, b *DiscoveryBudget) (*Validated, error) {
	if b != nil {
		var err error
		providers, err = reserveDefinition(b.Context, b.Budget, d, providers, 65536)
		if err != nil {
			return nil, err
		}
	}
	v, err := ValidateDefinition(d, registry, providers)
	if err == nil && b != nil {
		err = b.charge(0)
	}
	return v, err
}

type boundedDiscovery struct {
	access  *rootAccess
	options *DiscoveryBudget
	total   int64
}

func (a *boundedDiscovery) lstat(path string) (fs.FileInfo, error) {
	if err := a.options.charge(8*int64(len(path)) + 1); err != nil {
		return nil, err
	}
	return a.access.lstat(path)
}
func (a *boundedDiscovery) readRegular(path string) ([]byte, error) {
	b := a.options
	if err := b.charge(8*int64(len(path)) + 1); err != nil {
		return nil, err
	}
	parent, name, err := a.access.parent(path)
	if err != nil {
		return nil, err
	}
	defer closeReadHandle(parent)
	before, err := parent.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, errors.New("configuration must be a nonsymlink regular file")
	}
	if before.Size() > b.SourceBytes {
		return nil, &parser.LimitExceeded{Name: "config-bytes", Observed: before.Size(), Allowed: b.SourceBytes}
	}
	file, err := parent.OpenFile(name, os.O_RDONLY|configOpenFlags, 0)
	if err != nil {
		return nil, err
	}
	defer closeReadHandle(file)
	actual, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !actual.Mode().IsRegular() || !os.SameFile(before, actual) {
		return nil, errors.New("configuration changed during access")
	}
	var data []byte
	var scratch [32768]byte
	for {
		if err := b.charge(0); err != nil {
			return nil, err
		}
		size := min(int64(len(scratch)), b.SourceBytes-int64(len(data))+1, b.TotalBytes-a.total+1)
		n, readErr := file.Read(scratch[:size])
		a.total += int64(n)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return nil, readErr
		}
		if int64(len(data))+int64(n) > b.SourceBytes {
			return nil, &parser.LimitExceeded{Name: "config-bytes", Observed: int64(len(data)) + int64(n), Allowed: b.SourceBytes}
		}
		if a.total > b.TotalBytes {
			return nil, &parser.LimitExceeded{Name: "total-config-bytes", Observed: a.total, Allowed: b.TotalBytes}
		}
		if err := b.charge(3*int64(n) + int64(len(data)) + 1); err != nil {
			return nil, err
		}
		data = append(data, scratch[:n]...)
		if errors.Is(readErr, io.EOF) {
			break
		}
		if n == 0 {
			return nil, io.ErrNoProgress
		}
	}
	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	observed, err := parent.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !os.SameFile(before, after) || !os.SameFile(before, observed) || before.Size() != after.Size() || before.ModTime() != after.ModTime() || int64(len(data)) != after.Size() {
		return nil, errors.New("configuration changed during acquisition")
	}
	if err := b.charge(0); err != nil {
		return nil, err
	}
	return data, nil
}

// Sources returns same-read source identities in acquisition order.
func (s *Selection) Sources() []SourceSnapshot {
	if s == nil {
		return nil
	}
	return slices.Clone(s.sources)
}

// ExclusionOriginsBounded records each applicable definition without preventing
// explicit inspection. Only the selected definition supplies patterns, based at
// its defining directory; ancestor definitions supply permission denials only.
func (s *Selection) ExclusionOriginsBounded(ctx context.Context, budget *parser.OperationBudget, path string) ([]Origin, error) {
	if s == nil || s.validated == nil {
		return nil, errors.New("unresolved configuration selection")
	}
	var out []Origin
	for _, scope := range s.exclusions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := budget.ChargeWork(int64(len(path)+len(scope.Origin.Path))*8 + 1); err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(filepath.Dir(scope.Origin.Path), path)
		if err != nil {
			return nil, err
		}
		match, err := scope.Patterns.ExcludesBounded(ctx, budget, filepath.ToSlash(rel), false, inside(filepath.Dir(scope.Origin.Path), path))
		if err != nil {
			if errors.Is(err, ErrPatternLimit) {
				return nil, fmt.Errorf("%w: %w", err, &parser.LimitExceeded{Name: "configuration-pattern-work", Observed: MaxPatternMatchWork + 1, Allowed: MaxPatternMatchWork})
			}
			return nil, err
		}
		if match {
			out = append(out, cloneOrigin(scope.Origin))
		}
	}
	return out, nil
}

func permissionWork(p Permissions) int64 {
	var work int64
	add := func(origins []Origin) {
		for _, o := range origins {
			work += 16 * int64(len(o.Path)+len(o.PropertyPath)+16)
		}
	}
	add(p.Directives)
	add(p.NestedExcludes)
	for id, origins := range p.RuleDirectives {
		work += int64(len(id))
		add(origins)
	}
	return work
}
