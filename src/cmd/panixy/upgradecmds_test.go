package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/deadship2003/panixy/internal/paths"
)

// TestPruneCoreBackups 回归测试:曾因 matches[keep:] 在备份数 < keep 时
// panic(slice bounds out of range [3:1]),这里覆盖边界与正常裁剪。
func TestPruneCoreBackups(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "mihomo")
	p := paths.Paths{Bin: bin}

	// 少于 keep 份:不得 panic,也不得删除
	os.WriteFile(bin+".bak-v1.0.0", []byte("x"), 0o644)
	pruneCoreBackups(p, 3)
	if _, err := os.Stat(bin + ".bak-v1.0.0"); err != nil {
		t.Fatalf("少于 keep 份时不应删除: %v", err)
	}

	// 多于 keep 份:保留最新 keep 份,删最旧(倒序排序)
	for _, v := range []string{"v1.0.1", "v1.0.2", "v1.0.3", "v1.0.4"} {
		os.WriteFile(bin+".bak-"+v, []byte("x"), 0o644)
	}
	pruneCoreBackups(p, 3)
	for _, keep := range []string{"v1.0.4", "v1.0.3", "v1.0.2"} {
		if _, err := os.Stat(bin + ".bak-" + keep); err != nil {
			t.Errorf("应保留最新备份 %s: %v", keep, err)
		}
	}
	for _, del := range []string{"v1.0.1", "v1.0.0"} {
		if _, err := os.Stat(bin + ".bak-" + del); err == nil {
			t.Errorf("应删除最旧备份 %s", del)
		}
	}
}
