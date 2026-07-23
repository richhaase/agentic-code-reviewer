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
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve application data directory: %w", err)
	}
	return filepath.Join(base, "acr"), nil
}
