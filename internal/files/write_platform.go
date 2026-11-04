package files

import (
	"errors"
	"fmt"
)

// Refusals that do not depend on the host. Native replacement itself is
// implemented once, for Linux, macOS and Windows, in write_native.go.
var (
	errOwnerUnavailable = errors.New("file owner is unavailable")
	errXattrBound       = errors.New("extended attributes exceed the supported bound")
)

// unsupportedPlatformError is the explicit refusal for hosts outside linux,
// darwin and windows. It is a function of GOOS so tests can exercise it anywhere.
func unsupportedPlatformError(goos string) error {
	return fmt.Errorf("native replacement is unsupported on %s; apply supports linux, darwin and windows", goos)
}
