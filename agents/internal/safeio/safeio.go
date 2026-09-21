// Package safeio provides verified, observational reads of filesystem leaves.
package safeio

import (
	"fmt"
	"io"
	"os"
	"syscall"
)

// ReadRegular opens path without following a leaf symlink and without blocking
// on a FIFO, then verifies the opened object before reading it.
func ReadRegular(path string) ([]byte, error) {
	contents, _, err := ReadRegularInfo(path)
	return contents, err
}

// OpenRegular opens and verifies path without following a leaf symlink or
// blocking on a special file. On success the caller owns the returned file and
// must close it. On failure OpenRegular closes every descriptor it acquired.
func OpenRegular(path string) (*os.File, os.FileInfo, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, &os.PathError{Op: "open verified leaf", Path: path, Err: err}
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = syscall.Close(fd)
		return nil, nil, fmt.Errorf("open verified leaf")
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, fmt.Errorf("inspect verified leaf: %w", err)
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, nil, fmt.Errorf("verified leaf is not a regular file")
	}
	return file, info, nil
}

// ReadRegularInfo returns metadata from the same opened file descriptor as the
// bytes, avoiding a path-based inspection race after verification.
func ReadRegularInfo(path string) ([]byte, os.FileInfo, error) {
	file, info, err := OpenRegular(path)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	contents, err := io.ReadAll(file)
	if err != nil {
		return nil, nil, fmt.Errorf("read verified leaf: %w", err)
	}
	return contents, info, nil
}
