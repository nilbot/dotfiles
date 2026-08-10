package harness

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

const wireFIFOChildRoot = "AGENTS_TEST_WIRE_FIFO_ROOT"
const wireFIFOChildHarness = "AGENTS_TEST_WIRE_FIFO_HARNESS"

func adaptersForWireSafety(t *testing.T) []Adapter {
	t.Helper()
	var adapters []Adapter
	for _, name := range []string{"claude-code", "codex"} {
		a, ok := Get(name)
		if !ok {
			t.Fatalf("adapter %q is not registered", name)
		}
		adapters = append(adapters, a)
	}
	return adapters
}

// Generated config paths are repository-controlled. Following either a leaf
// symlink or a hardlink can rewrite an inode outside the repository while Wire
// still reports success.
func TestWireRefusesLinkedConfigLeavesWithoutExternalMutation(t *testing.T) {
	for _, adapter := range adaptersForWireSafety(t) {
		for _, kind := range []string{"symlink", "hardlink"} {
			t.Run(adapter.Name()+"/"+kind, func(t *testing.T) {
				root := t.TempDir()
				config := adapter.WireConfigPath(root)
				if err := os.MkdirAll(filepath.Dir(config), 0o755); err != nil {
					t.Fatal(err)
				}
				external := filepath.Join(t.TempDir(), "external.json")
				before := []byte("{\"outside\":\"PRIVATE-wire-sentinel\"}\n")
				if err := os.WriteFile(external, before, 0o600); err != nil {
					t.Fatal(err)
				}
				var err error
				if kind == "symlink" {
					err = os.Symlink(external, config)
				} else {
					err = os.Link(external, config)
				}
				if err != nil {
					t.Fatal(err)
				}

				if err := adapter.Wire(root, "/Users/n/bin/agents"); err == nil {
					t.Fatalf("Wire accepted a %s generated config", kind)
				}
				got, err := os.ReadFile(external)
				if err != nil || string(got) != string(before) {
					t.Fatalf("external config changed: bytes=%q err=%v", got, err)
				}
				externalInfo, err := os.Stat(external)
				if err != nil {
					t.Fatal(err)
				}
				configInfo, err := os.Stat(config)
				if err != nil || !os.SameFile(externalInfo, configInfo) {
					t.Fatalf("linked config leaf was replaced: info=%v err=%v", configInfo, err)
				}
			})
		}
	}
}

// The repository root is the trust anchor. A generated harness directory is
// content inside that root and must not redirect config or skills writes.
func TestWireRefusesSymlinkedHarnessDirectoryWithoutExternalMutation(t *testing.T) {
	for _, adapter := range adaptersForWireSafety(t) {
		t.Run(adapter.Name(), func(t *testing.T) {
			root := t.TempDir()
			dirName := filepath.Base(filepath.Dir(adapter.WireConfigPath(root)))
			externalDir := t.TempDir()
			externalConfig := filepath.Join(externalDir, filepath.Base(adapter.WireConfigPath(root)))
			before := []byte("{\"outside\":\"PRIVATE-parent-sentinel\"}\n")
			if err := os.WriteFile(externalConfig, before, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(externalDir, filepath.Join(root, dirName)); err != nil {
				t.Fatal(err)
			}

			if err := adapter.Wire(root, "/Users/n/bin/agents"); err == nil {
				t.Fatal("Wire accepted a symlinked harness directory")
			}
			got, err := os.ReadFile(externalConfig)
			if err != nil || string(got) != string(before) {
				t.Fatalf("external config changed: bytes=%q err=%v", got, err)
			}
			entries, err := os.ReadDir(externalDir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name() != filepath.Base(externalConfig) {
				t.Fatalf("external harness directory changed: %v", entries)
			}
		})
	}
}

// Skills ownership must be decided before publishing generated hook config.
// Otherwise a refused foreign skills leaf still leaves newly active hooks.
func TestWireRefusesForeignSkillsBeforeChangingConfig(t *testing.T) {
	for _, adapter := range adaptersForWireSafety(t) {
		for _, kind := range []string{"regular", "directory", "wrong-symlink"} {
			t.Run(adapter.Name()+"/"+kind, func(t *testing.T) {
				root := t.TempDir()
				config := adapter.WireConfigPath(root)
				parent := filepath.Dir(config)
				if err := os.MkdirAll(parent, 0o755); err != nil {
					t.Fatal(err)
				}
				before := []byte("{\"preserve\":true}\n")
				if err := os.WriteFile(config, before, 0o600); err != nil {
					t.Fatal(err)
				}
				skills := filepath.Join(parent, "skills")
				switch kind {
				case "regular":
					if err := os.WriteFile(skills, []byte("foreign\n"), 0o600); err != nil {
						t.Fatal(err)
					}
				case "directory":
					if err := os.Mkdir(skills, 0o700); err != nil {
						t.Fatal(err)
					}
				case "wrong-symlink":
					if err := os.Symlink("../foreign-skills", skills); err != nil {
						t.Fatal(err)
					}
				}

				if err := adapter.Wire(root, "/Users/n/bin/agents"); err == nil {
					t.Fatalf("Wire accepted %s skills content", kind)
				}
				got, err := os.ReadFile(config)
				if err != nil || string(got) != string(before) {
					t.Fatalf("config changed before skills refusal: bytes=%q err=%v", got, err)
				}
			})
		}
	}
}

func TestWireRefusesNonObjectHookContainersWithoutMutation(t *testing.T) {
	for _, adapter := range adaptersForWireSafety(t) {
		cases := map[string][]byte{
			"null-root":    []byte("null\n"),
			"scalar-hooks": []byte("{\"hooks\":\"foreign\"}\n"),
			"scalar-event": []byte(fmt.Sprintf("{\"hooks\":{%q:\"foreign\"}}\n", adapter.Events()[0].Vendor)),
		}
		for name, before := range cases {
			t.Run(adapter.Name()+"/"+name, func(t *testing.T) {
				root := t.TempDir()
				config := adapter.WireConfigPath(root)
				if err := os.MkdirAll(filepath.Dir(config), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(config, before, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := adapter.Wire(root, "/Users/n/bin/agents"); err == nil {
					t.Fatalf("Wire accepted %s", name)
				}
				got, err := os.ReadFile(config)
				if err != nil || string(got) != string(before) {
					t.Fatalf("rejected config changed: bytes=%q err=%v", got, err)
				}
				if _, err := os.Lstat(filepath.Join(filepath.Dir(config), "skills")); !os.IsNotExist(err) {
					t.Fatalf("rejected config still created skills: %v", err)
				}
			})
		}
	}
}

func TestWireAtomicRewritePreservesPrivateConfigModeAndCleansTemps(t *testing.T) {
	for _, adapter := range adaptersForWireSafety(t) {
		t.Run(adapter.Name(), func(t *testing.T) {
			root := t.TempDir()
			config := adapter.WireConfigPath(root)
			parent := filepath.Dir(config)
			if err := os.MkdirAll(parent, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(config, []byte("{\"preserve\":true}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := adapter.Wire(root, "/Users/n/bin/agents"); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(config)
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("rewritten config mode=%v err=%v, want 0600", info, err)
			}
			entries, err := os.ReadDir(parent)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if len(entry.Name()) >= len(".agents-wire-tmp-") && entry.Name()[:len(".agents-wire-tmp-")] == ".agents-wire-tmp-" {
					t.Fatalf("temporary config leaf leaked after successful wire: %s", entry.Name())
				}
			}
		})
	}
}

func TestWireDoesNotOverwriteConfigThatAppearsDuringPublish(t *testing.T) {
	for _, adapter := range adaptersForWireSafety(t) {
		t.Run(adapter.Name(), func(t *testing.T) {
			root := t.TempDir()
			config := adapter.WireConfigPath(root)
			foreign := []byte("{\"foreign\":\"appeared during wire\"}\n")
			oldBoundary := beforeWirePublish
			beforeWirePublish = func() {
				if err := os.WriteFile(config, foreign, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(func() { beforeWirePublish = oldBoundary })

			if err := adapter.Wire(root, "/Users/n/bin/agents"); err == nil {
				t.Fatal("Wire overwrote a config that appeared during publish")
			}
			got, err := os.ReadFile(config)
			if err != nil || string(got) != string(foreign) {
				t.Fatalf("concurrent config was lost: bytes=%q err=%v", got, err)
			}
			if target, err := os.Readlink(filepath.Join(filepath.Dir(config), "skills")); err != nil || target != filepath.Join("..", ".agents", "skills") {
				t.Fatalf("a failed config publish deleted managed skills: target=%q err=%v", target, err)
			}
		})
	}
}

func TestWireRefusesSkillsReplacementBeforePublishingConfig(t *testing.T) {
	for _, adapter := range adaptersForWireSafety(t) {
		t.Run(adapter.Name(), func(t *testing.T) {
			root := t.TempDir()
			config := adapter.WireConfigPath(root)
			parent := filepath.Dir(config)
			if err := os.MkdirAll(parent, 0o755); err != nil {
				t.Fatal(err)
			}
			before := []byte("{\"preserve\":true}\n")
			if err := os.WriteFile(config, before, 0o600); err != nil {
				t.Fatal(err)
			}
			skills := filepath.Join(parent, "skills")
			if err := os.Symlink(filepath.Join("..", ".agents", "skills"), skills); err != nil {
				t.Fatal(err)
			}
			oldBoundary := beforeWirePublish
			beforeWirePublish = func() {
				if err := os.Remove(skills); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(skills, []byte("foreign skills\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(func() { beforeWirePublish = oldBoundary })

			if err := adapter.Wire(root, "/Users/n/bin/agents"); err == nil {
				t.Fatal("Wire published config after the managed skills link changed")
			}
			got, err := os.ReadFile(config)
			if err != nil || string(got) != string(before) {
				t.Fatalf("config changed after skills replacement: bytes=%q err=%v", got, err)
			}
			got, err = os.ReadFile(skills)
			if err != nil || string(got) != "foreign skills\n" {
				t.Fatalf("foreign skills replacement was removed: bytes=%q err=%v", got, err)
			}
		})
	}
}

func TestConcurrentWireIsRefusedWhileFirstPublishOwnsTheLock(t *testing.T) {
	root := t.TempDir()
	adapter, _ := Get("codex")
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	oldBoundary := beforeWirePublish
	beforeWirePublish = func() {
		once.Do(func() {
			close(entered)
			<-release
		})
	}
	t.Cleanup(func() { beforeWirePublish = oldBoundary })

	firstDone := make(chan error, 1)
	go func() { firstDone <- adapter.Wire(root, "/Users/n/bin/agents") }()
	<-entered

	secondDone := make(chan error, 1)
	go func() { secondDone <- adapter.Wire(root, "/Users/n/bin/agents") }()
	select {
	case err := <-secondDone:
		if err == nil {
			t.Fatal("a concurrent Wire ignored the active per-harness lock")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("a concurrent Wire blocked instead of refusing the active lock")
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first Wire failed after lock release: %v", err)
	}
}

// A plain read of a FIFO blocks before it can discover that the object is not a
// config file. Run the old-behaviour reproducer in a bounded child so the RED is
// deterministic and cannot leak a goroutine or process.
func TestWireRefusesConfigFIFOWithoutBlocking(t *testing.T) {
	if root := os.Getenv(wireFIFOChildRoot); root != "" {
		adapter, ok := Get(os.Getenv(wireFIFOChildHarness))
		if !ok {
			t.Fatal("child adapter is not registered")
		}
		config := adapter.WireConfigPath(root)
		if err := os.MkdirAll(filepath.Dir(config), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Mkfifo(config, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := adapter.Wire(root, "/Users/n/bin/agents"); err == nil {
			t.Fatal("Wire accepted a FIFO generated config")
		}
		return
	}

	for _, adapter := range adaptersForWireSafety(t) {
		t.Run(adapter.Name(), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWireRefusesConfigFIFOWithoutBlocking$")
			cmd.Env = append(os.Environ(),
				wireFIFOChildRoot+"="+t.TempDir(),
				wireFIFOChildHarness+"="+adapter.Name(),
			)
			err := cmd.Run()
			if ctx.Err() == context.DeadlineExceeded {
				t.Fatal("Wire blocked while opening a FIFO generated config")
			}
			if err != nil {
				t.Fatalf("bounded FIFO child failed: %v", err)
			}
		})
	}
}
