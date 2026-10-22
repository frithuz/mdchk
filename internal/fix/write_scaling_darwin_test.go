//go:build darwin && arm64 && cgo

package fix

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/frithuz/mdchk/internal/engine"
	"github.com/frithuz/mdchk/internal/files"
	"github.com/frithuz/mdchk/internal/parser"
)

// This isolates retained-publication accounting with an explicit private approved
// manifest over actual native acquisitions. Production semantic approval remains
// covered by TestRetainedProductionApprovalNativePublication.
func publicationScalingFixture(t *testing.T, n int) (Validated, *files.WriteSet, *parser.OperationBudget, []string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]string, n)
	for i := range paths {
		paths[i] = filepath.Join(root, fmt.Sprintf("%04d.md", i))
		if err := os.WriteFile(paths[i], []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	selected, err := files.Select(files.Options{Paths: paths})
	if err != nil {
		t.Fatal(err)
	}
	limits := parser.DefaultLimits()
	limits.TotalNodes = int64(n) * 400
	limits.DocumentNodes = min(50000, limits.TotalNodes)
	limits.WorkUnits = 10_000_000_000
	limits.OperationTime = 120 * time.Second
	budget, err := parser.NewOperationBudget(limits)
	if err != nil {
		t.Fatal(err)
	}
	native, a, _, err := files.AcquireWrite(context.Background(), selected, files.WriteOptions{AcquireOptions: files.AcquireOptions{Budget: budget}})
	if err != nil || len(a.Documents) != n {
		t.Fatalf("acquire %v %+v", err, a)
	}
	t.Cleanup(func() { _ = native.Close() })
	v := Validated{publicationBudget: budget, publicationIndex: map[string]int{}, publicationSources: map[string]ResourceObservation{}, publicationDependencies: map[string][]string{}}
	for i, d := range a.Documents {
		v.files = append(v.files, validatedFile{path: d.Path, original: d.Source, candidate: parser.NewSource([]byte("x\n")), trace: &engine.InspectionTrace{}})
		v.publicationIndex[d.Path] = i
		v.publicationSources[d.Path] = ResourceObservation{Key: d.Path, State: ResourceContent, Source: d.Source}
	}
	return v, native, budget, paths
}
func usedPublicationNodes(t *testing.T, b *parser.OperationBudget) int64 {
	t.Helper()
	err := b.ChargeNodes(b.Limits().TotalNodes)
	var exceeded *parser.LimitExceeded
	if !errors.As(err, &exceeded) {
		t.Fatalf("node accounting probe: %v", err)
	}
	return exceeded.Observed - exceeded.Allowed
}
func TestIndependentPublicationLinearAccounting(t *testing.T) {
	var perFile []int64
	for _, n := range []int{128, 256, 512} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			v, native, budget, paths := publicationScalingFixture(t, n)
			for i, path := range paths {
				out := v.Publish(context.Background(), native, path)
				if out.Err != nil || !out.Published {
					t.Fatalf("publication %d/%d: %+v", i, n, out)
				}
			}
			used := usedPublicationNodes(t, budget)
			t.Logf("%d publications: %d nodes, %.2f nodes/file", n, used, float64(used)/float64(n))
			perFile = append(perFile, used/int64(n))
			if used > int64(n)*350 {
				t.Fatalf("independent accounting is not linear: %d nodes for %d files", used, n)
			}
		})
	}
	for _, nodes := range perFile {
		if nodes > perFile[0]+2 {
			t.Fatalf("per-file node growth %v", perFile)
		}
	}
}
func TestPublicationReachableGraphAndStickyExhaustion(t *testing.T) {
	t.Run("coupled-closure", func(t *testing.T) {
		v, native, budget, paths := publicationScalingFixture(t, 16)
		for i, path := range paths {
			v.publicationDependencies[path] = []string{paths[(i+1)%len(paths)], paths[(i+2)%len(paths)]}
		}
		out := v.Publish(context.Background(), native, paths[0])
		if out.Err != nil || !out.Published {
			t.Fatalf("coupled closure %+v", out)
		}
		t.Logf("cyclic 16-target/32-edge closure: %d nodes including acquisition", usedPublicationNodes(t, budget))
	})
	t.Run("sticky-exhaustion", func(t *testing.T) {
		v, native, budget, paths := publicationScalingFixture(t, 2)
		if err := budget.ChargeNodes(budget.Limits().TotalNodes); err == nil {
			t.Fatal("expected exhausted budget")
		}
		out := v.Publish(context.Background(), native, paths[0])
		if out.Err == nil || out.Published || out.Saved != (parser.Source{}) {
			t.Fatalf("exhausted publication revived %+v", out)
		}
		if err := budget.ChargeWork(0); err == nil {
			t.Fatal("exhaustion was not sticky")
		}
	})
}
