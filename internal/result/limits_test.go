package result

import "testing"

func TestCanonicalLimitUnits(t *testing.T) {
	for _, tc := range []struct{ name, unit string }{{"document-bytes", "bytes"}, {"config-bytes", "bytes"}, {"total-config-bytes", "bytes"}, {"total-content-bytes", "bytes"}, {"path-bytes", "bytes"}, {"operation-time", "nanoseconds"}, {"syntax-depth", "nested constructs"}, {"structured-input-depth", "nested constructs"}, {"path-depth", "nested constructs"}, {"document-nodes", "nodes or source records"}, {"total-nodes", "nodes or source records"}, {"work-units", "metered work units"}, {"configuration-pattern-work", "metered work units"}} {
		if got := LimitUnit(tc.name); got != tc.unit {
			t.Errorf("%s = %s, want %s", tc.name, got, tc.unit)
		}
	}
}
