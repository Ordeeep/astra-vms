//go:build linux

package durablefile

import "os"

// SyncDirectory makes preceding file creation, rename or unlink durable.
func SyncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
