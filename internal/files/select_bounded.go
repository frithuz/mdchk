package files

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/config"
	"io"
	"io/fs"
	"os"

	"github.com/frithuz/mdchk/internal/parser"
)

// SelectBounded retains Select's selection semantics while charging discovery
// effects and returned directory entries to the caller's existing operation.
func SelectBounded(ctx context.Context, options Options, budget *parser.OperationBudget) (*Selection, error) {
	if ctx == nil || budget == nil {
		return nil, errors.New("nil selection context or budget")
	}
	return selectFS(options, boundedSelectionFS{ctx, budget})
}

type boundedSelectionFS struct {
	ctx    context.Context
	budget *parser.OperationBudget
}

func (b boundedSelectionFS) charge(n int64) error {
	if err := b.ctx.Err(); err != nil {
		return err
	}
	return b.budget.ChargeWork(n)
}
func (b boundedSelectionFS) lstat(path string) (fs.FileInfo, error) {
	if err := b.charge(8*int64(len(path)) + 1); err != nil {
		return nil, err
	}
	return hostFS{}.lstat(path)
}
func (b boundedSelectionFS) readDir(path string) ([]fs.DirEntry, error) {
	if err := b.charge(8*int64(len(path)) + 1); err != nil {
		return nil, err
	}
	//nolint:gosec // G304: read-only enumeration of an operator-selected directory; acquisition separately revalidates target component identities.
	directory, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	entries, readErr := b.readEntries(directory.ReadDir)
	closeErr := directory.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return entries, nil
}

// A one-entry batch is admitted before ReadDir constructs its returned slice.
// Directory handles use a fixed-size native read buffer, not os.ReadDir's whole
// directory collection/sort. EOF can consume one unused reservation; no work is
// refunded. Admitted entries alone reach the selector's deterministic sorting.
func (b boundedSelectionFS) readEntries(read func(int) ([]fs.DirEntry, error)) ([]fs.DirEntry, error) {
	entries := []fs.DirEntry{}
	for {
		if err := b.charge(1); err != nil {
			return nil, err
		}
		if err := b.budget.ChargeNodes(1); err != nil {
			return nil, err
		}
		batch, err := read(1)
		if len(batch) > 1 {
			return nil, errors.New("directory enumerator exceeded admitted batch")
		}
		for _, entry := range batch {
			if chargeErr := b.charge(8*int64(len(entry.Name())) + 1); chargeErr != nil {
				return nil, chargeErr
			}
			entries = append(entries, entry)
		}
		if errors.Is(err, io.EOF) {
			return entries, nil
		}
		if err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			return nil, io.ErrNoProgress
		}
	}
}
func (b boundedSelectionFS) excludes(base, target string, isDir bool, patterns *config.Patterns) (bool, error) {
	if patterns == nil {
		patterns = &config.Patterns{}
	}
	return excludesMatching(base, target, isDir, func(name string, directory, inBase bool) (bool, error) {
		matched, err := patterns.ExcludesBounded(b.ctx, b.budget, name, directory, inBase)
		if errors.Is(err, config.ErrPatternLimit) {
			err = errors.Join(err, &parser.LimitExceeded{Name: "configuration-pattern-work", Observed: config.MaxPatternMatchWork + 1, Allowed: config.MaxPatternMatchWork})
		}
		return matched, err
	})
}
