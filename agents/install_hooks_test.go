package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// answeringStub is a binary that answers the installer's probe the way the real
// one does. Every fixture whose run reaches `install` needs it: the probe runs
// after validate_binary and before the first write, so a stub that exits 0
// silently is refused -- which is the test for §8 row 5.
const answeringStub = `#!/bin/sh
if [ "$1" = githook ] && [ "$2" = --probe ]; then printf 'githook: ok\n'; exit 0; fi
exit 0
`

// silentStub is a binary that runs, exits 0 for everything, and never answers
// `githook`: the shape of an older release whose unknown-command path a wrapper
// has swallowed.
const silentStub = "#!/bin/sh\nexit 0\n"

type hookInstallFixture struct {
	repoRoot     string
	home         string
	binary       string
	globalConfig string
}

func newHookInstallFixture(t *testing.T) hookInstallFixture {
	t.Helper()
	base := filepath.Join(t.TempDir(), "fixture with spaces")
	home := filepath.Join(base, "home dir")
	fixture := hookInstallFixture{
		repoRoot:     filepath.Join(base, "dotfiles root"),
		home:         home,
		globalConfig: filepath.Join(home, ".gitconfig"),
	}
	fixture.binary = filepath.Join(fixture.home, "bin", "agents")
	// The chain directory is deliberately NOT created here: stage 2's installer
	// creates the machine directory itself, and a fixture that had it already
	// would never exercise that.
	if err := os.MkdirAll(filepath.Join(fixture.repoRoot, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(fixture.binary), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.repoRoot, "git", "gitattributes"), []byte(".agents/reports/traces/*.jsonl merge=union\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.binary, []byte(answeringStub), 0o755); err != nil {
		t.Fatal(err)
	}
	return fixture
}

// task18RepoRoot locates the checkout these tests read tracked fixtures from.
// It resolves against packageDir rather than the working directory: TestMain
// moves every test out of the checkout, so `..` would name a temp directory.
func task18RepoRoot(t *testing.T) string {
	t.Helper()
	if packageDir == "" {
		t.Fatal("packageDir is unset; TestMain did not run")
	}
	root, err := filepath.Abs(filepath.Join(packageDir, ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func isolatedGitEnvironment(t *testing.T, home, globalConfig string) []string {
	t.Helper()
	output, err := exec.Command("go", "env", "GOPATH", "GOMODCACHE").Output()
	if err != nil {
		t.Fatalf("resolve Go cache paths: %v", err)
	}
	goPaths := strings.Split(strings.TrimSuffix(string(output), "\n"), "\n")
	if len(goPaths) != 2 || goPaths[0] == "" || goPaths[1] == "" {
		t.Fatalf("unexpected go env output: %q", output)
	}

	environment := make([]string, 0, len(os.Environ())+6)
	for _, item := range os.Environ() {
		if strings.HasPrefix(item, "HOME=") || strings.HasPrefix(item, "GIT_CONFIG_GLOBAL=") ||
			strings.HasPrefix(item, "GIT_CONFIG_NOSYSTEM=") || strings.HasPrefix(item, "GIT_TERMINAL_PROMPT=") ||
			strings.HasPrefix(item, "GOPATH=") || strings.HasPrefix(item, "GOMODCACHE=") {
			continue
		}
		environment = append(environment, item)
	}
	return append(environment,
		"HOME="+home,
		"GIT_CONFIG_GLOBAL="+globalConfig,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GOPATH="+goPaths[0],
		"GOMODCACHE="+goPaths[1],
	)
}

func TestIsolatedGitEnvironmentPinsGoCachePaths(t *testing.T) {
	t.Parallel()
	output, err := exec.Command("go", "env", "GOPATH", "GOMODCACHE").Output()
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Split(strings.TrimSuffix(string(output), "\n"), "\n")
	if len(want) != 2 || want[0] == "" || want[1] == "" {
		t.Fatalf("unexpected go env output: %q", output)
	}

	environment := isolatedGitEnvironment(t, t.TempDir(), filepath.Join(t.TempDir(), ".gitconfig"))
	got := make(map[string]string)
	for _, item := range environment {
		key, value, found := strings.Cut(item, "=")
		if found {
			got[key] = value
		}
	}
	if got["GOPATH"] != want[0] {
		t.Errorf("child GOPATH = %q, want go env value %q", got["GOPATH"], want[0])
	}
	if got["GOMODCACHE"] != want[1] {
		t.Errorf("child GOMODCACHE = %q, want go env value %q", got["GOMODCACHE"], want[1])
	}
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
	return runHookInstallerWithEnvironment(t, fixture, mode, isolatedGitEnvironment(t, fixture.home, fixture.globalConfig))
}

func runHookInstallerWithEnvironment(t *testing.T, fixture hookInstallFixture, mode string, environment []string) (string, error) {
	t.Helper()
	script := filepath.Join(task18RepoRoot(t), "git", "install-hooks.sh")
	command := exec.Command("bash", script, mode, fixture.repoRoot, fixture.home, fixture.binary)
	command.Env = environment
	output, err := command.CombinedOutput()
	return string(output), err
}

func environmentWithHookInstallTestOverrides(environment []string, overrides ...string) []string {
	keys := make(map[string]struct{}, len(overrides))
	for _, override := range overrides {
		key, _, _ := strings.Cut(override, "=")
		keys[key] = struct{}{}
	}
	result := make([]string, 0, len(environment)+len(overrides))
	for _, item := range environment {
		key, _, _ := strings.Cut(item, "=")
		if _, replaced := keys[key]; !replaced {
			result = append(result, item)
		}
	}
	return append(result, overrides...)
}

func runHookInstallerWithTimeout(t *testing.T, fixture hookInstallFixture, mode string) (string, error, bool) {
	t.Helper()
	script := filepath.Join(task18RepoRoot(t), "git", "install-hooks.sh")
	command := exec.Command("bash", script, mode, fixture.repoRoot, fixture.home, fixture.binary)
	command.Env = isolatedGitEnvironment(t, fixture.home, fixture.globalConfig)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Start(); err != nil {
		return output.String(), err, false
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		return output.String(), err, false
	// A watchdog, not a performance assertion: the property is "does not block
	// forever", and these tests run in parallel with each other and with the
	// rest of the package, so the budget has to sit far above a loaded run.
	// Ten seconds is a hang, not a slow machine.
	case <-time.After(10 * time.Second):
		processGroup := -command.Process.Pid
		_ = syscall.Kill(processGroup, syscall.SIGKILL)
		<-done
		if err := syscall.Kill(processGroup, 0); err == nil {
			return output.String(), fmt.Errorf("hook-installer process group %d remains alive after timeout cleanup", -processGroup), true
		} else if err != syscall.ESRCH {
			return output.String(), fmt.Errorf("verify hook-installer process group cleanup: %w", err), true
		}
		return output.String(), nil, true
	}
}

// hookInstallHookNames is the four names the installer manages, in order.
var hookInstallHookNames = []string{"pre-commit", "commit-msg", "post-merge", "post-checkout"}

// hookInstallObservational is the two names that report rather than guard: a
// missing banner there must not fail a `git checkout` or a `git switch`.
var hookInstallObservational = map[string]bool{"post-merge": true, "post-checkout": true}

// chainDir is where the chain lives after stage 2: machine-owned, under the
// home directory, outside every checkout. Nothing else may write there.
func chainDir(fixture hookInstallFixture) string {
	return filepath.Join(fixture.home, ".config", "agents", "hooks.d")
}

func hookEntryPath(fixture hookInstallFixture, hook string) string {
	return filepath.Join(chainDir(fixture), hook)
}

func allHookEntryPaths(fixture hookInstallFixture) []string {
	paths := make([]string, 0, len(hookInstallHookNames))
	for _, hook := range hookInstallHookNames {
		paths = append(paths, hookEntryPath(fixture, hook))
	}
	return paths
}

func chainRecordPath(fixture hookInstallFixture) string {
	return filepath.Join(chainDir(fixture), "chain.env")
}

// ensureChainDir creates the machine chain directory for the cases that put
// something in it BEFORE running the installer -- a fixture link, a foreign
// file, a read-only directory. A fresh install must find the directory missing,
// because creating it is part of stage 2, so only those cases call this.
func ensureChainDir(t *testing.T, fixture hookInstallFixture) {
	t.Helper()
	if err := os.MkdirAll(chainDir(fixture), 0o755); err != nil {
		t.Fatal(err)
	}
}

func hookInstallManagedPaths(fixture hookInstallFixture) []string {
	paths := []string{
		filepath.Join(fixture.home, ".gitattributes"),
		chainRecordPath(fixture),
	}
	for _, hook := range hookInstallHookNames {
		paths = append(paths, hookEntryPath(fixture, hook))
	}
	return paths
}

// chainRecord parses chain.env the way an entry does -- the generated comment
// line, then one key=value per line -- and fails the case on anything the
// installer was not supposed to write.
func chainRecord(t *testing.T, fixture hookInstallFixture) map[string]string {
	t.Helper()
	data, err := os.ReadFile(chainRecordPath(fixture))
	if err != nil {
		t.Fatal(err)
	}
	const header = "# Written by git/install-hooks.sh. Re-run the installer to change it."
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) < 4 || lines[0] != header {
		t.Fatalf("chain.env does not open with the generated comment line:\n%s", data)
	}
	record := make(map[string]string)
	for _, line := range lines[1:] {
		key, value, found := strings.Cut(line, "=")
		if !found {
			t.Fatalf("chain.env line %q is not key=value", line)
		}
		record[key] = value
	}
	if len(record) != 3 {
		t.Fatalf("chain.env carries %d keys, want exactly format, binary and checkout: %v", len(record), record)
	}
	return record
}

// assertEntriesInstalled is the shape a successful install leaves: four
// executable regular files, no symlink among them, each carrying the generated
// header for this checkout and exec'ing githook under its own name, plus a
// record whose three keys are exactly these values.
func assertEntriesInstalled(t *testing.T, fixture hookInstallFixture, wantBinary, wantCheckout string) {
	t.Helper()
	record := chainRecord(t, fixture)
	if record["format"] != "1" {
		t.Errorf("chain.env format = %q, want 1", record["format"])
	}
	if record["binary"] != wantBinary {
		t.Errorf("chain.env binary = %q, want %q", record["binary"], wantBinary)
	}
	if record["checkout"] != wantCheckout {
		t.Errorf("chain.env checkout = %q, want %q", record["checkout"], wantCheckout)
	}
	for _, hook := range hookInstallHookNames {
		path := hookEntryPath(fixture, hook)
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatalf("%s: %v", hook, err)
		}
		if !info.Mode().IsRegular() {
			t.Errorf("%s is not a regular file: mode %v", hook, info.Mode())
		}
		if info.Mode().Perm()&0o111 == 0 {
			t.Errorf("%s is not executable: mode %v", hook, info.Mode())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
		if lines[0] != "#!/bin/sh" {
			t.Errorf("%s line 1 = %q, want #!/bin/sh", hook, lines[0])
		}
		if want := "# Written by git/install-hooks.sh for " + wantCheckout + "."; lines[1] != want {
			t.Errorf("%s line 2 = %q, want %q", hook, lines[1], want)
		}
		if last := lines[len(lines)-1]; !strings.Contains(last, "exec ") ||
			!strings.Contains(last, " githook "+hook+" ") {
			t.Errorf("%s does not exec githook for its own name on the last line: %q", hook, last)
		}
	}
	assertNoHookInstallTemps(t, fixture)
}

// assertNoHookInstallTemps fails when an install left a temporary file behind.
// Every write goes to a temporary name in the hooks directory first, and a
// directory git reads is the last place a half-written hook may sit.
func assertNoHookInstallTemps(t *testing.T, fixture hookInstallFixture) {
	t.Helper()
	entries, err := os.ReadDir(chainDir(fixture))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".install-hooks.") {
			t.Errorf("temporary file left behind: %s", entry.Name())
		}
	}
}

// shellQuote wraps a path for the stub scripts below, so the fixture's
// space-containing paths reach the shell as one word.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// recordingAgentsBinary replaces the fixture's binary with one that appends its
// arguments to a file, and returns that file's path. Running an installed entry
// and reading the log is how these cases see exactly what git would have run.
func recordingAgentsBinary(t *testing.T, fixture hookInstallFixture) string {
	t.Helper()
	log := filepath.Join(t.TempDir(), "agents argv")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = githook ] && [ \"$2\" = --probe ]; then printf 'githook: ok\\n'; exit 0; fi\n" +
		"printf '%s\\n' \"$@\" >> " + shellQuote(log) + "\n"
	if err := os.WriteFile(fixture.binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(fixture.binary, 0o755); err != nil {
		t.Fatal(err)
	}
	return log
}

// runHookEntry executes one installed entry and returns what it printed, which
// is what git shows, and the status git acts on.
func runHookEntry(t *testing.T, fixture hookInstallFixture, hook string, args ...string) (string, int) {
	t.Helper()
	command := exec.Command(hookEntryPath(fixture, hook), args...)
	command.Env = isolatedGitEnvironment(t, fixture.home, fixture.globalConfig)
	output, err := command.CombinedOutput()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatalf("run %s: %v\n%s", hook, err, output)
	}
	return string(output), code
}

// writeChainRecord replaces the record with these bytes, for the cases that
// break it deliberately.
func writeChainRecord(t *testing.T, fixture hookInstallFixture, content string) {
	t.Helper()
	if err := os.WriteFile(chainRecordPath(fixture), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestInstallPreflightNeedsNoBinary is the ordering property preflight exists
// for: it runs BEFORE a binary has been built or installed, so it must not need
// one. The hook names it inspects and the keg guard it applies are string
// comparisons; only install validates the binary it is handed.
func TestInstallPreflightNeedsNoBinary(t *testing.T) {
	t.Parallel()
	fixture := newHookInstallFixture(t)
	if err := os.Remove(fixture.binary); err != nil {
		t.Fatal(err)
	}

	output, err := runHookInstaller(t, fixture, "preflight")
	if err != nil {
		t.Fatalf("preflight needed a binary that does not exist yet: %v\n%s", err, output)
	}
	if !strings.Contains(output, "preflight passed") {
		t.Errorf("preflight did not report success: %q", output)
	}
	assertNoHookInstallManagedPaths(t, fixture)

	output, err = runHookInstaller(t, fixture, "install")
	if err == nil {
		t.Fatal("install accepted a missing binary")
	}
	if !strings.Contains(output, "executable regular file") {
		t.Errorf("the refusal must say what is wrong with the binary: %q", output)
	}
	assertNoHookInstallManagedPaths(t, fixture)
}

// TestInstallLeavesNoTemporaryFileWhenAPublishFails drives the one failure a
// read-only hooks directory can produce: the entry cannot be written to its
// temporary name, so the installer must remove what it started and refuse
// before the record or the global key exists.
func TestInstallLeavesNoTemporaryFileWhenAPublishFails(t *testing.T) {
	t.Parallel()
	fixture := newHookInstallFixture(t)
	ensureChainDir(t, fixture)
	hooksDir := chainDir(fixture)
	if err := os.Chmod(hooksDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(hooksDir, 0o755) })

	output, err := runHookInstaller(t, fixture, "install")
	if err == nil {
		t.Fatal("install into a read-only hooks directory succeeded")
	}
	if !strings.Contains(output, "refusing") || !strings.Contains(output, "pre-commit") {
		t.Errorf("the refusal must name the entry it could not write: %q", output)
	}
	assertNoHookInstallTemps(t, fixture)
	if _, err := os.Lstat(chainRecordPath(fixture)); !os.IsNotExist(err) {
		t.Errorf("the record was written after a failed entry publish: %v", err)
	}
	if _, err := os.Lstat(fixture.globalConfig); !os.IsNotExist(err) {
		t.Errorf("the global key was written after a failed entry publish: %v", err)
	}
}

func assertNoHookInstallManagedPaths(t *testing.T, fixture hookInstallFixture) {
	t.Helper()
	for _, path := range hookInstallManagedPaths(fixture) {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Errorf("refusal created %s: %v", path, err)
		}
	}
}

// assertRefusalWroteNothing is assertNoHookInstallManagedPaths for a fixture
// that put something at a hook name itself: those paths are skipped, because a
// refusal must leave them exactly as it found them -- the test asserted that
// separately -- while everything else the installer owns must not appear.
func assertRefusalWroteNothing(t *testing.T, fixture hookInstallFixture, preexisting ...string) {
	t.Helper()
	skip := make(map[string]bool, len(preexisting))
	for _, path := range preexisting {
		skip[path] = true
	}
	for _, path := range hookInstallManagedPaths(fixture) {
		if skip[path] {
			continue
		}
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Errorf("refusal created %s: %v", path, err)
		}
	}
}

func fileHashForHookInstallTest(t *testing.T, path string) [sha256.Size]byte {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(contents)
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
	// The Makefile is no longer copied: nothing here runs make since the
	// githooks target was retired and this fixture drives install-hooks.sh
	// directly. The two git/ files were optional while they were being
	// introduced; they exist unconditionally now, so a missing one is a broken
	// checkout and should fail loudly in setup.
	for _, relative := range []string{
		"agents", "git/install-hooks.sh", "git/gitattributes",
	} {
		copyPathForHookInstallTest(t, filepath.Join(sourceRoot, relative), filepath.Join(destinationRoot, relative))
	}
	return destinationRoot
}

// runHookSequence performs what the Makefile's githooks target used to: the
// installer's preflight, then the build, then the install.
//
// That target went with the rest of provisioning in spec 2. The SEQUENCE did
// not: bootstrap's devtools phase runs these same three steps in this same
// order (bootstrap.d/internal/phase/devtools.go), because the installer links
// four hook names AT the binary it is handed -- a regular executable, or a
// symlink resolving into a Homebrew keg for agents -- so the build has to
// produce one first. Driving the steps directly attaches the two cases below to
// the ordering property itself rather than to whichever caller happens to run
// it.
func runHookSequence(t *testing.T, root, home, globalConfig string) (string, error) {
	t.Helper()
	// make's $(CURDIR) is the PHYSICAL working directory -- it comes from
	// getcwd() -- so the target used to hand the installer a root with every
	// symlink resolved. On macOS that is the difference between /var/... and
	// /private/var/..., and the installer records what it is given, so a
	// logical path here would write a core.hooksPath that does not match what
	// these cases assert. Resolving once keeps that property with the sequence.
	physicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	root = physicalRoot

	binary := filepath.Join(home, "bin", "agents")
	installer := filepath.Join(root, "git", "install-hooks.sh")
	environment := isolatedGitEnvironment(t, home, globalConfig)

	var combined strings.Builder
	run := func(name string, args ...string) error {
		command := exec.Command(name, args...)
		command.Dir = root
		command.Env = environment
		output, err := command.CombinedOutput()
		combined.Write(output)
		return err
	}

	// Preflight first, and its failure returns before anything is built or
	// linked -- that is the whole subject of the foreign-preflight case.
	if err := run("bash", installer, "preflight", root, home, binary); err != nil {
		return combined.String(), err
	}
	if err := os.MkdirAll(filepath.Dir(binary), 0o755); err != nil {
		return combined.String(), err
	}
	if err := run("go", "build", "-C", filepath.Join(root, "agents"),
		"-trimpath", "-o", binary, "."); err != nil {
		return combined.String(), err
	}
	err = run("bash", installer, "install", root, home, binary)
	return combined.String(), err
}

func TestHookInstallerCleanInstallCreatesExactInactiveStateThenConfiguresGlobalPath(t *testing.T) {
	t.Parallel()
	fixture := newHookInstallFixture(t)
	output, err := runHookInstaller(t, fixture, "install")
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, output)
	}

	wantBinary := fixture.binary
	assertEntriesInstalled(t, fixture, wantBinary, fixture.repoRoot)
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
	command.Env = isolatedGitEnvironment(t, fixture.home, fixture.globalConfig)
	configured, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	wantHooksPath := chainDir(fixture) + "\n"
	if string(configured) != wantHooksPath {
		t.Errorf("core.hooksPath = %q, want %q", configured, wantHooksPath)
	}
}

func TestHookInstallerRefusesRedirectedGlobalConfigBeforeAnyMutation(t *testing.T) {
	t.Parallel()
	fixture := newHookInstallFixture(t)
	trackedSource := filepath.Join(task18RepoRoot(t), "git", "gitconfig.shared")
	trackedContents, err := os.ReadFile(trackedSource)
	if err != nil {
		t.Fatal(err)
	}
	redirectedConfig := filepath.Join(fixture.repoRoot, "git", "copied tracked global config")
	if err := os.WriteFile(redirectedConfig, trackedContents, 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.globalConfig = redirectedConfig
	trackedSourceHash := fileHashForHookInstallTest(t, trackedSource)
	configHash := fileHashForHookInstallTest(t, redirectedConfig)
	binaryHash := fileHashForHookInstallTest(t, fixture.binary)

	output, err := runHookInstaller(t, fixture, "install")
	if err == nil {
		t.Error("redirected GIT_CONFIG_GLOBAL was accepted")
	}
	if !strings.Contains(output, "refusing") || !strings.Contains(output, "GIT_CONFIG_GLOBAL") ||
		!strings.Contains(output, filepath.Join(fixture.home, ".gitconfig")) {
		t.Errorf("redirected-config refusal is not actionable: %q", output)
	}
	if got := fileHashForHookInstallTest(t, redirectedConfig); got != configHash {
		t.Error("redirected global config was modified")
	}
	if got := fileHashForHookInstallTest(t, trackedSource); got != trackedSourceHash {
		t.Error("tracked global config source was modified")
	}
	if got := fileHashForHookInstallTest(t, fixture.binary); got != binaryHash {
		t.Error("agents binary was modified")
	}
	assertNoHookInstallManagedPaths(t, fixture)
}

func TestHookInstallerRefusesSymlinkedPrimaryGlobalConfigBeforeAnyMutation(t *testing.T) {
	t.Parallel()
	fixture := newHookInstallFixture(t)
	fixture.globalConfig = filepath.Join(fixture.home, ".gitconfig")
	sharedConfig := filepath.Join(fixture.repoRoot, "git", "shared tracked global config")
	if err := os.WriteFile(sharedConfig, []byte("[user]\n\tname = Preserved Symlink Target\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sharedConfig, fixture.globalConfig); err != nil {
		t.Fatal(err)
	}
	sharedHash := fileHashForHookInstallTest(t, sharedConfig)
	binaryHash := fileHashForHookInstallTest(t, fixture.binary)

	output, err := runHookInstaller(t, fixture, "install")
	if err == nil {
		t.Error("symlinked primary global config was accepted")
	}
	if !strings.Contains(output, "refusing") || !strings.Contains(output, fixture.globalConfig) {
		t.Errorf("symlinked-config refusal is not actionable: %q", output)
	}
	if target, readErr := os.Readlink(fixture.globalConfig); readErr != nil || target != sharedConfig {
		t.Errorf("primary global symlink changed: target=%q err=%v", target, readErr)
	}
	if got := fileHashForHookInstallTest(t, sharedConfig); got != sharedHash {
		t.Error("symlink target was modified")
	}
	if got := fileHashForHookInstallTest(t, fixture.binary); got != binaryHash {
		t.Error("agents binary was modified")
	}
	assertNoHookInstallManagedPaths(t, fixture)
}

func TestHookInstallerRefusesDanglingPrimaryGlobalConfigSymlinkBeforeAnyMutation(t *testing.T) {
	t.Parallel()
	fixture := newHookInstallFixture(t)
	missingTarget := filepath.Join(fixture.repoRoot, "git", "missing global config target")
	if err := os.Symlink(missingTarget, fixture.globalConfig); err != nil {
		t.Fatal(err)
	}
	binaryHash := fileHashForHookInstallTest(t, fixture.binary)

	output, err := runHookInstaller(t, fixture, "install")
	if err == nil {
		t.Error("dangling primary global config symlink was accepted")
	}
	if !strings.Contains(output, "refusing") || !strings.Contains(output, "non-symlink") {
		t.Errorf("dangling-symlink refusal is not actionable: %q", output)
	}
	if target, readErr := os.Readlink(fixture.globalConfig); readErr != nil || target != missingTarget {
		t.Errorf("dangling primary global symlink changed: target=%q err=%v", target, readErr)
	}
	if _, statErr := os.Lstat(missingTarget); !os.IsNotExist(statErr) {
		t.Errorf("installer created dangling symlink target: %v", statErr)
	}
	if got := fileHashForHookInstallTest(t, fixture.binary); got != binaryHash {
		t.Error("agents binary was modified")
	}
	assertNoHookInstallManagedPaths(t, fixture)
}

func TestHookInstallerRefusesHardLinkedPrimaryGlobalConfigBeforeAnyMutation(t *testing.T) {
	t.Parallel()
	fixture := newHookInstallFixture(t)
	sharedConfig := filepath.Join(fixture.repoRoot, "git", "hard-linked tracked global config")
	if err := os.WriteFile(sharedConfig, []byte("[user]\n\tname = Preserved Hard Link Target\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(sharedConfig, fixture.globalConfig); err != nil {
		t.Fatal(err)
	}
	sharedBefore, err := os.Stat(sharedConfig)
	if err != nil {
		t.Fatal(err)
	}
	configHash := fileHashForHookInstallTest(t, sharedConfig)
	binaryHash := fileHashForHookInstallTest(t, fixture.binary)

	output, installErr := runHookInstaller(t, fixture, "install")
	if installErr == nil {
		t.Error("hard-linked primary global config was accepted")
	}
	if !strings.Contains(output, "refusing") || !strings.Contains(output, "hard links") {
		t.Errorf("hard-linked-config refusal is not actionable: %q", output)
	}
	primaryAfter, statErr := os.Stat(fixture.globalConfig)
	if statErr != nil || !os.SameFile(sharedBefore, primaryAfter) {
		t.Errorf("primary hard link changed: info=%v err=%v", primaryAfter, statErr)
	}
	if got := fileHashForHookInstallTest(t, sharedConfig); got != configHash {
		t.Error("hard-linked shared config was modified")
	}
	if got := fileHashForHookInstallTest(t, fixture.binary); got != binaryHash {
		t.Error("agents binary was modified")
	}
	assertNoHookInstallManagedPaths(t, fixture)
}

func TestHookInstallerRefusesNonRegularPrimaryGlobalConfigBeforeAnyMutation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		configure func(*testing.T, hookInstallFixture) func()
		unchanged func(*testing.T, hookInstallFixture)
	}{
		{
			name: "directory",
			configure: func(t *testing.T, fixture hookInstallFixture) func() {
				if err := os.Mkdir(fixture.globalConfig, 0o700); err != nil {
					t.Fatal(err)
				}
				return func() {}
			},
			unchanged: func(t *testing.T, fixture hookInstallFixture) {
				if info, err := os.Lstat(fixture.globalConfig); err != nil || !info.IsDir() {
					t.Errorf("primary config directory changed: info=%v err=%v", info, err)
				}
			},
		},
		{
			name: "FIFO",
			configure: func(t *testing.T, fixture hookInstallFixture) func() {
				if err := syscall.Mkfifo(fixture.globalConfig, 0o600); err != nil {
					t.Fatal(err)
				}
				return func() {}
			},
			unchanged: func(t *testing.T, fixture hookInstallFixture) {
				if info, err := os.Lstat(fixture.globalConfig); err != nil || info.Mode()&os.ModeNamedPipe == 0 {
					t.Errorf("primary config FIFO changed: info=%v err=%v", info, err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newHookInstallFixture(t)
			cleanup := test.configure(t, fixture)
			defer cleanup()
			binaryHash := fileHashForHookInstallTest(t, fixture.binary)

			output, err, timedOut := runHookInstallerWithTimeout(t, fixture, "install")
			if timedOut {
				t.Error("installer blocked while inspecting a non-regular primary global config")
			}
			if err != nil && timedOut {
				t.Errorf("timed-out installer cleanup failed: %v", err)
			}
			if err == nil {
				t.Error("non-regular primary global config was accepted")
			}
			if !strings.Contains(output, "refusing") || !strings.Contains(output, "regular file") {
				t.Errorf("non-regular-config refusal is not actionable: %q", output)
			}
			test.unchanged(t, fixture)
			if got := fileHashForHookInstallTest(t, fixture.binary); got != binaryHash {
				t.Error("agents binary was modified")
			}
			assertNoHookInstallManagedPaths(t, fixture)
		})
	}
}

func TestHookInstallerRefusesUnwritablePrimaryGlobalConfigBeforeAnyMutation(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("permission check is not meaningful as root")
	}
	fixture := newHookInstallFixture(t)
	configBytes := []byte("[user]\n\tname = Preserved Read Only Config\n")
	if err := os.WriteFile(fixture.globalConfig, configBytes, 0o400); err != nil {
		t.Fatal(err)
	}
	configHash := fileHashForHookInstallTest(t, fixture.globalConfig)
	binaryHash := fileHashForHookInstallTest(t, fixture.binary)

	output, err := runHookInstaller(t, fixture, "install")
	if err == nil {
		t.Error("unwritable primary global config was accepted")
	}
	if !strings.Contains(output, "refusing") || !strings.Contains(output, "not writable") {
		t.Errorf("unwritable-config refusal is not actionable: %q", output)
	}
	if got := fileHashForHookInstallTest(t, fixture.globalConfig); got != configHash {
		t.Error("unwritable primary config was modified")
	}
	if got := fileHashForHookInstallTest(t, fixture.binary); got != binaryHash {
		t.Error("agents binary was modified")
	}
	assertNoHookInstallManagedPaths(t, fixture)
}

func TestHookInstallerRevalidatesBinaryImmediatelyBeforeGlobalConfigWrite(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		replacement string
	}{
		{name: "symlink replacement", replacement: "symlink"},
		{name: "non-executable replacement", replacement: "non-executable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newHookInstallFixture(t)
			realGit, err := exec.LookPath("git")
			if err != nil {
				t.Fatal(err)
			}
			shimDir := filepath.Join(t.TempDir(), "git swap shim bin")
			if err := os.MkdirAll(shimDir, 0o755); err != nil {
				t.Fatal(err)
			}
			replacementTarget := filepath.Join(t.TempDir(), "replacement agents target")
			if err := os.WriteFile(replacementTarget, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			counter := filepath.Join(t.TempDir(), "git invocation count")
			shim := filepath.Join(shimDir, "git")
			shimScript := []byte(`#!/bin/sh
count=0
if [ -f "$TEST_GIT_COUNT" ]; then
  read -r count < "$TEST_GIT_COUNT"
fi
count=$((count + 1))
printf '%s\n' "$count" > "$TEST_GIT_COUNT"
if [ "$count" -eq 2 ]; then
  case "$TEST_SWAP_KIND" in
    symlink)
      rm -f "$TEST_AGENTS_BINARY"
      ln -s "$TEST_SWAP_TARGET" "$TEST_AGENTS_BINARY"
      ;;
    non-executable)
      chmod 0644 "$TEST_AGENTS_BINARY"
      ;;
    *) exit 91 ;;
  esac
fi
exec "$TEST_REAL_GIT" "$@"
`)
			if err := os.WriteFile(shim, shimScript, 0o755); err != nil {
				t.Fatal(err)
			}
			environment := environmentWithHookInstallTestOverrides(
				isolatedGitEnvironment(t, fixture.home, fixture.globalConfig),
				"PATH="+shimDir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"TEST_REAL_GIT="+realGit,
				"TEST_GIT_COUNT="+counter,
				"TEST_SWAP_KIND="+test.replacement,
				"TEST_AGENTS_BINARY="+fixture.binary,
				"TEST_SWAP_TARGET="+replacementTarget,
			)

			output, installErr := runHookInstallerWithEnvironment(t, fixture, "install", environment)
			if installErr == nil {
				t.Error("installer activated hooks after the validated binary was replaced")
			}
			if !strings.Contains(output, "refusing") || !strings.Contains(output, "executable regular file") {
				t.Errorf("binary-replacement refusal is not actionable: %q", output)
			}
			if strings.Contains(output, "installed global Git hooks") {
				t.Errorf("installer printed a false success after binary replacement: %q", output)
			}
			if _, err := os.Lstat(fixture.globalConfig); !os.IsNotExist(err) {
				t.Errorf("binary replacement activated global hooks: %v", err)
			}
		})
	}
}

func TestHookInstallerRefusesInitiallyInvalidBinaryBeforeManagedLinks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		configure func(*testing.T, hookInstallFixture)
	}{
		{
			name: "missing",
			configure: func(t *testing.T, fixture hookInstallFixture) {
				if err := os.Remove(fixture.binary); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "non-executable",
			configure: func(t *testing.T, fixture hookInstallFixture) {
				if err := os.Chmod(fixture.binary, 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "symlink",
			configure: func(t *testing.T, fixture hookInstallFixture) {
				target := filepath.Join(t.TempDir(), "agents target")
				if err := os.WriteFile(target, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(fixture.binary); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, fixture.binary); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newHookInstallFixture(t)
			test.configure(t, fixture)
			output, err := runHookInstaller(t, fixture, "install")
			if err == nil {
				t.Error("initially invalid binary was accepted")
			}
			if !strings.Contains(output, "refusing") || !strings.Contains(output, "executable regular file") {
				t.Errorf("invalid-binary refusal is not actionable: %q", output)
			}
			assertNoHookInstallManagedPaths(t, fixture)
			if _, err := os.Lstat(fixture.globalConfig); !os.IsNotExist(err) {
				t.Errorf("invalid binary activated global hooks: %v", err)
			}
		})
	}
}

func TestHookInstallerRefusesForeignGlobalBeforeAnyMutation(t *testing.T) {
	t.Parallel()
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
		hookEntryPath(fixture, "pre-commit"),
		hookEntryPath(fixture, "commit-msg"),
		hookEntryPath(fixture, "post-merge"),
		hookEntryPath(fixture, "post-checkout"),
	} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Errorf("refusal created %s: %v", path, err)
		}
	}
}

func TestGitHookSequenceBuildsAndInstallsTwiceWithSpaceContainingPaths(t *testing.T) {
	t.Parallel()
	root := temporaryDotfilesCopy(t)
	home := filepath.Join(t.TempDir(), "temporary home with spaces")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	globalConfig := filepath.Join(home, ".gitconfig")
	for attempt := 1; attempt <= 2; attempt++ {
		output, err := runHookSequence(t, root, home, globalConfig)
		if err != nil {
			t.Fatalf("hook sequence attempt %d failed: %v\n%s", attempt, err, output)
		}
	}

	binary := filepath.Join(home, "bin", "agents")
	if info, err := os.Stat(binary); err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("built agents binary is not executable: %v", err)
	}
	physicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	// The installer writes the root it was handed, and runHookSequence hands it
	// a physical path -- a logical one would record a checkout path that does
	// not match the root these assertions resolve.
	assertEntriesInstalled(t,
		hookInstallFixture{repoRoot: root, home: home, binary: binary, globalConfig: globalConfig},
		binary, physicalRoot)
	gotAttrs, err := os.Readlink(filepath.Join(home, ".gitattributes"))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(physicalRoot, "git", "gitattributes"); gotAttrs != want {
		t.Errorf("attributes target = %q, want %q", gotAttrs, want)
	}
	configured := exec.Command("git", "config", "--global", "--get-all", "core.hooksPath")
	configured.Env = isolatedGitEnvironment(t, home, globalConfig)
	configuredPath, err := configured.Output()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".config", "agents", "hooks.d") + "\n"; string(configuredPath) != want {
		t.Errorf("configured core.hooksPath = %q, want %q", configuredPath, want)
	}
}

// Preflight mode is what a caller runs BEFORE spending a build, so its refusal
// has to be complete on its own: it must detect the foreign path and mutate
// nothing, without any later step having run.
//
// This case drives `preflight` directly rather than through a build-and-install
// sequence. Driving the sequence proved less than it appeared to: the caller
// returns on a preflight error before it builds, so "no binary exists
// afterwards" was guaranteed by the caller's control flow and could not fail
// whatever install-hooks.sh did. What the sequence's ordering is worth pinning
// for lives in the phase that now runs it --
// bootstrap.d/internal/phase/devtools_test.go asserts preflight's position
// relative to the build by index.
func TestHookInstallerPreflightRefusesForeignGlobalWithoutMutating(t *testing.T) {
	t.Parallel()
	fixture := newHookInstallFixture(t)
	before := []byte("[core]\n\thooksPath = /preserved/foreign/hooks\n")
	if err := os.WriteFile(fixture.globalConfig, before, 0o600); err != nil {
		t.Fatal(err)
	}

	output, err := runHookInstaller(t, fixture, "preflight")
	if err == nil {
		t.Fatal("preflight passed over a foreign global core.hooksPath, so a " +
			"caller would go on to build and link against it")
	}
	if !strings.Contains(output, "refusing") || !strings.Contains(output, "core.hooksPath") {
		t.Fatalf("refusal is not actionable: %q", output)
	}
	after, readErr := os.ReadFile(fixture.globalConfig)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("foreign global config changed:\nbefore=%q\nafter=%q", before, after)
	}
	assertNoHookInstallManagedPaths(t, fixture)
}

func TestHookInstallerSecondRunPreservesExactInstalledObjectsAndTrackedConfig(t *testing.T) {
	t.Parallel()
	fixture := newHookInstallFixture(t)
	trackedConfig := filepath.Join(fixture.repoRoot, "git", "gitconfig.shared")
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
		paths = append(paths, hookEntryPath(fixture, hook))
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
	t.Parallel()
	t.Run("included origin", func(t *testing.T) {
		fixture := newHookInstallFixture(t)
		included := filepath.Join(filepath.Dir(fixture.globalConfig), "included global config")
		hooksPath := chainDir(fixture)
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
		hooksPath := chainDir(fixture)
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
	t.Parallel()
	tests := []struct {
		name      string
		path      func(hookInstallFixture) string
		configure func(*testing.T, string)
	}{
		{
			name: "regular owned hook",
			path: func(f hookInstallFixture) string {
				return hookEntryPath(f, "pre-commit")
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
				return hookEntryPath(f, "commit-msg")
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
			// The foreign object goes where the installer is about to install,
			// so the machine directory has to exist first.
			ensureChainDir(t, fixture)
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
	t.Parallel()
	fixture := newHookInstallFixture(t)
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	shimDir := filepath.Join(t.TempDir(), "git shim bin")
	if err := os.MkdirAll(shimDir, 0o755); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(shimDir, "git")
	shimScript := []byte("#!/bin/sh\n" +
		"if [ \"$#\" -ge 3 ] && [ \"$1\" = config ] && [ \"$2\" = --global ] && [ \"$3\" = core.hooksPath ]; then\n" +
		"  printf '%s\\n' 'simulated config write failure' >&2\n" +
		"  exit 73\n" +
		"fi\n" +
		"exec \"$TEST_REAL_GIT\" \"$@\"\n")
	if err := os.WriteFile(shim, shimScript, 0o755); err != nil {
		t.Fatal(err)
	}
	environment := environmentWithHookInstallTestOverrides(
		isolatedGitEnvironment(t, fixture.home, fixture.globalConfig),
		"PATH="+shimDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"TEST_REAL_GIT="+realGit,
	)
	output, err := runHookInstallerWithEnvironment(t, fixture, "install", environment)
	if err == nil {
		t.Fatal("simulated final global config write unexpectedly succeeded")
	}
	if !strings.Contains(output, "simulated config write failure") {
		t.Fatalf("unexpected config failure: %q", output)
	}
	for _, path := range []string{
		filepath.Join(fixture.home, ".gitattributes"),
		chainRecordPath(fixture),
		hookEntryPath(fixture, "pre-commit"),
		hookEntryPath(fixture, "commit-msg"),
		hookEntryPath(fixture, "post-merge"),
		hookEntryPath(fixture, "post-checkout"),
	} {
		if _, statErr := os.Lstat(path); statErr != nil {
			t.Errorf("config was not the final operation; %s is absent: %v", path, statErr)
		}
	}
	assertNoHookInstallTemps(t, fixture)
}

func TestHookInstallerUsesExplicitHomeForGitWhenAmbientHomeDiffers(t *testing.T) {
	t.Parallel()
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
	want := chainDir(fixture) + "\n"
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
	command.Env = isolatedGitEnvironment(t, home, globalConfig)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
	return string(output)
}

// The chain is machine-owned now, and this is the property that buys in the
// checkout: an install leaves the repository reporting nothing untracked.
//
// Stage 1 bought it with a tracked git/hooks.d/.gitignore, which stage 2
// retires along with the directory; leaving the four entries there instead
// shows them as four untracked files (measured in a scratch repository), which
// is exactly what this case would catch.
func TestInstallLeavesTheCheckoutClean(t *testing.T) {
	t.Parallel()
	root := temporaryDotfilesCopy(t)
	home := filepath.Join(t.TempDir(), "isolated git home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	globalConfig := filepath.Join(home, ".gitconfig")
	runIsolatedGit(t, root, home, globalConfig, "init", "-q")
	// The identity goes in the FIXTURE's repository, not in the ambient config.
	// GIT_CONFIG_GLOBAL points at the fixture's own home, so the machine running
	// this test supplies nothing -- which is the point, and which failed on CI's
	// Ubuntu runners with `Author identity unknown` until these two lines
	// existed. A test that measured the runner's global config would be
	// measuring the runner.
	runIsolatedGit(t, root, home, globalConfig, "config", "user.email", "t@example.com")
	runIsolatedGit(t, root, home, globalConfig, "config", "user.name", "T")
	runIsolatedGit(t, root, home, globalConfig, "add", "-A")
	runIsolatedGit(t, root, home, globalConfig, "commit", "-qm", "init")

	binary := filepath.Join(home, "bin", "agents")
	if err := os.MkdirAll(filepath.Dir(binary), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte(answeringStub), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(binary, 0o755); err != nil {
		t.Fatal(err)
	}
	fixture := hookInstallFixture{repoRoot: root, home: home, binary: binary, globalConfig: globalConfig}
	output, err := runHookInstallerArgs(t, fixture, "install", root, home, binary)
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, output)
	}

	status := runIsolatedGit(t, root, home, globalConfig, "status", "--short", "--untracked-files=all")
	if status != "" {
		t.Fatalf("the install left the checkout reporting changes:\n%s", status)
	}
	if _, err := os.Lstat(filepath.Join(root, "git", "hooks.d")); !os.IsNotExist(err) {
		t.Errorf("the stage-1 chain directory still exists: %v", err)
	}
	assertEntriesInstalled(t, fixture, binary, root)
}

// The global attributes file carries no rules since the trace merge=union
// attribute retired with the tracked index. It still has to exist and still has
// to be free of private paths: core.attributesFile points at it, and it is
// tracked in a public repository.
func TestTask18TrackedAttributesCarryNoRulesAndNoPrivatePaths(t *testing.T) {
	t.Parallel()
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
	if len(rules) != 0 {
		t.Fatalf("global attributes rules = %q, want none", rules)
	}
	if strings.Contains(string(contents), "/Users/") || strings.Contains(string(contents), task18RepoRoot(t)) {
		t.Fatal("tracked global attributes contain a private absolute path")
	}
}

func TestTask18RetiresTemplateAndClaudeHookInstallers(t *testing.T) {
	t.Parallel()
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
	for _, relative := range []string{"git/gitconfig.shared", "global/AGENTS.md"} {
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

// runHookInstallerArgs drives the installer with an explicit argument list, so
// a case can pass a flag, a different binary path, or both. The four-argument
// form the other cases use stays with runHookInstaller.
func runHookInstallerArgs(t *testing.T, fixture hookInstallFixture, args ...string) (string, error) {
	t.Helper()
	script := filepath.Join(task18RepoRoot(t), "git", "install-hooks.sh")
	command := exec.Command("bash", append([]string{script}, args...)...)
	command.Env = isolatedGitEnvironment(t, fixture.home, fixture.globalConfig)
	output, err := command.CombinedOutput()
	return string(output), err
}

// fakeHomebrewKeg writes an agents binary where Homebrew would, and the stable
// path that it points at, then returns both. The installer matches on these
// names: <prefix>/Cellar/agents/<version>/bin/agents is the version-specific
// keg a package upgrade deletes, and <prefix>/bin/agents is the stable path an
// upgrade repoints, which is why only the second one survives an upgrade.
func fakeHomebrewKeg(t *testing.T, fixture hookInstallFixture, version string) (keg, stable string) {
	t.Helper()
	prefix := filepath.Join(fixture.home, "homebrew")
	keg = filepath.Join(prefix, "Cellar", "agents", version, "bin", "agents")
	if err := os.MkdirAll(filepath.Dir(keg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keg, []byte(answeringStub), 0o755); err != nil {
		t.Fatal(err)
	}
	stable = filepath.Join(prefix, "bin", "agents")
	if err := os.MkdirAll(filepath.Dir(stable), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(keg, stable); err != nil {
		t.Fatal(err)
	}
	return keg, stable
}

// A symlink is accepted when every hop lands in a Homebrew keg for agents, and
// the path RECORDED is the one passed in rather than the keg it resolves to.
// Recording the keg would satisfy the same checks today and break the hooks on
// the next package upgrade, which is the whole reason the stable path is the
// one worth accepting.
func TestHookInstallerAcceptsASymlinkThatResolvesIntoAKeg(t *testing.T) {
	t.Parallel()
	fixture := newHookInstallFixture(t)
	keg, stable := fakeHomebrewKeg(t, fixture, "9.9.9")

	output, err := runHookInstallerArgs(t, fixture, "install", fixture.repoRoot, fixture.home, stable)
	if err != nil {
		t.Fatalf("install with a keg-resolving symlink failed: %v\n%s", err, output)
	}
	assertEntriesInstalled(t, fixture, stable, fixture.repoRoot)
	if info, err := os.Lstat(keg); err != nil || info.Mode()&0o111 == 0 {
		t.Errorf("the keg the installer was handed stopped being executable: %v", err)
	}
	if !strings.Contains(output, "installed global Git hooks") {
		t.Errorf("install did not report success: %q", output)
	}
	// The note exists to point a pinned install at the stable path, so a stable
	// install must not print it.
	if strings.Contains(output, "version-specific keg path") {
		t.Errorf("a stable install warned about pinning: %q", output)
	}
}

// Installing through the keg path is what the previous advice produced, and it
// is what the next upgrade breaks. The refusal has to say so, and it must not
// offer --adopt-owned: adopting here would replace a path that survives
// upgrades with one that does not.
func TestHookInstallerRefusesAKegPathWhenTheStablePathIsInstalled(t *testing.T) {
	t.Parallel()
	fixture := newHookInstallFixture(t)
	keg, stable := fakeHomebrewKeg(t, fixture, "9.9.9")
	if output, err := runHookInstallerArgs(t, fixture, "install", fixture.repoRoot, fixture.home, stable); err != nil {
		t.Fatalf("initial install failed: %v\n%s", err, output)
	}
	before, err := os.Lstat(hookEntryPath(fixture, "pre-commit"))
	if err != nil {
		t.Fatal(err)
	}

	output, err := runHookInstallerArgs(t, fixture, "install", fixture.repoRoot, fixture.home, keg)
	if err == nil {
		t.Fatal("the keg path was accepted over an installed stable path")
	}
	if !strings.Contains(output, "refusing") || !strings.Contains(output, stable) {
		t.Errorf("refusal does not name the stable path to keep: %q", output)
	}
	if strings.Contains(output, "--adopt-owned") {
		t.Errorf("refusal offers a flag that would re-pin the hooks: %q", output)
	}
	if got := chainRecord(t, fixture)["binary"]; got != stable {
		t.Errorf("chain.env binary = %q after a refused run, want %q", got, stable)
	}
	after, err := os.Lstat(hookEntryPath(fixture, "pre-commit"))
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Error("a refused run replaced pre-commit")
	}
}

// The post-upgrade shape: our own links pointing at a keg that no longer
// exists. git runs a dangling hook as if no hook existed, so the commit guard
// is off and nothing says so until this refusal. Default stays strict; the flag
// is what repairs it, and only for links that are ours.
func TestHookInstallerAdoptsOwnedLinksFromAnEarlierBinary(t *testing.T) {
	t.Parallel()
	fixture := newHookInstallFixture(t)
	_, stable := fakeHomebrewKeg(t, fixture, "9.9.9")
	ensureChainDir(t, fixture)
	stale := filepath.Join(fixture.home, "homebrew", "Cellar", "agents", "0.0.1", "bin", "agents")
	for _, hook := range hookInstallHookNames {
		if err := os.Symlink(stale, hookEntryPath(fixture, hook)); err != nil {
			t.Fatal(err)
		}
	}

	output, err := runHookInstallerArgs(t, fixture, "install", fixture.repoRoot, fixture.home, stable)
	if err == nil {
		t.Fatal("stale owned links were adopted without the flag")
	}
	if !strings.Contains(output, "--adopt-owned") {
		t.Errorf("refusal does not name the flag that repairs it: %q", output)
	}
	assertRefusalWroteNothing(t, fixture, allHookEntryPaths(fixture)...)

	output, err = runHookInstallerArgs(t, fixture, "install", "--adopt-owned", fixture.repoRoot, fixture.home, stable)
	if err != nil {
		t.Fatalf("--adopt-owned did not repair stale owned links: %v\n%s", err, output)
	}
	assertEntriesInstalled(t, fixture, stable, fixture.repoRoot)
	for _, hook := range hookInstallHookNames {
		info, err := os.Lstat(hookEntryPath(fixture, hook))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			t.Errorf("%s is still a symlink after adoption", hook)
		}
	}
	if !strings.Contains(output, "replaced pre-commit with a generated entry") {
		t.Errorf("adoption was not reported: %q", output)
	}
}

// Adoption is scoped to links this installer could have written. A link to
// another program stays refused with the flag, and nothing is mutated.
func TestHookInstallerRefusesToAdoptForeignLinks(t *testing.T) {
	t.Parallel()
	fixture := newHookInstallFixture(t)
	_, stable := fakeHomebrewKeg(t, fixture, "9.9.9")
	ensureChainDir(t, fixture)
	foreign := hookEntryPath(fixture, "pre-commit")
	if err := os.Symlink("/preserved/foreign/hook", foreign); err != nil {
		t.Fatal(err)
	}

	output, err := runHookInstallerArgs(t, fixture, "install", "--adopt-owned", fixture.repoRoot, fixture.home, stable)
	if err == nil {
		t.Fatal("--adopt-owned adopted a foreign link")
	}
	if !strings.Contains(output, "refusing") {
		t.Errorf("foreign refusal is not actionable: %q", output)
	}
	target, err := os.Readlink(foreign)
	if err != nil || target != "/preserved/foreign/hook" {
		t.Errorf("foreign link was touched: target=%q err=%v", target, err)
	}
}

// `install --adopt-owned` reads as the verb and its modifier and
// `--adopt-owned install` as the option and the verb. Both are accepted because
// doctor prints the first form, and a remedy the human has to edit first is not
// a remedy.
func TestHookInstallerAcceptsTheAdoptFlagOnEitherSideOfTheMode(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"install", "--adopt-owned"},
		{"--adopt-owned", "install"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			fixture := newHookInstallFixture(t)
			_, stable := fakeHomebrewKeg(t, fixture, "9.9.9")
			ensureChainDir(t, fixture)
			stale := filepath.Join(fixture.home, "homebrew", "Cellar", "agents", "0.0.1", "bin", "agents")
			if err := os.Symlink(stale, hookEntryPath(fixture, "pre-commit")); err != nil {
				t.Fatal(err)
			}

			full := append(append([]string{}, args...), fixture.repoRoot, fixture.home, stable)
			output, err := runHookInstallerArgs(t, fixture, full...)
			if err != nil {
				t.Fatalf("%v failed: %v\n%s", args, err, output)
			}
			assertEntriesInstalled(t, fixture, stable, fixture.repoRoot)
		})
	}
}

// TestInstallConversionTable is §6's table: what the installer does with each
// shape it can find at a hook name, with and without --adopt-owned.
func TestInstallConversionTable(t *testing.T) {
	t.Parallel()

	t.Run("nothing at the hook name", func(t *testing.T) {
		fixture := newHookInstallFixture(t)
		if output, err := runHookInstaller(t, fixture, "install"); err != nil {
			t.Fatalf("install failed: %v\n%s", err, output)
		}
		assertEntriesInstalled(t, fixture, fixture.binary, fixture.repoRoot)
	})

	// The conversion the flag exists to permit. Nothing else turns four
	// symlinks into four entries, so an exact link must not be a success state.
	t.Run("an exact symlink this installer wrote", func(t *testing.T) {
		fixture := newHookInstallFixture(t)
		ensureChainDir(t, fixture)
		for _, hook := range hookInstallHookNames {
			if err := os.Symlink(fixture.binary, hookEntryPath(fixture, hook)); err != nil {
				t.Fatal(err)
			}
		}

		output, err := runHookInstaller(t, fixture, "install")
		if err == nil {
			t.Fatal("four symlinks naming the handed binary converted without --adopt-owned")
		}
		if !strings.Contains(output, "pre-commit") || !strings.Contains(output, "--adopt-owned") {
			t.Errorf("the refusal must name the file and the flag: %q", output)
		}
		assertRefusalWroteNothing(t, fixture, allHookEntryPaths(fixture)...)

		output, err = runHookInstallerArgs(t, fixture, "install", "--adopt-owned",
			fixture.repoRoot, fixture.home, fixture.binary)
		if err != nil {
			t.Fatalf("--adopt-owned did not convert the symlinks: %v\n%s", err, output)
		}
		assertEntriesInstalled(t, fixture, fixture.binary, fixture.repoRoot)
		for _, hook := range hookInstallHookNames {
			info, err := os.Lstat(hookEntryPath(fixture, hook))
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode()&os.ModeSymlink != 0 {
				t.Errorf("%s is still a symlink after conversion", hook)
			}
		}
	})

	t.Run("a symlink to something else", func(t *testing.T) {
		for _, adopt := range []bool{false, true} {
			fixture := newHookInstallFixture(t)
			ensureChainDir(t, fixture)
			foreign := hookEntryPath(fixture, "pre-commit")
			if err := os.Symlink("/preserved/foreign/hook", foreign); err != nil {
				t.Fatal(err)
			}
			args := []string{"install"}
			if adopt {
				args = append(args, "--adopt-owned")
			}
			output, err := runHookInstallerArgs(t, fixture, append(args, fixture.repoRoot, fixture.home, fixture.binary)...)
			if err == nil {
				t.Errorf("adopt=%v: a foreign symlink was accepted", adopt)
			}
			if !strings.Contains(output, foreign) {
				t.Errorf("adopt=%v: the refusal must name the file: %q", adopt, output)
			}
			if target, err := os.Readlink(foreign); err != nil || target != "/preserved/foreign/hook" {
				t.Errorf("adopt=%v: the foreign link changed: target=%q err=%v", adopt, target, err)
			}
			assertRefusalWroteNothing(t, fixture, foreign)
		}
	})

	t.Run("a regular file this installer did not write", func(t *testing.T) {
		for _, adopt := range []bool{false, true} {
			fixture := newHookInstallFixture(t)
			ensureChainDir(t, fixture)
			foreign := hookEntryPath(fixture, "pre-commit")
			if err := os.WriteFile(foreign, []byte("foreign hook\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			args := []string{"install"}
			if adopt {
				args = append(args, "--adopt-owned")
			}
			output, err := runHookInstallerArgs(t, fixture, append(args, fixture.repoRoot, fixture.home, fixture.binary)...)
			if err == nil {
				t.Errorf("adopt=%v: a foreign regular file was accepted", adopt)
			}
			if !strings.Contains(output, foreign) {
				t.Errorf("adopt=%v: the refusal must name the file: %q", adopt, output)
			}
			content, err := os.ReadFile(foreign)
			if err != nil || string(content) != "foreign hook\n" {
				t.Errorf("adopt=%v: the foreign file changed: %q %v", adopt, content, err)
			}
			assertRefusalWroteNothing(t, fixture, foreign)
		}
	})

	// Row 4: an entry that is ours but not what this run would write. It is
	// refused without the flag and replaced with it.
	t.Run("our entry from another checkout", func(t *testing.T) {
		fixture := newHookInstallFixture(t)
		if output, err := runHookInstaller(t, fixture, "install"); err != nil {
			t.Fatalf("install failed: %v\n%s", err, output)
		}
		other := filepath.Join(filepath.Dir(fixture.repoRoot), "another checkout")
		rewritten := strings.Replace(
			"# Written by git/install-hooks.sh for "+fixture.repoRoot+".",
			fixture.repoRoot, other, 1)
		entryPath := hookEntryPath(fixture, "pre-commit")
		data, err := os.ReadFile(entryPath)
		if err != nil {
			t.Fatal(err)
		}
		patched := strings.Replace(string(data),
			"# Written by git/install-hooks.sh for "+fixture.repoRoot+".", rewritten, 1)
		if patched == string(data) {
			t.Fatal("the generated header was not found in the entry")
		}
		if err := os.WriteFile(entryPath, []byte(patched), 0o755); err != nil {
			t.Fatal(err)
		}
		before, err := os.Lstat(entryPath)
		if err != nil {
			t.Fatal(err)
		}

		output, err := runHookInstaller(t, fixture, "install")
		if err == nil {
			t.Fatal("an entry naming another checkout was replaced without --adopt-owned")
		}
		if !strings.Contains(output, entryPath) || !strings.Contains(output, "--adopt-owned") {
			t.Errorf("the refusal must name the file and the flag: %q", output)
		}
		after, err := os.Lstat(entryPath)
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(before, after) {
			t.Error("a refused run replaced the entry")
		}

		if output, err := runHookInstallerArgs(t, fixture, "install", "--adopt-owned",
			fixture.repoRoot, fixture.home, fixture.binary); err != nil {
			t.Fatalf("--adopt-owned did not replace the entry: %v\n%s", err, output)
		}
		assertEntriesInstalled(t, fixture, fixture.binary, fixture.repoRoot)
	})
}

// An installed entry runs the binary the record names, under its own hook name,
// with the checkout from the record and Git's arguments unchanged -- which is
// what commit-msg depends on, since Git hands it the message file's path.
func TestInstallEntriesExecTheRecordedBinaryWithGitArguments(t *testing.T) {
	t.Parallel()
	fixture := newHookInstallFixture(t)
	log := recordingAgentsBinary(t, fixture)
	if output, err := runHookInstaller(t, fixture, "install"); err != nil {
		t.Fatalf("install failed: %v\n%s", err, output)
	}

	for _, hook := range hookInstallHookNames {
		if err := os.Remove(log); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		output, code := runHookEntry(t, fixture, hook, "first arg", "COMMIT_EDITMSG")
		if code != 0 {
			t.Fatalf("%s exited %d:\n%s", hook, code, output)
		}
		data, err := os.ReadFile(log)
		if err != nil {
			t.Fatalf("%s did not run the recorded binary: %v", hook, err)
		}
		got := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
		want := []string{"githook", hook, "--checkout", fixture.repoRoot, "first arg", "COMMIT_EDITMSG"}
		if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
			t.Errorf("%s ran %q, want %q", hook, got, want)
		}
	}
}

// The record is data, not code. A record that is malformed must be refused by
// name, and a line that happens to be a shell command must never run.
func TestInstallRefusesAMalformedRecordWithoutRunningIt(t *testing.T) {
	t.Parallel()
	fixture := newHookInstallFixture(t)
	log := recordingAgentsBinary(t, fixture)
	if output, err := runHookInstaller(t, fixture, "install"); err != nil {
		t.Fatalf("install failed: %v\n%s", err, output)
	}
	valid := "# c\nformat=1\nbinary=" + fixture.binary + "\ncheckout=" + fixture.repoRoot + "\n"

	t.Run("unknown key", func(t *testing.T) {
		writeChainRecord(t, fixture, valid+"pwned=1\n")
		output, code := runHookEntry(t, fixture, "pre-commit")
		if code == 0 {
			t.Errorf("a record with an unknown key was accepted:\n%s", output)
		}
		if !strings.Contains(output, "pwned") {
			t.Errorf("the refusal must name the unknown key: %q", output)
		}
	})

	t.Run("truncated after binary", func(t *testing.T) {
		writeChainRecord(t, fixture, "# c\nformat=1\nbinary="+fixture.binary+"\n")
		output, code := runHookEntry(t, fixture, "pre-commit")
		if code == 0 {
			t.Errorf("a truncated record was accepted:\n%s", output)
		}
		if !strings.Contains(output, "checkout") {
			t.Errorf("the refusal must name the key it could not read: %q", output)
		}
	})

	t.Run("a line that is a shell command", func(t *testing.T) {
		pwned := filepath.Join(t.TempDir(), "pwned")
		writeChainRecord(t, fixture, valid+": > "+pwned+"\n")
		output, code := runHookEntry(t, fixture, "pre-commit")
		if code == 0 {
			t.Errorf("a record line that is a shell command was accepted:\n%s", output)
		}
		if _, err := os.Lstat(pwned); !os.IsNotExist(err) {
			t.Errorf("the record was sourced, not parsed: %s ran", pwned)
		}
	})

	t.Run("a value that is a shell command", func(t *testing.T) {
		pwned := filepath.Join(t.TempDir(), "pwned-value")
		writeChainRecord(t, fixture, "# c\nformat=1\nbinary="+fixture.binary+"; : > "+pwned+"\ncheckout="+fixture.repoRoot+"\n")
		output, code := runHookEntry(t, fixture, "pre-commit")
		if code == 0 {
			t.Errorf("a record value carrying a shell command was accepted:\n%s", output)
		}
		if _, err := os.Lstat(pwned); !os.IsNotExist(err) {
			t.Errorf("a value was interpreted as shell: %s ran", pwned)
		}
	})

	// Nothing above may have reached the binary: every record was broken.
	if _, err := os.Lstat(log); !os.IsNotExist(err) {
		t.Errorf("an entry executed the binary with a malformed record")
	}
}

// The exit status is decided by the hook name and by nothing else. The guard
// names fail closed on EVERY error -- a broken record blocks every commit --
// and the observational names report on every error at exit 0, so a broken
// record never fails a `git checkout` or a `git switch`. Git propagates a
// hook's non-zero status, and `git checkout -b` switches the branch and THEN
// reports failure, so a record the installer wrote badly would break both.
func TestInstallEntryExitStatusFollowsTheHookKindOnEveryError(t *testing.T) {
	t.Parallel()

	// Every error an entry can meet, as bytes in chain.env.
	brokenRecords := map[string]string{
		"a missing binary":        "",
		"an empty record":         "",
		"a truncated record":      "# c\nformat=1\n",
		"a wrong format":          "# c\nformat=2\nbinary=%s\ncheckout=%s\n",
		"an unknown key":          "# c\nformat=1\nbinary=%s\ncheckout=%s\npwned=1\n",
		"a relative binary":       "# c\nformat=1\nbinary=bin/agents\ncheckout=%s\n",
		"a non-absolute checkout": "# c\nformat=1\nbinary=%s\ncheckout=somewhere\n",
	}
	for name, record := range brokenRecords {
		t.Run(name, func(t *testing.T) {
			fixture := newHookInstallFixture(t)
			if output, err := runHookInstaller(t, fixture, "install"); err != nil {
				t.Fatalf("install failed: %v\n%s", err, output)
			}
			if name == "a missing binary" {
				if err := os.Remove(fixture.binary); err != nil {
					t.Fatal(err)
				}
			} else {
				body := record
				if strings.Count(body, "%s") == 2 {
					body = strings.Replace(body, "%s", fixture.binary, 1)
					body = strings.Replace(body, "%s", fixture.repoRoot, 1)
				} else if strings.Count(body, "%s") == 1 {
					body = strings.Replace(body, "%s", fixture.binary, 1)
				}
				writeChainRecord(t, fixture, body)
			}

			for _, hook := range hookInstallHookNames {
				want := 1
				if hookInstallObservational[hook] {
					want = 0
				}
				output, code := runHookEntry(t, fixture, hook)
				if code != want {
					t.Errorf("%s exited %d on %s, want %d:\n%s", hook, code, name, want, output)
				}
				if output == "" {
					t.Errorf("%s said nothing about %s; a loss with no line is the silence this design removes", hook, name)
				}
			}
		})
	}
}

// generatedEntryFor is the text git/install-hooks.sh generates, reproduced here
// so a case can lay out the stage-1 chain the move has to find.
func generatedEntryFor(root, hook string) string {
	return "#!/bin/sh\n" +
		"# Written by git/install-hooks.sh for " + root + ".\n" +
		"# The checkout is read from our own header, so a broken record is still repairable.\n" +
		"exec \"$binary\" githook " + hook + " --checkout \"$checkout\" \"$@\"\n"
}

func generatedRecordFor(binary, checkout string) string {
	return "# Written by git/install-hooks.sh. Re-run the installer to change it.\n" +
		"format=1\nbinary=" + binary + "\ncheckout=" + checkout + "\n"
}

// configureGlobalHooksPath writes the machine-local global config with exactly
// this core.hooksPath value, which is what the installer's own guard reads.
func configureGlobalHooksPath(t *testing.T, fixture hookInstallFixture, hooksPath string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(fixture.globalConfig), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.globalConfig, []byte("[core]\n\thooksPath = "+hooksPath+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func installedGlobalHooksPath(t *testing.T, fixture hookInstallFixture) string {
	t.Helper()
	command := exec.Command("git", "config", "--global", "--get-all", "core.hooksPath")
	command.Env = isolatedGitEnvironment(t, fixture.home, fixture.globalConfig)
	out, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSuffix(string(out), "\n")
}

// The move, end to end. A stage-1 machine's config names the chain inside the
// checkout; the installer must accept that VALUE (the origin test is unchanged),
// write the machine-owned chain, repoint the key, and retire what stage 1 left
// in the checkout -- otherwise the checkout reports four untracked files.
func TestInstallMovesTheChainOutOfTheCheckout(t *testing.T) {
	t.Parallel()
	fixture := newHookInstallFixture(t)
	legacy := filepath.Join(fixture.repoRoot, "git", "hooks.d")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, hook := range hookInstallHookNames {
		path := filepath.Join(legacy, hook)
		if err := os.WriteFile(path, []byte(generatedEntryFor(fixture.repoRoot, hook)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(legacy, "chain.env"),
		[]byte(generatedRecordFor(fixture.binary, fixture.repoRoot)), 0o644); err != nil {
		t.Fatal(err)
	}
	configureGlobalHooksPath(t, fixture, legacy)

	output, err := runHookInstallerArgs(t, fixture, "install", fixture.repoRoot, fixture.home, fixture.binary)
	if err != nil {
		t.Fatalf("the move refused the stage-1 value: %v\n%s", err, output)
	}
	if got := installedGlobalHooksPath(t, fixture); got != chainDir(fixture) {
		t.Errorf("core.hooksPath = %q, want the machine directory %q", got, chainDir(fixture))
	}
	assertEntriesInstalled(t, fixture, fixture.binary, fixture.repoRoot)
	if _, err := os.Lstat(legacy); !os.IsNotExist(err) {
		t.Errorf("the stage-1 chain directory survived the move: %v", err)
	}
}

// Retirement is narrow: it removes what this installer wrote and nothing else.
// A file the human put in the stage-1 directory keeps both its bytes and the
// directory, because the directory is theirs once it holds their file.
func TestInstallRetirementKeepsForeignFilesInTheStageOneChain(t *testing.T) {
	t.Parallel()
	fixture := newHookInstallFixture(t)
	legacy := filepath.Join(fixture.repoRoot, "git", "hooks.d")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, hook := range hookInstallHookNames {
		if err := os.WriteFile(filepath.Join(legacy, hook),
			[]byte(generatedEntryFor(fixture.repoRoot, hook)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(legacy, "chain.env"),
		[]byte(generatedRecordFor(fixture.binary, fixture.repoRoot)), 0o644); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(legacy, "pre-push")
	if err := os.WriteFile(foreign, []byte("#!/bin/sh\necho mine\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	configureGlobalHooksPath(t, fixture, legacy)

	if output, err := runHookInstallerArgs(t, fixture, "install", fixture.repoRoot, fixture.home, fixture.binary); err != nil {
		t.Fatalf("install failed: %v\n%s", err, output)
	}
	if contents, err := os.ReadFile(foreign); err != nil || string(contents) != "#!/bin/sh\necho mine\n" {
		t.Errorf("the foreign file was touched: %q %v", contents, err)
	}
	for _, hook := range hookInstallHookNames {
		if _, err := os.Lstat(filepath.Join(legacy, hook)); !os.IsNotExist(err) {
			t.Errorf("%s was not retired from the stage-1 directory: %v", hook, err)
		}
	}
	assertEntriesInstalled(t, fixture, fixture.binary, fixture.repoRoot)
}

// The machine directory this installer owns must not itself be a symlink: a
// link can be aimed back inside a checkout, which is the property the move
// exists to buy. A missing directory is fine -- install creates it.
func TestInstallRefusesASymlinkedMachineChainDirectory(t *testing.T) {
	t.Parallel()
	fixture := newHookInstallFixture(t)
	if err := os.MkdirAll(filepath.Join(fixture.home, ".config"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(fixture.repoRoot, "git", "elsewhere")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(fixture.home, ".config", "agents")); err != nil {
		t.Fatal(err)
	}

	output, err := runHookInstaller(t, fixture, "install")
	if err == nil {
		t.Fatal("a symlinked chain directory was accepted")
	}
	if !strings.Contains(output, "must not be a symlink") {
		t.Errorf("the refusal must say why a symlink is wrong here: %q", output)
	}
	if _, err := os.Lstat(filepath.Join(target, "chain.env")); !os.IsNotExist(err) {
		t.Errorf("the refusal wrote through the symlink: %v", err)
	}
}

// Preflight runs before anything is written and must leave the machine exactly
// as it found it -- including not creating the chain directory, which is
// install's job.
func TestInstallPreflightCreatesNothing(t *testing.T) {
	t.Parallel()
	fixture := newHookInstallFixture(t)
	output, err := runHookInstaller(t, fixture, "preflight")
	if err != nil {
		t.Fatalf("preflight refused a fresh machine: %v\n%s", err, output)
	}
	if _, err := os.Lstat(chainDir(fixture)); !os.IsNotExist(err) {
		t.Errorf("preflight created the chain directory: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(fixture.home, ".config", "agents")); !os.IsNotExist(err) {
		t.Errorf("preflight created the agents directory: %v", err)
	}
	assertNoHookInstallManagedPaths(t, fixture)
}

// §8 row 5. The installer-first window: a chain written against a binary that
// cannot answer `githook` succeeds here and fails at the first commit, at which
// point every entry fails and `git checkout` and `git switch` fail with them.
// The probe is what turns that into a refusal with nothing written.
func TestInstallRefusesABinaryThatCannotAnswerGithook(t *testing.T) {
	t.Parallel()
	fixture := newHookInstallFixture(t)
	// A release older than the entries: it runs, it exits 0 to a plain
	// invocation, and `githook --probe` is not something it knows.
	if err := os.WriteFile(fixture.binary, []byte(silentStub), 0o755); err != nil {
		t.Fatal(err)
	}

	output, err := runHookInstaller(t, fixture, "install")
	if err == nil {
		t.Fatal("the installer wrote a chain for a binary that cannot run it")
	}
	if !strings.Contains(output, fixture.binary) {
		t.Errorf("the refusal must name the binary it was handed: %q", output)
	}
	if !strings.Contains(output, "githook") || !strings.Contains(output, "v0.8.0") {
		t.Errorf("the refusal must name what is missing and the release that has it: %q", output)
	}
	assertNoHookInstallManagedPaths(t, fixture)
	if _, err := os.Lstat(chainDir(fixture)); !os.IsNotExist(err) {
		t.Errorf("the refusal created the chain directory: %v", err)
	}
	if _, err := os.Lstat(fixture.globalConfig); !os.IsNotExist(err) {
		t.Errorf("the refusal wrote the global config: %v", err)
	}
}

// §8 row 6. The probe must not be reachable from preflight: preflight runs
// before the binary is installed -- on a fresh machine before `brew bundle`
// installs the release -- so a refusal there would stop a run that is about to
// put the right binary in place. The same binary that install refuses passes
// preflight untouched.
func TestInstallPreflightIsBlindToABinaryThatCannotAnswerGithook(t *testing.T) {
	t.Parallel()
	fixture := newHookInstallFixture(t)
	if err := os.WriteFile(fixture.binary, []byte(silentStub), 0o755); err != nil {
		t.Fatal(err)
	}

	output, err := runHookInstaller(t, fixture, "preflight")
	if err != nil {
		t.Fatalf("preflight refused before the binary it needs has been installed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "preflight passed") {
		t.Errorf("preflight did not report success: %q", output)
	}
	assertNoHookInstallManagedPaths(t, fixture)

	// And the very next step, install, is where it stops -- which is the pair
	// of behaviours the two rows pin.
	if output, err := runHookInstaller(t, fixture, "install"); err == nil {
		t.Fatalf("install accepted the same binary preflight was right to ignore:\n%s", output)
	}
}
