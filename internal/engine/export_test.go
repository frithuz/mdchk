package engine

import (
	"testing"
	"time"
)

// SetReportingReserveTimeForTest overrides the reporting reserve's
// operation-time limit until t finishes.
func SetReportingReserveTimeForTest(t testing.TB, d time.Duration) {
	t.Helper()
	old := reportingReserveTime
	reportingReserveTime = d
	t.Cleanup(func() { reportingReserveTime = old })
}
