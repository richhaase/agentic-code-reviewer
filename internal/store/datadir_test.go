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

func TestDataDir_DefaultsUnderXDGDataHome(t *testing.T) {
	t.Setenv(DataDirEnvVar, "")
	t.Setenv("XDG_DATA_HOME", "/xdg/data")

	dir, err := DataDir()
	if err != nil {
		t.Fatalf("DataDir: %v", err)
	}
	if dir != "/xdg/data/acr" {
		t.Fatalf("expected XDG_DATA_HOME to be honored, got %q", dir)
	}
}

func TestDataDir_DefaultsUnderDotLocalShareWhenXDGUnset(t *testing.T) {
	t.Setenv(DataDirEnvVar, "")
	t.Setenv("XDG_DATA_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir, err := DataDir()
	if err != nil {
		t.Fatalf("DataDir: %v", err)
	}
	want := filepath.Join(home, ".local", "share", "acr")
	if dir != want {
		t.Fatalf("expected %q, got %q", want, dir)
	}
}
