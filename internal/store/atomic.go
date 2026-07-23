package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func writeTempFile(dir, base string, data []byte, perm os.FileMode) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create directory %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".tmp-"+base+"-*")
	if err != nil {
		return "", fmt.Errorf("create temporary file in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", fmt.Errorf("write temporary file %s: %w", tmpPath, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return "", fmt.Errorf("sync temporary file %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close temporary file %s: %w", tmpPath, err)
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		return "", fmt.Errorf("set permissions on temporary file %s: %w", tmpPath, err)
	}
	ok = true
	return tmpPath, nil
}

func syncDir(dir string) {
	if dirFile, err := os.Open(dir); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmpPath, err := writeTempFile(dir, filepath.Base(path), data, perm)
	if err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temporary file %s to %s: %w", tmpPath, path, err)
	}
	syncDir(dir)
	return nil
}

func writeNewFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmpPath, err := writeTempFile(dir, filepath.Base(path), data, perm)
	if err != nil {
		return err
	}
	if err := os.Link(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("record already exists at %s", path)
		}
		return fmt.Errorf("link temporary file %s to %s: %w", tmpPath, path, err)
	}
	os.Remove(tmpPath)
	syncDir(dir)
	return nil
}
