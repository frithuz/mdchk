//go:build windows

package files

import (
	"context"
	"os"
	"testing"

	"github.com/frithuz/mdchk/internal/parser"
)

// A target held open without share-delete cannot be replaced. MoveFileEx fails,
// the refusal is a normal I/O error, the original is untouched and the stage is
// removed. os.Open shares read and write but not delete.
func TestNativeHeldOpenTargetRefusesWithoutMutation(t *testing.T) {
	root, path := nativeFixture(t)
	set, a := nativeAcquire(t, []string{path}, WriteOptions{})
	held, err := os.Open(path) //nolint:gosec // Synthetic fixture path created by the test.
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Close() }()
	out := set.Publish(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("candidate\n")), nil)
	if out.Err == nil || out.Published || out.Uncertain {
		t.Fatalf("held-open target replaced %+v", out)
	}
	assertNativeBytes(t, path, a.Documents[0].Source.Bytes())
	assertNoStage(t, root)
}

// Alternate data streams, including Mark-of-the-Web, live outside the primary
// stream and are not copied. This pins the accepted limit.
func TestNativeAlternateDataStreamsAreDropped(t *testing.T) {
	_, path := nativeFixture(t)
	if err := os.WriteFile(path+":Zone.Identifier", []byte("[ZoneTransfer]\r\nZoneId=3\r\n"), 0600); err != nil {
		t.Skipf("filesystem has no alternate data streams: %v", err)
	}
	set, a := nativeAcquire(t, []string{path}, WriteOptions{})
	out := set.Publish(context.Background(), path, a.Documents[0].Source, parser.NewSource([]byte("candidate\n")), nil)
	if out.Err != nil || !out.Published {
		t.Fatalf("publication %+v", out)
	}
	assertNativeBytes(t, path, []byte("candidate\n"))
	if _, err := os.ReadFile(path + ":Zone.Identifier"); err == nil {
		t.Fatal("alternate data stream survived replacement; update native-writes.md")
	}
}
