package systemdunit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/deadship2003/panoxy/internal/paths"
)

func TestInstalled(t *testing.T) {
	dir := t.TempDir()
	p := paths.Paths{UnitDir: dir}
	if Installed(p) {
		t.Fatal("no unit written yet, Installed should be false")
	}
	if err := os.WriteFile(filepath.Join(dir, unitMain), []byte("[Unit]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !Installed(p) {
		t.Fatal("main unit written, Installed should be true")
	}
}
