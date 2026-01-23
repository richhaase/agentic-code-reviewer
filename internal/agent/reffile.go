package agent

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

// RefFileSizeThreshold is the diff size (in bytes) above which ref-file mode
// is automatically used to avoid "prompt too long" errors.
// All supported agents (Claude, Codex, Gemini) have file system access and can
// read files from the working directory when instructed via the prompt.
const RefFileSizeThreshold = 100 * 1024 // 100KB

// GetWorkDir returns the working directory to use for temp files.
// If workDir is non-empty, returns it. Otherwise returns os.Getwd().
// Returns an error if unable to determine the working directory.
func GetWorkDir(workDir string) (string, error) {
	if workDir != "" {
		return workDir, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}
	return wd, nil
}

// WriteDiffToTempFile writes the diff content to a temporary file in the working directory.
// Returns the absolute path to the temp file.
// The caller is responsible for cleaning up the file (use CleanupTempFile).
func WriteDiffToTempFile(workDir, diff string) (string, error) {
	wd, err := GetWorkDir(workDir)
	if err != nil {
		return "", err
	}

	tempPath := filepath.Join(wd, fmt.Sprintf(".acr-diff-%s.patch", uuid.New().String()))
	if err := os.WriteFile(tempPath, []byte(diff), 0600); err != nil {
		return "", fmt.Errorf("failed to write diff to temp file: %w", err)
	}

	absPath, err := filepath.Abs(tempPath)
	if err != nil {
		// Clean up the temp file since we can't return a valid path
		if rmErr := os.Remove(tempPath); rmErr != nil && !os.IsNotExist(rmErr) {
			fmt.Fprintf(os.Stderr, "Warning: failed to clean up temp file %s during error handling: %v", tempPath, rmErr)
		}
		return "", fmt.Errorf("failed to get absolute path for temp file: %w", err)
	}

	return absPath, nil
}

// WriteInputToTempFile writes input content (e.g., summary input JSON) to a temporary file.
// Returns the absolute path to the temp file.
// The caller is responsible for cleaning up the file (use CleanupTempFile).
func WriteInputToTempFile(workDir string, input []byte, suffix string) (string, error) {
	wd, err := GetWorkDir(workDir)
	if err != nil {
		return "", err
	}

	tempPath := filepath.Join(wd, fmt.Sprintf(".acr-%s-%s", suffix, uuid.New().String()))
	if err := os.WriteFile(tempPath, input, 0600); err != nil {
		return "", fmt.Errorf("failed to write input to temp file: %w", err)
	}

	absPath, err := filepath.Abs(tempPath)
	if err != nil {
		// Clean up the temp file since we can't return a valid path
		if rmErr := os.Remove(tempPath); rmErr != nil && !os.IsNotExist(rmErr) {
			fmt.Fprintf(os.Stderr, "Warning: failed to clean up temp file %s during error handling: %v", tempPath, rmErr)
		}
		return "", fmt.Errorf("failed to get absolute path for temp file: %w", err)
	}

	return absPath, nil
}

// CleanupTempFile removes a temporary file. If removal fails, it logs a warning
// but does not return an error since cleanup failures are non-fatal.
func CleanupTempFile(path string) {
	if path == "" {
		return
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Warning: failed to clean up temp file %s: %v", path, err)
	}
}
