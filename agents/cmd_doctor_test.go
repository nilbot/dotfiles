package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

	var environment []string
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if key == "HOME" || key == "XDG_STATE_HOME" || key == "GIT_CONFIG_GLOBAL" ||
			key == "GIT_CONFIG_NOSYSTEM" || key == "GIT_TERMINAL_PROMPT" || key == "PATH" ||
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
	if code, out := run("doctor"); code != exitcode.Advisory || !strings.Contains(out, "wiring:codex") || !strings.Contains(out, "recording:codex") {
		t.Fatalf("isolated doctor exit=%d want=1\n%s", code, out)
	}
}
