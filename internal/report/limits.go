package report

import "time"

// LimitDefaults are the accepted application profile, not an execution engine.
// Values are bytes, counts, or nanoseconds. Keep aligned with limits.json.
var LimitDefaults = map[string]int64{
	"document-bytes": 1048576, "total-content-bytes": 8388608, "request-bytes": 16777216, "response-bytes": 8388608,
	"target-files": 5000, "resource-identities": 25000, "resource-lookups": 25000, "directory-entries": 100000,
	"path-bytes": 4096, "path-depth": 64, "config-bytes": 1048576, "total-config-bytes": 8388608,
	"structured-input-depth": 64, "syntax-depth": 256, "document-nodes": 250000, "total-nodes": 2000000,
	"work-units": 250000000, "snapshot-retries": 2, "operation-time": int64(60 * time.Second), "reporting-time": int64(2 * time.Second),
	"report-reserve-bytes": 65536, "active-calls": 4, "queued-calls": 8, "queue-time": int64(10 * time.Second),
	"project-fixes": 1, "workers-per-operation": 1, "fix-rounds": 10,
}

var ValidationLimitNames = []string{"config-bytes", "total-config-bytes", "directory-entries", "path-bytes", "path-depth", "structured-input-depth", "syntax-depth", "document-nodes", "total-nodes", "work-units", "operation-time", "response-bytes", "reporting-time", "report-reserve-bytes"}

func ValidationLimitApplicable(name string) bool {
	for _, n := range ValidationLimitNames {
		if name == n {
			return true
		}
	}
	return false
}
