//go:build !linux && !darwin && !windows

package files

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

func TestUnsupportedHostRefusesAcquisition(t *testing.T) {
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	set, _, _, err := AcquireWrite(context.Background(), &Selection{}, WriteOptions{AcquireOptions: AcquireOptions{Budget: budget}})
	if err == nil || set != nil || !strings.Contains(err.Error(), "unsupported on "+runtime.GOOS) {
		t.Fatalf("unsupported host enabled apply: %v %v", set, err)
	}
}
