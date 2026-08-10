// Package safeio provides verified, observational reads of filesystem leaves.
package safeio

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
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

// OpenDir treats path's parent as the trust anchor, rejects a symlink in the
// final directory component, and returns a handle bound to the same directory
// that was inspected. An intentional symlink above path remains supported.
func OpenDir(path string) (*os.Root, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("%s is not a real directory", path)
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		_ = root.Close()
		return nil, fmt.Errorf("%s changed while it was being opened", path)
	}
	return root, nil
}

// OpenDirAt applies OpenDir's check to one child of an already-open root.
func OpenDirAt(parent *os.Root, name string) (*os.Root, error) {
	path := filepath.Join(parent.Name(), name)
	info, err := parent.Lstat(name)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("%s is not a real directory", path)
	}
	child, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	opened, err := child.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		_ = child.Close()
		return nil, fmt.Errorf("%s changed while it was being opened", path)
	}
	return child, nil
}

// OpenRegularAt verifies a leaf beneath an already-open root, then returns the
// exact descriptor whose identity was compared. It never follows the leaf and
// never blocks while determining that the object is special.
func OpenRegularAt(root *os.Root, name string) (*os.File, os.FileInfo, error) {
	path := filepath.Join(root.Name(), name)
	info, err := root.Lstat(name)
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("%s is not a regular file", path)
	}
	file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, err
	}
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		_ = file.Close()
		return nil, nil, fmt.Errorf("%s changed while it was being opened", path)
	}
	return file, opened, nil
}

// ReadRegularAt reads a verified regular leaf beneath root.
func ReadRegularAt(root *os.Root, name string) ([]byte, os.FileInfo, error) {
	file, info, err := OpenRegularAt(root, name)
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
