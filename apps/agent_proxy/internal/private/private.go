// Package private atomically replaces private files without exposing partial content.
package private

import (
	"os"
	"path/filepath"
)

// Write atomically replaces a 0600 file, including on initial creation.
func Write(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".yukino-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := f.Chmod(0600); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
