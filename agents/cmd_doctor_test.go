package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nilbot/dotfiles/agents/internal/doctor"
	"github.com/nilbot/dotfiles/agents/internal/exitcode"
	"github.com/nilbot/dotfiles/agents/internal/repo"
)

func commandDepsForDoctor(t *testing.T, checks []doctor.Check, runErr error) doctorCommandDependencies {
	t.Helper()
	root := newRepo(t)
	return doctorCommandDependencies{
		Getwd:      func() (string, error) { return root, nil },
		Discover:   repo.Discover,
		ReadID:     func() (string, error) { return "m1", nil },
		BinaryPath: func() (string, error) { return filepath.Join(root, "agents"), nil },
		Now:        func() time.Time { return time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC) },
		DoctorDeps: doctor.Dependencies{},
		Run: func(string, string, string, string, doctor.Thresholds, time.Time, doctor.Dependencies) ([]doctor.Check, error) {
			return checks, runErr
		},
	}
}

func realDoctorCommandDeps(t *testing.T, root string) doctorCommandDependencies {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "agents")
	if err := os.WriteFile(binary, []byte("test binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	return doctorCommandDependencies{
		Getwd:      func() (string, error) { return root, nil },
		Discover:   repo.Discover,
		ReadID:     func() (string, error) { return "m1", nil },
		BinaryPath: func() (string, error) { return binary, nil },
		Now:        func() time.Time { return time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC) },
		DoctorDeps: doctor.Dependencies{},
		Run:        doctor.RunWithDeps,
	}
}

// releaseFIFOAfter prevents a deliberately unsafe old reader from becoming a
// test-process orphan. A safe reader closes stop before the timer fires.
func releaseFIFOAfter(path string, delay time.Duration, stop <-chan struct{}) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-time.After(delay):
			f, _ := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0)
			if f != nil {
				_ = f.Close()
			}
		case <-stop:
		}
	}()
	return done
}

// doctorCheckStatus returns the status mark doctor printed for one check, or
// "<missing>" when it printed none. Remedy lines begin with "->" and so never
// match a check name.
func doctorCheckStatus(output, name string) string {
	for _, line := range strings.Split(output, "\n") {
		if fields := strings.Fields(line); len(fields) >= 2 && fields[1] == name {
			return fields[0]
		}
	}
	return "<missing>"
}

// TestDoctorCommandNamesThisBinarysCheckoutInsteadOfAssumingHome pins the one
// line that fixes the reported bug: the doctor command asking DotfilesRoot()
// which checkout this binary belongs to, instead of letting doctor assume
// ~/dotfiles and report three failures and a warning against a healthy machine.
//
// DotfilesRoot and DependenciesFor are each covered alone, but covering two
// halves says nothing about whether they are joined. This builds the real
// dependencies through the real constructor, so reverting that line to
// DefaultDependencies() fails here -- which is the whole value of the pin.
func TestDoctorCommandNamesThisBinarysCheckoutInsteadOfAssumingHome(t *testing.T) {
	stampRoot(t, "")
	root := filepath.Join(t.TempDir(), "src", "dotfiles")
	home := t.TempDir()
	t.Setenv("AGENTS_DOTFILES_ROOT", root)
	t.Setenv("HOME", home)

	deps := defaultDoctorCommandDependencies().DoctorDeps

	for _, c := range []struct{ field, got, want string }{
		{"HooksDir", deps.HooksDir, filepath.Join(root, "git", "hooks.d")},
		{"AttributesSource", deps.AttributesSource, filepath.Join(root, "git", "gitattributes")},
		{"SharedGitConfig", deps.SharedGitConfig, filepath.Join(root, "git", "gitconfig.shared")},
	} {
		if c.got != c.want {
			t.Errorf("doctor command DoctorDeps.%s = %q, want %q; doctor compares this "+
				"against what Git reports, so a binary that assumes %s instead fails "+
				"its own check on a correctly provisioned machine",
				c.field, c.got, c.want, filepath.Join(home, "dotfiles"))
		}
	}
}

func TestDoctorCommandExitContract(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		checks []doctor.Check
		err    error
		want   int
	}{
		{"all ok", nil, []doctor.Check{{Name: "x", Status: doctor.OK, Detail: "ok"}}, nil, exitcode.OK},
		{"warn", nil, []doctor.Check{{Name: "x", Status: doctor.Warn, Detail: "warn"}}, nil, exitcode.Advisory},
		{"fail label is advisory exit", nil, []doctor.Check{{Name: "x", Status: doctor.Fail, Detail: "fail"}}, nil, exitcode.Advisory},
		{"bad flag", []string{"--unknown"}, nil, nil, exitcode.Malformed},
		{"extra arg", []string{"extra"}, nil, nil, exitcode.Malformed},
		{"invalid threshold", []string{"--recording-freshness=0s"}, nil, nil, exitcode.Malformed},
		{"core failure", nil, nil, errors.New("private failure"), exitcode.NoRecord},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			got := runDoctorWithDependencies(tc.args, &out, commandDepsForDoctor(t, tc.checks, tc.err))
			if got != tc.want || got == exitcode.Block {
				t.Fatalf("exit=%d want=%d output=%q", got, tc.want, out.String())
			}
			if strings.Contains(out.String(), "private failure") {
				t.Fatalf("core error leaked dependency detail: %q", out.String())
			}
		})
	}
}

func TestDoctorMapsUnsafeTraceLeavesToContentSafeNoRecord(t *testing.T) {
	for _, kind := range []string{"symlink", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			root := newRepo(t)
			traceLeaf := filepath.Join(root, ".agents", "reports", "traces", "2026-08-20.jsonl")
			private := "PRIVATE-doctor-trace-sentinel"
			var stop chan struct{}
			var released <-chan struct{}
			if kind == "symlink" {
				target := filepath.Join(t.TempDir(), "outside.jsonl")
				body := `{"when":"2026-08-20T12:00:00Z","agent_id":"` + private + `"}` + "\n"
				if err := os.WriteFile(target, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, traceLeaf); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := syscall.Mkfifo(traceLeaf, 0o600); err != nil {
					t.Fatal(err)
				}
				stop = make(chan struct{})
				released = releaseFIFOAfter(traceLeaf, 300*time.Millisecond, stop)
			}

			var out bytes.Buffer
			start := time.Now()
			code := runDoctorWithDependencies(nil, &out, realDoctorCommandDeps(t, root))
			elapsed := time.Since(start)
			if stop != nil {
				close(stop)
				<-released
				if elapsed >= 150*time.Millisecond {
					t.Fatalf("doctor blocked on trace FIFO for %v", elapsed)
				}
			}
			if code != exitcode.NoRecord || !strings.Contains(out.String(), "could not complete the diagnostic") {
				t.Fatalf("trace %s: exit=%d output=%q, want content-safe NoRecord", kind, code, out.String())
			}
			if strings.Contains(out.String(), private) {
				t.Fatalf("trace %s exposed target content: %q", kind, out.String())
			}
		})
	}
}

func TestDoctorCompletesWithFailAdvisoryForUnsafeMemoryLeaves(t *testing.T) {
	for _, kind := range []string{"symlink", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			root := newRepo(t)
			memoryDir := filepath.Join(root, ".agents", "memory")
			if err := os.MkdirAll(memoryDir, 0o755); err != nil {
				t.Fatal(err)
			}
			memoryLeaf := filepath.Join(memoryDir, "entry.md")
			private := "PRIVATE-doctor-memory-sentinel"
			var stop chan struct{}
			var released <-chan struct{}
			if kind == "symlink" {
				target := filepath.Join(t.TempDir(), "outside.md")
				body := "---\nname: " + private + "\ndescription: outside\nmetadata:\n  type: project\n---\n\nbody\n"
				if err := os.WriteFile(target, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, memoryLeaf); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := syscall.Mkfifo(memoryLeaf, 0o600); err != nil {
					t.Fatal(err)
				}
				stop = make(chan struct{})
				released = releaseFIFOAfter(memoryLeaf, 300*time.Millisecond, stop)
			}

			var out bytes.Buffer
			start := time.Now()
			code := runDoctorWithDependencies(nil, &out, realDoctorCommandDeps(t, root))
			elapsed := time.Since(start)
			if stop != nil {
				close(stop)
				<-released
				if elapsed >= 150*time.Millisecond {
					t.Fatalf("doctor blocked on memory FIFO for %v", elapsed)
				}
			}
			if code != exitcode.Advisory || !strings.Contains(out.String(), "FAIL  memory") {
				t.Fatalf("memory %s: exit=%d output=%q, want completed fail/advisory", kind, code, out.String())
			}
			if strings.Contains(out.String(), private) {
				t.Fatalf("memory %s exposed target content: %q", kind, out.String())
			}
		})
	}
}

func TestDoctorOutsideRepositorySkips(t *testing.T) {
	deps := commandDepsForDoctor(t, nil, nil)
	dir := t.TempDir()
	deps.Getwd = func() (string, error) { return dir, nil }
	deps.Discover = func(string) (*repo.Context, error) { return nil, repo.ErrNotARepo }
	var out bytes.Buffer
	if got := runDoctorWithDependencies(nil, &out, deps); got != exitcode.Skip {
		t.Fatalf("exit=%d output=%q", got, out.String())
	}
}

func TestDoctorOperationalDiscoveryFailureDoesNotSkip(t *testing.T) {
	deps := commandDepsForDoctor(t, nil, nil)
	deps.Discover = func(string) (*repo.Context, error) { return nil, errors.New("Git executable unavailable") }
	var out bytes.Buffer
	if got := runDoctorWithDependencies(nil, &out, deps); got != exitcode.NoRecord {
		t.Fatalf("exit=%d output=%q, want NoRecord", got, out.String())
	}
	if strings.Contains(out.String(), "Git executable unavailable") {
		t.Fatalf("operational discovery detail leaked: %q", out.String())
	}
}

func TestDoctorMissingMachineIDIsDiagnosticNotStateCreation(t *testing.T) {
	deps := commandDepsForDoctor(t, []doctor.Check{{Name: "machine-id", Status: doctor.Warn, Detail: "missing"}}, nil)
	state := t.TempDir()
	deps.ReadID = func() (string, error) { return "", os.ErrNotExist }
	before, _ := os.ReadDir(state)
	var out bytes.Buffer
	if got := runDoctorWithDependencies(nil, &out, deps); got != exitcode.Advisory {
		t.Fatalf("exit=%d output=%q", got, out.String())
	}
	after, _ := os.ReadDir(state)
	if len(before) != len(after) {
		t.Fatal("doctor created machine state")
	}
}

func TestDoctorOutputEscapesHostileFields(t *testing.T) {
	checks := []doctor.Check{{Name: "name\nFAIL forged", Status: doctor.Warn, Detail: "detail\rforged", Remedy: "remedy\x1b[31m"}}
	var out bytes.Buffer
	if got := runDoctorWithDependencies(nil, &out, commandDepsForDoctor(t, checks, nil)); got != exitcode.Advisory {
		t.Fatalf("exit=%d", got)
	}
	text := out.String()
	if strings.Contains(text, "name\nFAIL forged") || strings.Contains(text, "detail\rforged") || strings.Contains(text, "\x1b") {
		t.Fatalf("hostile output forged control text: %q", text)
	}
	if !strings.Contains(text, `name\nFAIL forged`) || !strings.Contains(text, `detail\rforged`) || !strings.Contains(text, `\x1b`) {
		t.Fatalf("hostile output was not visibly escaped: %q", text)
	}
}

func TestDoctorRunsInFullyIsolatedTempRepository(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home")
	state := filepath.Join(base, "state")
	dotfiles := filepath.Join(home, "dotfiles")
	binDir := filepath.Join(home, "bin")
	binary := filepath.Join(binDir, "agents")
	hooksDir := filepath.Join(dotfiles, "git", "hooks.d")
	attrsSource := filepath.Join(dotfiles, "git", "gitattributes")
	globalConfig := filepath.Join(home, ".gitconfig")
	for _, dir := range []string{binDir, hooksDir, filepath.Join(home, ".codex")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = "."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build temp agents: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(binDir, "gitleaks"), []byte("#!/bin/sh\nexit 99\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(attrsSource, []byte(".agents/reports/traces/*.jsonl merge=union\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(attrsSource, filepath.Join(home, ".gitattributes")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"pre-commit", "commit-msg", "post-merge", "post-checkout"} {
		if err := os.Symlink(binary, filepath.Join(hooksDir, name)); err != nil {
			t.Fatal(err)
		}
	}
	config := "[core]\n\thooksPath = " + hooksDir + "\n\tattributesFile = ~/.gitattributes\n"
	if err := os.WriteFile(globalConfig, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Join(base, "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	gitInit := exec.Command("git", "init", "-b", "main")
	gitInit.Dir = repoRoot
	if out, err := gitInit.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	// AGENTS_DOTFILES_ROOT is dropped and never re-added: the child binary is
	// built unstamped, so it must fall through to HOME to find the checkout this
	// fixture laid out. A developer or CI runner with that variable exported
	// would otherwise point the "fully isolated" child at the real checkout, and
	// this test's name would be stating something untrue.
	var environment []string
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if key == "HOME" || key == "XDG_STATE_HOME" || key == "GIT_CONFIG_GLOBAL" ||
			key == "GIT_CONFIG_NOSYSTEM" || key == "GIT_TERMINAL_PROMPT" || key == "PATH" ||
			key == "AGENTS_DOTFILES_ROOT" ||
			key == "GIT_DIR" || key == "GIT_WORK_TREE" || key == "GIT_INDEX_FILE" ||
			key == "GIT_CONFIG_COUNT" || key == "GIT_CONFIG_PARAMETERS" ||
			strings.HasPrefix(key, "GIT_CONFIG_KEY_") || strings.HasPrefix(key, "GIT_CONFIG_VALUE_") {
			continue
		}
		environment = append(environment, item)
	}
	environment = append(environment,
		"HOME="+home,
		"XDG_STATE_HOME="+state,
		"GIT_CONFIG_GLOBAL="+globalConfig,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"PATH="+binDir+":"+os.Getenv("PATH"),
	)
	run := func(args ...string) (int, string) {
		t.Helper()
		cmd := exec.Command(binary, args...)
		cmd.Dir = repoRoot
		cmd.Env = environment
		out, err := cmd.CombinedOutput()
		if err == nil {
			return 0, string(out)
		}
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode(), string(out)
		}
		t.Fatalf("run agents %v: %v\n%s", args, err, out)
		return -1, ""
	}
	if code, out := run("init"); code != exitcode.Advisory {
		t.Fatalf("isolated init exit=%d want=1\n%s", code, out)
	}
	code, out := run("doctor")
	if code != exitcode.Advisory || !strings.Contains(out, "wiring:codex") || !strings.Contains(out, "recording:codex") {
		t.Fatalf("isolated doctor exit=%d want=1\n%s", code, out)
	}
	// This fixture provisions the checkout under its own HOME correctly, so the
	// checks derived from the checkout root must come back ok -- they are exactly
	// the ones the reported bug turned red on a healthy machine.
	//
	// The exit code cannot see them: doctor returns Advisory whenever any check
	// is non-ok, and this fixture always carries warnings (nothing has recorded
	// here yet). Without these assertions the environment sanitizing above is
	// unfalsifiable -- an ambient AGENTS_DOTFILES_ROOT could point the child at
	// the real checkout and every assertion here would still pass.
	for _, name := range []string{"git-hooks:global", "git-hooks:effective", "git-hooks:links"} {
		if status := doctorCheckStatus(out, name); status != "ok" {
			t.Errorf("isolated doctor reported %q for %s, want ok; this fixture is "+
				"provisioned correctly, so any other status means the child resolved "+
				"a checkout other than the one under its own HOME\n%s", status, name, out)
		}
	}
}
