package doctor

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nilbot/dotfiles/agents/internal/harness"
	"github.com/nilbot/dotfiles/agents/internal/record"
	"github.com/nilbot/dotfiles/agents/internal/drift"
	"github.com/nilbot/dotfiles/agents/internal/scaffold"
	"github.com/nilbot/dotfiles/agents/internal/trace"
)

func checkByName(t *testing.T, checks []Check, name string) Check {
	t.Helper()
	for _, c := range checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("missing check %q in %+v", name, checks)
	return Check{}
}

func executableFile(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCheckBinaryRequiresSameFileIdentity(t *testing.T) {
	dir := t.TempDir()
	running := executableFile(t, dir, "running")
	alias := filepath.Join(dir, "agents")
	if err := os.Symlink(running, alias); err != nil {
		t.Fatal(err)
	}
	if got := checkBinary(running, func(string) (string, error) { return alias, nil }); got.Status != OK {
		t.Fatalf("same-inode alias = %+v, want ok", got)
	}

	other := executableFile(t, dir, "other")
	if got := checkBinary(running, func(string) (string, error) { return other, nil }); got.Status != Fail {
		t.Fatalf("distinct binary = %+v, want fail", got)
	}
	if got := checkBinary(running, func(string) (string, error) { return "", errors.New("missing") }); got.Status != Fail {
		t.Fatalf("missing PATH binary = %+v, want fail", got)
	}
}

func adapterNamed(t *testing.T, name string) harness.Adapter {
	t.Helper()
	a, ok := harness.Get(name)
	if !ok {
		t.Fatal("adapter missing")
	}
	return a
}

func readJSONMap(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func writeJSONMap(t *testing.T, path string, value map[string]any) {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckWiringValidatesEveryStructuralField(t *testing.T) {
	binary := executableFile(t, t.TempDir(), "agents")
	a := adapterNamed(t, "codex")
	validRoot := t.TempDir()
	if err := a.Wire(validRoot, binary); err != nil {
		t.Fatal(err)
	}
	check, _, keys := checkWiring(a, validRoot, binary)
	if check.Status != OK || len(keys) != len(a.Events()) {
		t.Fatalf("valid wiring = %+v keys=%v", check, keys)
	}

	for _, mutation := range []struct {
		name string
		edit func(map[string]any)
	}{
		{"missing event", func(cfg map[string]any) { delete(cfg["hooks"].(map[string]any), "Stop") }},
		{"wrong matcher", func(cfg map[string]any) {
			cfg["hooks"].(map[string]any)["SessionStart"].([]any)[0].(map[string]any)["matcher"] = "resume"
		}},
		{"matcher must be absent", func(cfg map[string]any) {
			cfg["hooks"].(map[string]any)["Stop"].([]any)[0].(map[string]any)["matcher"] = ""
		}},
		{"wrong type", func(cfg map[string]any) {
			cfg["hooks"].(map[string]any)["Stop"].([]any)[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["type"] = "prompt"
		}},
		{"wrong semantic", func(cfg map[string]any) {
			cfg["hooks"].(map[string]any)["Stop"].([]any)[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"] = binary + " hook session-start --harness codex"
		}},
		{"wrong harness", func(cfg map[string]any) {
			cfg["hooks"].(map[string]any)["Stop"].([]any)[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"] = binary + " hook stop --harness claude-code"
		}},
		{"wrong executable", func(cfg map[string]any) {
			cfg["hooks"].(map[string]any)["Stop"].([]any)[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"] = "/old/agents hook stop --harness codex"
		}},
		{"duplicate", func(cfg map[string]any) {
			group := cfg["hooks"].(map[string]any)["Stop"].([]any)[0].(map[string]any)
			group["hooks"] = append(group["hooks"].([]any), group["hooks"].([]any)[0])
		}},
		{"binary unrelated field", func(cfg map[string]any) {
			hooks := cfg["hooks"].(map[string]any)
			delete(hooks, "Stop")
			cfg["note"] = binary
		}},
		{"extra generated hook under wrong vendor", func(cfg map[string]any) {
			cfg["hooks"].(map[string]any)["OtherEvent"] = []any{map[string]any{
				"hooks": []any{map[string]any{"type": "command", "command": binary + " hook stop --harness codex"}},
			}}
		}},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			root := t.TempDir()
			if err := a.Wire(root, binary); err != nil {
				t.Fatal(err)
			}
			path := a.WireConfigPath(root)
			cfg := readJSONMap(t, path)
			mutation.edit(cfg)
			writeJSONMap(t, path, cfg)
			got, _, _ := checkWiring(a, root, binary)
			if got.Status != Fail {
				t.Fatalf("mutation survived: %+v", got)
			}
		})
	}
}

func TestCheckWiringAllowsForeignHooks(t *testing.T) {
	binary := executableFile(t, t.TempDir(), "agents")
	a := adapterNamed(t, "claude-code")
	root := t.TempDir()
	if err := a.Wire(root, binary); err != nil {
		t.Fatal(err)
	}
	path := a.WireConfigPath(root)
	cfg := readJSONMap(t, path)
	groups := cfg["hooks"].(map[string]any)["Stop"].([]any)
	cfg["hooks"].(map[string]any)["Stop"] = append(groups, map[string]any{
		"hooks": []any{
			map[string]any{"type": "command", "command": "/foreign/audit"},
			map[string]any{"type": "command", "command": "/vendor/tool hook audit --harness external"},
		},
	})
	writeJSONMap(t, path, cfg)
	if got, _, _ := checkWiring(a, root, binary); got.Status != OK {
		t.Fatalf("foreign hook rejected: %+v", got)
	}
}

// The failure this reproduces: a test wired this repository with the ephemeral
// `agents.test` binary, and because stripOurs refuses to delete a command whose
// basename is not `agents`, the entries survived every subsequent `agents
// wire`. Four accumulated per run, all of them erroring at session start, while
// doctor reported the wiring exact -- it counted only hooks it owned.
func TestCheckWiringReportsHookCommandsThatLookOursButRunAnotherBinary(t *testing.T) {
	binary := executableFile(t, t.TempDir(), "agents")
	a := adapterNamed(t, "claude-code")
	root := t.TempDir()
	if err := a.Wire(root, binary); err != nil {
		t.Fatal(err)
	}
	path := a.WireConfigPath(root)
	cfg := readJSONMap(t, path)
	stale := "/tmp/go-build123/b001/agents.test hook stop --harness claude-code"
	groups := cfg["hooks"].(map[string]any)["Stop"].([]any)
	cfg["hooks"].(map[string]any)["Stop"] = append(groups, map[string]any{
		"hooks": []any{map[string]any{"type": "command", "command": stale}},
	})
	writeJSONMap(t, path, cfg)

	got, _, _ := checkWiring(a, root, binary)
	if got.Status != Warn {
		t.Fatalf("status = %v, want Warn; a hook that fails at every session start must not read as exact wiring: %+v", got.Status, got)
	}
	if !strings.Contains(got.Detail, "agents.test") {
		t.Errorf("detail does not name the offending command: %q", got.Detail)
	}
	// It is not wire's to remove -- deleting a command we cannot prove is ours
	// is the worse failure -- so the remedy must not prescribe re-wiring, which
	// is the one thing that provably does not fix this.
	if strings.Contains(got.Remedy, "run `agents wire`") {
		t.Errorf("remedy sends the user to a command that cannot fix this: %q", got.Remedy)
	}
	if !strings.Contains(got.Remedy, "by hand") {
		t.Errorf("remedy does not say what to actually do: %q", got.Remedy)
	}
}

// The counterweight to the test above: widening what we *report* must not
// widen what we delete, and a genuine third-party hook must survive both.
func TestWireLeavesAResemblingForeignHookAlone(t *testing.T) {
	binary := executableFile(t, t.TempDir(), "agents")
	a := adapterNamed(t, "claude-code")
	root := t.TempDir()
	if err := a.Wire(root, binary); err != nil {
		t.Fatal(err)
	}
	path := a.WireConfigPath(root)
	cfg := readJSONMap(t, path)
	foreign := "/opt/vendor/auditor hook stop --harness claude-code"
	groups := cfg["hooks"].(map[string]any)["Stop"].([]any)
	cfg["hooks"].(map[string]any)["Stop"] = append(groups, map[string]any{
		"hooks": []any{map[string]any{"type": "command", "command": foreign}},
	})
	writeJSONMap(t, path, cfg)

	if err := a.Wire(root, binary); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(path); err != nil {
		t.Fatal(err)
	} else if !strings.Contains(string(b), foreign) {
		t.Error("wire deleted a hook it could not prove was ours; reporting is allowed, deleting is not")
	}
}

func TestDoctorReadsGeneratedWiringAsVerifiedRegularLeaf(t *testing.T) {
	binary := executableFile(t, t.TempDir(), "agents")
	a := adapterNamed(t, "codex")
	root := t.TempDir()
	if err := a.Wire(root, binary); err != nil {
		t.Fatal(err)
	}
	path := a.WireConfigPath(root)
	target := filepath.Join(t.TempDir(), "hooks.json")
	if err := os.Rename(path, target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := checkWiring(a, root, binary); got.Status != Fail {
		t.Fatalf("symlinked generated config = %+v, want fail", got)
	}
}

func TestCodexTrustUsesParsedCurrentHookKeys(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "config.toml")
	keys := []string{"/repo/.codex/hooks.json:session_start:0:0", "/repo/.codex/hooks.json:stop:0:0"}

	cases := []struct {
		name, body, status, detail string
	}{
		{"empty", "", Warn, "no persisted trust entry"},
		{"unrelated substring", "note = 'trusted_hash = fake'\n", Warn, "no persisted trust entry"},
		{"partial", "[hooks.state.\"/repo/.codex/hooks.json:session_start:0:0\"]\ntrusted_hash='x'\n", Warn, "incomplete"},
		{"all", "[hooks.state.\"/repo/.codex/hooks.json:session_start:0:0\"]\ntrusted_hash='x'\n[hooks.state.\"/repo/.codex/hooks.json:stop:0:0\"]\ntrusted_hash='y'\n", OK, "persisted trust entries present; no current hook is explicitly disabled; `/hooks` review state is not disclosed"},
		{"explicitly enabled", "[hooks.state.\"/repo/.codex/hooks.json:session_start:0:0\"]\ntrusted_hash='x'\nenabled=true\n[hooks.state.\"/repo/.codex/hooks.json:stop:0:0\"]\ntrusted_hash='y'\nenabled=true\n", OK, "no current hook is explicitly disabled"},
		{"one disabled", "[hooks.state.\"/repo/.codex/hooks.json:session_start:0:0\"]\ntrusted_hash='x'\nenabled=false\n[hooks.state.\"/repo/.codex/hooks.json:stop:0:0\"]\ntrusted_hash='y'\n", Warn, "1/2 current hooks explicitly disabled"},
		{"malformed", "[hooks.state\nprivate-secret", Fail, "malformed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(config, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			got := checkCodexTrust(config, keys)
			if got.Status != tc.status || !strings.Contains(got.Detail, tc.detail) {
				t.Fatalf("trust = %+v, want status %s detail containing %q", got, tc.status, tc.detail)
			}
			if strings.Contains(got.Detail, "private-secret") {
				t.Fatalf("trust diagnostic leaked TOML content: %+v", got)
			}
			if got.Status != OK && (!strings.Contains(got.Remedy, "/hooks") || !strings.Contains(got.Remedy, "Installed") || !strings.Contains(got.Remedy, "Active")) {
				t.Fatalf("trust remedy is not the measured human check: %+v", got)
			}
		})
	}
}

func TestCodexTrustRejectsSymlinkLeaf(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "private")
	key := "/repo/.codex/hooks.json:stop:0:0"
	if err := os.WriteFile(target, []byte("[hooks.state.\""+key+"\"]\ntrusted_hash='PRIVATE'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "config.toml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	got := checkCodexTrust(link, []string{key})
	if got.Status != Fail || strings.Contains(got.Detail, "PRIVATE") {
		t.Fatalf("symlinked Codex config = %+v, want content-safe fail", got)
	}
}

func TestRecordingFreshnessBoundaries(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	wired := now.Add(-48 * time.Hour)
	fresh := 7 * 24 * time.Hour
	cases := []struct {
		name           string
		recs           []record.Record
		wiring         time.Time
		status, detail string
	}{
		{"never", nil, wired, Warn, "never recorded"},
		{"predates wiring", []record.Record{{When: wired.Add(-time.Nanosecond), Harness: "codex"}}, wired, Warn, "predates current wiring"},
		{"equal wiring", []record.Record{{When: wired, Harness: "codex"}}, wired, OK, "recent"},
		{"equal freshness", []record.Record{{When: now.Add(-fresh), Harness: "codex"}}, now.Add(-10 * 24 * time.Hour), OK, "recent"},
		{"stale", []record.Record{{When: now.Add(-fresh - time.Nanosecond), Harness: "codex"}}, now.Add(-10 * 24 * time.Hour), Warn, "stale"},
		{"future", []record.Record{{When: now.Add(time.Nanosecond), Harness: "codex"}}, wired, Warn, "future clock skew"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := checkRecording(adapterNamed(t, "codex"), tc.recs, tc.wiring, fresh, now)
			if got.Status != tc.status || !strings.Contains(got.Detail, tc.detail) {
				t.Fatalf("recording = %+v, want %s %q", got, tc.status, tc.detail)
			}
		})
	}
}

func TestLaneHealthStrictThresholdsAndWindow(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	th := Thresholds{Window: 30 * 24 * time.Hour, Modules: 3, Days: 14, Sessions: 20}
	recs := []record.Record{
		{When: now.Add(-30 * 24 * time.Hour), Lane: "boundary", Cwd: "a/x", SessionID: "1"},
		{When: now, Lane: "boundary", Cwd: "b/x", SessionID: "2"},
		{When: now, Lane: "boundary", Cwd: "c/x", SessionID: "3"},
	}
	if got := LaneHealth(recs, th, now); len(got) != 1 || got[0].Status != Warn || !strings.Contains(got[0].Detail, "30 days") {
		t.Fatalf("day span above threshold should warn while module equality passes: %+v", got)
	}
	recs[0].When = now.Add(-30*24*time.Hour - time.Nanosecond)
	if got := LaneHealth(recs, th, now); len(got) != 0 {
		t.Fatalf("record before external window influenced lane: %+v", got)
	}
}

type fakeGit map[string]GitResult

func (f fakeGit) run(_ string, args ...string) GitResult {
	key := strings.Join(args, "\x00")
	if result, ok := f[key]; ok {
		return result
	}
	return GitResult{Code: 99}
}

func gitKey(args ...string) string { return strings.Join(args, "\x00") }

func originValue(path, value string) string {
	return "file:" + path + "\x00" + value + "\x00"
}

func goodGitFixture(hooksDir, globalConfig, sharedConfig string) fakeGit {
	return fakeGit{
		gitKey("config", "--global", "--includes", "--null", "--show-origin", "--get-all", "core.hooksPath"):      {Output: originValue(globalConfig, hooksDir)},
		gitKey("config", "--local", "--get-all", "core.hooksPath"):                                                {Code: 1},
		gitKey("config", "--get", "core.hooksPath"):                                                               {Output: hooksDir + "\n"},
		gitKey("config", "--global", "--includes", "--null", "--show-origin", "--get-all", "core.attributesFile"): {Output: originValue(sharedConfig, "~/.gitattributes")},
	}
}

func newGitFiles(t *testing.T) (Dependencies, string, string) {
	t.Helper()
	root := t.TempDir()
	binary := executableFile(t, root, "agents")
	hooksDir := filepath.Join(root, "hooks.d")
	legacyDir := filepath.Join(root, "legacy")
	globalConfig := filepath.Join(root, "home", ".gitconfig")
	sharedConfig := filepath.Join(root, "dotfiles", "git", "gitconfig.shared")
	attrsSource := filepath.Join(root, "global-attributes")
	attrsLink := filepath.Join(root, "home-attributes")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"pre-commit", "commit-msg", "post-merge", "post-checkout"} {
		if err := os.Symlink(binary, filepath.Join(hooksDir, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(attrsSource, []byte(".agents/reports/traces/*.jsonl merge=union\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(attrsSource, attrsLink); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	deps := Dependencies{
		LegacyHooksPath:       func(string) (string, error) { return legacyDir, nil },
		HooksDir:              hooksDir,
		AttributesLink:        attrsLink,
		AttributesSource:      attrsSource,
		AttributesConfigValue: "~/.gitattributes",
		GlobalGitConfig:       globalConfig,
		SharedGitConfig:       sharedConfig,
		Root:                  root,
	}
	deps.Git = goodGitFixture(hooksDir, globalConfig, sharedConfig).run
	return deps, binary, legacyDir
}

// newGitFiles cannot see the constant it is checking.
//
// It feeds one sharedConfig value into BOTH the dependency and the fixture, so
// every case built on it passes for any value -- including a value nothing in
// this repository ships. That is not hypothetical: the gitconfig.symlink ->
// gitconfig.shared rename on the bootstrap branch left doctor.go naming a file
// that no longer existed, so `agents doctor` reported the git-attributes origin
// failing on every healthy machine, and only install_hooks_test.go noticed.
//
// This case is deliberately written against neither the fixture nor the helper:
//
//   - the literal pins doctor.go itself, so a rename that updates the fixtures
//     and forgets DependenciesFor -- where the path is actually built -- fails
//     here;
//   - the file check pins the repository, so a rename that forgets BOTH -- the
//     shape the branch actually shipped -- fails here too. The include line in
//     git/gitconfig.local.template names this same path, and bootstrap's
//     gitconfig migration rewrites it; a file renamed underneath all three is
//     the seam this test exists for.
//
// Reading a tracked file means the Go build cache does not know when the answer
// changes: run this module's tests with -count=1 after any rename.
func TestDependenciesNameTheSharedGitConfigThisRepositoryShips(t *testing.T) {
	const relative = "git/gitconfig.shared"

	got := filepath.ToSlash(DependenciesFor(filepath.Join("any", "checkout")).SharedGitConfig)
	if !strings.HasSuffix(got, "/"+relative) {
		t.Errorf("DependenciesFor(...).SharedGitConfig = %q, want it to end in %q; "+
			"doctor compares core.attributesFile's origin against this path, so a "+
			"name no checkout carries makes the attributes check fail on every "+
			"healthy machine", got, relative)
	}

	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repoRoot, filepath.FromSlash(relative))
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("%s is not in this checkout (%v); doctor names it, "+
			"git/gitconfig.local.template includes it, and bootstrap's gitconfig "+
			"migration rewrites that include -- renaming it silently breaks all three",
			relative, err)
	}
	if !info.Mode().IsRegular() {
		t.Errorf("%s is %v, want a regular tracked file", relative, info.Mode())
	}
}

// TestDependenciesForDeriveTheCheckoutPathsFromTheRootItIsGiven pins the
// distinction the ~/dotfiles assumption erased.
//
// doctor compares the checkout-relative paths against what Git actually
// reports, so a binary that guesses the wrong checkout reports
// git-hooks:global, git-hooks:links and git-attributes failing -- and
// git-hooks:effective warning -- against a machine that is correctly
// provisioned. The remaining paths are genuinely home-relative: they must not
// follow the root, or a relocated checkout would look inside itself for files
// Git only ever reads from the home directory.
func TestDependenciesForDeriveTheCheckoutPathsFromTheRootItIsGiven(t *testing.T) {
	root := filepath.Join(t.TempDir(), "src", "dotfiles")
	deps := DependenciesFor(root)

	for _, c := range []struct{ field, got, want string }{
		{"HooksDir", deps.HooksDir, filepath.Join(root, "git", "hooks.d")},
		{"AttributesSource", deps.AttributesSource, filepath.Join(root, "git", "gitattributes")},
		{"SharedGitConfig", deps.SharedGitConfig, filepath.Join(root, "git", "gitconfig.shared")},
	} {
		if c.got != c.want {
			t.Errorf("DependenciesFor(%q).%s = %q, want %q", root, c.field, c.got, c.want)
		}
	}

	prefix := root + string(filepath.Separator)
	for _, c := range []struct{ field, got string }{
		{"CodexConfig", deps.CodexConfig},
		{"AntigravityConfig", deps.AntigravityConfig},
		{"AttributesLink", deps.AttributesLink},
		{"GlobalGitConfig", deps.GlobalGitConfig},
	} {
		if strings.HasPrefix(c.got, prefix) {
			t.Errorf("DependenciesFor(%q).%s = %q, want a path outside the checkout; "+
				"this one lives in the home directory wherever the checkout is", root, c.field, c.got)
		}
	}

	if deps.AttributesConfigValue != "~/.gitattributes" {
		t.Errorf("DependenciesFor(%q).AttributesConfigValue = %q, want %q; doctor "+
			"compares core.attributesFile against this literal", root, deps.AttributesConfigValue, "~/.gitattributes")
	}
	if deps.LookPath == nil || deps.Git == nil || deps.LegacyHooksPath == nil {
		t.Error("DependenciesFor left a runner nil; doctor then reports its git " +
			"checks unavailable instead of running them")
	}
}

func TestDependenciesForEmptyRootLeavesCheckoutPathsEmpty(t *testing.T) {
	deps := DependenciesFor("")

	if deps.Root != "" {
		t.Errorf("DependenciesFor(\"\").Root = %q, want \"\"", deps.Root)
	}
	if deps.HooksDir != "" {
		t.Errorf("DependenciesFor(\"\").HooksDir = %q, want \"\"", deps.HooksDir)
	}
	if deps.AttributesSource != "" {
		t.Errorf("DependenciesFor(\"\").AttributesSource = %q, want \"\"", deps.AttributesSource)
	}
	if deps.SharedGitConfig != "" {
		t.Errorf("DependenciesFor(\"\").SharedGitConfig = %q, want \"\"", deps.SharedGitConfig)
	}
	if deps.LookPath == nil || deps.Git == nil || deps.LegacyHooksPath == nil {
		t.Error("DependenciesFor left a runner nil; doctor then reports its git " +
			"checks unavailable instead of running them")
	}
}

func TestGitDiagnosticsCoverConfigLinksAttributesAndLegacy(t *testing.T) {
	deps, binary, _ := newGitFiles(t)
	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, ".gitattributes"), []byte(".agents/reports/traces/*.jsonl merge=union\n.agents/** linguist-generated=true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	checks := checkGitHooks(repoRoot, binary, deps)
	for _, name := range []string{"git-hooks:global", "git-hooks:local", "git-hooks:effective", "git-hooks:links", "git-hooks:legacy"} {
		if got := checkByName(t, checks, name); got.Status != OK {
			t.Fatalf("%s = %+v, want ok", name, got)
		}
	}
	if got := checkGitAttributes(repoRoot, deps); got.Status != OK {
		t.Fatalf("attributes = %+v, want ok", got)
	}

	t.Run("global unset", func(t *testing.T) {
		bad := deps
		git := goodGitFixture(deps.HooksDir, deps.GlobalGitConfig, deps.SharedGitConfig)
		git[gitKey("config", "--global", "--includes", "--null", "--show-origin", "--get-all", "core.hooksPath")] = GitResult{Code: 1}
		bad.Git = git.run
		if got := checkByName(t, checkGitHooks(t.TempDir(), binary, bad), "git-hooks:global"); got.Status != Fail {
			t.Fatalf("unset global = %+v", got)
		}
	})
	t.Run("global multiple", func(t *testing.T) {
		bad := deps
		git := goodGitFixture(deps.HooksDir, deps.GlobalGitConfig, deps.SharedGitConfig)
		git[gitKey("config", "--global", "--includes", "--null", "--show-origin", "--get-all", "core.hooksPath")] = GitResult{Output: originValue(deps.GlobalGitConfig, deps.HooksDir) + originValue(deps.GlobalGitConfig, "/other")}
		bad.Git = git.run
		if got := checkByName(t, checkGitHooks(t.TempDir(), binary, bad), "git-hooks:global"); got.Status != Fail {
			t.Fatalf("multiple global = %+v", got)
		}
	})
	t.Run("local override warns", func(t *testing.T) {
		bad := deps
		git := goodGitFixture(deps.HooksDir, deps.GlobalGitConfig, deps.SharedGitConfig)
		git[gitKey("config", "--local", "--get-all", "core.hooksPath")] = GitResult{Output: "/local/hooks\n"}
		git[gitKey("config", "--get", "core.hooksPath")] = GitResult{Output: "/local/hooks\n"}
		bad.Git = git.run
		checks := checkGitHooks(t.TempDir(), binary, bad)
		if got := checkByName(t, checks, "git-hooks:local"); got.Status != Warn {
			t.Fatalf("local = %+v", got)
		}
		if got := checkByName(t, checks, "git-hooks:effective"); got.Status != Warn {
			t.Fatalf("effective = %+v", got)
		}
	})
	t.Run("included same-value hook mutation fails origin", func(t *testing.T) {
		bad := deps
		git := goodGitFixture(deps.HooksDir, deps.GlobalGitConfig, deps.SharedGitConfig)
		git[gitKey("config", "--global", "--includes", "--null", "--show-origin", "--get-all", "core.hooksPath")] = GitResult{Output: originValue(filepath.Join(t.TempDir(), "included"), deps.HooksDir)}
		bad.Git = git.run
		if got := checkByName(t, checkGitHooks(t.TempDir(), binary, bad), "git-hooks:global"); got.Status != Fail {
			t.Fatalf("redirected hook origin = %+v", got)
		}
	})
	t.Run("git failure is not ok", func(t *testing.T) {
		bad := deps
		bad.Git = func(string, ...string) GitResult { return GitResult{Code: 128} }
		bad.LegacyHooksPath = func(string) (string, error) { return "", errors.New("Git failed") }
		checks := checkGitHooks(t.TempDir(), binary, bad)
		for _, name := range []string{"git-hooks:global", "git-hooks:local", "git-hooks:effective", "git-hooks:legacy"} {
			if got := checkByName(t, checks, name); got.Status == OK {
				t.Fatalf("Git failure reported ok for %s: %+v", name, got)
			}
		}
	})
}

func TestEveryInstalledLinkMustBeAnExactBinarySymlink(t *testing.T) {
	for _, name := range []string{"pre-commit", "commit-msg", "post-merge", "post-checkout"} {
		t.Run(name, func(t *testing.T) {
			deps, binary, _ := newGitFiles(t)
			path := filepath.Join(deps.HooksDir, name)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("regular"), 0o755); err != nil {
				t.Fatal(err)
			}
			if got := checkByName(t, checkGitHooks(t.TempDir(), binary, deps), "git-hooks:links"); got.Status != Fail || !strings.Contains(got.Detail, name) {
				t.Fatalf("bad %s link = %+v", name, got)
			}
		})
	}
}

func TestInstalledLinkLeafTypesFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name  string
		build func(t *testing.T, path string)
	}{
		{"missing", func(t *testing.T, path string) {}},
		{"regular", func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte("foreign"), 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		{"broken", func(t *testing.T, path string) {
			if err := os.Symlink(filepath.Join(t.TempDir(), "gone"), path); err != nil {
				t.Fatal(err)
			}
		}},
		{"wrong", func(t *testing.T, path string) {
			other := executableFile(t, t.TempDir(), "other")
			if err := os.Symlink(other, path); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps, binary, _ := newGitFiles(t)
			path := filepath.Join(deps.HooksDir, "pre-commit")
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			tc.build(t, path)
			if got := checkByName(t, checkGitHooks(t.TempDir(), binary, deps), "git-hooks:links"); got.Status != Fail {
				t.Fatalf("%s link = %+v", tc.name, got)
			}
		})
	}
}

func TestLegacyCheckMatchesOnlyExactRetiredFingerprint(t *testing.T) {
	deps, binary, legacy := newGitFiles(t)
	source := filepath.Join("..", "githook", "testdata", "retired-commit-msg")
	b, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(legacy, "commit-msg")
	if err := os.WriteFile(path, b, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := checkByName(t, checkGitHooks(t.TempDir(), binary, deps), "git-hooks:legacy"); got.Status != Warn {
		t.Fatalf("exact legacy = %+v", got)
	}
	b[0] ^= 1
	if err := os.WriteFile(path, b, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := checkByName(t, checkGitHooks(t.TempDir(), binary, deps), "git-hooks:legacy"); got.Status != OK {
		t.Fatalf("near-match foreign hook = %+v", got)
	}
}

func TestAttributeDiagnosticsRequireConfigLinkSourceAndRepoLines(t *testing.T) {
	deps, _, _ := newGitFiles(t)
	repoRoot := t.TempDir()
	wantRepo := ".agents/reports/traces/*.jsonl merge=union\n.agents/** linguist-generated=true\n"
	if err := os.WriteFile(filepath.Join(repoRoot, ".gitattributes"), []byte(wantRepo), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := checkGitAttributes(repoRoot, deps); got.Status != OK {
		t.Fatalf("valid attrs = %+v", got)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, ".gitattributes"), []byte(".agents/reports/traces/*.jsonl merge=union\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := checkGitAttributes(repoRoot, deps); got.Status != Fail {
		t.Fatalf("missing repo line = %+v", got)
	}
}

func TestAttributeDiagnosticsReadOnlyVerifiedRegularLeaves(t *testing.T) {
	for _, leaf := range []string{"source", "repository"} {
		t.Run(leaf, func(t *testing.T) {
			deps, _, _ := newGitFiles(t)
			repoRoot := t.TempDir()
			repoPath := filepath.Join(repoRoot, ".gitattributes")
			contents := []byte(".agents/reports/traces/*.jsonl merge=union\n.agents/** linguist-generated=true\n")
			if err := os.WriteFile(repoPath, contents, 0o644); err != nil {
				t.Fatal(err)
			}
			var path string
			if leaf == "source" {
				path = deps.AttributesSource
			} else {
				path = repoPath
			}
			target := filepath.Join(t.TempDir(), "attributes")
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, original, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
			if got := checkGitAttributes(repoRoot, deps); got.Status != Fail {
				t.Fatalf("symlinked %s attributes = %+v, want fail", leaf, got)
			}
		})
	}
}

func TestLaneHealthModuleAndSessionBoundaries(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	th := Thresholds{Window: 30 * 24 * time.Hour, Modules: 3, Days: 14, Sessions: 3}
	var equal []record.Record
	for i, module := range []string{"a/x", "b/x", "c/x"} {
		equal = append(equal, record.Record{When: now, Lane: "equal", Cwd: module, SessionID: string(rune('a' + i))})
	}
	if got := LaneHealth(equal, th, now); len(got) != 0 {
		t.Fatalf("equality boundary warned: %+v", got)
	}
	above := append(append([]record.Record{}, equal...), record.Record{When: now, Lane: "equal", Cwd: "d/x", SessionID: "d"})
	if got := LaneHealth(above, th, now); len(got) != 1 || !strings.Contains(got[0].Detail, "4 modules") || !strings.Contains(got[0].Detail, "4 sessions") {
		t.Fatalf("above boundary did not warn: %+v", got)
	}
}

func TestGitleaksCheckOnlyLooksUpExecutable(t *testing.T) {
	calls := 0
	got := checkGitleaks(func(name string) (string, error) {
		calls++
		if name != "gitleaks" {
			t.Fatalf("lookup = %q", name)
		}
		return "/tmp/gitleaks", nil
	})
	if got.Status != OK || calls != 1 {
		t.Fatalf("gitleaks = %+v calls=%d", got, calls)
	}
	if got := checkGitleaks(func(string) (string, error) { return "", errors.New("missing") }); got.Status != Warn || !strings.Contains(got.Remedy, "brew install gitleaks") {
		t.Fatalf("missing gitleaks = %+v", got)
	}
}

func TestRunWithDependenciesReportsSkippedTraceAndAllSignals(t *testing.T) {
	repoRoot := t.TempDir()
	agentsDir := filepath.Join(repoRoot, ".agents")
	if err := os.MkdirAll(filepath.Join(agentsDir, "reports", "traces"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(agentsDir, "memory"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The unreadable line goes in the store, which is the index now.
	storeTraces := filepath.Join(repoRoot, ".git", "agents", "traces")
	if err := os.MkdirAll(storeTraces, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(storeTraces, "2026-08-20.jsonl"), []byte("not-json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "CLAUDE.md"), []byte("Run `agents doctor` early and report any warnings before relying on this context.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, ".gitattributes"), []byte(".agents/reports/traces/*.jsonl merge=union\n.agents/** linguist-generated=true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deps, binary, _ := newGitFiles(t)
	deps.LookPath = func(name string) (string, error) {
		if name == "agents" {
			return binary, nil
		}
		if name == "gitleaks" {
			return "/tmp/gitleaks", nil
		}
		return "", errors.New("missing")
	}
	deps.CodexConfig = filepath.Join(t.TempDir(), "missing-config.toml")
	for _, adapter := range harness.All() {
		if err := adapter.Wire(repoRoot, binary); err != nil {
			t.Fatal(err)
		}
	}

	checks, err := RunWithDeps(repoRoot, agentsDir, filepath.Join(repoRoot, ".git", "agents"), "", binary, DefaultThresholds(), time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC), deps)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"binary", "wiring:claude-code", "wiring:codex", "wiring:antigravity", "trust:codex", "trust:antigravity", "recording:claude-code", "recording:codex", "recording:antigravity", "gitleaks", "git-hooks:global", "git-attributes", "machine-id", "trace-index", "pointers:unverified", "scaffold:router", "scaffold:symlink", "scaffold:domain", "scaffold:skill-recording", "scaffold:skill-migrating"} {
		_ = checkByName(t, checks, name)
	}
	if got := checkByName(t, checks, "trace-index"); got.Status != Warn || !strings.Contains(got.Detail, "1") {
		t.Fatalf("skipped trace = %+v", got)
	}
	if got := checkByName(t, checks, "machine-id"); got.Status != Warn {
		t.Fatalf("missing machine = %+v", got)
	}
}

func TestRunReturnsContentSafeErrorForUnreadableTrace(t *testing.T) {
	repoRoot := t.TempDir()
	agentsDir := filepath.Join(repoRoot, ".agents")
	traceDir := filepath.Join(agentsDir, "reports", "traces")
	if err := os.MkdirAll(traceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	private := "private-trace-name"
	// Under the store, which is where the index lives and where RunWithDeps
	// is told to read.
	storeTraces := filepath.Join(repoRoot, ".git", "agents", "traces")
	if err := os.MkdirAll(storeTraces, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), private), filepath.Join(storeTraces, "2026-08-20.jsonl")); err != nil {
		t.Fatal(err)
	}
	_, err := RunWithDeps(repoRoot, agentsDir, filepath.Join(repoRoot, ".git", "agents"), "", filepath.Join(t.TempDir(), "agents"), DefaultThresholds(), time.Now(), Dependencies{})
	if err == nil || strings.Contains(err.Error(), private) {
		t.Fatalf("trace error = %v, want content-safe failure", err)
	}
}

func TestRunWithIncompleteDependenciesReturnsDiagnosticsInsteadOfPanicking(t *testing.T) {
	repoRoot := t.TempDir()
	agentsDir := filepath.Join(repoRoot, ".agents")
	if err := os.MkdirAll(filepath.Join(agentsDir, "reports", "traces"), 0o755); err != nil {
		t.Fatal(err)
	}
	checks, err := RunWithDeps(repoRoot, agentsDir, filepath.Join(repoRoot, ".git", "agents"), "", filepath.Join(repoRoot, "missing-agents"), DefaultThresholds(), time.Now(), Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	if got := checkByName(t, checks, "binary"); got.Status != Fail {
		t.Fatalf("missing binary dependency = %+v", got)
	}
	if got := checkByName(t, checks, "git-attributes"); got.Status != Fail {
		t.Fatalf("missing Git dependency = %+v", got)
	}
}

// The remedy that could not clear the warning.
//
// This check stat'd the harness's own path and nothing else, so a transcript
// successfully copied into the cache stayed "unreachable" forever. Its remedy
// reads "cache reachable transcripts before harness cleanup" -- advice that,
// followed perfectly, changed the number not at all. A guard whose remedy does
// not move it teaches the reader to stop reading it.
//
// Three classes now, because they call for different things: still at the
// source, gone but saved, gone for good. Only the last is anyone's problem.
func TestPointersCountACachedTranscriptAsSavedRatherThanUnreachable(t *testing.T) {
	cacheRoot := t.TempDir()
	live := filepath.Join(t.TempDir(), "still-here.jsonl")
	if err := os.WriteFile(live, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Deleted by the harness, but copied first: exactly what the cache is for.
	rescued := record.Record{
		Transcript:      filepath.Join(t.TempDir(), "agent-rescued.jsonl"),
		PointerVerified: true, Machine: "m1", Harness: "claude-code",
	}
	saveTo := trace.CachedPath(cacheRoot, rescued)
	if err := os.MkdirAll(filepath.Dir(saveTo), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(saveTo, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lost := record.Record{
		Transcript:      filepath.Join(t.TempDir(), "agent-lost.jsonl"),
		PointerVerified: true, Machine: "m1", Harness: "claude-code",
	}

	checks := checkPointers([]record.Record{
		{Transcript: live, PointerVerified: true, Machine: "m1", Harness: "claude-code"},
		rescued,
		lost,
	}, "m1", cacheRoot)

	_ = lost // reported by no check: unreachable is a normal state now.
	saved := checkByName(t, checks, "pointers:cached")
	if saved.Status != OK {
		t.Errorf("pointers:cached = %+v, want OK: caching worked, and a warning "+
			"here would say the opposite of what happened", saved)
	}
	if !strings.Contains(saved.Detail, "1") {
		t.Errorf("pointers:cached detail = %q, want it to count the rescued transcript", saved.Detail)
	}
}

// With nothing cached the check must not appear at all: a permanent "0 saved"
// row is noise on every healthy machine.
func TestPointersReportNoCachedRowWhenNothingWasSaved(t *testing.T) {
	checks := checkPointers([]record.Record{
		{Transcript: filepath.Join(t.TempDir(), "gone.jsonl"), PointerVerified: true, Machine: "m1", Harness: "codex"},
	}, "m1", t.TempDir())
	for _, c := range checks {
		if c.Name == "pointers:cached" {
			t.Errorf("pointers:cached appeared with nothing cached: %+v", c)
		}
	}
	for _, c := range checks {
		if c.Name == "pointers:local-unreachable" {
			t.Errorf("the unreachable-pointer warning came back: %+v", c)
		}
	}
}

// The write-side indicator that replaced the queue depth check.
//
// Three states, and the first is the one that matters: a repository with no
// docs/qna must not be reported as unhealthy. Most repositories have not
// adopted this, and a check that fails everywhere teaches people to skim past
// the whole report.
func TestDocsFreshnessReportsWithoutJudging(t *testing.T) {
	now := time.Now()

	root := t.TempDir()
	got := checkDocsFreshness(root, now)
	if got.Status != OK || !strings.Contains(got.Detail, "no docs/qna") {
		t.Errorf("a repository without docs/qna = %+v, want a quiet OK", got)
	}

	dir := filepath.Join(root, "docs", "qna")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# form\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The README describes the form; it is not an entry, and counting it would
	// report a store as populated the moment it was created.
	if got := checkDocsFreshness(root, now); !strings.Contains(got.Detail, "nothing recorded yet") {
		t.Errorf("README.md counted as an entry: %+v", got)
	}

	entry := filepath.Join(dir, "why-x-happens.md")
	if err := os.WriteFile(entry, []byte("# why\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-72 * time.Hour)
	if err := os.Chtimes(entry, old, old); err != nil {
		t.Fatal(err)
	}
	got = checkDocsFreshness(root, now)
	if got.Status != OK || !strings.Contains(got.Detail, "1 entr") || !strings.Contains(got.Detail, "3 day") {
		t.Errorf("one entry three days old = %+v", got)
	}
}

func TestStoreSizeCheckWarnsOverTheCap(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "trace-cache", "claude-code")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "a.jsonl"), bytes.Repeat([]byte("x"), 4096), 0o644); err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(cache)
	if got := checkStoreSize(root, 1<<30); got.Status != OK {
		t.Errorf("under the cap = %+v", got)
	}
	got := checkStoreSize(root, 1024)
	if got.Status != Warn {
		t.Errorf("over the cap = %+v, want warn", got)
	}
	if !strings.Contains(got.Remedy, "--retention") {
		t.Errorf("the remedy does not name the command that fixes it: %q", got.Remedy)
	}
}

// A binary stamped to a checkout that no longer exists runs none of the
// personal git hooks and says nothing about it: githook treats a missing extras
// directory as "no personal hooks" and carries on at exit 0. Every
// path-comparing check passes, because the paths agree with each other -- they
// simply both name nothing.
//
// Measured before this test was written, on a real stamped binary with
// core.hooksPath agreeing with the stamp: doctor's output was byte-identical
// before and after the worktree was deleted, 2691 bytes each time, and never
// named the deleted path.
func TestDoctorFailsWhenTheStampedRootIsGone(t *testing.T) {
	deps := DependenciesFor(filepath.Join(t.TempDir(), "deleted-worktree"))
	found := findCheck(t, rootChecks(deps), "root:exists")
	if found.Status != Fail {
		t.Errorf("root:exists = %v, want Fail: the consequence is a correctness mechanism silently not running", found.Status)
	}
	if !strings.Contains(found.Detail, "deleted-worktree") {
		t.Errorf("the detail must name the missing checkout, or the reader cannot act on it: %q", found.Detail)
	}
}

// The check must pass on a root that is there, or it would fail every healthy
// machine and be turned off within a day.
func TestDoctorPassesWhenTheStampedRootExists(t *testing.T) {
	found := findCheck(t, rootChecks(DependenciesFor(t.TempDir())), "root:exists")
	if found.Status != OK {
		t.Errorf("root:exists = %v on an existing checkout, want OK (%q)", found.Status, found.Detail)
	}
}

// A path that exists but is a FILE is not a checkout. Statting without asking
// what was found would call that healthy.
func TestDoctorFailsWhenTheStampedRootIsAFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-checkout")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	found := findCheck(t, rootChecks(DependenciesFor(file)), "root:exists")
	if found.Status != Fail {
		t.Errorf("root:exists = %v on a regular file, want Fail", found.Status)
	}
}

func findCheck(t *testing.T, checks []Check, name string) Check {
	t.Helper()
	for _, c := range checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no %s check was produced; got %+v", name, checks)
	return Check{}
}

func TestCheckWiringAntigravityNamedGroups(t *testing.T) {
	binary := executableFile(t, t.TempDir(), "agents")
	a := adapterNamed(t, "antigravity")
	validRoot := t.TempDir()
	if err := a.Wire(validRoot, binary); err != nil {
		t.Fatal(err)
	}
	check, _, keys := checkWiring(a, validRoot, binary)
	if check.Status != OK || len(keys) != len(a.Events()) {
		t.Fatalf("valid wiring = %+v keys=%v", check, keys)
	}
	if !strings.HasSuffix(keys[0], ":stop:0:0") {
		t.Errorf("keys[0] = %q, want suffix :stop:0:0", keys[0])
	}

	for _, mutation := range []struct {
		name string
		edit func(map[string]any)
	}{
		{"missing agents object", func(cfg map[string]any) { delete(cfg, "agents") }},
		{"missing event", func(cfg map[string]any) { delete(cfg["agents"].(map[string]any), "Stop") }},
		{"wrong type", func(cfg map[string]any) {
			cfg["agents"].(map[string]any)["Stop"].([]any)[0].(map[string]any)["type"] = "prompt"
		}},
		{"wrong semantic", func(cfg map[string]any) {
			cfg["agents"].(map[string]any)["Stop"].([]any)[0].(map[string]any)["command"] = binary + " hook session-start --harness antigravity"
		}},
		{"wrong harness", func(cfg map[string]any) {
			cfg["agents"].(map[string]any)["Stop"].([]any)[0].(map[string]any)["command"] = binary + " hook stop --harness claude-code"
		}},
		{"wrong executable", func(cfg map[string]any) {
			cfg["agents"].(map[string]any)["Stop"].([]any)[0].(map[string]any)["command"] = "/old/agents hook stop --harness antigravity"
		}},
		{"matcher must be absent for lifecycle event", func(cfg map[string]any) {
			cfg["agents"].(map[string]any)["Stop"].([]any)[0].(map[string]any)["matcher"] = "*"
		}},
		{"nested hooks must be absent for lifecycle event", func(cfg map[string]any) {
			stop := cfg["agents"].(map[string]any)["Stop"].([]any)[0].(map[string]any)
			cfg["agents"].(map[string]any)["Stop"] = []any{map[string]any{
				"hooks": []any{stop},
			}}
		}},
		{"duplicate", func(cfg map[string]any) {
			stopList := cfg["agents"].(map[string]any)["Stop"].([]any)
			cfg["agents"].(map[string]any)["Stop"] = append(stopList, stopList[0])
		}},
		{"extra generated hook under other event", func(cfg map[string]any) {
			cfg["agents"].(map[string]any)["OtherEvent"] = []any{map[string]any{
				"type": "command", "command": binary + " hook stop --harness antigravity",
			}}
		}},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			root := t.TempDir()
			if err := a.Wire(root, binary); err != nil {
				t.Fatal(err)
			}
			path := a.WireConfigPath(root)
			cfg := readJSONMap(t, path)
			mutation.edit(cfg)
			writeJSONMap(t, path, cfg)
			got, _, _ := checkWiring(a, root, binary)
			if got.Status != Fail {
				t.Fatalf("mutation %q survived: %+v", mutation.name, got)
			}
		})
	}
}

func TestCheckWiringAntigravityAllowsForeignHooks(t *testing.T) {
	binary := executableFile(t, t.TempDir(), "agents")
	a := adapterNamed(t, "antigravity")
	root := t.TempDir()
	if err := a.Wire(root, binary); err != nil {
		t.Fatal(err)
	}
	path := a.WireConfigPath(root)
	cfg := readJSONMap(t, path)
	agentsObj := cfg["agents"].(map[string]any)
	stopList := agentsObj["Stop"].([]any)
	agentsObj["Stop"] = append(stopList, map[string]any{
		"type":    "command",
		"command": "/foreign/lint-check",
	})
	writeJSONMap(t, path, cfg)

	if got, _, _ := checkWiring(a, root, binary); got.Status != OK {
		t.Fatalf("foreign hook rejected: %+v", got)
	}
}

func TestCheckWiringAntigravityReportsHookCommandsThatLookOursButRunAnotherBinary(t *testing.T) {
	binary := executableFile(t, t.TempDir(), "agents")
	a := adapterNamed(t, "antigravity")
	root := t.TempDir()
	if err := a.Wire(root, binary); err != nil {
		t.Fatal(err)
	}
	path := a.WireConfigPath(root)
	cfg := readJSONMap(t, path)
	stale := "/tmp/go-build123/b001/agents.test hook stop --harness antigravity"
	agentsObj := cfg["agents"].(map[string]any)
	agentsObj["Stop"] = append(agentsObj["Stop"].([]any), map[string]any{
		"type":    "command",
		"command": stale,
	})
	writeJSONMap(t, path, cfg)

	got, _, _ := checkWiring(a, root, binary)
	if got.Status != Warn {
		t.Fatalf("status = %v, want Warn; %+v", got.Status, got)
	}
	if !strings.Contains(got.Detail, "agents.test") {
		t.Errorf("detail does not name offending command: %q", got.Detail)
	}
}

func TestCheckAntigravityTrustAccurateStates(t *testing.T) {
	repoRoot := filepath.Join(t.TempDir(), "my-repo")
	cliConfig := filepath.Join(t.TempDir(), "settings.json")

	t.Run("missing cli config", func(t *testing.T) {
		got := checkAntigravityTrust(filepath.Join(t.TempDir(), "nonexistent.json"), repoRoot)
		if got.Status != OK || !strings.Contains(got.Detail, "Desktop App executes on open") {
			t.Fatalf("missing config = %+v, want OK", got)
		}
	})

	t.Run("repo in trustedWorkspaces", func(t *testing.T) {
		cfg := map[string]any{
			"trustedWorkspaces": []string{repoRoot, "/other/repo"},
		}
		writeJSONMap(t, cliConfig, cfg)
		got := checkAntigravityTrust(cliConfig, repoRoot)
		if got.Status != OK || !strings.Contains(got.Detail, "confirmed") {
			t.Fatalf("trusted repo = %+v, want OK with confirmed", got)
		}
	})

	t.Run("repo not in trustedWorkspaces", func(t *testing.T) {
		cfg := map[string]any{
			"trustedWorkspaces": []string{"/other/repo"},
		}
		writeJSONMap(t, cliConfig, cfg)
		got := checkAntigravityTrust(cliConfig, repoRoot)
		if got.Status != OK || !strings.Contains(got.Detail, "not found") {
			t.Fatalf("untrusted repo = %+v, want OK with not found", got)
		}
	})
}

func TestCheckScaffoldGranularChecks(t *testing.T) {
	t.Run("canonical scaffold all ok", func(t *testing.T) {
		root := t.TempDir()
		gitInit := exec.Command("git", "init", "-b", "main")
		gitInit.Dir = root
		if out, err := gitInit.CombinedOutput(); err != nil {
			t.Fatalf("git init: %v\n%s", err, out)
		}
		if err := scaffold.Create(root, false); err != nil {
			t.Fatal(err)
		}
		checks := checkScaffold(root)
		if len(checks) != 5 {
			t.Fatalf("got %d checks, want 5", len(checks))
		}

		cRouter := checkByName(t, checks, "scaffold:router")
		if cRouter.Status != OK || cRouter.Detail != "root AGENTS.md matches canonical template" {
			t.Errorf("scaffold:router = %+v, want OK canonical", cRouter)
		}

		cSymlink := checkByName(t, checks, "scaffold:symlink")
		if cSymlink.Status != OK || cSymlink.Detail != "CLAUDE.md is a relative symlink to AGENTS.md" {
			t.Errorf("scaffold:symlink = %+v, want OK symlink", cSymlink)
		}

		cDomain := checkByName(t, checks, "scaffold:domain")
		if cDomain.Status != OK || cDomain.Detail != ".agents/AGENTS.md domain context is present" {
			t.Errorf("scaffold:domain = %+v, want OK domain", cDomain)
		}

		cRec := checkByName(t, checks, "scaffold:skill-recording")
		if cRec.Status != OK || cRec.Detail != ".agents/skills/recording-what-you-learn/ is present" {
			t.Errorf("scaffold:skill-recording = %+v, want OK recording", cRec)
		}

		cMig := checkByName(t, checks, "scaffold:skill-migrating")
		if cMig.Status != OK || cMig.Detail != ".agents/skills/migrating-fleet-context/ is present" {
			t.Errorf("scaffold:skill-migrating = %+v, want OK migrating", cMig)
		}
	})

	t.Run("router legacy and drifted and missing", func(t *testing.T) {
		// Clean legacy
		rootLegacy := t.TempDir()
		if err := os.WriteFile(filepath.Join(rootLegacy, "AGENTS.md"), []byte(drift.LegacySingleBulletRouter), 0o644); err != nil {
			t.Fatal(err)
		}
		cLegacy := checkByName(t, checkScaffold(rootLegacy), "scaffold:router")
		if cLegacy.Status != Warn || cLegacy.Detail != "root AGENTS.md uses a legacy canonical template" || cLegacy.Remedy != "run the 'migrating-fleet-context' agent skill to update" {
			t.Errorf("clean legacy router = %+v", cLegacy)
		}

		// Drifted
		rootDrifted := t.TempDir()
		if err := os.WriteFile(filepath.Join(rootDrifted, "AGENTS.md"), []byte("# Custom rules\nDo not edit\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cDrifted := checkByName(t, checkScaffold(rootDrifted), "scaffold:router")
		if cDrifted.Status != Warn || cDrifted.Detail != "root AGENTS.md contains unpartitioned domain rules or custom drift" || cDrifted.Remedy != "run the 'migrating-fleet-context' agent skill to un-nest domain rules into .agents/AGENTS.md" {
			t.Errorf("drifted router = %+v", cDrifted)
		}

		// Missing
		rootMissing := t.TempDir()
		cMissing := checkByName(t, checkScaffold(rootMissing), "scaffold:router")
		if cMissing.Status != Fail || cMissing.Detail != "root AGENTS.md is missing" || cMissing.Remedy != "run 'agents init' to scaffold" {
			t.Errorf("missing router = %+v", cMissing)
		}
	})

	t.Run("symlink invalid states", func(t *testing.T) {
		// Missing
		rootMissing := t.TempDir()
		cMissing := checkByName(t, checkScaffold(rootMissing), "scaffold:symlink")
		if cMissing.Status != Fail || cMissing.Detail != "CLAUDE.md symlink is invalid (missing)" || !strings.Contains(cMissing.Remedy, "ln -s AGENTS.md CLAUDE.md") {
			t.Errorf("missing symlink = %+v", cMissing)
		}

		// Not symlink (regular file)
		rootRegular := t.TempDir()
		if err := os.WriteFile(filepath.Join(rootRegular, "CLAUDE.md"), []byte("regular file"), 0o644); err != nil {
			t.Fatal(err)
		}
		cRegular := checkByName(t, checkScaffold(rootRegular), "scaffold:symlink")
		if cRegular.Status != Fail || cRegular.Detail != "CLAUDE.md symlink is invalid (not_symlink)" {
			t.Errorf("not symlink = %+v", cRegular)
		}

		// Broken symlink
		rootBroken := t.TempDir()
		if err := os.Symlink("NONEXISTENT.md", filepath.Join(rootBroken, "CLAUDE.md")); err != nil {
			t.Fatal(err)
		}
		cBroken := checkByName(t, checkScaffold(rootBroken), "scaffold:symlink")
		if cBroken.Status != Fail || cBroken.Detail != "CLAUDE.md symlink is invalid (broken)" {
			t.Errorf("broken symlink = %+v", cBroken)
		}
	})

	t.Run("domain context missing", func(t *testing.T) {
		root := t.TempDir()
		c := checkByName(t, checkScaffold(root), "scaffold:domain")
		if c.Status != Warn || c.Detail != ".agents/AGENTS.md is missing" || c.Remedy != "run 'agents init' to populate starter template" {
			t.Errorf("missing domain = %+v", c)
		}
	})

	t.Run("skill recording states", func(t *testing.T) {
		// Clean legacy
		rootLegacy := t.TempDir()
		recDir := filepath.Join(rootLegacy, ".agents", "skills", "recording-what-you-learn")
		if err := os.MkdirAll(recDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(recDir, "SKILL.md"), []byte(drift.LegacyRecordingSkill), 0o644); err != nil {
			t.Fatal(err)
		}
		cLegacy := checkByName(t, checkScaffold(rootLegacy), "scaffold:skill-recording")
		if cLegacy.Status != OK || cLegacy.Detail != ".agents/skills/recording-what-you-learn/ matches legacy template" {
			t.Errorf("clean legacy recording = %+v", cLegacy)
		}

		// Customized
		rootCustom := t.TempDir()
		customRecDir := filepath.Join(rootCustom, ".agents", "skills", "recording-what-you-learn")
		if err := os.MkdirAll(customRecDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(customRecDir, "SKILL.md"), []byte("---\nname: recording-what-you-learn\n---\nCustom skill content\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cCustom := checkByName(t, checkScaffold(rootCustom), "scaffold:skill-recording")
		if cCustom.Status != OK || cCustom.Detail != ".agents/skills/recording-what-you-learn/ carries repository customizations" {
			t.Errorf("customized recording = %+v", cCustom)
		}

		// Missing
		rootMissing := t.TempDir()
		cMissing := checkByName(t, checkScaffold(rootMissing), "scaffold:skill-recording")
		if cMissing.Status != Warn || cMissing.Detail != ".agents/skills/recording-what-you-learn/ is missing" || cMissing.Remedy != "run 'agents init' to populate bundled skill" {
			t.Errorf("missing recording = %+v", cMissing)
		}
	})

	t.Run("skill migrating states", func(t *testing.T) {
		// Missing
		rootMissing := t.TempDir()
		cMissing := checkByName(t, checkScaffold(rootMissing), "scaffold:skill-migrating")
		if cMissing.Status != Warn || cMissing.Detail != ".agents/skills/migrating-fleet-context/ is missing" || cMissing.Remedy != "run 'agents update' or 'agents init' to refresh infrastructure skills" {
			t.Errorf("missing migrating = %+v", cMissing)
		}
	})
}

func TestDoctorStandaloneMode(t *testing.T) {
	repoRoot := t.TempDir()
	gitInit := exec.Command("git", "init", "-b", "main")
	gitInit.Dir = repoRoot
	if out, err := gitInit.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	if err := scaffold.Create(repoRoot, false); err != nil {
		t.Fatal(err)
	}

	binary := executableFile(t, repoRoot, "agents")
	for _, adapter := range harness.All() {
		if err := adapter.Wire(repoRoot, binary); err != nil {
			t.Fatal(err)
		}
	}

	deps := DependenciesFor("")
	deps.LookPath = func(name string) (string, error) {
		if name == "agents" {
			return binary, nil
		}
		if name == "gitleaks" {
			return "/tmp/gitleaks", nil
		}
		return "", errors.New("missing")
	}

	storeDir := filepath.Join(repoRoot, ".git", "agents")
	agentsDir := filepath.Join(repoRoot, ".agents")
	checks, err := RunWithDeps(repoRoot, agentsDir, storeDir, "standalone-box", binary, DefaultThresholds(), time.Now(), deps)
	if err != nil {
		t.Fatalf("RunWithDeps returned error: %v", err)
	}

	omittedChecks := map[string]bool{
		"root:exists":      true,
		"git-hooks:global": true,
		"git-hooks:links":  true,
	}

	foundAttrs := false
	for _, c := range checks {
		if omittedChecks[c.Name] {
			t.Errorf("check %q was unexpectedly present in standalone mode: %+v", c.Name, c)
		}
		if c.Status == Fail {
			t.Errorf("unexpected check failure for %s: %s (remedy: %s)", c.Name, c.Detail, c.Remedy)
		}
		if c.Name == "git-attributes" {
			foundAttrs = true
			if c.Status != OK {
				t.Errorf("git-attributes status = %q, want %q", c.Status, OK)
			}
			if c.Detail != "repository attributes are exact" {
				t.Errorf("git-attributes detail = %q, want %q", c.Detail, "repository attributes are exact")
			}
		}
	}
	if !foundAttrs {
		t.Error("git-attributes check was not found in checks")
	}
}
