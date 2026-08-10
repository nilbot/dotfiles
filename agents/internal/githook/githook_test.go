package githook

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

func writeScript(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// Catches accidentally turning every Git hook basename into an agents command,
// which would also leave stdin semantics undefined for hooks we do not install.
func TestIsHookNameRestrictsInstalledHooks(t *testing.T) {
	for _, name := range []string{"pre-commit", "commit-msg", "post-merge", "post-checkout"} {
		if !IsHookName(name) {
			t.Errorf("IsHookName(%q) = false", name)
		}
	}
	for _, name := range []string{"agents", "init", "pre-push", "prepare-commit-msg", ""} {
		if IsHookName(name) {
			t.Errorf("IsHookName(%q) = true", name)
		}
	}
}

// Catches either shadowing a repository's existing hook or depending on
// filesystem enumeration order for personal hooks.
func TestChainRunsRepoThenSortedExtras(t *testing.T) {
	repoHooks := t.TempDir()
	extras := t.TempDir()
	order := filepath.Join(t.TempDir(), "order")
	t.Setenv("HOOK_ORDER", order)
	writeScript(t, filepath.Join(repoHooks, "pre-commit"), `printf 'repo\n' >> "$HOOK_ORDER"`)
	writeScript(t, filepath.Join(extras, "z.pre-commit"), `printf 'extra-z\n' >> "$HOOK_ORDER"`)
	writeScript(t, filepath.Join(extras, "a.pre-commit"), `printf 'extra-a\n' >> "$HOOK_ORDER"`)

	var stdout, stderr bytes.Buffer
	code := Run(Chain{RepoHooksDir: repoHooks, ExtrasDir: extras, SkipBuiltin: true},
		"pre-commit", nil, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run exit = %d; stderr=%q", code, stderr.String())
	}
	got, err := os.ReadFile(order)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "repo\nextra-a\nextra-z\n" {
		t.Fatalf("order = %q", got)
	}
}

// Catches lossy argument reconstruction and filtering Git's hook environment.
func TestChainForwardsExactArgumentsAndGitEnvironment(t *testing.T) {
	repoHooks := t.TempDir()
	extras := t.TempDir()
	repoObserved := filepath.Join(t.TempDir(), "repo-observed")
	extraObserved := filepath.Join(t.TempDir(), "extra-observed")
	t.Setenv("REPO_OBSERVED", repoObserved)
	t.Setenv("EXTRA_OBSERVED", extraObserved)
	t.Setenv("GIT_INDEX_FILE", "/tmp/index with spaces")
	writeScript(t, filepath.Join(repoHooks, "commit-msg"), `
for arg do printf '%s\n' "$arg"; done > "$REPO_OBSERVED"
printf 'GIT_INDEX_FILE=%s\n' "$GIT_INDEX_FILE" >> "$REPO_OBSERVED"`)
	writeScript(t, filepath.Join(extras, "a.commit-msg"), `
for arg do printf '%s\n' "$arg"; done > "$EXTRA_OBSERVED"
printf 'GIT_INDEX_FILE=%s\n' "$GIT_INDEX_FILE" >> "$EXTRA_OBSERVED"`)

	args := []string{".git/message with spaces", "", "HEAD"}
	var stdout, stderr bytes.Buffer
	if code := Run(Chain{RepoHooksDir: repoHooks, ExtrasDir: extras, SkipBuiltin: true}, "commit-msg", args,
		strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("Run exit = %d; stderr=%q", code, stderr.String())
	}
	want := ".git/message with spaces\n\nHEAD\nGIT_INDEX_FILE=/tmp/index with spaces\n"
	for _, path := range []string{repoObserved, extraObserved} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != want {
			t.Fatalf("%s values = %q, want %q", filepath.Base(path), b, want)
		}
	}
}

// Catches treating the first failure as advisory or continuing into a later
// stage after the repository has already refused the operation.
func TestChainStopsAtFirstNonzeroAndPropagatesIt(t *testing.T) {
	extras := t.TempDir()
	observed := filepath.Join(t.TempDir(), "observed")
	t.Setenv("HOOK_OBSERVED", observed)
	writeScript(t, filepath.Join(extras, "a.pre-commit"), `printf 'first\n' >> "$HOOK_OBSERVED"; exit 7`)
	writeScript(t, filepath.Join(extras, "b.pre-commit"), `printf 'second\n' >> "$HOOK_OBSERVED"`)

	var stdout, stderr bytes.Buffer
	code := Run(Chain{ExtrasDir: extras, SkipBuiltin: true}, "pre-commit", nil,
		strings.NewReader(""), &stdout, &stderr)
	if code != 7 {
		t.Fatalf("Run exit = %d, want 7", code)
	}
	if !strings.Contains(stderr.String(), "a.pre-commit") || !strings.Contains(stderr.String(), "exited 7") {
		t.Fatalf("failure diagnostic is not actionable: %q", stderr.String())
	}
	b, err := os.ReadFile(observed)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "first\n" {
		t.Fatalf("later stage ran after failure: %q", b)
	}
}

func TestChainQuotesConfiguredDirectoryEnumerationFailure(t *testing.T) {
	extras := filepath.Join(t.TempDir(), "hooks\nbad")
	if err := os.WriteFile(extras, []byte("not a directory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run(Chain{ExtrasDir: extras, SkipBuiltin: true}, "pre-commit", nil,
		strings.NewReader(""), &stdout, &stderr); code == 0 {
		t.Fatal("configured directory enumeration failure was skipped")
	}
	if strings.Contains(stderr.String(), extras) || !strings.Contains(stderr.String(), `\n`) {
		t.Fatalf("configured path crossed diagnostic boundary unsafely: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "personal hooks directory") {
		t.Fatalf("enumeration diagnostic is not actionable: %q", stderr.String())
	}
}

func TestChainSpawnFailureStopsLaterStages(t *testing.T) {
	extras := t.TempDir()
	observed := filepath.Join(t.TempDir(), "observed")
	t.Setenv("HOOK_OBSERVED", observed)
	if err := os.WriteFile(filepath.Join(extras, "a.post-merge"), []byte("not an executable format\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeScript(t, filepath.Join(extras, "b.post-merge"), `printf 'ran\n' > "$HOOK_OBSERVED"`)

	var stdout, stderr bytes.Buffer
	if code := Run(Chain{ExtrasDir: extras, SkipBuiltin: true}, "post-merge", nil,
		strings.NewReader(""), &stdout, &stderr); code == 0 {
		t.Fatal("spawn failure was reported as success")
	}
	if _, err := os.Stat(observed); !os.IsNotExist(err) {
		t.Fatalf("later stage ran after spawn failure: %v", err)
	}
	if !strings.Contains(stderr.String(), "a.post-merge") {
		t.Fatalf("spawn diagnostic is not actionable: %q", stderr.String())
	}
}

// Catches reintroducing filepath.Glob: '[' in the configured directory is a
// literal path component, not pattern syntax.
func TestChainTreatsMetacharactersInExtrasPathLiterally(t *testing.T) {
	extras := filepath.Join(t.TempDir(), "hooks[personal]")
	observed := filepath.Join(t.TempDir(), "observed")
	t.Setenv("HOOK_OBSERVED", observed)
	writeScript(t, filepath.Join(extras, "a.post-checkout"), `printf 'ran\n' > "$HOOK_OBSERVED"`)

	var stdout, stderr bytes.Buffer
	if code := Run(Chain{ExtrasDir: extras, SkipBuiltin: true}, "post-checkout",
		[]string{"old", "new", "1"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("Run failed for literal metacharacter path: %d, %q", code, stderr.String())
	}
	if b, err := os.ReadFile(observed); err != nil || string(b) != "ran\n" {
		t.Fatalf("literal extras path was not run: %q, %v", b, err)
	}
}

func TestChainPreservesExecutableSymlinksAndSkipsOtherEntries(t *testing.T) {
	extras := t.TempDir()
	observed := filepath.Join(t.TempDir(), "observed")
	t.Setenv("HOOK_OBSERVED", observed)
	target := filepath.Join(t.TempDir(), "target")
	writeScript(t, target, `printf 'symlink\n' >> "$HOOK_OBSERVED"`)
	if err := os.Symlink(target, filepath.Join(extras, "a.post-merge")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extras, "b.post-merge"), []byte("must not run\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(extras, "c.post-merge"), 0o755); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := Run(Chain{ExtrasDir: extras, SkipBuiltin: true}, "post-merge", nil,
		strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("Run exit = %d; stderr=%q", code, stderr.String())
	}
	b, err := os.ReadFile(observed)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Fields(string(b)); !reflect.DeepEqual(got, []string{"symlink"}) {
		t.Fatalf("executed entries = %v", got)
	}
}

func TestChainFailsClosedOnBrokenRepositoryAndPersonalSymlinks(t *testing.T) {
	t.Run("truly absent repository hook", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		if code := Run(Chain{RepoHooksDir: t.TempDir(), SkipBuiltin: true}, "post-merge", nil,
			strings.NewReader(""), &stdout, &stderr); code != 0 {
			t.Fatalf("absent repository hook should be skipped: code=%d stderr=%q", code, stderr.String())
		}
	})

	for _, tc := range []struct {
		name       string
		configure  func(*testing.T, *Chain) string
		diagnostic string
	}{
		{
			name: "repository hook",
			configure: func(t *testing.T, chain *Chain) string {
				chain.RepoHooksDir = filepath.Join(t.TempDir(), "repo\nhooks")
				if err := os.MkdirAll(chain.RepoHooksDir, 0o755); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(chain.RepoHooksDir, "pre-commit")
				if err := os.Symlink("missing-target", path); err != nil {
					t.Fatal(err)
				}
				return path
			},
			diagnostic: "repository hook",
		},
		{
			name: "personal hook",
			configure: func(t *testing.T, chain *Chain) string {
				chain.ExtrasDir = filepath.Join(t.TempDir(), "personal\nhooks")
				if err := os.MkdirAll(chain.ExtrasDir, 0o755); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(chain.ExtrasDir, "a.pre-commit")
				if err := os.Symlink("missing-target", path); err != nil {
					t.Fatal(err)
				}
				return path
			},
			diagnostic: "personal hook",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chain := Chain{SkipBuiltin: true}
			hostilePath := tc.configure(t, &chain)
			var stdout, stderr bytes.Buffer
			if code := Run(chain, "pre-commit", nil, strings.NewReader(""), &stdout, &stderr); code == 0 {
				t.Fatal("broken symlink was silently skipped")
			}
			if strings.Contains(stderr.String(), hostilePath) || !strings.Contains(stderr.String(), `\n`) ||
				!strings.Contains(stderr.String(), tc.diagnostic) {
				t.Fatalf("broken-symlink diagnostic is not actionable and ASCII-quoted: %q", stderr.String())
			}
		})
	}
}

func TestChainRejectsUnknownHookName(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run(Chain{SkipBuiltin: true}, "pre-push", nil,
		strings.NewReader(""), &stdout, &stderr); code == 0 {
		t.Fatal("unsupported hook name was accepted")
	}
}

func canonicalTemplateHook(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("testdata", "retired-"+name)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Catches drift between the exact retired artifacts and the fingerprints used
// to skip them. A broad shell-script match would erase foreign hooks.
func TestRetiredTemplateFingerprintsMatchCanonicalBytes(t *testing.T) {
	cases := []struct {
		name string
		size int
		sum  string
	}{
		{"run-hooks.sh", 428, "e86bca20c7faa344867c0db807c42608ec2963a9adde0a4d9133d57c7d14c43a"},
		{"commit-msg", 659, "b4b2cf4da1231db9a379aee9ca0cf714ff7ca5b66a7acbfbe60d8020354e68b0"},
	}
	for _, tc := range cases {
		b := canonicalTemplateHook(t, tc.name)
		if len(b) != tc.size || fmt.Sprintf("%x", sha256.Sum256(b)) != tc.sum {
			t.Fatalf("canonical %s fingerprint changed: size=%d sha256=%x", tc.name, len(b), sha256.Sum256(b))
		}
	}
}

func TestChainSkipsOnlyExactRetiredTemplateShim(t *testing.T) {
	for _, tc := range []struct {
		name     string
		template string
		symlink  bool
	}{
		{"symlink target", "run-hooks.sh", true},
		{"copied commit-msg", "commit-msg", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repoHooks := t.TempDir()
			home := t.TempDir()
			extras := filepath.Join(home, "dotfiles", "git", "hooks")
			observed := filepath.Join(t.TempDir(), "observed")
			t.Setenv("HOME", home)
			t.Setenv("HOOK_OBSERVED", observed)
			hookName := "pre-commit"
			if tc.template == "commit-msg" {
				hookName = "commit-msg"
			}
			writeScript(t, filepath.Join(extras, "a."+hookName), `printf 'extra\n' >> "$HOOK_OBSERVED"`)
			templateBytes := canonicalTemplateHook(t, tc.template)
			if tc.symlink {
				target := filepath.Join(repoHooks, tc.template)
				if err := os.WriteFile(target, templateBytes, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("./"+tc.template, filepath.Join(repoHooks, hookName)); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(filepath.Join(repoHooks, hookName), templateBytes, 0o755); err != nil {
				t.Fatal(err)
			}

			var stdout, stderr bytes.Buffer
			if code := Run(Chain{RepoHooksDir: repoHooks, ExtrasDir: extras, SkipBuiltin: true},
				hookName, []string{"message"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
				t.Fatalf("Run exit = %d; stderr=%q", code, stderr.String())
			}
			b, err := os.ReadFile(observed)
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != "extra\n" {
				t.Fatalf("retired shim duplicated personal hook: %q", b)
			}
		})
	}
}

// Catches broad recognition that would silently discard a user-modified hook.
func TestOneByteMutatedRetiredShimRunsAsForeign(t *testing.T) {
	repoHooks := t.TempDir()
	home := t.TempDir()
	extras := filepath.Join(home, "dotfiles", "git", "hooks")
	observed := filepath.Join(t.TempDir(), "observed")
	t.Setenv("HOME", home)
	t.Setenv("HOOK_OBSERVED", observed)
	writeScript(t, filepath.Join(extras, "a.pre-commit"), `printf 'extra\n' >> "$HOOK_OBSERVED"`)
	mutated := append(canonicalTemplateHook(t, "run-hooks.sh"), '\n')
	target := filepath.Join(repoHooks, "run-hooks.sh")
	if err := os.WriteFile(target, mutated, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("./run-hooks.sh", filepath.Join(repoHooks, "pre-commit")); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := Run(Chain{RepoHooksDir: repoHooks, ExtrasDir: extras, SkipBuiltin: true},
		"pre-commit", nil, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("Run exit = %d; stderr=%q", code, stderr.String())
	}
	b, err := os.ReadFile(observed)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "extra\nextra\n" {
		t.Fatalf("mutated foreign shim did not run before extras: %q", b)
	}
}

func TestChainRejectsRepositoryHookResolvingToDispatcher(t *testing.T) {
	repoHooks := t.TempDir()
	extras := t.TempDir()
	dispatcher := filepath.Join(t.TempDir(), "agents")
	writeScript(t, dispatcher, "exit 0")
	if err := os.Symlink(dispatcher, filepath.Join(repoHooks, "pre-commit")); err != nil {
		t.Fatal(err)
	}
	observed := filepath.Join(t.TempDir(), "observed")
	t.Setenv("HOOK_OBSERVED", observed)
	writeScript(t, filepath.Join(extras, "a.pre-commit"), `printf 'ran\n' > "$HOOK_OBSERVED"`)

	var stdout, stderr bytes.Buffer
	code := Run(Chain{RepoHooksDir: repoHooks, ExtrasDir: extras, DispatcherPath: dispatcher, SkipBuiltin: true},
		"pre-commit", nil, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatal("self-recursive repository hook was accepted")
	}
	if _, err := os.Stat(observed); !os.IsNotExist(err) {
		t.Fatalf("extras ran after recursion refusal: %v", err)
	}
	if !strings.Contains(stderr.String(), "resolves to the agents dispatcher") {
		t.Fatalf("self-recursion diagnostic is not actionable: %q", stderr.String())
	}
}

func TestStripFootersRemovesOnlyTrailingAIAttributionLines(t *testing.T) {
	in := []byte("feat: preserve intent\n\n" +
		"Co-Authored-By: Claude <noreply@anthropic.com>\n" +
		"That identical line above is quoted body prose and stays.\n\n" +
		"🤖 Generated with [Claude Code](https://claude.com/claude-code)\n\n" +
		"cO-aUtHoReD-bY: cLaUdE <NOREPLY@ANTHROPIC.COM>\n")
	want := []byte("feat: preserve intent\n\n" +
		"Co-Authored-By: Claude <noreply@anthropic.com>\n" +
		"That identical line above is quoted body prose and stays.\n")
	if got := StripFooters(in); !bytes.Equal(got, want) {
		t.Fatalf("StripFooters = %q, want %q", got, want)
	}
}

func TestStripFootersStopsBeforeProseSeparatedFromActualTrailerSuffix(t *testing.T) {
	in := []byte("docs: explain trailer examples\n\n" +
		"Co-Authored-By: Claude <noreply@anthropic.com>\n" +
		"The preceding attribution-looking line is quoted prose.\n\n\n" +
		"Signed-Off-By: A Person <a@example.com>\n" +
		"Co-Authored-By: Claude <noreply@anthropic.com>\n")
	want := []byte("docs: explain trailer examples\n\n" +
		"Co-Authored-By: Claude <noreply@anthropic.com>\n" +
		"The preceding attribution-looking line is quoted prose.\n\n" +
		"Signed-Off-By: A Person <a@example.com>\n")
	if got := StripFooters(in); !bytes.Equal(got, want) {
		t.Fatalf("StripFooters crossed prose boundary: got %q want %q", got, want)
	}
}

func TestStripFootersPreservesOtherTrailers(t *testing.T) {
	in := []byte("fix: retain credit\n\n" +
		"Co-Authored-By: Claude <noreply@anthropic.com>\n" +
		"Co-Authored-By: A Person <a@example.com>\n")
	want := []byte("fix: retain credit\n\nCo-Authored-By: A Person <a@example.com>\n")
	if got := StripFooters(in); !bytes.Equal(got, want) {
		t.Fatalf("StripFooters = %q, want %q", got, want)
	}
}

func TestStripFootersUsesValidTrailerSeparatorAndStopsAtDistinctBlocks(t *testing.T) {
	for _, input := range [][]byte{
		[]byte("subject\nCo-Authored-By: Claude <noreply@anthropic.com>\n"),
		[]byte("subject\r\nCo-Authored-By: Claude <noreply@anthropic.com>"),
	} {
		if got := StripFooters(input); !bytes.Equal(got, input) {
			t.Errorf("no-separator body prose was removed: got %q want %q", got, input)
		}
	}

	in := []byte("docs: preserve prior block\n\n" +
		"Co-Authored-By: Claude <noreply@anthropic.com>\n\n\n" +
		"Signed-Off-By: A Person <a@example.com>\n" +
		"Co-Authored-By: Claude <noreply@anthropic.com>\n")
	want := []byte("docs: preserve prior block\n\n" +
		"Co-Authored-By: Claude <noreply@anthropic.com>\n\n" +
		"Signed-Off-By: A Person <a@example.com>\n")
	if got := StripFooters(in); !bytes.Equal(got, want) {
		t.Fatalf("StripFooters crossed a distinct prior block: got %q want %q", got, want)
	}
}

func TestStripFootersRemovesTrailerBeforeCommentSuffixWithoutTouchingComments(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want []byte
	}{
		{
			name: "editor status comments",
			in: []byte("feat: comments stay\n\n" +
				"🤖 Generated with [Claude Code](https://claude.com/claude-code)\n\n" +
				"Co-Authored-By: Claude <noreply@anthropic.com>\n\n" +
				"# Please enter the commit message.\n# On branch main\n"),
			want: []byte("feat: comments stay\n\n# Please enter the commit message.\n# On branch main\n"),
		},
		{
			name: "scissors suffix",
			in: []byte("fix: scissors stay\n\n" +
				"Co-Authored-By: Claude <noreply@anthropic.com>\n" +
				"# ------------------------ >8 ------------------------\n" +
				"# diff payload remains\n"),
			want: []byte("fix: scissors stay\n# ------------------------ >8 ------------------------\n# diff payload remains\n"),
		},
		{
			name: "CRLF no final newline",
			in: []byte("feat: CRLF\r\n\r\n" +
				"Generated with Claude Code\r\n\r\n" +
				"Co-Authored-By: Claude Code <noreply@anthropic.com>\r\n\r\n" +
				"# status comment\r\n# second comment"),
			want: []byte("feat: CRLF\r\n\r\n# status comment\r\n# second comment"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := StripFooters(tc.in); !bytes.Equal(got, tc.want) {
				t.Fatalf("StripFooters = %q, want %q", got, tc.want)
			}
		})
	}
}

// Catches a normalizer that rewrites every commit message even when there is
// no forbidden trailer, including the important zero-byte message boundary.
func TestStripFootersReturnsUnchangedBytesByteIdentically(t *testing.T) {
	for _, input := range [][]byte{
		nil,
		{},
		[]byte("subject without newline"),
		[]byte("subject\r\n\r\nBody spacing stays.\r\n"),
	} {
		got := StripFooters(input)
		if !bytes.Equal(got, input) {
			t.Errorf("StripFooters(%q) = %q", input, got)
		}
	}
}

func runCommitMsg(t *testing.T, args ...string) (int, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(Chain{}, "commit-msg", args, strings.NewReader(""), &stdout, &stderr)
	if stdout.Len() != 0 {
		t.Fatalf("commit-msg wrote unexpected stdout: %q", stdout.String())
	}
	return code, stderr.String()
}

func TestCommitMsgRequiresAReadableRegularWritableFile(t *testing.T) {
	t.Run("missing argument", func(t *testing.T) {
		if code, stderr := runCommitMsg(t); code == 0 || !strings.Contains(stderr, "message file argument") {
			t.Fatalf("code=%d stderr=%q", code, stderr)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing")
		if code, stderr := runCommitMsg(t, path); code == 0 || !strings.Contains(stderr, "cannot inspect") {
			t.Fatalf("code=%d stderr=%q", code, stderr)
		}
	})

	t.Run("directory", func(t *testing.T) {
		path := t.TempDir()
		if code, stderr := runCommitMsg(t, path); code == 0 || !strings.Contains(stderr, "regular file") {
			t.Fatalf("code=%d stderr=%q", code, stderr)
		}
	})

	t.Run("symlink", func(t *testing.T) {
		target := filepath.Join(t.TempDir(), "target")
		original := []byte("fix: x\n\nCo-Authored-By: Claude <noreply@anthropic.com>\n")
		if err := os.WriteFile(target, original, 0o600); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(t.TempDir(), "message")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if code, stderr := runCommitMsg(t, link); code == 0 || !strings.Contains(stderr, "regular file") {
			t.Fatalf("code=%d stderr=%q", code, stderr)
		}
		if got, err := os.ReadFile(target); err != nil || !bytes.Equal(got, original) {
			t.Fatalf("symlink target changed: %q, %v", got, err)
		}
	})

	t.Run("unreadable", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "message")
		if err := os.WriteFile(path, []byte("fix: x\n"), 0o200); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
		if code, stderr := runCommitMsg(t, path); code == 0 || !strings.Contains(stderr, "cannot read") {
			t.Fatalf("code=%d stderr=%q", code, stderr)
		}
	})

	t.Run("unwritable", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "message")
		original := []byte("fix: x\n\nCo-Authored-By: Claude <noreply@anthropic.com>\n")
		if err := os.WriteFile(path, original, 0o400); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
		if code, stderr := runCommitMsg(t, path); code == 0 || !strings.Contains(stderr, "not writable") {
			t.Fatalf("code=%d stderr=%q", code, stderr)
		}
		if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, original) {
			t.Fatalf("unwritable message changed: %q, %v", got, err)
		}
	})
}

func TestCommitMsgAtomicallyRewritesAndPreservesMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "message")
	original := []byte("feat: x\n\nCo-Authored-By: Claude <noreply@anthropic.com>\n")
	if err := os.WriteFile(path, original, 0o640); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if code, stderr := runCommitMsg(t, path); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) {
		t.Fatal("modified message was rewritten in place instead of atomically replaced")
	}
	if after.Mode().Perm() != 0o640 {
		t.Fatalf("mode=%#o, want 0640", after.Mode().Perm())
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "feat: x\n" {
		t.Fatalf("message=%q err=%v", got, err)
	}
}

func TestCommitMsgLeavesUnchangedFileIdentityAndBytesAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "message")
	original := []byte("feat: ordinary\n\nBody spacing.\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if code, stderr := runCommitMsg(t, path); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("unchanged message file was replaced or rewritten")
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, original) {
		t.Fatalf("unchanged message bytes changed: %q, %v", got, err)
	}
}

func TestCommitMsgRejectsFIFOReplacementBetweenInitialInspectAndOpenWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "message\nname")
	original := []byte("fix: private-a\n\nCo-Authored-By: Claude <noreply@anthropic.com>\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	ready := make(chan struct{})
	var replaceErr error
	assertCommitMsgFIFOReplacementDoesNotBlock(t, path, ready, func(stderr io.Writer) int {
		return stripFootersInFileAtBoundaries(path, stderr, func() {
			if err := os.Remove(path); err != nil {
				replaceErr = err
				return
			}
			if err := syscall.Mkfifo(path, 0o600); err != nil {
				replaceErr = err
				return
			}
			close(ready)
		}, nil)
	}, func() error { return replaceErr })
}

func TestCommitMsgRejectsFIFOReplacementBeforeFinalReopenWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "message\nname")
	original := []byte("fix: private-a\n\nCo-Authored-By: Claude <noreply@anthropic.com>\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	ready := make(chan struct{})
	var replaceErr error
	assertCommitMsgFIFOReplacementDoesNotBlock(t, path, ready, func(stderr io.Writer) int {
		return stripFootersInFileAtBoundaries(path, stderr, nil, func() {
			if err := os.Remove(path); err != nil {
				replaceErr = err
				return
			}
			if err := syscall.Mkfifo(path, 0o600); err != nil {
				replaceErr = err
				return
			}
			close(ready)
		})
	}, func() error { return replaceErr })
}

func assertCommitMsgFIFOReplacementDoesNotBlock(
	t *testing.T,
	path string,
	ready <-chan struct{},
	run func(io.Writer) int,
	replacementError func() error,
) {
	t.Helper()
	type result struct {
		code   int
		stderr string
	}
	done := make(chan result, 1)
	go func() {
		var stderr bytes.Buffer
		done <- result{code: run(&stderr), stderr: stderr.String()}
	}()

	select {
	case <-ready:
	case got := <-done:
		if err := replacementError(); err != nil {
			t.Fatalf("could not install FIFO replacement: %v", err)
		}
		t.Fatalf("commit-msg returned before the FIFO replacement was ready: code=%d stderr=%q", got.code, got.stderr)
	case <-time.After(2 * time.Second):
		t.Fatal("FIFO replacement seam did not complete")
	}

	var got result
	select {
	case got = <-done:
	case <-time.After(250 * time.Millisecond):
		// A blocking read-only FIFO open is released by a writer so the test
		// can fail without leaking its goroutine or leaving an open FIFO.
		writer, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			t.Fatalf("commit-msg blocked and rescue writer could not open FIFO: %v", err)
		}
		_ = writer.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("commit-msg remained blocked after FIFO rescue writer closed")
		}
		t.Fatal("commit-msg blocked while opening a FIFO replacement")
	}

	if got.code == 0 {
		t.Fatalf("FIFO replacement was accepted: stderr=%q", got.stderr)
	}
	if strings.Contains(got.stderr, "private-a") {
		t.Fatalf("FIFO replacement diagnostic leaked message content: %q", got.stderr)
	}
	if strings.Contains(got.stderr, path) || !strings.Contains(got.stderr, `\n`) {
		t.Fatalf("FIFO replacement diagnostic did not ASCII-quote the path: %q", got.stderr)
	}
}

func TestCommitMsgRejectsSameInodeRewriteAtLastPreReplaceBoundary(t *testing.T) {
	original := []byte("fix: private-a\n\nCo-Authored-By: Claude <noreply@anthropic.com>\n")
	concurrentSameSize := []byte("fix: private-b\n\nCo-Authored-By: Claude <noreply@anthropic.com>\n")
	for _, tc := range []struct {
		name     string
		mutate   func(string) error
		want     []byte
		wantMode os.FileMode
	}{
		{
			name: "content with same size",
			mutate: func(path string) error {
				return os.WriteFile(path, concurrentSameSize, 0o640)
			},
			want:     concurrentSameSize,
			wantMode: 0o640,
		},
		{
			name: "size and content",
			mutate: func(path string) error {
				return os.WriteFile(path, append(append([]byte(nil), original...), 'x'), 0o640)
			},
			want:     append(append([]byte(nil), original...), 'x'),
			wantMode: 0o640,
		},
		{
			name: "mode",
			mutate: func(path string) error {
				return os.Chmod(path, 0o600)
			},
			want:     original,
			wantMode: 0o600,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "message")
			if err := os.WriteFile(path, original, 0o640); err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			var stderr bytes.Buffer
			code := stripFootersInFileBeforeReplace(path, &stderr, func() {
				if err := tc.mutate(path); err != nil {
					t.Fatal(err)
				}
			})
			if code == 0 {
				t.Fatal("same-inode concurrent change was overwritten")
			}
			after, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(before, after) {
				t.Fatal("test seam unexpectedly replaced the message inode")
			}
			if after.Mode().Perm() != tc.wantMode {
				t.Fatalf("concurrent mode = %#o, want %#o", after.Mode().Perm(), tc.wantMode)
			}
			if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, tc.want) {
				t.Fatalf("concurrent bytes were not preserved: got %q err=%v", got, err)
			}
			if strings.Contains(stderr.String(), "private-a") || strings.Contains(stderr.String(), "private-b") {
				t.Fatalf("concurrent-rewrite diagnostic leaked message content: %q", stderr.String())
			}
			matches, err := filepath.Glob(filepath.Join(dir, ".agents-commit-msg-*"))
			if err != nil || len(matches) != 0 {
				t.Fatalf("atomic rewrite temp leaked: matches=%v err=%v", matches, err)
			}
		})
	}
}

func TestCommitMsgRewriteFailureIsBlockingAndContentFree(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "message\nname")
	original := []byte("fix: private body marker\n\nCo-Authored-By: Claude <noreply@anthropic.com>\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	code, stderr := runCommitMsg(t, path)
	if code == 0 {
		t.Fatal("rewrite failure was reported as success")
	}
	if strings.Contains(stderr, "private body marker") || strings.Contains(stderr, path) {
		t.Fatalf("diagnostic leaked message content or raw path: %q", stderr)
	}
	if !strings.Contains(stderr, `\n`) {
		t.Fatalf("diagnostic did not quote hostile path: %q", stderr)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, original) {
		t.Fatalf("failed rewrite changed original: %q, %v", got, err)
	}
}
