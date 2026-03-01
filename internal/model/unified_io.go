package model

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Save writes the UnifiedMatchFile to path as gzip-compressed JSON.
// The write is atomic: data is first written to a temp file in the same
// directory, then renamed to path to avoid partial writes on failure.
func (f *UnifiedMatchFile) Save(path string) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".csdem_tmp_*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	// Ensure temp file is cleaned up on any error path.
	ok := false
	defer func() {
		if !ok {
			os.Remove(tmpPath)
		}
	}()

	gz := gzip.NewWriter(tmp)
	enc := json.NewEncoder(gz)
	if err := enc.Encode(f); err != nil {
		tmp.Close()
		return fmt.Errorf("encode unified match: %w", err)
	}
	if err := gz.Close(); err != nil {
		tmp.Close()
		return fmt.Errorf("close gzip writer: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename to final path: %w", err)
	}

	ok = true
	return nil
}

// LoadUnifiedMatchFile reads a .csdem.gz file and returns the decoded UnifiedMatchFile.
// Returns an error if the file cannot be opened, is not valid gzip+JSON, has a
// mismatched version, or has a nil Match.
func LoadUnifiedMatchFile(path string) (*UnifiedMatchFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("gzip reader %s: %w", path, err)
	}
	defer gz.Close()

	var uf UnifiedMatchFile
	if err := json.NewDecoder(gz).Decode(&uf); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	if uf.Version != UnifiedMatchFileVersion {
		return nil, fmt.Errorf("unsupported version %d in %s (expected %d)",
			uf.Version, path, UnifiedMatchFileVersion)
	}
	if uf.Match == nil {
		return nil, fmt.Errorf("nil match in %s", path)
	}
	return &uf, nil
}
