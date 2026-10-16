package hostid

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateGeneratesAndSticks(t *testing.T) {
	dir := t.TempDir()
	id1, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("首次 LoadOrCreate: %v", err)
	}
	if len(id1) != 32 || !isLowerHex(id1) {
		t.Fatalf("身份格式不符: %q", id1)
	}
	info, err := os.Stat(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatalf("身份文件未落盘: %v", err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("身份文件权限 %04o 宽于 0600", perm)
	}
	id2, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("二次 LoadOrCreate: %v", err)
	}
	if id1 != id2 {
		t.Errorf("身份不稳定: %q vs %q", id1, id2)
	}
}

func isLowerHex(s string) bool {
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func TestLoadOrCreateRegeneratesCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte("zz"), 0o600); err != nil {
		t.Fatalf("写坏文件: %v", err)
	}
	id, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	if len(id) != 32 {
		t.Fatalf("损坏文件后应重新生成 32 hex，得到 %q", id)
	}
}

func TestLoadOrCreateUnwritableDir(t *testing.T) {
	id, err := LoadOrCreate("/proc/does-not-exist")
	if err == nil || id != "" {
		t.Fatalf("不可写目录应报错并返回空身份，得到 (%q, %v)", id, err)
	}
}
