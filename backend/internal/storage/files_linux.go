//go:build linux

package storage

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func acquireLock(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("storage already owned or inaccessible: %w", err)
	}
	return f, nil
}

func volumeInfo(path string) (int64, int64, error) {
	var info unix.Statfs_t
	if err := unix.Statfs(path, &info); err != nil {
		return 0, 0, err
	}
	// Qualify local ext4 first. Network, overlay and copy-on-write filesystems
	// have different allocation contracts and must not silently use this path.
	if info.Type != unix.EXT4_SUPER_MAGIC || info.Bsize != 4096 {
		return 0, 0, fmt.Errorf("Linux recording writer requires local ext4 with 4096-byte blocks")
	}
	if info.Flags&unix.ST_RDONLY != 0 {
		return 0, 0, fmt.Errorf("recording volume is read-only")
	}
	// Bavail excludes blocks reserved for root, including when inspected by root.
	return info.Bsize, int64(info.Bavail) * info.Bsize, nil
}

func allocate(f *os.File, n int64) error {
	if n == 0 {
		return nil
	}
	// Real allocation, without changing EOF or exposing padding as video.
	return unix.Fallocate(int(f.Fd()), unix.FALLOC_FL_KEEP_SIZE, 0, n)
}

// ext4 accounts extent-tree blocks in st_blocks as well as video data. At
// most four one-block extents fit inside a fresh inode. Beyond that, reserve
// a deliberately pessimistic five tree blocks per data block (maximum tree
// depth five, even with one extent per leaf). Only the data is preallocated;
// Finish replaces this temporary admission charge with observed allocation.
// This avoids relying on contiguous extents or minimum node occupancy.
func reservationSize(data int64) int64 {
	if data <= 4*4096 {
		return data
	}
	return 6 * data
}

func allocation(f *os.File) (int64, error) {
	var info unix.Stat_t
	if err := unix.Fstat(int(f.Fd()), &info); err != nil {
		return 0, err
	}
	return info.Blocks * 512, nil
}
