//go:build !linux && !darwin && !windows

package files

import (
	"context"
	"runtime"

	"github.com/frithuz/mdchk/internal/parser"
)

// nativeWriteSet exists so the portable write API compiles everywhere. No state
// is held: acquisition always refuses on these hosts.
type nativeWriteSet struct{}

func acquireNativeWrite(context.Context, *Selection, WriteOptions) (*nativeWriteSet, *Acquisition, *Acquisition, error) {
	return nil, nil, nil, unsupportedPlatformError(runtime.GOOS)
}
func (*nativeWriteSet) close() error { return nil }
func (*nativeWriteSet) publish(_ context.Context, path string, _, _ parser.Source, _ []string, _ *parser.OperationBudget) Publication {
	return Publication{Path: path, Err: writeError(path, unsupportedPlatformError(runtime.GOOS).Error())}
}
func (*nativeWriteSet) matches(string, parser.Source, bool) bool { return false }
