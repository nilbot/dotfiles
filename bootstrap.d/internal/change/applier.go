package change

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// Applier performs operations for real.
type Applier struct{ out io.Writer }

func NewApplier(out io.Writer) *Applier { return &Applier{out: out} }

func (a *Applier) Lstat(path string) (FileInfo, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
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
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if err := a.Dir(filepath.Dir(target)); err != nil {
		return err
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return err
	}
	report(a.out, "ok    seeded %s from %s", target, source)
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
