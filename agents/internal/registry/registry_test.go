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

// Kills: collapsing an indeterminate stat failure into confirmed absence,
// while pinning intentional .agents directory symlink support.
func TestReconcileDetailedSeparatesUnknownAndFollowsAgentsDirectorySymlink(t *testing.T) {
	presentRoot := t.TempDir()
	sharedAgents := t.TempDir()
	if err := os.Symlink(sharedAgents, filepath.Join(presentRoot, ".agents")); err != nil {
		t.Fatal(err)
	}
	missingRoot := t.TempDir()
	unknownRoot := t.TempDir()
	if err := os.Symlink(".agents", filepath.Join(unknownRoot, ".agents")); err != nil {
		t.Fatal(err)
	}

	r := &Registry{Repos: []Entry{
		{Path: presentRoot, Added: time.Unix(1, 0).UTC()},
		{Path: missingRoot, Added: time.Unix(2, 0).UTC()},
		{Path: unknownRoot, Added: time.Unix(3, 0).UTC()},
	}}
	present, missing, unknown := r.ReconcileDetailed()
	if len(present) != 1 || present[0].Path != presentRoot {
		t.Fatalf("present = %+v, want the shared .agents directory symlink", present)
	}
	if len(missing) != 1 || missing[0].Path != missingRoot {
		t.Fatalf("missing = %+v, want only confirmed absence", missing)
	}
	if len(unknown) != 1 || unknown[0].Path != unknownRoot {
		t.Fatalf("unknown = %+v, want the symlink-loop stat failure", unknown)
	}
	_, compatibilityMissing := r.Reconcile()
	if len(compatibilityMissing) != 1 || compatibilityMissing[0].Path != missingRoot {
		t.Fatalf("compatibility Reconcile collapsed unknown into missing: %+v", compatibilityMissing)
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
					saveErr := (&Registry{Repos: []Entry{{Path: "/work/a", Added: time.Unix(1, 0).UTC()}}}).Save()
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

// Kills: accepting a multiply linked privacy-sensitive leaf. In particular,
// chmodding an accepted lock alias would change an inode outside XDG state,
// while replacing an accepted registry alias would change its link count.
func TestRegistryAndLockRefuseHardlinksWithoutChangingExternalInode(t *testing.T) {
	for _, leaf := range []string{"registry.json", "registry.lock"} {
		t.Run(leaf, func(t *testing.T) {
			state := t.TempDir()
			t.Setenv("XDG_STATE_HOME", state)
			dir := filepath.Join(state, "agents")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			external := filepath.Join(t.TempDir(), "external")
			body := []byte("lock sentinel\n")
			if leaf == "registry.json" {
				body = []byte("{\"repos\":[]}\n")
			}
			if err := os.WriteFile(external, body, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Link(external, filepath.Join(dir, leaf)); err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat(external)
			if err != nil {
				t.Fatal(err)
			}
			beforeStat := before.Sys().(*syscall.Stat_t)

			if leaf == "registry.json" {
				if _, err := Load(); err == nil {
					t.Fatal("Load accepted a hardlinked registry leaf")
				}
				r := &Registry{Repos: []Entry{{Path: "/work/a", Added: time.Unix(1, 0).UTC()}}}
				if err := r.Save(); err == nil {
					t.Fatal("Save accepted a hardlinked registry leaf")
				}
			} else if _, err := Register("/work/a", false); err == nil {
				t.Fatal("Register accepted a hardlinked lock leaf")
			}

			after, err := os.Stat(external)
			if err != nil {
				t.Fatal(err)
			}
			afterStat := after.Sys().(*syscall.Stat_t)
			got, err := os.ReadFile(external)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(body) || after.Mode().Perm() != before.Mode().Perm() || afterStat.Nlink != beforeStat.Nlink {
				t.Fatalf("external inode changed: body=%q mode=%v links=%d; want body=%q mode=%v links=%d", got, after.Mode().Perm(), afterStat.Nlink, body, before.Mode().Perm(), beforeStat.Nlink)
			}
		})
	}
}

// Kills: following the final XDG state directory through a symlink and placing
// the lock, temporary snapshot, or registry in an external directory.
func TestRegistryRefusesFinalStateDirectorySymlinkWithoutChangingTarget(t *testing.T) {
	for _, operation := range []string{"load", "register"} {
		t.Run(operation, func(t *testing.T) {
			state := t.TempDir()
			t.Setenv("XDG_STATE_HOME", state)
			external := t.TempDir()
			sentinel := filepath.Join(external, "sentinel")
			if err := os.WriteFile(sentinel, []byte("do not change\n"), 0o640); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(external, filepath.Join(state, "agents")); err != nil {
				t.Fatal(err)
			}

			var err error
			if operation == "load" {
				_, err = Load()
			} else {
				_, err = Register("/work/a", false)
			}
			if err == nil {
				t.Fatalf("%s accepted a symlinked final state directory", operation)
			}
			entries, readErr := os.ReadDir(external)
			if readErr != nil {
				t.Fatal(readErr)
			}
			got, readErr := os.ReadFile(sentinel)
			if readErr != nil {
				t.Fatal(readErr)
			}
			info, statErr := os.Stat(sentinel)
			if statErr != nil {
				t.Fatal(statErr)
			}
			if len(entries) != 1 || entries[0].Name() != "sentinel" || string(got) != "do not change\n" || info.Mode().Perm() != 0o640 {
				t.Fatalf("external state target changed: entries=%v body=%q mode=%v", entries, got, info.Mode().Perm())
			}
		})
	}
}

func TestRegistryRefusesSpecialFinalStateDirectory(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	stateLeaf := filepath.Join(state, "agents")
	if err := os.WriteFile(stateLeaf, []byte("not a directory\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("Load accepted a regular file as the final state directory")
	}
	if _, err := Register("/work/a", false); err == nil {
		t.Fatal("Register accepted a regular file as the final state directory")
	}
	got, err := os.ReadFile(stateLeaf)
	if err != nil || string(got) != "not a directory\n" {
		t.Fatalf("special state leaf changed: body=%q err=%v", got, err)
	}
}

// A symlink in the configured XDG root is an intentional deployment choice;
// only the final agents directory itself is a protected non-alias boundary.
func TestRegistryAllowsSymlinkedXDGRoot(t *testing.T) {
	realState := t.TempDir()
	parent := t.TempDir()
	alias := filepath.Join(parent, "state")
	if err := os.Symlink(realState, alias); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_STATE_HOME", alias)
	if _, err := Register("/work/a", false); err != nil {
		t.Fatalf("Register rejected a symlinked XDG root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(realState, "agents", "registry.json")); err != nil {
		t.Fatalf("registry not written through configured XDG root: %v", err)
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

// Kills: surfacing a time parser's input in a diagnostic, or accepting a
// registry whose paths can resolve relative to the command cwd, alias another
// entry, or repeat a wire operation.
func TestLoadRejectsMalformedOrUnsafeSchemaWithoutEchoingContents(t *testing.T) {
	const validTime = "1970-01-01T00:00:01Z"
	cases := []struct {
		name string
		body string
	}{
		{"malformed time", `{"repos":[{"path":"/work/a","added":"PRIVATE_TIME","local":false}]}`},
		{"relative path", `{"repos":[{"path":"PRIVATE_RELATIVE/repo","added":"` + validTime + `","local":false}]}`},
		{"unclean path", `{"repos":[{"path":"/work/PRIVATE_UNCLEAN/../repo","added":"` + validTime + `","local":false}]}`},
		{"duplicate path", `{"repos":[{"path":"/work/PRIVATE_DUP","added":"` + validTime + `","local":false},{"path":"/work/PRIVATE_DUP","added":"` + validTime + `","local":true}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := t.TempDir()
			t.Setenv("XDG_STATE_HOME", state)
			path := filepath.Join(state, "agents", "registry.json")
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := Load()
			if err == nil {
				t.Fatal("Load accepted an unsafe registry schema")
			}
			if !strings.Contains(err.Error(), "registry.json") || strings.Contains(err.Error(), "PRIVATE_") || strings.Contains(err.Error(), "\n") {
				t.Fatalf("schema error is not content-free and actionable: %q", err)
			}
		})
	}
}

func TestSaveAndRegisterRejectUnsafePathsBeforeWritingState(t *testing.T) {
	paths := []struct {
		name string
		path string
	}{
		{"relative", "PRIVATE_RELATIVE/repo"},
		{"unclean", "/work/PRIVATE_UNCLEAN/../repo"},
	}
	for _, tc := range paths {
		t.Run(tc.name, func(t *testing.T) {
			state := t.TempDir()
			t.Setenv("XDG_STATE_HOME", state)
			r := &Registry{Repos: []Entry{{Path: tc.path, Added: time.Unix(1, 0).UTC()}}}
			if err := r.Save(); err == nil || strings.Contains(err.Error(), "PRIVATE_") {
				t.Fatalf("Save error = %q, want content-free unsafe-path rejection", err)
			}
			if _, err := Register(tc.path, false); err == nil || strings.Contains(err.Error(), "PRIVATE_") {
				t.Fatalf("Register error = %q, want content-free unsafe-path rejection", err)
			}
			if _, err := os.Lstat(filepath.Join(state, "agents", "registry.json")); !os.IsNotExist(err) {
				t.Fatalf("unsafe path wrote registry state: %v", err)
			}
		})
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
