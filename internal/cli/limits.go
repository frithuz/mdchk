package cli

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/frithuz/mdchk/internal/parser"
	"github.com/frithuz/mdchk/internal/report"
)

// newOperationBudget scales the work and node allowances with acquired content
// so a large project is not refused by a fixed operation-wide allowance. The
// configured value stays the floor. A limit the operator set explicitly stays a
// hard cap.
func newOperationBudget(profile parser.Limits, overrides map[string]int64) (*parser.OperationBudget, error) {
	budget, err := parser.NewOperationBudget(profile)
	if err != nil {
		return nil, err
	}
	var work, nodes int64 = parser.ContentWorkPerByte, parser.ContentNodesPerByte
	if _, pinned := overrides["work-units"]; pinned {
		work = 0
	}
	if _, pinned := overrides["total-nodes"]; pinned {
		nodes = 0
	}
	budget.ScaleWithContent(work, nodes)
	return budget, nil
}

func parseLimit(raw string) (string, int64, error) {
	name, value, ok := strings.Cut(raw, "=")
	if !ok || value == "" {
		return "", 0, fmt.Errorf("limit must be NAME=VALUE: %q", raw)
	}
	if _, ok := report.LimitDefaults[name]; !ok {
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
		return report.LimitDefaults[name]
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
