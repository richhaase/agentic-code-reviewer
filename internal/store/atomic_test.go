package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestAtomicWriteFile_WritesAndReplaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "record.json")

	if err := atomicWriteFile(path, []byte(`{"a":1}`), 0o644); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := atomicWriteFile(path, []byte(`{"a":2}`), 0o644); err != nil {
		t.Fatalf("second write: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != `{"a":2}` {
		t.Fatalf("expected latest content, got %q", data)
	}
}

func TestAtomicWriteFile_LeavesNoTempFileBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "record.json")

	if err := atomicWriteFile(path, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "record.json" {
		t.Fatalf("expected only the final file to remain, got %v", entries)
	}
}

func TestAtomicWriteFile_StrayTempFileIgnoredByCaller(t *testing.T) {
	dir := t.TempDir()
	strayPath := filepath.Join(dir, ".tmp-record.json-abc123")
	if err := os.WriteFile(strayPath, []byte(`{"trunc`), 0o644); err != nil {
		t.Fatalf("seed stray temp file: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".") {
			t.Fatalf("expected only hidden stray files, found %q", entry.Name())
		}
	}
}

func TestWriteNewFile_ConcurrentWritersToTheSamePathNeverBothSucceed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "record.json")

	const writers = 20
	var wg sync.WaitGroup
	results := make([]error, writers)
	wg.Add(writers)
	for i := 0; i < writers; i++ {
		i := i
		go func() {
			defer wg.Done()
			results[i] = writeNewFile(path, []byte(fmt.Sprintf(`{"writer":%d}`, i)), 0o600)
		}()
	}
	wg.Wait()

	successes := 0
	for _, err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("expected exactly one of %d concurrent writers to the same path to succeed, got %d", writers, successes)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "record.json" {
		t.Fatalf("expected exactly one surviving record and no leftover temp files, got %v", entries)
	}
}

func TestWriteNewFile_RefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "record.json")

	if err := writeNewFile(path, []byte(`{"a":1}`), 0o644); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := writeNewFile(path, []byte(`{"a":2}`), 0o644); err == nil {
		t.Fatal("expected an error when writing over an existing record")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != `{"a":1}` {
		t.Fatalf("original record must be preserved, got %q", data)
	}
}
