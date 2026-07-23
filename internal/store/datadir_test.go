package store

import (
	"path/filepath"
	"testing"
)

func TestDataDir_EnvVarOverride(t *testing.T) {
	t.Setenv(DataDirEnvVar, "/custom/data/dir")

	dir, err := DataDir()
	if err != nil {
		t.Fatalf("DataDir: %v", err)
	}
	if dir != "/custom/data/dir" {
		t.Fatalf("expected env override to win, got %q", dir)
	}
}

func TestDataDir_DefaultsUnderUserCacheDir(t *testing.T) {
	t.Setenv(DataDirEnvVar, "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", "")

	dir, err := DataDir()
	if err != nil {
		t.Fatalf("DataDir: %v", err)
	}
	if filepath.Base(dir) != "acr" {
		t.Fatalf("expected data dir to be named acr, got %q", dir)
	}
}
