//go:build windows

package durablefile

// Preserve the existing Windows storage behavior. POSIX directory fsync is
// required on Linux and is not supported by os.File.Sync on Windows directories.
func SyncDirectory(path string) error { return nil }
