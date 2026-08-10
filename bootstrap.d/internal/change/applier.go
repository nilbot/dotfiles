package change

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// Applier performs operations for real.
//
// root is the checkout this executor serves, and it is held here rather than
// passed to Seed because an executor operating on a machine must know which
// checkout it serves. Threading it through each call would let two call sites
// disagree about it, and the whole point of substitution is that exactly one
// answer is right per machine.
type Applier struct {
	out  io.Writer
	root string
}

func NewApplier(out io.Writer, root string) *Applier { return &Applier{out: out, root: root} }

func (a *Applier) Lstat(path string) (FileInfo, error) {
	info, err := os.Lstat(path)
	// ENOTDIR means an ancestor is not a directory, so this path does not exist
	// and cannot -- which is what FileInfo{} says. os.IsNotExist is false for
	// it (Errno.Is maps only ENOENT to ErrNotExist), so without this arm the
	// error escapes as a raw *fs.PathError naming the leaf, and ancestorConflict
	// never gets to name the ancestor that is actually in the way.
	if os.IsNotExist(err) || errors.Is(err, syscall.ENOTDIR) {
		return FileInfo{}, nil
	}
	if err != nil {
		return FileInfo{}, err
	}
	return FileInfo{
		Exists:    true,
		IsDir:     info.IsDir(),
		IsLink:    info.Mode()&os.ModeSymlink != 0,
		IsRegular: info.Mode().IsRegular(),
	}, nil
}

func (a *Applier) Readlink(path string) (string, error) { return os.Readlink(path) }
func (a *Applier) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

func (a *Applier) LookPath(name string) (string, error) { return exec.LookPath(name) }

func (a *Applier) Dir(path string) error {
	info, err := a.Lstat(path)
	if err != nil {
		return err
	}
	v, err := dirVerdict(info, path)
	if err != nil || v == verdictSatisfied {
		return err
	}
	if err := ancestorConflict(a, path); err != nil {
		return err
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	report(a.out, "ok    created directory %s", path)
	return nil
}

func (a *Applier) Link(source, target string) error {
	info, err := a.Lstat(target)
	if err != nil {
		return err
	}
	var current string
	if info.IsLink {
		if current, err = a.Readlink(target); err != nil {
			return err
		}
	}
	v, err := linkVerdict(info, current, source, target)
	if err != nil || v == verdictSatisfied {
		return err
	}
	srcInfo, err := a.Lstat(source)
	if err != nil {
		return err
	}
	if err := linkSourceVerdict(srcInfo, source); err != nil {
		return err
	}
	if err := a.Dir(filepath.Dir(target)); err != nil {
		return err
	}
	if err := os.Symlink(source, target); err != nil {
		return err
	}
	report(a.out, "ok    linked %s -> %s", target, source)
	return nil
}

func (a *Applier) Seed(source, target string) error {
	info, err := a.Lstat(target)
	if err != nil {
		return err
	}
	v, err := seedVerdict(info, target)
	if err != nil || v == verdictSatisfied {
		return err
	}
	srcInfo, err := a.Lstat(source)
	if err != nil {
		return err
	}
	if err := seedSourceVerdict(srcInfo, source); err != nil {
		return err
	}
	// Still read before creating the parent: the verdict cannot rule out every
	// read failure, and a failed read must not leave a stray directory behind.
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	// The one transformation a seed applies. Between the read and the write, so
	// a template is never rewritten in place and a failed read still leaves the
	// tree untouched.
	data = substituteRoot(data, a.root)
	if err := a.Dir(filepath.Dir(target)); err != nil {
		return err
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return err
	}
	report(a.out, "ok    seeded %s from %s", target, source)
	return nil
}

// Copy duplicates source at target, recursively, without following symlinks:
// a symlink is recreated as a symlink, so a plugin directory full of them keeps
// its shape instead of being expanded into copies.
//
// Nothing about the source is disturbed. That is the whole point -- the fish
// migration copies fisher's generated state out of the checkout and only then
// removes it, because none of that state is in git.
func (a *Applier) Copy(source, target string) error {
	srcInfo, err := a.Lstat(source)
	if err != nil {
		return err
	}
	dstInfo, err := a.Lstat(target)
	if err != nil {
		return err
	}
	if err := copyVerdict(srcInfo, dstInfo, source, target); err != nil {
		return err
	}
	if err := copyTree(source, target); err != nil {
		return err
	}
	report(a.out, "ok    copied %s -> %s", source, target)
	return nil
}

// copyTree is Copy's recursion, after the verdict. Modes are set explicitly
// rather than left to Mkdir and WriteFile, whose arguments the umask masks: a
// completions directory that arrives 0700 because the caller's umask was 077 is
// a machine that behaves differently after a migration than before it.
func copyTree(source, target string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		dest, err := os.Readlink(source)
		if err != nil {
			return err
		}
		return os.Symlink(dest, target)
	case info.IsDir():
		// Created writable and chmodded to the source's mode LAST, after its
		// children are in it. Setting the mode first works for the 0755
		// directories fisher makes and fails for anything read-only -- the
		// children cannot then be created inside it -- which is a trap that
		// would not appear until some machine had one.
		if err := os.Mkdir(target, 0o755); err != nil {
			return err
		}
		entries, err := os.ReadDir(source)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyTree(filepath.Join(source, entry.Name()),
				filepath.Join(target, entry.Name())); err != nil {
				return err
			}
		}
		return os.Chmod(target, info.Mode().Perm())
	case info.Mode().IsRegular():
		data, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, data, info.Mode().Perm()); err != nil {
			return err
		}
		return os.Chmod(target, info.Mode().Perm())
	}
	// A socket, a device node or a fifo. Copying one is not something this
	// design has a right answer for, and skipping it in silence is how a
	// migration reports success over data it did not move.
	return refuse(source,
		"is neither a regular file, a directory nor a symlink, so it cannot be copied",
		"move it aside deliberately, then retry")
}

func (a *Applier) Rename(source, target string) error {
	srcInfo, err := a.Lstat(source)
	if err != nil {
		return err
	}
	dstInfo, err := a.Lstat(target)
	if err != nil {
		return err
	}
	if err := renameVerdict(srcInfo, dstInfo, source, target); err != nil {
		return err
	}
	if err := os.Rename(source, target); err != nil {
		return err
	}
	report(a.out, "ok    renamed %s -> %s", source, target)
	return nil
}

// RemoveAll is the only operation in this package that destroys anything, and
// it is deliberately unguarded beyond "is it there": the fish migration removes
// directories, so a guard refusing directories would refuse the one caller.
// What may be removed is decided by the migration, whose Pending and Run consult
// one shared account of the machine's state -- see internal/migrate.
func (a *Applier) RemoveAll(path string) error {
	info, err := a.Lstat(path)
	if err != nil {
		return err
	}
	if removeVerdict(info) == verdictSatisfied {
		return nil
	}
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	report(a.out, "ok    removed %s", path)
	return nil
}

// WriteFile replaces a machine-local file's contents. Seed cannot do this by
// design -- it never overwrites -- and the gitconfig migration must, because the
// file it repoints is the one `git config --global` has been writing to.
//
// No parent directory is created. Its one caller rewrites a file that is already
// there, and a migration that invented a path would be doing something other
// than reconciling.
func (a *Applier) WriteFile(path string, data []byte) error {
	info, err := a.Lstat(path)
	if err != nil {
		return err
	}
	if err := writeVerdict(info, path); err != nil {
		return err
	}
	// 0o644 applies only when the file is CREATED: os.WriteFile passes perm to
	// OpenFile with O_CREATE, and the kernel ignores it for a file that already
	// exists. So a ~/.gitconfig the user chmodded 0600 -- plausible, it names
	// the path to their secrets -- keeps that mode through the rewrite rather
	// than being quietly widened.
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	report(a.out, "ok    rewrote %s", path)
	return nil
}

func (a *Applier) Run(name string, args ...string) error  { return a.run(name, args, false) }
func (a *Applier) Sudo(name string, args ...string) error { return a.run(name, args, true) }

func (a *Applier) run(name string, args []string, elevate bool) error {
	if elevate {
		args = append([]string{name}, args...)
		name = "sudo"
	}
	cmd := exec.Command(name, args...)
	cmd.Stdout = a.out
	cmd.Stderr = a.out
	return cmd.Run()
}
