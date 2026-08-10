// Package registry stores the machine-local cache used by fleet commands.
//
// Presence of .agents/ in a registered repository remains the truth. The
// registry only avoids an unbounded filesystem scan; it is never repo state.
package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"syscall"
	"time"

	"github.com/nilbot/dotfiles/agents/internal/machine"
	"github.com/nilbot/dotfiles/agents/internal/safetext"
)

type Entry struct {
	Path  string    `json:"path"`
	Added time.Time `json:"added"`
	Local bool      `json:"local"`
}

type Registry struct {
	Repos []Entry `json:"repos"`
}

func Path() string { return filepath.Join(machine.StateDir(), "registry.json") }

func lockPath() string { return filepath.Join(machine.StateDir(), "registry.lock") }

// Load reads one atomic snapshot. It does not need to take the writer lock:
// writers publish a complete file with rename.
func Load() (*Registry, error) { return load(Path()) }

func load(path string) (*Registry, error) {
	missingState, err := verifyStateDir(false)
	if err != nil {
		return nil, err
	}
	if missingState {
		return &Registry{}, nil
	}
	b, missing, err := readRegular(path)
	if err != nil {
		return nil, err
	}
	if missing {
		return &Registry{}, nil
	}
	var r Registry
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, invalidRegistry(path)
	}
	if !validRegistry(&r) {
		return nil, invalidRegistry(path)
	}
	return &r, nil
}

// Save atomically replaces the registry with this snapshot under the writer
// lock. Callers doing read-modify-write must use Update instead: Save's snapshot
// replacement semantics intentionally cannot merge a concurrently read value.
func (r *Registry) Save() error {
	if !validRegistry(r) {
		return invalidRegistry(Path())
	}
	lock, err := acquireLock()
	if err != nil {
		return err
	}
	defer lock.close()
	return saveUnlocked(r, os.Rename)
}

// Update holds the cross-process registry lock across a fresh load, mutation,
// and atomic save. The lock uses flock on the supported Darwin/Unix hosts; no
// claim is made for platforms where this package does not build.
func Update(fn func(*Registry) (bool, error)) (bool, error) {
	lock, err := acquireLock()
	if err != nil {
		return false, err
	}
	defer lock.close()

	r, err := load(Path())
	if err != nil {
		return false, err
	}
	changed, err := fn(r)
	if err != nil || !changed {
		return changed, err
	}
	if err := saveUnlocked(r, os.Rename); err != nil {
		return false, err
	}
	return true, nil
}

// Register records a repository without exposing an unsafe Load/Add/Save
// sequence to command callers.
func Register(path string, local bool) (bool, error) {
	if !validEntryPath(path) {
		return false, fmt.Errorf("registry repository path must be absolute and cleaned")
	}
	return Update(func(r *Registry) (bool, error) { return r.Add(path, local), nil })
}

// Add reports whether it added the path or changed its Local metadata. A
// metadata update preserves Added: that timestamp means first registration.
func (r *Registry) Add(path string, local bool) bool {
	for i := range r.Repos {
		if r.Repos[i].Path != path {
			continue
		}
		if r.Repos[i].Local == local {
			return false
		}
		r.Repos[i].Local = local
		return true
	}
	r.Repos = append(r.Repos, Entry{Path: path, Added: time.Now().UTC(), Local: local})
	return true
}

func (r *Registry) Remove(path string) bool {
	for i := range r.Repos {
		if r.Repos[i].Path == path {
			r.Repos = append(r.Repos[:i], r.Repos[i+1:]...)
			return true
		}
	}
	return false
}

// Reconcile preserves the original two-slice API while returning only
// confirmed classifications. An entry whose .agents marker cannot be
// inspected is in neither slice; callers that report or mutate drift should
// use ReconcileDetailed so they can retain and surface it.
func (r *Registry) Reconcile() (present, missing []Entry) {
	present, missing, _ = r.ReconcileDetailed()
	return present, missing
}

// ReconcileDetailed is deliberately one-way: it classifies registered
// entries and does not scan for unregistered repositories. Unknown entries are
// kept separate so a permission or I/O failure can never authorize pruning.
func (r *Registry) ReconcileDetailed() (present, missing, unknown []Entry) {
	for _, e := range r.Repos {
		fi, err := os.Stat(filepath.Join(e.Path, ".agents"))
		if err == nil && fi.IsDir() {
			present = append(present, e)
		} else if err == nil || errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			missing = append(missing, e)
		} else {
			unknown = append(unknown, e)
		}
	}
	return present, missing, unknown
}

type fileLock struct{ file *os.File }

func acquireLock() (*fileLock, error) {
	if _, err := verifyStateDir(true); err != nil {
		return nil, err
	}
	path := lockPath()
	if err := requireSingleRegularOrMissing(path, "registry lock"); err != nil {
		return nil, err
	}
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_CREAT|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open registry lock %s: %s", safePath(path), safeError(err))
	}
	f := os.NewFile(uintptr(fd), path)
	if f == nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("open registry lock %s", safePath(path))
	}
	var st syscall.Stat_t
	if err := syscall.Fstat(fd, &st); err != nil {
		f.Close()
		return nil, fmt.Errorf("inspect registry lock %s: %s", safePath(path), safeError(err))
	}
	if st.Mode&syscall.S_IFMT != syscall.S_IFREG || st.Nlink != 1 {
		f.Close()
		return nil, fmt.Errorf("registry lock %s must be a singly linked regular file", safePath(path))
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return nil, fmt.Errorf("make registry lock private %s: %s", safePath(path), safeError(err))
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("lock registry %s: %s", safePath(path), safeError(err))
	}
	return &fileLock{file: f}, nil
}

func (l *fileLock) close() {
	_ = syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	_ = l.file.Close()
}

func saveUnlocked(r *Registry, rename func(string, string) error) error {
	if !validRegistry(r) {
		return invalidRegistry(Path())
	}
	copyRepos := append([]Entry(nil), r.Repos...)
	sort.Slice(copyRepos, func(i, j int) bool { return copyRepos[i].Path < copyRepos[j].Path })
	b, err := json.MarshalIndent(&Registry{Repos: copyRepos}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode registry: %w", err)
	}
	return atomicWrite(Path(), append(b, '\n'), rename)
}

func validRegistry(r *Registry) bool {
	seen := make(map[string]struct{}, len(r.Repos))
	for _, entry := range r.Repos {
		if !validEntryPath(entry.Path) || entry.Added.IsZero() {
			return false
		}
		if _, duplicate := seen[entry.Path]; duplicate {
			return false
		}
		seen[entry.Path] = struct{}{}
	}
	return true
}

func validEntryPath(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path
}

func invalidRegistry(path string) error {
	return fmt.Errorf("registry %s has invalid JSON or schema (safe to delete; it is a cache)", safePath(path))
}

func atomicWrite(path string, data []byte, rename func(string, string) error) (retErr error) {
	dir := filepath.Dir(path)
	if _, err := verifyStateDir(true); err != nil {
		return err
	}
	if err := requireRegularOrMissing(path); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".registry-*.tmp")
	if err != nil {
		return fmt.Errorf("create private registry temporary file in %s: %s", safePath(dir), safeError(err))
	}
	tmp := f.Name()
	defer func() {
		_ = f.Close()
		_ = os.Remove(tmp)
	}()
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write registry temporary file: %s", safeError(err))
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync registry temporary file: %s", safeError(err))
	}
	if err := f.Chmod(0o600); err != nil {
		return fmt.Errorf("make registry temporary file private: %s", safeError(err))
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close registry temporary file: %s", safeError(err))
	}
	// Recheck after the slow writes. If another local actor installed a special
	// leaf, refuse it. A swap after this point is still safe: rename replaces the
	// directory entry and never follows its target.
	if err := requireRegularOrMissing(path); err != nil {
		return err
	}
	if err := rename(tmp, path); err != nil {
		return fmt.Errorf("publish registry %s: %s", safePath(path), safeError(err))
	}
	return nil
}

// verifyStateDir protects the final agents directory as the boundary for every
// registry read and write. The configured XDG root may itself resolve through
// a symlink, but the final directory must be a real directory entry.
func verifyStateDir(create bool) (missing bool, retErr error) {
	dir := machine.StateDir()
	fi, err := os.Lstat(dir)
	if os.IsNotExist(err) && !create {
		return true, nil
	}
	if os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return false, fmt.Errorf("create registry state directory %s: %s", safePath(dir), safeError(err))
		}
		fi, err = os.Lstat(dir)
	}
	if err != nil {
		return false, fmt.Errorf("inspect registry state directory %s: %s", safePath(dir), safeError(err))
	}
	if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("registry state directory %s must be a real directory", safePath(dir))
	}
	return false, nil
}

func readRegular(path string) ([]byte, bool, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, syscall.ENOENT) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("open registry %s: %s", safePath(path), safeError(err))
	}
	f := os.NewFile(uintptr(fd), path)
	if f == nil {
		syscall.Close(fd)
		return nil, false, fmt.Errorf("open registry %s", safePath(path))
	}
	defer f.Close()
	var st syscall.Stat_t
	if err := syscall.Fstat(fd, &st); err != nil {
		return nil, false, fmt.Errorf("inspect registry %s: %s", safePath(path), safeError(err))
	}
	if st.Mode&syscall.S_IFMT != syscall.S_IFREG || st.Nlink != 1 {
		return nil, false, fmt.Errorf("registry %s must be a regular file", safePath(path))
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return nil, false, fmt.Errorf("read registry %s: %s", safePath(path), safeError(err))
	}
	return b, false, nil
}

func requireRegularOrMissing(path string) error {
	return requireSingleRegularOrMissing(path, "registry")
}

func requireSingleRegularOrMissing(path, description string) error {
	fi, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect %s %s: %s", description, safePath(path), safeError(err))
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !fi.Mode().IsRegular() || !ok || st.Nlink != 1 {
		return fmt.Errorf("%s %s must be a singly linked regular file", description, safePath(path))
	}
	return nil
}

func safePath(path string) string { return strconv.QuoteToASCII(path) }

func safeError(err error) string { return safetext.Flatten(err.Error()) }
