package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type hookInstallFixture struct {
	repoRoot     string
	home         string
	binary       string
	globalConfig string
}

func newHookInstallFixture(t *testing.T) hookInstallFixture {
	t.Helper()
	base := filepath.Join(t.TempDir(), "fixture with spaces")
	fixture := hookInstallFixture{
		repoRoot:     filepath.Join(base, "dotfiles root"),
		home:         filepath.Join(base, "home dir"),
		globalConfig: filepath.Join(base, "global gitconfig"),
	}
	fixture.binary = filepath.Join(fixture.home, "bin", "agents")
	if err := os.MkdirAll(filepath.Join(fixture.repoRoot, "git", "hooks.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(fixture.binary), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.repoRoot, "git", "gitattributes"), []byte(".agents/reports/traces/*.jsonl merge=union\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.binary, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func task18RepoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func isolatedGitEnvironment(home, globalConfig string) []string {
	environment := make([]string, 0, len(os.Environ())+4)
	for _, item := range os.Environ() {
		if strings.HasPrefix(item, "HOME=") || strings.HasPrefix(item, "GIT_CONFIG_GLOBAL=") ||
			strings.HasPrefix(item, "GIT_CONFIG_NOSYSTEM=") || strings.HasPrefix(item, "GIT_TERMINAL_PROMPT=") {
			continue
		}
		environment = append(environment, item)
	}
	return append(environment,
		"HOME="+home,
		"GIT_CONFIG_GLOBAL="+globalConfig,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
	)
}

func isolatedGitEnvironmentWithoutGlobal(home string) []string {
	environment := make([]string, 0, len(os.Environ())+3)
	for _, item := range os.Environ() {
		if strings.HasPrefix(item, "HOME=") || strings.HasPrefix(item, "GIT_CONFIG_GLOBAL=") ||
			strings.HasPrefix(item, "GIT_CONFIG_NOSYSTEM=") || strings.HasPrefix(item, "GIT_TERMINAL_PROMPT=") {
			continue
		}
		environment = append(environment, item)
	}
	return append(environment,
		"HOME="+home,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
	)
}

func runHookInstaller(t *testing.T, fixture hookInstallFixture, mode string) (string, error) {
	t.Helper()
	script := filepath.Join(task18RepoRoot(t), "git", "install-hooks.sh")
	command := exec.Command("bash", script, mode, fixture.repoRoot, fixture.home, fixture.binary)
	command.Env = isolatedGitEnvironment(fixture.home, fixture.globalConfig)
	output, err := command.CombinedOutput()
	return string(output), err
}

func copyPathForHookInstallTest(t *testing.T, source, destination string) {
	t.Helper()
	info, err := os.Lstat(source)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(source)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, destination); err != nil {
			t.Fatal(err)
		}
		return
	}
	if info.IsDir() {
		if err := os.MkdirAll(destination, info.Mode().Perm()); err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(source)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			copyPathForHookInstallTest(t, filepath.Join(source, entry.Name()), filepath.Join(destination, entry.Name()))
		}
		return
	}
	contents, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, contents, info.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
}

func temporaryDotfilesCopy(t *testing.T) string {
	t.Helper()
	sourceRoot := task18RepoRoot(t)
	destinationRoot := filepath.Join(t.TempDir(), "temporary dotfiles with spaces")
	for _, relative := range []string{"Makefile", "agents", "git/install-hooks.sh"} {
		copyPathForHookInstallTest(t, filepath.Join(sourceRoot, relative), filepath.Join(destinationRoot, relative))
	}
	// These files are part of Task 18. Keeping them optional here lets the RED
	// boundary fail on the missing Make target rather than in test setup.
	for _, relative := range []string{"git/gitattributes", "git/hooks.d/.gitignore"} {
		source := filepath.Join(sourceRoot, relative)
		if _, err := os.Lstat(source); err == nil {
			copyPathForHookInstallTest(t, source, filepath.Join(destinationRoot, relative))
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	return destinationRoot
}

func runMakeGitHooks(t *testing.T, root, home, globalConfig string) (string, error) {
	t.Helper()
	command := exec.Command("make", "--no-print-directory", "githooks", "HOME="+home)
	command.Dir = root
	command.Env = isolatedGitEnvironment(home, globalConfig)
	output, err := command.CombinedOutput()
	return string(output), err
}

func TestHookInstallerCleanInstallCreatesExactInactiveStateThenConfiguresGlobalPath(t *testing.T) {
	fixture := newHookInstallFixture(t)
	output, err := runHookInstaller(t, fixture, "install")
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, output)
	}

	wantBinary := fixture.binary
	for _, hook := range []string{"pre-commit", "commit-msg", "post-merge", "post-checkout"} {
		path := filepath.Join(fixture.repoRoot, "git", "hooks.d", hook)
		got, err := os.Readlink(path)
		if err != nil {
			t.Fatalf("read %s: %v", hook, err)
		}
		if got != wantBinary {
			t.Errorf("%s target = %q, want %q", hook, got, wantBinary)
		}
	}
	attrsPath := filepath.Join(fixture.home, ".gitattributes")
	gotAttrs, err := os.Readlink(attrsPath)
	if err != nil {
		t.Fatal(err)
	}
	wantAttrs := filepath.Join(fixture.repoRoot, "git", "gitattributes")
	if gotAttrs != wantAttrs {
		t.Errorf(".gitattributes target = %q, want %q", gotAttrs, wantAttrs)
	}

	command := exec.Command("git", "config", "--global", "--get-all", "core.hooksPath")
	command.Env = isolatedGitEnvironment(fixture.home, fixture.globalConfig)
	configured, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	wantHooksPath := filepath.Join(fixture.repoRoot, "git", "hooks.d") + "\n"
	if string(configured) != wantHooksPath {
		t.Errorf("core.hooksPath = %q, want %q", configured, wantHooksPath)
	}
}

func TestHookInstallerRefusesForeignGlobalBeforeAnyMutation(t *testing.T) {
	fixture := newHookInstallFixture(t)
	if err := os.WriteFile(fixture.globalConfig, []byte("[core]\n\thooksPath = /foreign/hooks\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(fixture.globalConfig)
	if err != nil {
		t.Fatal(err)
	}

	output, installErr := runHookInstaller(t, fixture, "install")
	if installErr == nil {
		t.Fatal("foreign global core.hooksPath was overwritten")
	}
	if !strings.Contains(output, "refusing") || !strings.Contains(output, "core.hooksPath") ||
		!strings.Contains(output, "git config --global --unset-all core.hooksPath") {
		t.Fatalf("refusal is not actionable: %q", output)
	}
	after, err := os.ReadFile(fixture.globalConfig)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("global config changed on refusal:\nbefore=%q\nafter=%q", before, after)
	}
	for _, path := range []string{
		filepath.Join(fixture.home, ".gitattributes"),
		filepath.Join(fixture.repoRoot, "git", "hooks.d", "pre-commit"),
		filepath.Join(fixture.repoRoot, "git", "hooks.d", "commit-msg"),
		filepath.Join(fixture.repoRoot, "git", "hooks.d", "post-merge"),
		filepath.Join(fixture.repoRoot, "git", "hooks.d", "post-checkout"),
	} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Errorf("refusal created %s: %v", path, err)
		}
	}
}

func TestMakeGitHooksBuildsAndInstallsTwiceWithSpaceContainingPaths(t *testing.T) {
	root := temporaryDotfilesCopy(t)
	home := filepath.Join(t.TempDir(), "temporary home with spaces")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	globalConfig := filepath.Join(home, "global config")
	for attempt := 1; attempt <= 2; attempt++ {
		output, err := runMakeGitHooks(t, root, home, globalConfig)
		if err != nil {
			t.Fatalf("make githooks attempt %d failed: %v\n%s", attempt, err, output)
		}
	}

	binary := filepath.Join(home, "bin", "agents")
	if info, err := os.Stat(binary); err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("built agents binary is not executable: %v", err)
	}
	for _, hook := range []string{"pre-commit", "commit-msg", "post-merge", "post-checkout"} {
		got, err := os.Readlink(filepath.Join(root, "git", "hooks.d", hook))
		if err != nil {
			t.Fatal(err)
		}
		if got != binary {
			t.Errorf("%s target = %q, want %q", hook, got, binary)
		}
	}
	gotAttrs, err := os.Readlink(filepath.Join(home, ".gitattributes"))
	if err != nil {
		t.Fatal(err)
	}
	physicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(physicalRoot, "git", "gitattributes"); gotAttrs != want {
		t.Errorf("attributes target = %q, want %q", gotAttrs, want)
	}
	configured := exec.Command("git", "config", "--global", "--get-all", "core.hooksPath")
	configured.Env = isolatedGitEnvironment(home, globalConfig)
	configuredPath, err := configured.Output()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(physicalRoot, "git", "hooks.d") + "\n"; string(configuredPath) != want {
		t.Errorf("make-configured core.hooksPath = %q, want %q", configuredPath, want)
	}
}

func TestMakeGitHooksForeignPreflightRunsBeforeBuildOrLinks(t *testing.T) {
	root := temporaryDotfilesCopy(t)
	home := filepath.Join(t.TempDir(), "foreign preflight home with spaces")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	globalConfig := filepath.Join(home, "global config")
	before := []byte("[core]\n\thooksPath = /preserved/foreign/hooks\n")
	if err := os.WriteFile(globalConfig, before, 0o600); err != nil {
		t.Fatal(err)
	}

	output, err := runMakeGitHooks(t, root, home, globalConfig)
	if err == nil {
		t.Fatal("make githooks overwrote a foreign global path")
	}
	if !strings.Contains(output, "refusing") || !strings.Contains(output, "core.hooksPath") {
		t.Fatalf("make refusal is not actionable: %q", output)
	}
	for _, path := range []string{
		filepath.Join(home, "bin", "agents"),
		filepath.Join(home, ".gitattributes"),
		filepath.Join(root, "git", "hooks.d", "pre-commit"),
		filepath.Join(root, "git", "hooks.d", "commit-msg"),
		filepath.Join(root, "git", "hooks.d", "post-merge"),
		filepath.Join(root, "git", "hooks.d", "post-checkout"),
	} {
		if _, statErr := os.Lstat(path); !os.IsNotExist(statErr) {
			t.Errorf("foreign preflight created %s: %v", path, statErr)
		}
	}
	after, readErr := os.ReadFile(globalConfig)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("foreign global config changed:\nbefore=%q\nafter=%q", before, after)
	}
}

func TestHookInstallerSecondRunPreservesExactInstalledObjectsAndTrackedConfig(t *testing.T) {
	fixture := newHookInstallFixture(t)
	trackedConfig := filepath.Join(fixture.repoRoot, "git", "gitconfig.symlink")
	trackedBytes := []byte("[core]\n\tattributesfile = ~/.gitattributes\n")
	if err := os.WriteFile(trackedConfig, trackedBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	output, err := runHookInstaller(t, fixture, "install")
	if err != nil {
		t.Fatalf("first install failed: %v\n%s", err, output)
	}

	paths := []string{filepath.Join(fixture.home, ".gitattributes")}
	for _, hook := range []string{"pre-commit", "commit-msg", "post-merge", "post-checkout"} {
		paths = append(paths, filepath.Join(fixture.repoRoot, "git", "hooks.d", hook))
	}
	beforeInfo := make(map[string]os.FileInfo, len(paths))
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		beforeInfo[path] = info
	}
	beforeGlobal, err := os.ReadFile(fixture.globalConfig)
	if err != nil {
		t.Fatal(err)
	}

	output, err = runHookInstaller(t, fixture, "install")
	if err != nil {
		t.Fatalf("second install failed: %v\n%s", err, output)
	}
	for _, path := range paths {
		afterInfo, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(beforeInfo[path], afterInfo) {
			t.Errorf("second install replaced %s", path)
		}
	}
	afterGlobal, err := os.ReadFile(fixture.globalConfig)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterGlobal, beforeGlobal) {
		t.Fatalf("second install rewrote global config:\nbefore=%q\nafter=%q", beforeGlobal, afterGlobal)
	}
	afterTracked, err := os.ReadFile(trackedConfig)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterTracked, trackedBytes) {
		t.Fatalf("installer mutated tracked gitconfig: %q", afterTracked)
	}
}

func TestHookInstallerRefusesIncludedOriginAndMultipleGlobalValues(t *testing.T) {
	t.Run("included origin", func(t *testing.T) {
		fixture := newHookInstallFixture(t)
		included := filepath.Join(filepath.Dir(fixture.globalConfig), "included global config")
		hooksPath := filepath.Join(fixture.repoRoot, "git", "hooks.d")
		if err := os.WriteFile(included, []byte("[core]\n\thooksPath = "+hooksPath+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		mainBytes := []byte("[include]\n\tpath = " + included + "\n")
		if err := os.WriteFile(fixture.globalConfig, mainBytes, 0o600); err != nil {
			t.Fatal(err)
		}

		output, err := runHookInstaller(t, fixture, "install")
		if err == nil {
			t.Fatal("included-origin core.hooksPath was accepted")
		}
		if !strings.Contains(output, "refusing") || !strings.Contains(output, "from 'file:"+included+"'") {
			t.Fatalf("included-origin refusal is not exact: %q", output)
		}
		got, readErr := os.ReadFile(fixture.globalConfig)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !bytes.Equal(got, mainBytes) {
			t.Fatalf("main global config changed: %q", got)
		}
	})

	t.Run("multiple values", func(t *testing.T) {
		fixture := newHookInstallFixture(t)
		hooksPath := filepath.Join(fixture.repoRoot, "git", "hooks.d")
		configBytes := []byte("[core]\n\thooksPath = " + hooksPath + "\n\thooksPath = /foreign/hooks\n")
		if err := os.WriteFile(fixture.globalConfig, configBytes, 0o600); err != nil {
			t.Fatal(err)
		}
		output, err := runHookInstaller(t, fixture, "install")
		if err == nil {
			t.Fatal("multiple global core.hooksPath values were accepted")
		}
		if !strings.Contains(output, "multiple values") || !strings.Contains(output, "unset-all") {
			t.Fatalf("multiple-value refusal is not actionable: %q", output)
		}
		got, readErr := os.ReadFile(fixture.globalConfig)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !bytes.Equal(got, configBytes) {
			t.Fatalf("multiple-value config changed: %q", got)
		}
	})
}

func TestHookInstallerRefusesForeignOwnedEntriesAndAttributes(t *testing.T) {
	tests := []struct {
		name      string
		path      func(hookInstallFixture) string
		configure func(*testing.T, string)
	}{
		{
			name: "regular owned hook",
			path: func(f hookInstallFixture) string {
				return filepath.Join(f.repoRoot, "git", "hooks.d", "pre-commit")
			},
			configure: func(t *testing.T, path string) {
				if err := os.WriteFile(path, []byte("foreign hook\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "foreign owned hook symlink",
			path: func(f hookInstallFixture) string {
				return filepath.Join(f.repoRoot, "git", "hooks.d", "commit-msg")
			},
			configure: func(t *testing.T, path string) {
				if err := os.Symlink("/preserved/foreign/hook", path); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "regular attributes file",
			path: func(f hookInstallFixture) string { return filepath.Join(f.home, ".gitattributes") },
			configure: func(t *testing.T, path string) {
				if err := os.WriteFile(path, []byte("preserve this\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "foreign attributes symlink",
			path: func(f hookInstallFixture) string { return filepath.Join(f.home, ".gitattributes") },
			configure: func(t *testing.T, path string) {
				if err := os.Symlink("/preserved/foreign/attributes", path); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newHookInstallFixture(t)
			foreignPath := test.path(fixture)
			test.configure(t, foreignPath)
			before, err := os.Lstat(foreignPath)
			if err != nil {
				t.Fatal(err)
			}

			output, err := runHookInstaller(t, fixture, "install")
			if err == nil {
				t.Fatal("foreign entry was overwritten")
			}
			if !strings.Contains(output, "refusing") || !strings.Contains(output, foreignPath) {
				t.Fatalf("foreign-entry refusal is not actionable: %q", output)
			}
			after, statErr := os.Lstat(foreignPath)
			if statErr != nil {
				t.Fatal(statErr)
			}
			if !os.SameFile(before, after) {
				t.Fatalf("foreign entry was replaced: %s", foreignPath)
			}
			if _, statErr := os.Lstat(fixture.globalConfig); !os.IsNotExist(statErr) {
				t.Fatalf("refusal created global config: %v", statErr)
			}
		})
	}
}

func TestHookInstallerConfigWriteFailureLeavesLinksInactive(t *testing.T) {
	fixture := newHookInstallFixture(t)
	fixture.globalConfig = "/dev/null"
	output, err := runHookInstaller(t, fixture, "install")
	if err == nil {
		t.Fatal("writing global config through /dev/null unexpectedly succeeded")
	}
	if !strings.Contains(output, "could not lock config file") {
		t.Fatalf("unexpected config failure: %q", output)
	}
	for _, path := range []string{
		filepath.Join(fixture.home, ".gitattributes"),
		filepath.Join(fixture.repoRoot, "git", "hooks.d", "pre-commit"),
		filepath.Join(fixture.repoRoot, "git", "hooks.d", "commit-msg"),
		filepath.Join(fixture.repoRoot, "git", "hooks.d", "post-merge"),
		filepath.Join(fixture.repoRoot, "git", "hooks.d", "post-checkout"),
	} {
		if _, statErr := os.Lstat(path); statErr != nil {
			t.Errorf("config was not the final operation; %s is absent: %v", path, statErr)
		}
	}
}

func TestHookInstallerUsesExplicitHomeForGitWhenAmbientHomeDiffers(t *testing.T) {
	fixture := newHookInstallFixture(t)
	ambientHome := filepath.Join(t.TempDir(), "unrelated ambient home")
	if err := os.MkdirAll(ambientHome, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(task18RepoRoot(t), "git", "install-hooks.sh")
	command := exec.Command("bash", script, "install", fixture.repoRoot, fixture.home, fixture.binary)
	command.Env = isolatedGitEnvironmentWithoutGlobal(ambientHome)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("install with an explicit home failed: %v\n%s", err, output)
	}

	configured := exec.Command("git", "config", "--global", "--get-all", "core.hooksPath")
	configured.Env = isolatedGitEnvironmentWithoutGlobal(fixture.home)
	value, err := configured.Output()
	if err != nil {
		t.Fatalf("explicit home was not configured: %v", err)
	}
	want := filepath.Join(fixture.repoRoot, "git", "hooks.d") + "\n"
	if string(value) != want {
		t.Fatalf("explicit-home core.hooksPath = %q, want %q", value, want)
	}
	if _, err := os.Lstat(filepath.Join(ambientHome, ".gitconfig")); !os.IsNotExist(err) {
		t.Fatalf("installer mutated ambient HOME instead of its explicit input: %v", err)
	}
}

func runIsolatedGit(t *testing.T, dir, home, globalConfig string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	command.Env = isolatedGitEnvironment(home, globalConfig)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
	return string(output)
}

func TestTask18HookDirectoryIgnoresMachineLinksButTracksItsIgnoreFile(t *testing.T) {
	sourceRoot := task18RepoRoot(t)
	root := filepath.Join(t.TempDir(), "ignore behavior repo")
	home := filepath.Join(t.TempDir(), "isolated git home")
	if err := os.MkdirAll(filepath.Join(root, "git", "hooks.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	copyPathForHookInstallTest(t,
		filepath.Join(sourceRoot, "git", "hooks.d", ".gitignore"),
		filepath.Join(root, "git", "hooks.d", ".gitignore"))
	for _, hook := range []string{"pre-commit", "commit-msg", "post-merge", "post-checkout"} {
		if err := os.Symlink("/private/machine/path/bin/agents", filepath.Join(root, "git", "hooks.d", hook)); err != nil {
			t.Fatal(err)
		}
	}
	globalConfig := filepath.Join(home, "global config")
	runIsolatedGit(t, root, home, globalConfig, "init", "-q")
	runIsolatedGit(t, root, home, globalConfig, "add", "git/hooks.d")
	tracked := runIsolatedGit(t, root, home, globalConfig, "ls-files", "--cached")
	if tracked != "git/hooks.d/.gitignore\n" {
		t.Fatalf("tracked hook-directory files = %q", tracked)
	}
	status := runIsolatedGit(t, root, home, globalConfig, "status", "--short", "--untracked-files=all")
	if status != "A  git/hooks.d/.gitignore\n" {
		t.Fatalf("machine-specific links leaked into status: %q", status)
	}
}

func TestTask18TrackedAttributesContainOnlyTheGlobalTraceRule(t *testing.T) {
	root := task18RepoRoot(t)
	contents, err := os.ReadFile(filepath.Join(root, "git", "gitattributes"))
	if err != nil {
		t.Fatal(err)
	}
	var rules []string
	for _, line := range strings.Split(string(contents), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			rules = append(rules, line)
		}
	}
	if len(rules) != 1 || rules[0] != ".agents/reports/traces/*.jsonl merge=union" {
		t.Fatalf("global attributes rules = %q", rules)
	}
	if strings.Contains(string(contents), "/Users/") || strings.Contains(string(contents), task18RepoRoot(t)) {
		t.Fatal("tracked global attributes contain a private absolute path")
	}
}

func TestTask18RetiresTemplateAndClaudeHookInstallers(t *testing.T) {
	root := task18RepoRoot(t)
	retired := []string{
		"git/templates/hooks/commit-msg",
		"git/templates/hooks/post-checkout",
		"git/templates/hooks/post-merge",
		"git/templates/hooks/pre-commit",
		"git/templates/hooks/run-hooks.sh",
		"claude/update-repo-hooks.sh",
		"claude/commit-msg",
		"claude/check-commits.sh",
		"claude/setup-protection.sh",
	}
	for _, relative := range retired {
		if _, err := os.Lstat(filepath.Join(root, relative)); !os.IsNotExist(err) {
			t.Errorf("retired artifact still exists: %s (%v)", relative, err)
		}
	}
	for _, relative := range []string{"git/gitconfig.symlink", "claude/CLAUDE.md"} {
		contents, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			t.Fatal(err)
		}
		for _, retiredReference := range []string{
			"init.templatedir", "templatedir =", "git/templates", "update-repo-hooks.sh",
			"check-commits.sh", "setup-protection.sh", "~/.claude/commit-msg",
		} {
			if strings.Contains(string(contents), retiredReference) {
				t.Errorf("%s still advertises %q", relative, retiredReference)
			}
		}
	}
}
