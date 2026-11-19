// Package durablefile persists small private configuration files atomically.
package durablefile

import (
	"os"
	"path/filepath"
)

func Write(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".astra-config-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	return SyncDirectory(filepath.Dir(path))
}
