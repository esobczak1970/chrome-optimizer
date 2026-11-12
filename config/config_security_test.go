package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadRejectsSymlinkEscapingConfigDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks may require Windows privileges")
	}
	outside := filepath.Join(t.TempDir(), "other.yaml")
	if err := os.WriteFile(outside, []byte("timeout: 15s\n"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.Symlink(outside, path); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load must not follow a symlink outside its configuration directory")
	}
}
