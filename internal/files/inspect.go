package files

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// ErrNotRegularTarget identifies directory and other nonregular inspection arguments.
var ErrNotRegularTarget = errors.New("inspection target is not a regular file")

// AcquireExplicit resolves one explicitly named inspection target, including
// symlinks, and reuses the regular-file pinned acquisition path. It does not
// select directories, consult exclusions, or change Select/MCP behavior. A raced
// identity or changing read is an error; no retries or mixed snapshots occur.
func AcquireExplicit(ctx context.Context, path string, options AcquireOptions) (*Acquisition, error) {
	return acquireExplicit(ctx, path, options, nil)
}
func acquireExplicit(ctx context.Context, path string, options AcquireOptions, selected func(Target)) (*Acquisition, error) {
	if ctx == nil {
		return nil, errors.New("nil acquisition context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if options.Budget != nil {
		if err := options.Budget.ChargeWork(65536 + 8*int64(len(path))); err != nil {
			return nil, err
		}
	}
	// Resolve the raw components before Abs can erase symlink/.. traversal.
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	canonical, err := filepath.Abs(resolved)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(canonical)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, &fs.PathError{Op: "stat", Path: canonical, Err: ErrNotRegularTarget}
	}
	if options.Budget != nil {
		var count int64 = 1
		for parent := canonical; filepath.Dir(parent) != parent; parent = filepath.Dir(parent) {
			count++
		}
		if err := options.Budget.ChargeWork(8*int64(len(canonical))*count + 1); err != nil {
			return nil, err
		}
	}
	s := selector{access: hostFS{}}
	identities, err := s.identities(canonical, info)
	if err != nil {
		return nil, err
	}
	target := Target{Path: canonical, identities: identities, inspection: true}
	if selected != nil {
		selected(target)
	}
	return Acquire(ctx, &Selection{Targets: []Target{target}}, options)
}
