//go:build !darwin || !arm64 || !cgo

package files

import (
	"context"
	"github.com/frithuz/mdchk/internal/parser"
	"strings"
	"testing"
)

func TestUnverifiedNativeWriteAdapterRefuses(t *testing.T) {
	budget, _ := parser.NewOperationBudget(parser.DefaultLimits())
	set, _, _, err := AcquireWrite(context.Background(), &Selection{}, WriteOptions{AcquireOptions: AcquireOptions{Budget: budget}})
	if err == nil || set != nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unverified platform enabled %v %v", set, err)
	}
}
