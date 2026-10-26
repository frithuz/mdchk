//go:build linux || darwin || windows

package resource_test

import (
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/resource"
	"testing"
)

// Keep real engine finalization at the outer integration boundary; the private
// resource experiments and every original assertion remain in inner test helpers.
func TestFilesystemFragmentNoKeySelfCandidateAndStoppedQueryReport(t *testing.T) {
	resource.CheckFilesystemFragmentNoKeySelfCandidateAndStoppedQueryReport(t, engine.FinalizeLintResult)
}
