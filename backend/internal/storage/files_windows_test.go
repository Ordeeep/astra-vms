//go:build windows

package storage_test

import (
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"unsafe"
)

// Independent filesystem observation is explicitly part of B04's test seam.
func measuredAllocation(t *testing.T, root string) int64 {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(root, "*.media"))
	if err != nil {
		t.Fatal(err)
	}
	var total int64
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		var raw [24]byte
		ok, _, err := syscall.NewLazyDLL("kernel32.dll").NewProc("GetFileInformationByHandleEx").Call(f.Fd(), 1, uintptr(unsafe.Pointer(&raw[0])), 24)
		f.Close()
		if ok == 0 {
			t.Fatal(err)
		}
		total += *(*int64)(unsafe.Pointer(&raw[0]))
	}
	return total
}

func preventDelete(t *testing.T, path string) func() {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	return func() { once.Do(func() { f.Close() }) }
}
