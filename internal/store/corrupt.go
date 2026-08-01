package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type CorruptRecord struct {
	Path string
	Err  error
}

type timestampedRecord[T any] struct {
	recordedAt time.Time
	record     T
}

func listRecords[T any](dir string, decode func([]byte) (T, error)) ([]T, []CorruptRecord, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("read directory %s: %w", dir, err)
	}

	var records []T
	var corrupt []CorruptRecord
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".json") {
			continue
		}
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			corrupt = append(corrupt, CorruptRecord{Path: path, Err: fmt.Errorf("read %s: %w", path, err)})
			continue
		}
		record, err := decode(data)
		if err != nil {
			corrupt = append(corrupt, CorruptRecord{Path: path, Err: fmt.Errorf("decode %s: %w", path, err)})
			continue
		}
		records = append(records, record)
	}
	return records, corrupt, nil
}

func listTimestampedRecords[T any](dir string, decode func([]byte) (T, error)) ([]timestampedRecord[T], []CorruptRecord, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("read directory %s: %w", dir, err)
	}

	var records []timestampedRecord[T]
	var corrupt []CorruptRecord
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".json") {
			continue
		}
		path := filepath.Join(dir, name)
		recordedAt, err := parseRecordTimestamp(name)
		if err != nil {
			corrupt = append(corrupt, CorruptRecord{Path: path, Err: err})
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			corrupt = append(corrupt, CorruptRecord{Path: path, Err: fmt.Errorf("read %s: %w", path, err)})
			continue
		}
		record, err := decode(data)
		if err != nil {
			corrupt = append(corrupt, CorruptRecord{Path: path, Err: fmt.Errorf("decode %s: %w", path, err)})
			continue
		}
		records = append(records, timestampedRecord[T]{recordedAt: recordedAt, record: record})
	}
	return records, corrupt, nil
}
