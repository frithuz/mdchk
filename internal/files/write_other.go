//go:build !darwin || !arm64 || !cgo

package files

import (
	"context"
	"errors"
	"github.com/frithuz/mdchk/internal/parser"
)

type nativeWriteSet struct{}

func acquireNativeWrite(context.Context, *Selection, WriteOptions) (*nativeWriteSet, *Acquisition, *Acquisition, error) {
	return nil, nil, nil, errors.New("native replacement requires verified Darwin arm64 APFS and cgo; this adapter is unsupported")
}
func (*nativeWriteSet) close() error { return nil }
func (*nativeWriteSet) publish(_ context.Context, path string, _, _ parser.Source, _ []string, _ *parser.OperationBudget) Publication {
	return Publication{Path: path, Err: writeError(path, "unsupported native adapter")}
}

func (*nativeWriteSet) matches(string, parser.Source, bool) bool { return false }
