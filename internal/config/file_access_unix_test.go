//go:build unix

package config

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestDiscoveryRejectsSpecialFilesWithoutOpening(t *testing.T) {
	root := discoveryRoot(t)
	discoveryDir(t, root, ".git")
	fifo := filepath.Join(root, Filename)
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	_ = discoveryReject(t, SelectionOptions{WorkingDirectory: root})
	_ = discoveryReject(t, SelectionOptions{WorkingDirectory: root, ExplicitPath: fifo})
	if d, err := Load(fifo); d != nil || err == nil {
		t.Fatalf("FIFO Load = %+v, %v", d, err)
	}
}
func TestDiscoveryBoundsFileReadBeforeDecoding(t *testing.T) {
	root := discoveryRoot(t)
	discoveryDir(t, root, ".git")
	path := filepath.Join(root, Filename)
	if err := os.WriteFile(path, []byte(strings.Repeat("#", configBytes+1)), 0600); err != nil {
		t.Fatal(err)
	}
	_ = discoveryReject(t, SelectionOptions{WorkingDirectory: root})
	if d, err := Load(path); d != nil || err == nil {
		t.Fatalf("oversized Load = %+v, %v", d, err)
	}
}
