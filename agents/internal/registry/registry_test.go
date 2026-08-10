package registry

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Kills: Save writing anywhere other than the injected machine-local state or
// leaving an existing permissive mode in place.
func TestRoundTripUsesPrivateMachineLocalState(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)

	r, err := Load()
	if err != nil {
		t.Fatalf("Load on fresh state: %v", err)
	}
	if !r.Add("/work/a", true) {
		t.Fatal("first Add reported no change")
	}

	path := filepath.Join(state, "agents", "registry.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"repos":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	again, err := Load()
	if err != nil {
		t.Fatalf("Load saved registry: %v", err)
	}
	if len(again.Repos) != 1 || again.Repos[0].Path != "/work/a" || !again.Repos[0].Local {
		t.Fatalf("round trip = %+v", again.Repos)
	}
	if again.Repos[0].Added.Equal(time.Time{}) {
		t.Fatal("Added did not round trip")
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Fatalf("registry mode = %v, want 0600", got)
	}
}

// Kills: treating path deduplication as permission to discard a changed
// --local value, or redefining Added as the most recent registration time.
func TestAddChangedLocalPreservesOriginalAdded(t *testing.T) {
	r := &Registry{}
	if !r.Add("/work/a", false) {
		t.Fatal("first Add reported no change")
	}
	added := r.Repos[0].Added
	if !r.Add("/work/a", true) {
		t.Fatal("changed Local metadata reported no change")
	}
	if !r.Repos[0].Added.Equal(added) {
		t.Fatalf("metadata update changed Added: got %s, want %s", r.Repos[0].Added, added)
	}
	if !r.Repos[0].Local {
		t.Fatal("changed Local metadata was not stored")
	}
}

// Kills: rewriting the cache and first-seen timestamp for an identical re-add.
func TestAddIdenticalEntryReportsNoChange(t *testing.T) {
	added := time.Unix(1, 0).UTC()
	r := &Registry{Repos: []Entry{{Path: "/work/a", Added: added, Local: true}}}
	if r.Add("/work/a", true) {
		t.Fatal("identical Add reported a change")
	}
	if len(r.Repos) != 1 || !r.Repos[0].Added.Equal(added) {
		t.Fatalf("identical Add mutated registry: %+v", r.Repos)
	}
}

// Kills: Reconcile treating a real repository directory as sufficient without
// its .agents truth marker, or following only whether the parent path exists.
func TestReconcileIsOneWayFromRegisteredToPresentOrMissing(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	presentRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(presentRoot, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	missingRoot := t.TempDir()

	r := &Registry{Repos: []Entry{
		{Path: presentRoot, Added: time.Unix(1, 0).UTC()},
		{Path: missingRoot, Added: time.Unix(2, 0).UTC()},
	}}
	present, missing := r.Reconcile()
	if len(present) != 1 || present[0].Path != presentRoot {
		t.Fatalf("present = %+v", present)
	}
	if len(missing) != 1 || missing[0].Path != missingRoot {
		t.Fatalf("missing = %+v", missing)
	}
}

// Kills: publishing the new bytes before the operation that can fail, or
// leaving a privacy-sensitive temporary snapshot behind after an error.
func TestAtomicSaveFailurePreservesLastValidFileAndCleansTemp(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	original := &Registry{Repos: []Entry{{Path: "/work/original", Added: time.Unix(1, 0).UTC()}}}
	if err := original.Save(); err != nil {
		t.Fatal(err)
	}
	path := Path()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	replacement := &Registry{Repos: []Entry{{Path: "/work/replacement", Added: time.Unix(2, 0).UTC()}}}
	wantErr := errors.New("injected rename failure")
	if err := saveUnlocked(replacement, func(string, string) error { return wantErr }); err == nil || !strings.Contains(err.Error(), wantErr.Error()) {
		t.Fatalf("save error = %v, want injected rename failure", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("failed save changed last valid file:\nbefore %s\nafter  %s", before, after)
	}
	temps, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".registry-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(temps) != 0 {
		t.Fatalf("failed save left temporary files: %v", temps)
	}
}

// Kills: following an attacker-controlled registry or lock leaf to a file
// outside the state directory, and blocking forever while opening a FIFO.
func TestRegistryAndLockRefuseSymlinksAndNonRegularLeaves(t *testing.T) {
	for _, leaf := range []string{"registry.json", "registry.lock"} {
		for _, kind := range []string{"symlink", "fifo"} {
			t.Run(leaf+"/"+kind, func(t *testing.T) {
				state := t.TempDir()
				t.Setenv("XDG_STATE_HOME", state)
				dir := filepath.Join(state, "agents")
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(dir, leaf)
				external := filepath.Join(t.TempDir(), "external")
				if err := os.WriteFile(external, []byte("do not change\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if kind == "symlink" {
					if err := os.Symlink(external, path); err != nil {
						t.Fatal(err)
					}
				} else if err := syscall.Mkfifo(path, 0o600); err != nil {
					t.Fatal(err)
				}

				var err error
				if leaf == "registry.json" {
					_, loadErr := Load()
					saveErr := (&Registry{Repos: []Entry{{Path: "/work/a"}}}).Save()
					if loadErr == nil || saveErr == nil {
						t.Fatalf("registry leaf was accepted: Load err=%v Save err=%v", loadErr, saveErr)
					}
					err = loadErr
				} else {
					_, err = Register("/work/a", false)
				}
				if err == nil || !strings.Contains(err.Error(), leaf) {
					t.Fatalf("error = %v, want refusal naming %s", err, leaf)
				}
				got, readErr := os.ReadFile(external)
				if readErr != nil || string(got) != "do not change\n" {
					t.Fatalf("external target changed: bytes=%q err=%v", got, readErr)
				}
			})
		}
	}
}

// Kills: a diagnostic printing an injected control character as a new terminal
// line, or hiding which disposable cache file needs attention.
func TestCorruptRegistryErrorIsSingleLineAndNamesTheCache(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state\nforged")
	t.Setenv("XDG_STATE_HOME", state)
	path := filepath.Join(state, "agents", "registry.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{private-value"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load()
	if err == nil {
		t.Fatal("Load accepted corrupt JSON")
	}
	if !strings.Contains(err.Error(), "registry.json") || strings.Contains(err.Error(), "private-value") {
		t.Fatalf("error is not content-free and actionable: %q", err)
	}
	if strings.Count(err.Error(), "\n") != 0 {
		t.Fatalf("error contains a terminal line break: %q", err)
	}
}

// TestRegistryProcessHelper is only entered by TestConcurrentRegistration.
func TestRegistryProcessHelper(t *testing.T) {
	mode := os.Getenv("AGENTS_REGISTRY_HELPER_MODE")
	if mode == "" {
		return
	}
	path := os.Getenv("AGENTS_REGISTRY_HELPER_PATH")
	switch mode {
	case "hold":
		_, err := Update(func(r *Registry) (bool, error) {
			changed := r.Add(path, false)
			if err := os.WriteFile(os.Getenv("AGENTS_REGISTRY_READY"), []byte("ready"), 0o600); err != nil {
				return false, err
			}
			for {
				if _, err := os.Stat(os.Getenv("AGENTS_REGISTRY_RELEASE")); err == nil {
					break
				}
				time.Sleep(time.Millisecond)
			}
			return changed, nil
		})
		if err != nil {
			t.Fatal(err)
		}
	case "add":
		if err := os.WriteFile(os.Getenv("AGENTS_REGISTRY_STARTED"), []byte("started"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Register(path, true); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unknown helper mode %q", mode)
	}
}

// Kills: locking only the rename while two processes both load the same old
// snapshot, causing whichever Save runs last to erase the other registration.
func TestConcurrentRegistrationDoesNotLoseAnEntry(t *testing.T) {
	state := t.TempDir()
	ready := filepath.Join(t.TempDir(), "ready")
	release := filepath.Join(t.TempDir(), "release")
	started := filepath.Join(t.TempDir(), "started")

	helper := func(mode, path string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=^TestRegistryProcessHelper$")
		cmd.Env = append(os.Environ(),
			"XDG_STATE_HOME="+state,
			"AGENTS_REGISTRY_HELPER_MODE="+mode,
			"AGENTS_REGISTRY_HELPER_PATH="+path,
			"AGENTS_REGISTRY_READY="+ready,
			"AGENTS_REGISTRY_RELEASE="+release,
			"AGENTS_REGISTRY_STARTED="+started,
		)
		return cmd
	}

	first := helper("hold", "/work/first")
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, ready)
	second := helper("add", "/work/second")
	if err := second.Start(); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, started)
	secondDone := make(chan error, 1)
	go func() { secondDone <- second.Wait() }()
	select {
	case err := <-secondDone:
		t.Fatalf("second writer did not block behind the held cross-process lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if err := os.WriteFile(release, []byte("release"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := first.Wait(); err != nil {
		t.Fatalf("first helper: %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second helper: %v", err)
	}

	t.Setenv("XDG_STATE_HOME", state)
	r, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Repos) != 2 || r.Repos[0].Path != "/work/first" || r.Repos[1].Path != "/work/second" {
		t.Fatalf("concurrent registry = %+v, want both entries", r.Repos)
	}
	lockInfo, err := os.Stat(filepath.Join(state, "agents", "registry.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if got := lockInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("lock mode = %v, want 0600", got)
	}
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for helper marker %s", filepath.Base(path))
}
