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

// ReadRegularInfo returns metadata from the same opened file descriptor as the
// bytes, avoiding a path-based inspection race after verification.
func ReadRegularInfo(path string) ([]byte, os.FileInfo, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, &os.PathError{Op: "open verified leaf", Path: path, Err: err}
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = syscall.Close(fd)
		return nil, nil, fmt.Errorf("open verified leaf")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, nil, fmt.Errorf("inspect verified leaf: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("verified leaf is not a regular file")
	}
	contents, err := io.ReadAll(file)
	if err != nil {
		return nil, nil, fmt.Errorf("read verified leaf: %w", err)
	}
	return contents, info, nil
}
