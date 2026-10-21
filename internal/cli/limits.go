package cli

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// These defaults are the accepted application profile, not an execution engine.
// Values are bytes, counts, or nanoseconds. Keep aligned with limits.json.
var limitDefaults = map[string]int64{
	"document-bytes": 1048576, "total-content-bytes": 8388608, "request-bytes": 16777216, "response-bytes": 8388608,
	"target-files": 5000, "resource-identities": 25000, "resource-lookups": 25000, "directory-entries": 100000,
	"path-bytes": 4096, "path-depth": 64, "config-bytes": 1048576, "total-config-bytes": 8388608,
	"structured-input-depth": 64, "syntax-depth": 256, "document-nodes": 250000, "total-nodes": 2000000,
	"work-units": 250000000, "snapshot-retries": 2, "operation-time": int64(60 * time.Second), "reporting-time": int64(2 * time.Second),
	"report-reserve-bytes": 65536, "active-calls": 4, "queued-calls": 8, "queue-time": int64(10 * time.Second),
	"project-fixes": 1, "workers-per-operation": 1, "fix-rounds": 10,
}

func parseLimit(raw string) (string, int64, error) {
	name, value, ok := strings.Cut(raw, "=")
	if !ok || value == "" {
		return "", 0, fmt.Errorf("limit must be NAME=VALUE: %q", raw)
	}
	if _, ok := limitDefaults[name]; !ok {
		return "", 0, fmt.Errorf("unknown limit %q", name)
	}
	var n int64
	var err error
	switch {
	case strings.HasSuffix(name, "-time"):
		var duration time.Duration
		duration, err = time.ParseDuration(value)
		n = int64(duration)
	case strings.HasSuffix(name, "-bytes"):
		factor := int64(1)
		for _, unit := range []struct {
			suffix string
			factor int64
		}{{"KiB", 1024}, {"MiB", 1048576}, {"GiB", 1073741824}, {"B", 1}} {
			if strings.HasSuffix(value, unit.suffix) {
				value = strings.TrimSuffix(value, unit.suffix)
				factor = unit.factor
				break
			}
		}
		n, err = decimal(value)
		if err == nil {
			if n > math.MaxInt64/factor {
				err = fmt.Errorf("byte size overflows")
			} else {
				n *= factor
			}
		}
	default:
		n, err = decimal(value)
	}
	if err != nil || n <= 0 {
		return "", 0, fmt.Errorf("limit %q requires a positive finite value", name)
	}
	if name == "fix-rounds" && n > 10 {
		return "", 0, fmt.Errorf("fix-rounds cannot exceed 10")
	}
	return name, n, nil
}
func decimal(value string) (int64, error) {
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("expected decimal integer")
		}
	}
	return strconv.ParseInt(value, 10, 64)
}
func validateLimits(overrides map[string]int64) error {
	effective := func(name string) int64 {
		if n, ok := overrides[name]; ok {
			return n
		}
		return limitDefaults[name]
	}
	for _, pair := range [][2]string{{"document-bytes", "total-content-bytes"}, {"config-bytes", "total-config-bytes"}, {"queue-time", "operation-time"}, {"report-reserve-bytes", "response-bytes"}} {
		if effective(pair[0]) > effective(pair[1]) {
			return fmt.Errorf("limit %s must not exceed %s", pair[0], pair[1])
		}
	}
	if effective("report-reserve-bytes") < 65536 {
		return fmt.Errorf("report-reserve-bytes must be at least 65536")
	}
	return nil
}
