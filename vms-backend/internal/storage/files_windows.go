//go:build windows

package storage

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

var kernel = syscall.NewLazyDLL("kernel32.dll")
var setFileInfo = kernel.NewProc("SetFileInformationByHandle")
var getFileInfo = kernel.NewProc("GetFileInformationByHandleEx")
var diskSpace = kernel.NewProc("GetDiskFreeSpaceW")
var diskFree = kernel.NewProc("GetDiskFreeSpaceExW")

func acquireLock(path string) (*os.File, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, fmt.Errorf("storage already owned or inaccessible: %w", err)
	}
	return os.NewFile(uintptr(h), path), nil
}

func volumeInfo(path string) (int64, int64, error) {
	directory, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	// A mounted volume can have a different cluster size from C:\.
	var volumePath [32768]uint16
	ok, _, callErr := kernel.NewProc("GetVolumePathNameW").Call(uintptr(unsafe.Pointer(directory)), uintptr(unsafe.Pointer(&volumePath[0])), uintptr(len(volumePath)))
	if ok == 0 {
		return 0, 0, callErr
	}
	root := &volumePath[0]
	var fsName [64]uint16
	ok, _, callErr = kernel.NewProc("GetVolumeInformationW").Call(uintptr(unsafe.Pointer(root)), 0, 0, 0, 0, 0, uintptr(unsafe.Pointer(&fsName[0])), uintptr(len(fsName)))
	if ok == 0 {
		return 0, 0, callErr
	}
	if syscall.UTF16ToString(fsName[:]) != "NTFS" {
		return 0, 0, fmt.Errorf("recording writer currently supports NTFS only")
	}
	attrs, err := syscall.GetFileAttributes(directory)
	if err != nil {
		return 0, 0, err
	}
	if attrs&(0x800|0x4000) != 0 {
		return 0, 0, fmt.Errorf("compressed or encrypted recording directories are not supported")
	}
	var sectors, bytes, freeClusters, clusters uint32
	ok, _, callErr = diskSpace.Call(uintptr(unsafe.Pointer(root)), uintptr(unsafe.Pointer(&sectors)), uintptr(unsafe.Pointer(&bytes)), uintptr(unsafe.Pointer(&freeClusters)), uintptr(unsafe.Pointer(&clusters)))
	if ok == 0 {
		return 0, 0, callErr
	}
	var available uint64
	ok, _, callErr = diskFree.Call(uintptr(unsafe.Pointer(root)), uintptr(unsafe.Pointer(&available)), 0, 0)
	if ok == 0 {
		return 0, 0, callErr
	}
	return int64(sectors) * int64(bytes), int64(available), nil
}

func allocate(f *os.File, n int64) error {
	ok, _, err := setFileInfo.Call(f.Fd(), 5 /* FileAllocationInfo */, uintptr(unsafe.Pointer(&n)), unsafe.Sizeof(n))
	if ok == 0 {
		return err
	}
	return nil
}

func allocation(f *os.File) (int64, error) {
	var info struct {
		AllocationSize int64
		EndOfFile      int64
		NumberOfLinks  uint32
		DeletePending  byte
		Directory      byte
	}
	ok, _, err := getFileInfo.Call(f.Fd(), 1 /* FileStandardInfo */, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info))
	if ok == 0 {
		return 0, err
	}
	return info.AllocationSize, nil
}
