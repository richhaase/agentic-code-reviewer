package store

import (
	"fmt"
	"os"
	"path/filepath"
)

const DataDirEnvVar = "ACR_DATA_DIR"

func DataDir() (string, error) {
	if dir := os.Getenv(DataDirEnvVar); dir != "" {
		return dir, nil
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "acr"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve application data directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "acr"), nil
}
