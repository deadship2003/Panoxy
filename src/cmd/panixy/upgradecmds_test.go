package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/deadship2003/Panoxy/internal/paths"
)

// TestPruneCoreBackups regression test: matches[keep:] once panicked when the backup count was below keep
// (slice bounds out of range [3:1]); this covers the boundary and normal pruning.
func TestPruneCoreBackups(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "mihomo")
	p := paths.Paths{Bin: bin}

	// Fewer than keep backups: must not panic, and must not delete.
	os.WriteFile(bin+".bak-v1.0.0", []byte("x"), 0o644)
	pruneCoreBackups(p, 3)
	if _, err := os.Stat(bin + ".bak-v1.0.0"); err != nil {
		t.Fatalf("should not delete when below keep: %v", err)
	}

	// More than keep backups: keep the newest keep, delete the oldest (reverse-sorted).
	for _, v := range []string{"v1.0.1", "v1.0.2", "v1.0.3", "v1.0.4"} {
		os.WriteFile(bin+".bak-"+v, []byte("x"), 0o644)
	}
	pruneCoreBackups(p, 3)
	for _, keep := range []string{"v1.0.4", "v1.0.3", "v1.0.2"} {
		if _, err := os.Stat(bin + ".bak-" + keep); err != nil {
			t.Errorf("should keep the newest backup %s: %v", keep, err)
		}
	}
	for _, del := range []string{"v1.0.1", "v1.0.0"} {
		if _, err := os.Stat(bin + ".bak-" + del); err == nil {
			t.Errorf("should delete the oldest backup %s", del)
		}
	}
}
