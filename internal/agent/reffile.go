package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

const RefFileSizeThreshold = 100 * 1024

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
		return "", errors.Join(
			fmt.Errorf("failed to get absolute path for temp file: %w", err),
			CleanupTempFile(tempPath),
		)
	}

	return absPath, nil
}

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
		return "", errors.Join(
			fmt.Errorf("failed to get absolute path for temp file: %w", err),
			CleanupTempFile(tempPath),
		)
	}

	return absPath, nil
}

func CleanupTempFile(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to clean up temp file %s: %w", path, err)
	}
	return nil
}
