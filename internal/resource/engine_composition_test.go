package resource_test

import (
	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/resource"
	"testing"
)

// Keep real engine finalization at the outer integration boundary; the private
// resource experiments and every original assertion remain in inner test helpers.
func TestFragmentFinalIndexCauseIdentityAcrossViews(t *testing.T) {
	resource.CheckFragmentFinalIndexCauseIdentityAcrossViews(t, engine.FinalizeLintResult)
}
func TestFragmentEarlierDirectoryCauseRemainsIndependent(t *testing.T) {
	resource.CheckFragmentEarlierDirectoryCauseRemainsIndependent(t, engine.FinalizeLintResult)
}
func TestFragmentDirectoryLookupQuotaKeepsFinalCauseIdentity(t *testing.T) {
	resource.CheckFragmentDirectoryLookupQuotaKeepsFinalCauseIdentity(t, engine.FinalizeLintResult)
}
func TestServiceReturnedErrorMutationCannotChangeCachedCause(t *testing.T) {
	resource.CheckServiceReturnedErrorMutationCannotChangeCachedCause(t, engine.FinalizeLintResult)
}
func TestServiceReturnedMetadataCannotEraseCanonicalStops(t *testing.T) {
	resource.CheckServiceReturnedMetadataCannotEraseCanonicalStops(t, engine.FinalizeLintResult)
}
func TestServiceCombinedCanonicalStopsAreAllProjected(t *testing.T) {
	resource.CheckServiceCombinedCanonicalStopsAreAllProjected(t, engine.FinalizeLintResult)
}
