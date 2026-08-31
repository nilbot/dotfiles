// Package doctor observes the installation and repo context without repairing it.
package doctor

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/BurntSushi/toml"

	"github.com/nilbot/dotfiles/agents/internal/githook"
	"github.com/nilbot/dotfiles/agents/internal/harness"
	"github.com/nilbot/dotfiles/agents/internal/record"
	"github.com/nilbot/dotfiles/agents/internal/repo"
	"github.com/nilbot/dotfiles/agents/internal/safeio"
	"github.com/nilbot/dotfiles/agents/internal/drift"
	"github.com/nilbot/dotfiles/agents/internal/trace"
)

const (
	OK   = "ok"
	Warn = "warn"
	Fail = "fail"
)

type Check struct {
	Name   string
	Status string // ok | warn | fail
	Detail string
	Remedy string
}

type Thresholds struct {
	Window             time.Duration
	Modules            int
	Days               int
	Sessions           int
	RecordingFreshness time.Duration
	// QueueDepth is where a backlog stops being a backlog and starts being an
	// inbox nobody empties.
	QueueDepth int
	// CacheMaxBytes mirrors the cap the subagent-stop hook enforces, so doctor
	// reports the same boundary the tool acts on rather than a second opinion.
	CacheMaxBytes int64
}

type GitResult struct {
	Output string
	Code   int
}

type Dependencies struct {
	LookPath              func(string) (string, error)
	Git                   func(dir string, args ...string) GitResult
	LegacyHooksPath       func(string) (string, error)
	TraceCacheDir         func(string) (string, error)
	CodexConfig           string
	AntigravityConfig     string
	HooksDir              string
	AttributesLink        string
	AttributesSource      string
	AttributesConfigValue string
	GlobalGitConfig       string
	SharedGitConfig       string
	// Root is the checkout this binary was stamped to. Kept rather than only
	// derived from, because every other path here is built by joining onto it,
	// so nothing existing can report that the root itself is gone.
	Root string
}

func DefaultThresholds() Thresholds {
	return Thresholds{
		Window:             30 * 24 * time.Hour,
		Modules:            3,
		Days:               14,
		Sessions:           20,
		RecordingFreshness: 7 * 24 * time.Hour,
		QueueDepth:         10,
		CacheMaxBytes:      1 << 30,
	}
}

// DependenciesFor builds the diagnostic against a named dotfiles checkout.
//
// The checkout root is a caller's answer, not doctor's guess: doctor compares
// HooksDir and SharedGitConfig against what Git reports, so a wrong root makes
// those checks fail on a correctly provisioned machine. The remaining paths
// stay home-relative because Git reads them from the home directory wherever
// the checkout lives.
func DependenciesFor(root string) Dependencies {
	home, _ := os.UserHomeDir()
	deps := Dependencies{
		LookPath:              exec.LookPath,
		Git:                   runGit,
		LegacyHooksPath:       repo.LegacyHooksPath,
		TraceCacheDir:         repo.TraceCacheDir,
		CodexConfig:           filepath.Join(home, ".codex", "config.toml"),
		AntigravityConfig:     filepath.Join(home, ".gemini", "antigravity-cli", "settings.json"),
		AttributesLink:        filepath.Join(home, ".gitattributes"),
		AttributesConfigValue: "~/.gitattributes",
		GlobalGitConfig:       filepath.Join(home, ".gitconfig"),
		Root:                  root,
	}
	if root != "" {
		deps.HooksDir = filepath.Join(root, "git", "hooks.d")
		deps.AttributesSource = filepath.Join(root, "git", "gitattributes")
		deps.SharedGitConfig = filepath.Join(root, "git", "gitconfig.shared")
	}
	return deps
}

func runGit(dir string, args ...string) GitResult {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = sanitizedGitEnvironment(os.Environ())
	out, err := cmd.CombinedOutput()
	if err == nil {
		return GitResult{Output: string(out)}
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return GitResult{Output: string(out), Code: exit.ExitCode()}
	}
	return GitResult{Code: 127}
}

func sanitizedGitEnvironment(environment []string) []string {
	var out []string
	for _, item := range environment {
		key, _, _ := strings.Cut(item, "=")
		if key == "GIT_DIR" || key == "GIT_WORK_TREE" || key == "GIT_INDEX_FILE" ||
			key == "GIT_OBJECT_DIRECTORY" || key == "GIT_ALTERNATE_OBJECT_DIRECTORIES" ||
			key == "GIT_CONFIG_COUNT" || key == "GIT_CONFIG_PARAMETERS" ||
			strings.HasPrefix(key, "GIT_CONFIG_KEY_") || strings.HasPrefix(key, "GIT_CONFIG_VALUE_") ||
			key == "GIT_TERMINAL_PROMPT" {
			continue
		}
		out = append(out, item)
	}
	return append(out, "GIT_TERMINAL_PROMPT=0")
}

// RunWithDeps takes storeDir as a parameter rather than resolving it through
// Dependencies.
//
// A faked-out store that silently resolves to nothing reports "all trace index
// lines are readable" and "this harness has never recorded here" -- a clean
// bill of health for a diagnostic that never found the index. That is the
// undiscriminating double this repository already has a memory entry about.
// An explicit parameter has no nil case to be wrong about.
func RunWithDeps(repoRoot, agentsDir, storeDir, thisMachine, binary string, th Thresholds, now time.Time, deps Dependencies) ([]Check, error) {
	traceResult, err := trace.Query(storeDir, trace.Filter{}, now)
	if err != nil {
		return nil, errors.New("trace index could not be read")
	}

	var checks []Check
	checks = append(checks, checkBinary(binary, deps.LookPath))
	wiringTimes := map[string]time.Time{}
	var codexTrustKeys []string
	for _, adapter := range harness.All() {
		check, wiredAt, trustKeys := checkWiring(adapter, repoRoot, binary)
		checks = append(checks, check)
		wiringTimes[adapter.Name()] = wiredAt
		if adapter.Name() == "codex" {
			codexTrustKeys = trustKeys
		}
	}
	checks = append(checks, checkCodexTrust(deps.CodexConfig, codexTrustKeys))
	checks = append(checks, checkAntigravityTrust(deps.AntigravityConfig, repoRoot))
	for _, adapter := range harness.All() {
		checks = append(checks, checkRecording(adapter, traceResult.Records, wiringTimes[adapter.Name()], th.RecordingFreshness, now))
	}
	checks = append(checks, checkGitleaks(deps.LookPath))
	checks = append(checks, rootChecks(deps)...)
	checks = append(checks, checkGitHooks(repoRoot, binary, deps)...)
	checks = append(checks, checkGitAttributes(repoRoot, deps))
	checks = append(checks, checkMachine(thisMachine))
	if traceResult.Skipped > 0 {
		checks = append(checks, Check{Name: "trace-index", Status: Warn, Detail: fmt.Sprintf("%d unreadable line(s) skipped", traceResult.Skipped), Remedy: "repair malformed JSONL or merge conflict markers; preserve valid lines"})
	} else {
		checks = append(checks, Check{Name: "trace-index", Status: OK, Detail: "all trace index lines are readable"})
	}
	// An unresolvable cache root is not a reason to skip the pointer checks: an
	// empty root simply makes every gone transcript count as lost, which is what
	// the check said before the cache existed at all.
	var cacheRoot string
	if deps.TraceCacheDir != nil {
		cacheRoot, _ = deps.TraceCacheDir(repoRoot)
	}
	checks = append(checks, checkPointers(traceResult.Records, thisMachine, cacheRoot)...)
	checks = append(checks, checkScaffold(repoRoot)...)
	checks = append(checks, checkDocsFreshness(repoRoot, now))
	checks = append(checks, checkStoreSize(cacheRoot, th.CacheMaxBytes))
	checks = append(checks, LaneHealth(traceResult.Records, th, now)...)
	return checks, nil
}

func checkBinary(binary string, lookPath func(string) (string, error)) Check {
	if lookPath == nil {
		return Check{Name: "binary", Status: Fail, Detail: "agents executable lookup is unavailable", Remedy: "rebuild and reinstall agents"}
	}
	pathBinary, err := lookPath("agents")
	if err != nil {
		return Check{Name: "binary", Status: Fail, Detail: "agents is not available on PATH", Remedy: "install the current agents binary and ensure it is on PATH"}
	}
	running, err := filepath.EvalSymlinks(binary)
	if err != nil {
		return Check{Name: "binary", Status: Fail, Detail: "the running agents executable cannot be resolved", Remedy: "rebuild and reinstall agents"}
	}
	onPath, err := filepath.EvalSymlinks(pathBinary)
	if err != nil {
		return Check{Name: "binary", Status: Fail, Detail: "the agents executable on PATH cannot be resolved", Remedy: "repair or reinstall the agents executable on PATH"}
	}
	runningInfo, err := os.Stat(running)
	if err != nil {
		return Check{Name: "binary", Status: Fail, Detail: "the running agents executable cannot be inspected", Remedy: "rebuild and reinstall agents"}
	}
	pathInfo, err := os.Stat(onPath)
	if err != nil || !os.SameFile(runningInfo, pathInfo) {
		return Check{Name: "binary", Status: Fail, Detail: "agents on PATH is not the running executable", Remedy: "rebuild and reinstall agents so PATH resolves to this executable"}
	}
	return Check{Name: "binary", Status: OK, Detail: "PATH resolves to the running executable"}
}

func checkWiring(a harness.Adapter, repoRoot, binary string) (Check, time.Time, []string) {
	name := "wiring:" + a.Name()
	path := a.WireConfigPath(repoRoot)
	b, info, err := safeio.ReadRegularInfo(path)
	if err != nil {
		return Check{Name: name, Status: Fail, Detail: "generated hook config is unavailable", Remedy: "run `agents wire`"}, time.Time{}, nil
	}
	var cfg map[string]any
	if err := json.Unmarshal(b, &cfg); err != nil {
		return Check{Name: name, Status: Fail, Detail: "generated hook config is malformed JSON", Remedy: "fix or remove it, then run `agents wire`"}, info.ModTime(), nil
	}

	if a.Name() == "antigravity" {
		return checkWiringNamedGroups(a, path, cfg, info, binary)
	}
	return checkWiringNestedHooks(a, path, cfg, info, binary)
}

func checkWiringNamedGroups(a harness.Adapter, path string, cfg map[string]any, info os.FileInfo, binary string) (Check, time.Time, []string) {
	name := "wiring:" + a.Name()
	agentsGroup, ok := cfg["agents"].(map[string]any)
	if !ok {
		return Check{Name: name, Status: Fail, Detail: "generated hook config has no agents object", Remedy: "run `agents wire`"}, info.ModTime(), nil
	}

	var keys []string
	for _, ev := range a.Events() {
		rawList, ok := agentsGroup[ev.Vendor].([]any)
		if !ok {
			return Check{Name: name, Status: Fail, Detail: "required event " + ev.Vendor + " is missing", Remedy: "run `agents wire`"}, info.ModTime(), nil
		}
		wantCommand := harness.HookCommand(binary, a.Name(), ev.Semantic)
		matches := 0
		isToolEvent := ev.Vendor == "PreToolUse" || ev.Vendor == "PostToolUse"

		if isToolEvent {
			for groupIndex, rawGroup := range rawList {
				group, ok := rawGroup.(map[string]any)
				if !ok {
					continue
				}
				matcher, matcherPresent := group["matcher"]
				matcherOK := (!matcherPresent && ev.Matcher == "") || (matcherPresent && ev.Matcher != "" && matcher == ev.Matcher)
				inner, _ := group["hooks"].([]any)
				for hookIndex, rawHook := range inner {
					hook, ok := rawHook.(map[string]any)
					if !ok {
						continue
					}
					command, _ := hook["command"].(string)
					typ, _ := hook["type"].(string)
					if command == wantCommand && typ == "command" && matcherOK {
						matches++
						keys = append(keys, path+":"+snakeEvent(ev.Vendor)+fmt.Sprintf(":%d:%d", groupIndex, hookIndex))
						continue
					}
					if command == wantCommand || harness.IsOwnedHookCommand(command) {
						return Check{Name: name, Status: Fail, Detail: "generated hook for " + ev.Vendor + " has stale structural fields", Remedy: "run `agents wire`"}, info.ModTime(), nil
					}
				}
			}
		} else {
			for itemIndex, rawItem := range rawList {
				item, ok := rawItem.(map[string]any)
				if !ok {
					continue
				}
				if innerHooks, hasHooks := item["hooks"].([]any); hasHooks {
					for _, rawHook := range innerHooks {
						if hook, ok := rawHook.(map[string]any); ok {
							cmd, _ := hook["command"].(string)
							if cmd == wantCommand || harness.IsOwnedHookCommand(cmd) {
								return Check{Name: name, Status: Fail, Detail: "generated hook for " + ev.Vendor + " has stale structural fields", Remedy: "run `agents wire`"}, info.ModTime(), nil
							}
						}
					}
					continue
				}
				command, _ := item["command"].(string)
				typ, _ := item["type"].(string)
				_, hasMatcher := item["matcher"]
				if command == wantCommand && typ == "command" && !hasMatcher {
					matches++
					keys = append(keys, path+":"+snakeEvent(ev.Vendor)+fmt.Sprintf(":%d:0", itemIndex))
					continue
				}
				if command == wantCommand || harness.IsOwnedHookCommand(command) {
					return Check{Name: name, Status: Fail, Detail: "generated hook for " + ev.Vendor + " has stale structural fields", Remedy: "run `agents wire`"}, info.ModTime(), nil
				}
			}
		}

		if matches != 1 {
			return Check{Name: name, Status: Fail, Detail: fmt.Sprintf("required event %s has %d exact generated hooks", ev.Vendor, matches), Remedy: "run `agents wire`"}, info.ModTime(), nil
		}
	}

	generatedCount := 0
	for _, rawList := range agentsGroup {
		items, _ := rawList.([]any)
		for _, rawItem := range items {
			item, _ := rawItem.(map[string]any)
			if inner, ok := item["hooks"].([]any); ok {
				for _, rawHook := range inner {
					hook, _ := rawHook.(map[string]any)
					command, _ := hook["command"].(string)
					if harness.IsOwnedHookCommand(command) {
						generatedCount++
					}
				}
			} else {
				command, _ := item["command"].(string)
				if harness.IsOwnedHookCommand(command) {
					generatedCount++
				}
			}
		}
	}
	if generatedCount != len(a.Events()) {
		return Check{Name: name, Status: Fail, Detail: fmt.Sprintf("hook config contains %d generated commands; want %d exact required hooks", generatedCount, len(a.Events())), Remedy: "run `agents wire`"}, info.ModTime(), nil
	}

	if stale := resemblingButUnownedNamedGroups(agentsGroup); len(stale) > 0 {
		return Check{
			Name:   name,
			Status: Warn,
			Detail: fmt.Sprintf("%d hook command(s) look generated but run a different binary, e.g. %s", len(stale), stale[0]),
			Remedy: "these are not `agents wire`'s to remove; delete them from " + path + " by hand",
		}, info.ModTime(), keys
	}

	return Check{Name: name, Status: OK, Detail: "all required generated hooks are exact"}, info.ModTime(), keys
}

func checkWiringNestedHooks(a harness.Adapter, path string, cfg map[string]any, info os.FileInfo, binary string) (Check, time.Time, []string) {
	name := "wiring:" + a.Name()
	hooks, ok := cfg["hooks"].(map[string]any)
	if !ok {
		return Check{Name: name, Status: Fail, Detail: "generated hook config has no hooks object", Remedy: "run `agents wire`"}, info.ModTime(), nil
	}

	var keys []string
	for _, ev := range a.Events() {
		groups, ok := hooks[ev.Vendor].([]any)
		if !ok {
			return Check{Name: name, Status: Fail, Detail: "required event " + ev.Vendor + " is missing", Remedy: "run `agents wire`"}, info.ModTime(), nil
		}
		wantCommand := harness.HookCommand(binary, a.Name(), ev.Semantic)
		matches := 0
		for groupIndex, rawGroup := range groups {
			group, ok := rawGroup.(map[string]any)
			if !ok {
				continue
			}
			matcher, matcherPresent := group["matcher"]
			matcherOK := (!matcherPresent && ev.Matcher == "") || (matcherPresent && ev.Matcher != "" && matcher == ev.Matcher)
			inner, _ := group["hooks"].([]any)
			for hookIndex, rawHook := range inner {
				hook, ok := rawHook.(map[string]any)
				if !ok {
					continue
				}
				command, _ := hook["command"].(string)
				typ, _ := hook["type"].(string)
				if command == wantCommand && typ == "command" && matcherOK {
					matches++
					keys = append(keys, path+":"+snakeEvent(ev.Vendor)+fmt.Sprintf(":%d:%d", groupIndex, hookIndex))
					continue
				}
				if command == wantCommand || harness.IsOwnedHookCommand(command) {
					return Check{Name: name, Status: Fail, Detail: "generated hook for " + ev.Vendor + " has stale structural fields", Remedy: "run `agents wire`"}, info.ModTime(), nil
				}
			}
		}
		if matches != 1 {
			return Check{Name: name, Status: Fail, Detail: fmt.Sprintf("required event %s has %d exact generated hooks", ev.Vendor, matches), Remedy: "run `agents wire`"}, info.ModTime(), nil
		}
	}
	generatedCount := 0
	for _, rawGroups := range hooks {
		groups, _ := rawGroups.([]any)
		for _, rawGroup := range groups {
			group, _ := rawGroup.(map[string]any)
			inner, _ := group["hooks"].([]any)
			for _, rawHook := range inner {
				hook, _ := rawHook.(map[string]any)
				command, _ := hook["command"].(string)
				if harness.IsOwnedHookCommand(command) {
					generatedCount++
				}
			}
		}
	}
	if generatedCount != len(a.Events()) {
		return Check{Name: name, Status: Fail, Detail: fmt.Sprintf("hook config contains %d generated commands; want %d exact required hooks", generatedCount, len(a.Events())), Remedy: "run `agents wire`"}, info.ModTime(), nil
	}
	// Everything above counts hooks we own. An entry shaped like ours but run
	// from some other binary is owned by nobody: `agents wire` will not replace
	// it, because replacing it would mean deleting a command we cannot prove is
	// ours, and the harness runs it anyway -- so it fails at every session
	// start while this check says the wiring is exact. Report it; never delete.
	if stale := resemblingButUnowned(hooks); len(stale) > 0 {
		return Check{
			Name:   name,
			Status: Warn,
			Detail: fmt.Sprintf("%d hook command(s) look generated but run a different binary, e.g. %s", len(stale), stale[0]),
			Remedy: "these are not `agents wire`'s to remove; delete them from " + path + " by hand",
		}, info.ModTime(), keys
	}
	return Check{Name: name, Status: OK, Detail: "all required generated hooks are exact"}, info.ModTime(), keys
}

func resemblingButUnownedNamedGroups(agentsGroup map[string]any) []string {
	var found []string
	for _, rawList := range agentsGroup {
		items, _ := rawList.([]any)
		for _, rawItem := range items {
			item, _ := rawItem.(map[string]any)
			if inner, ok := item["hooks"].([]any); ok {
				for _, rawHook := range inner {
					hook, _ := rawHook.(map[string]any)
					command, _ := hook["command"].(string)
					if harness.ResemblesHookCommand(command) && !harness.IsOwnedHookCommand(command) {
						found = append(found, command)
					}
				}
			} else {
				command, _ := item["command"].(string)
				if harness.ResemblesHookCommand(command) && !harness.IsOwnedHookCommand(command) {
					found = append(found, command)
				}
			}
		}
	}
	sort.Strings(found)
	return found
}

// resemblingButUnowned returns the hook commands that have our shape but that
// ParseHookCommand refuses, newest-looking first is not meaningful here so the
// order is whatever the config gives.
func resemblingButUnowned(hooks map[string]any) []string {
	var found []string
	for _, rawGroups := range hooks {
		groups, _ := rawGroups.([]any)
		for _, rawGroup := range groups {
			group, _ := rawGroup.(map[string]any)
			inner, _ := group["hooks"].([]any)
			for _, rawHook := range inner {
				hook, _ := rawHook.(map[string]any)
				command, _ := hook["command"].(string)
				if harness.ResemblesHookCommand(command) && !harness.IsOwnedHookCommand(command) {
					found = append(found, command)
				}
			}
		}
	}
	sort.Strings(found)
	return found
}

func snakeEvent(s string) string {
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func checkCodexTrust(configPath string, currentKeys []string) Check {
	remedy := "open human Codex `/hooks` and compare Installed and Active counts; doctor never grants trust"
	var cfg struct {
		Hooks struct {
			State map[string]struct {
				TrustedHash string `toml:"trusted_hash"`
				Enabled     *bool  `toml:"enabled"`
			} `toml:"state"`
		} `toml:"hooks"`
	}
	b, err := safeio.ReadRegular(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Check{Name: "trust:codex", Status: Warn, Detail: "Codex config is missing; no persisted trust entry", Remedy: remedy}
		}
		return Check{Name: "trust:codex", Status: Fail, Detail: "Codex config is not a readable regular file", Remedy: remedy}
	}
	if _, err := toml.Decode(string(b), &cfg); err != nil {
		return Check{Name: "trust:codex", Status: Fail, Detail: "Codex config is unreadable or malformed", Remedy: remedy}
	}
	if len(currentKeys) == 0 {
		return Check{Name: "trust:codex", Status: Warn, Detail: "current Codex hook positions are unavailable; trust cannot be correlated", Remedy: remedy}
	}
	present := 0
	disabled := 0
	for _, key := range currentKeys {
		if state, ok := cfg.Hooks.State[key]; ok && state.TrustedHash != "" {
			present++
			if state.Enabled != nil && !*state.Enabled {
				disabled++
			}
		}
	}
	switch {
	case present == 0:
		return Check{Name: "trust:codex", Status: Warn, Detail: "no persisted trust entry for current hook positions", Remedy: remedy}
	case present != len(currentKeys):
		return Check{Name: "trust:codex", Status: Warn, Detail: fmt.Sprintf("persisted trust entries incomplete (%d/%d); current match unknown", present, len(currentKeys)), Remedy: remedy}
	case disabled > 0:
		return Check{Name: "trust:codex", Status: Warn, Detail: fmt.Sprintf("persisted trust entries present but %d/%d current hooks explicitly disabled", disabled, len(currentKeys)), Remedy: remedy}
	default:
		return Check{Name: "trust:codex", Status: OK, Detail: "persisted trust entries present; no current hook is explicitly disabled; `/hooks` review state is not disclosed"}
	}
}

func checkAntigravityTrust(configPath, repoRoot string) Check {
	name := "trust:antigravity"
	remedy := "for CLI: add repository root to trustedWorkspaces in ~/.gemini/antigravity-cli/settings.json"

	if configPath == "" {
		return Check{
			Name:   name,
			Status: OK,
			Detail: "Desktop App executes on open (no trust gate); CLI config not specified",
			Remedy: remedy,
		}
	}

	b, err := safeio.ReadRegular(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Check{
				Name:   name,
				Status: OK,
				Detail: "Desktop App executes on open (no trust gate); CLI config not found",
				Remedy: remedy,
			}
		}
		return Check{
			Name:   name,
			Status: Warn,
			Detail: "Desktop App executes on open (no trust gate); CLI config is not a readable regular file",
			Remedy: remedy,
		}
	}

	var cfg struct {
		TrustedWorkspaces []string `json:"trustedWorkspaces"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return Check{
			Name:   name,
			Status: Warn,
			Detail: "Desktop App executes on open (no trust gate); CLI config is malformed JSON",
			Remedy: remedy,
		}
	}

	for _, ws := range cfg.TrustedWorkspaces {
		if ws == repoRoot || filepath.Clean(ws) == filepath.Clean(repoRoot) {
			return Check{
				Name:   name,
				Status: OK,
				Detail: "Desktop App executes on open (no trust gate); CLI trustedWorkspaces entry confirmed",
			}
		}
	}

	return Check{
		Name:   name,
		Status: OK,
		Detail: "Desktop App executes on open (no trust gate); CLI trustedWorkspaces entry not found",
		Remedy: remedy,
	}
}

func checkRecording(a harness.Adapter, recs []record.Record, wiringTime time.Time, freshness time.Duration, now time.Time) Check {
	name := "recording:" + a.Name()
	var latest time.Time
	for _, rec := range recs {
		if rec.Harness == a.Name() && rec.When.After(latest) {
			latest = rec.When
		}
	}
	remedy := "use the harness trust UI and confirm a new trace appears after the next session event"
	if latest.IsZero() {
		return Check{Name: name, Status: Warn, Detail: "this harness has never recorded here", Remedy: remedy}
	}
	if latest.After(now) {
		return Check{Name: name, Status: Warn, Detail: "latest record has future clock skew", Remedy: "check clocks on machines writing this trace index"}
	}
	if !wiringTime.IsZero() && latest.Before(wiringTime) {
		return Check{Name: name, Status: Warn, Detail: "latest record predates current wiring", Remedy: remedy}
	}
	if now.Sub(latest) > freshness {
		return Check{Name: name, Status: Warn, Detail: "latest record is stale", Remedy: remedy}
	}
	return Check{Name: name, Status: OK, Detail: "recent recording observed"}
}

var installedHookNames = []string{"pre-commit", "commit-msg", "post-merge", "post-checkout"}

func checkLocalHooks(repoRoot string, git func(dir string, args ...string) GitResult) Check {
	local := git(repoRoot, "config", "--local", "--get-all", "core.hooksPath")
	localValues := configValues(local.Output)
	switch {
	case local.Code == 1:
		return Check{Name: "git-hooks:local", Status: OK, Detail: "no repository-local core.hooksPath override"}
	case local.Code != 0:
		return Check{Name: "git-hooks:local", Status: Fail, Detail: "repository-local core.hooksPath could not be read", Remedy: "inspect repository or linked-worktree Git configuration"}
	case len(localValues) == 0:
		return Check{Name: "git-hooks:local", Status: Fail, Detail: "repository-local core.hooksPath returned an empty value"}
	default:
		return Check{Name: "git-hooks:local", Status: Warn, Detail: fmt.Sprintf("repository-local core.hooksPath override is set (%d value(s))", len(localValues)), Remedy: "the global agents hooks are shadowed here; chain them from the local hook directory if desired"}
	}
}

func checkGitHooks(repoRoot, binary string, deps Dependencies) []Check {
	if deps.Git == nil {
		name := "git-hooks:global"
		if deps.Root == "" || deps.HooksDir == "" {
			name = "git-hooks:local"
		}
		return []Check{{Name: name, Status: Fail, Detail: "Git diagnostic runner is unavailable"}}
	}
	if deps.Root == "" || deps.HooksDir == "" {
		return []Check{
			checkLocalHooks(repoRoot, deps.Git),
			checkLegacyHooks(repoRoot, deps),
		}
	}
	var checks []Check
	global := deps.Git(repoRoot, "config", "--global", "--includes", "--null", "--show-origin", "--get-all", "core.hooksPath")
	globalValues, globalParseErr := configOriginValues(global.Output)
	switch {
	case global.Code == 1:
		checks = append(checks, Check{Name: "git-hooks:global", Status: Fail, Detail: "global core.hooksPath is unset", Remedy: "run the reviewed global hook installer"})
	case global.Code != 0:
		checks = append(checks, Check{Name: "git-hooks:global", Status: Fail, Detail: "global core.hooksPath could not be read", Remedy: "inspect global Git configuration"})
	case globalParseErr != nil || len(globalValues) != 1:
		checks = append(checks, Check{Name: "git-hooks:global", Status: Fail, Detail: fmt.Sprintf("global core.hooksPath has %d values", len(globalValues)), Remedy: "resolve the global values deliberately"})
	case globalValues[0].Origin != "file:"+deps.GlobalGitConfig || globalValues[0].Value != deps.HooksDir:
		checks = append(checks, Check{Name: "git-hooks:global", Status: Fail, Detail: "global core.hooksPath value or origin is unexpected", Remedy: "preserve included settings and restore the reviewed primary global setting deliberately"})
	default:
		checks = append(checks, Check{Name: "git-hooks:global", Status: OK, Detail: "global core.hooksPath is exact"})
	}

	checks = append(checks, checkLocalHooks(repoRoot, deps.Git))

	effective := deps.Git(repoRoot, "config", "--get", "core.hooksPath")
	effectiveValues := configValues(effective.Output)
	switch {
	case effective.Code != 0:
		checks = append(checks, Check{Name: "git-hooks:effective", Status: Fail, Detail: "effective core.hooksPath could not be read", Remedy: "inspect all Git configuration scopes"})
	case len(effectiveValues) != 1:
		checks = append(checks, Check{Name: "git-hooks:effective", Status: Fail, Detail: fmt.Sprintf("effective core.hooksPath has %d values", len(effectiveValues)), Remedy: "inspect all Git configuration scopes"})
	case effectiveValues[0] != deps.HooksDir:
		checks = append(checks, Check{Name: "git-hooks:effective", Status: Warn, Detail: "effective core.hooksPath shadows the agents hook directory", Remedy: "inspect local, worktree, command, and environment Git configuration"})
	default:
		checks = append(checks, Check{Name: "git-hooks:effective", Status: OK, Detail: "effective core.hooksPath is exact"})
	}

	checks = append(checks, checkInstalledLinks(deps.HooksDir, binary))
	checks = append(checks, checkLegacyHooks(repoRoot, deps))
	return checks
}

func configValues(output string) []string {
	var values []string
	for _, line := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
		if line != "" {
			values = append(values, line)
		}
	}
	return values
}

type configOriginValue struct {
	Origin string
	Value  string
}

func configOriginValues(output string) ([]configOriginValue, error) {
	if output == "" {
		return nil, nil
	}
	parts := strings.Split(output, "\x00")
	if parts[len(parts)-1] != "" || len(parts)%2 != 1 {
		return nil, errors.New("malformed Git origin output")
	}
	parts = parts[:len(parts)-1]
	values := make([]configOriginValue, 0, len(parts)/2)
	for i := 0; i < len(parts); i += 2 {
		if parts[i] == "" {
			return nil, errors.New("malformed Git origin output")
		}
		values = append(values, configOriginValue{Origin: parts[i], Value: parts[i+1]})
	}
	return values, nil
}

func checkInstalledLinks(hooksDir, binary string) Check {
	binaryInfo, err := os.Stat(binary)
	if err != nil {
		return Check{Name: "git-hooks:links", Status: Fail, Detail: "current binary cannot be inspected", Remedy: "rebuild and reinstall agents"}
	}
	for _, name := range installedHookNames {
		path := filepath.Join(hooksDir, name)
		info, err := os.Lstat(path)
		if err != nil {
			return Check{Name: "git-hooks:links", Status: Fail, Detail: name + " hook link is missing or unreadable", Remedy: "run the reviewed global hook installer"}
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return Check{Name: "git-hooks:links", Status: Fail, Detail: name + " is not an owned symlink", Remedy: "preserve or move the foreign hook deliberately before installing"}
		}
		resolved, err := os.Stat(path)
		if err != nil || !os.SameFile(binaryInfo, resolved) {
			return Check{Name: "git-hooks:links", Status: Fail, Detail: name + " does not resolve to the current binary", Remedy: "run the reviewed global hook installer"}
		}
	}
	return Check{Name: "git-hooks:links", Status: OK, Detail: "all four installed hook links resolve to the current binary"}
}

func checkLegacyHooks(repoRoot string, deps Dependencies) Check {
	if deps.LegacyHooksPath == nil {
		return Check{Name: "git-hooks:legacy", Status: Fail, Detail: "repository legacy hooks directory could not be resolved", Remedy: "inspect the repository Git directory"}
	}
	dir, err := deps.LegacyHooksPath(repoRoot)
	if err != nil || !filepath.IsAbs(dir) {
		return Check{Name: "git-hooks:legacy", Status: Fail, Detail: "repository legacy hooks directory could not be resolved", Remedy: "inspect the repository Git directory"}
	}
	var found []string
	for _, name := range installedHookNames {
		if githook.IsRetiredShim(filepath.Join(dir, name)) {
			found = append(found, name)
		}
	}
	if len(found) > 0 {
		return Check{Name: "git-hooks:legacy", Status: Warn, Detail: "exact retired legacy dispatcher remains for " + strings.Join(found, ", "), Remedy: "remove only the exact retired shim after preserving foreign hooks"}
	}
	return Check{Name: "git-hooks:legacy", Status: OK, Detail: "no exact retired legacy dispatcher detected"}
}

var repoAttributeLines = []string{
	".agents/** linguist-generated=true",
}

func checkGitAttributes(repoRoot string, deps Dependencies) Check {
	if deps.Root == "" || deps.AttributesSource == "" {
		repoAttrs, err := safeio.ReadRegular(filepath.Join(repoRoot, ".gitattributes"))
		if err != nil {
			return Check{Name: "git-attributes", Status: Fail, Detail: "repository .gitattributes is unavailable", Remedy: "run `agents init` after reviewing existing attributes"}
		}
		for _, line := range repoAttributeLines {
			if !hasExactLine(repoAttrs, line) {
				return Check{Name: "git-attributes", Status: Fail, Detail: "repository .gitattributes lacks an exact agents rule", Remedy: "run `agents init` after reviewing existing attributes"}
			}
		}
		return Check{Name: "git-attributes", Status: OK, Detail: "repository attributes are exact"}
	}
	if deps.Git == nil {
		return Check{Name: "git-attributes", Status: Fail, Detail: "Git diagnostic runner is unavailable", Remedy: "inspect global Git attributes configuration"}
	}
	result := deps.Git(repoRoot, "config", "--global", "--includes", "--null", "--show-origin", "--get-all", "core.attributesFile")
	values, parseErr := configOriginValues(result.Output)
	if result.Code != 0 || parseErr != nil || len(values) != 1 || values[0].Origin != "file:"+deps.SharedGitConfig || values[0].Value != deps.AttributesConfigValue {
		return Check{Name: "git-attributes", Status: Fail, Detail: "global core.attributesFile is missing, unreadable, multiple, or unexpected", Remedy: "restore the reviewed global attributes configuration"}
	}
	linkInfo, err := os.Lstat(deps.AttributesLink)
	if err != nil || linkInfo.Mode()&os.ModeSymlink == 0 {
		return Check{Name: "git-attributes", Status: Fail, Detail: "global attributes link is missing or not a symlink", Remedy: "run the reviewed global hook installer"}
	}
	linkTarget, err := os.Stat(deps.AttributesLink)
	if err != nil {
		return Check{Name: "git-attributes", Status: Fail, Detail: "global attributes link is broken", Remedy: "run the reviewed global hook installer"}
	}
	sourceInfo, err := os.Stat(deps.AttributesSource)
	if err != nil || !sourceInfo.Mode().IsRegular() || !os.SameFile(linkTarget, sourceInfo) {
		return Check{Name: "git-attributes", Status: Fail, Detail: "global attributes link does not resolve to the tracked source", Remedy: "run the reviewed global hook installer"}
	}
	// The source has to be readable and has to be the tracked file, checked
	// above. Its contents are no longer asserted: the one rule that lived here
	// was the trace merge=union attribute, which retired with the tracked
	// index. Asserting a specific line again would mean this check fails the
	// moment the file legitimately holds nothing.
	if _, err := safeio.ReadRegular(deps.AttributesSource); err != nil {
		return Check{Name: "git-attributes", Status: Fail, Detail: "global attributes source is unreadable", Remedy: "restore the tracked attributes source"}
	}
	repoAttrs, err := safeio.ReadRegular(filepath.Join(repoRoot, ".gitattributes"))
	if err != nil {
		return Check{Name: "git-attributes", Status: Fail, Detail: "repository .gitattributes is unavailable", Remedy: "run `agents init` after reviewing existing attributes"}
	}
	for _, line := range repoAttributeLines {
		if !hasExactLine(repoAttrs, line) {
			return Check{Name: "git-attributes", Status: Fail, Detail: "repository .gitattributes lacks an exact agents rule", Remedy: "run `agents init` after reviewing existing attributes"}
		}
	}
	return Check{Name: "git-attributes", Status: OK, Detail: "global and repository attributes are exact"}
}

func hasExactLine(contents []byte, want string) bool {
	for _, line := range strings.Split(strings.ReplaceAll(string(contents), "\r\n", "\n"), "\n") {
		if line == want {
			return true
		}
	}
	return false
}

func checkPointers(recs []record.Record, thisMachine, cacheRoot string) []Check {
	unverified := 0
	for _, rec := range recs {
		if rec.Transcript == "" || !rec.PointerVerified {
			unverified++
		}
	}
	checks := []Check{{Name: "pointers:unverified", Status: OK, Detail: fmt.Sprintf("%d unverified pointer(s)", unverified)}}
	if unverified > 0 {
		checks[0].Status = Warn
		checks[0].Remedy = "unverified pointers cannot be materialized reliably"
	}
	if thisMachine == "" {
		verified := len(recs) - unverified
		checks = append(checks, Check{Name: "pointers:ownership", Status: Warn, Detail: fmt.Sprintf("machine identity unavailable; %d verified pointer(s) not classified", verified), Remedy: "restore the machine-local identity file"})
		return checks
	}
	localUnreachable := 0
	cached := 0
	remote := map[string]int{}
	unknownOwnership := 0
	for _, rec := range recs {
		if rec.Transcript == "" || !rec.PointerVerified {
			continue
		}
		switch {
		case rec.Machine == "":
			unknownOwnership++
		case rec.Machine != thisMachine:
			remote[rec.Machine]++
		default:
			// Three answers, not two. Asking only whether the harness still has
			// it counted a transcript we had successfully copied as lost, so the
			// remedy this check prints could be followed perfectly and never
			// move the number -- which teaches the reader to ignore the row.
			if _, _, err := trace.Resolve(cacheRoot, rec); err != nil {
				localUnreachable++
			} else if _, serr := os.Stat(rec.Transcript); serr != nil {
				cached++
			}
		}
	}
	if cached > 0 {
		// ok, not a warning: the harness dropped these and the copy is why they
		// still exist. Reported rather than silent because it is the only
		// evidence that caching is doing anything.
		checks = append(checks, Check{Name: "pointers:cached", Status: OK, Detail: fmt.Sprintf("%d transcript(s) survive only in the local cache", cached)})
	}
	// There is deliberately no check for locally unreachable pointers.
	//
	// It used to warn, with the remedy "run `agents trace cache` sooner". That
	// is advice to win a race the design cannot win: subagent transcripts are
	// deleted mid-session on a schedule nothing can anticipate, which is why
	// the subagent-stop hook caches unconditionally. On this repository the
	// check stood at 30 of 63 records and exited 1 on a healthy machine, every
	// day, for a condition nobody could act on. A diagnostic that reports an
	// unfixable normal state teaches its reader to ignore diagnostics.
	if len(remote) > 0 {
		machines := make([]string, 0, len(remote))
		for machine := range remote {
			machines = append(machines, machine)
		}
		sort.Strings(machines)
		parts := make([]string, 0, len(machines))
		for _, machine := range machines {
			parts = append(parts, fmt.Sprintf("%s=%d", machine, remote[machine]))
		}
		checks = append(checks, Check{Name: "pointers:remote", Status: Warn, Detail: strings.Join(parts, ", "), Remedy: "materialize those pointers on their source machines"})
	}
	if unknownOwnership > 0 {
		checks = append(checks, Check{Name: "pointers:ownership", Status: Warn, Detail: fmt.Sprintf("%d verified pointer(s) have no machine provenance", unknownOwnership), Remedy: "preserve the record but do not resolve it as local"})
	}
	return checks
}

func checkMachine(thisMachine string) Check {
	if thisMachine == "" {
		return Check{Name: "machine-id", Status: Warn, Detail: "machine identity is missing or unreadable", Remedy: "restore the machine-local identity; doctor does not create it"}
	}
	return Check{Name: "machine-id", Status: OK, Detail: "machine identity is readable"}
}

// checkDocsFreshness is the write-side leading indicator, and the only one.
//
// The capture apparatus this replaced measured itself thoroughly -- draft rate,
// promotion rate, the age of the oldest pending draft -- and measured retrieval
// not at all. Deleting it without leaving anything behind would have ended with
// less instrumentation than before, so this is what remains: how long since the
// repository last learned something it wrote down.
//
// It is deliberately weak. It cannot tell a quiet fortnight from a broken
// habit, and it says nothing about whether an entry was ever read. Both limits
// are stated in docs/design/2026-08-19-knowledge-is-documentation.md rather than
// papered over with a proxy metric that would measure what is easy instead of
// what matters.
//
// Absent docs/qna/ is not applicable rather than a failure: most repositories
// have not adopted this, and a check that fails everywhere teaches people to
// ignore the whole report.
func checkDocsFreshness(repoRoot string, now time.Time) Check {
	dir := filepath.Join(repoRoot, "docs", "qna")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Check{Name: "docs:qna", Status: OK, Detail: "no docs/qna in this repository"}
	}
	var newest time.Time
	count := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || e.Name() == "README.md" {
			continue
		}
		count++
		if info, err := e.Info(); err == nil && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
	}
	if count == 0 {
		return Check{Name: "docs:qna", Status: OK, Detail: "docs/qna is empty; nothing recorded yet"}
	}
	days := int(now.Sub(newest).Hours() / 24)
	return Check{Name: "docs:qna", Status: OK,
		Detail: fmt.Sprintf("%d entr(ies); newest written %d day(s) ago", count, days)}
}

// checkStoreSize reports the cache against the caps the hook enforces.
func checkStoreSize(cacheRoot string, maxBytes int64) Check {
	if cacheRoot == "" {
		return Check{Name: "store:size", Status: OK, Detail: "no local cache resolved"}
	}
	var total int64
	err := filepath.WalkDir(cacheRoot, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		return Check{Name: "store:size", Status: Warn, Detail: "the local cache size could not be measured", Remedy: "inspect the machine-local store"}
	}
	mb := float64(total) / (1024 * 1024)
	if maxBytes > 0 && total > maxBytes {
		return Check{Name: "store:size", Status: Warn, Detail: fmt.Sprintf("the transcript cache holds %.0f MB, over the %.0f MB cap", mb, float64(maxBytes)/(1024*1024)), Remedy: "run `agents trace cache prune --retention --yes`"}
	}
	return Check{Name: "store:size", Status: OK, Detail: fmt.Sprintf("the transcript cache holds %.0f MB", mb)}
}

func checkScaffold(repoRoot string) []Check {
	report, _ := drift.InspectRepo(repoRoot)
	var checks []Check

	// 1. scaffold:router
	switch report.RouterState {
	case drift.RouterCleanCurrent:
		checks = append(checks, Check{
			Name:   "scaffold:router",
			Status: OK,
			Detail: "root AGENTS.md matches canonical template",
		})
	case drift.RouterCleanLegacy:
		checks = append(checks, Check{
			Name:   "scaffold:router",
			Status: Warn,
			Detail: "root AGENTS.md uses a legacy canonical template",
			Remedy: "run the 'migrating-fleet-context' agent skill to update",
		})
	case drift.RouterDrifted:
		checks = append(checks, Check{
			Name:   "scaffold:router",
			Status: Warn,
			Detail: "root AGENTS.md contains unpartitioned domain rules or custom drift",
			Remedy: "run the 'migrating-fleet-context' agent skill to un-nest domain rules into .agents/AGENTS.md",
		})
	case drift.RouterMissing:
		fallthrough
	default:
		checks = append(checks, Check{
			Name:   "scaffold:router",
			Status: Fail,
			Detail: "root AGENTS.md is missing",
			Remedy: "run 'agents init' to scaffold",
		})
	}

	// 2. scaffold:symlink
	if report.SymlinkState == "ok" {
		checks = append(checks, Check{
			Name:   "scaffold:symlink",
			Status: OK,
			Detail: "CLAUDE.md is a relative symlink to AGENTS.md",
		})
	} else {
		checks = append(checks, Check{
			Name:   "scaffold:symlink",
			Status: Fail,
			Detail: "CLAUDE.md symlink is invalid (" + report.SymlinkState + ")",
			Remedy: "run 'agents init' or recreate relative symlink: ln -s AGENTS.md CLAUDE.md",
		})
	}

	// 3. scaffold:domain
	if report.DomainState == "ok" {
		checks = append(checks, Check{
			Name:   "scaffold:domain",
			Status: OK,
			Detail: ".agents/AGENTS.md domain context is present",
		})
	} else {
		checks = append(checks, Check{
			Name:   "scaffold:domain",
			Status: Warn,
			Detail: ".agents/AGENTS.md is missing",
			Remedy: "run 'agents init' to populate starter template",
		})
	}

	// 4. scaffold:skill-recording
	switch report.Skills["recording-what-you-learn"] {
	case string(drift.ComponentOK):
		checks = append(checks, Check{
			Name:   "scaffold:skill-recording",
			Status: OK,
			Detail: ".agents/skills/recording-what-you-learn/ is present",
		})
	case string(drift.ComponentCleanLegacy):
		checks = append(checks, Check{
			Name:   "scaffold:skill-recording",
			Status: OK,
			Detail: ".agents/skills/recording-what-you-learn/ matches legacy template",
		})
	case string(drift.ComponentCustomized):
		checks = append(checks, Check{
			Name:   "scaffold:skill-recording",
			Status: OK,
			Detail: ".agents/skills/recording-what-you-learn/ carries repository customizations",
		})
	default:
		checks = append(checks, Check{
			Name:   "scaffold:skill-recording",
			Status: Warn,
			Detail: ".agents/skills/recording-what-you-learn/ is missing",
			Remedy: "run 'agents init' to populate bundled skill",
		})
	}

	// 5. scaffold:skill-migrating
	switch report.Skills["migrating-fleet-context"] {
	case string(drift.ComponentOK):
		checks = append(checks, Check{
			Name:   "scaffold:skill-migrating",
			Status: OK,
			Detail: ".agents/skills/migrating-fleet-context/ is present",
		})
	case string(drift.ComponentCleanLegacy):
		checks = append(checks, Check{
			Name:   "scaffold:skill-migrating",
			Status: OK,
			Detail: ".agents/skills/migrating-fleet-context/ matches legacy template",
		})
	case string(drift.ComponentCustomized):
		checks = append(checks, Check{
			Name:   "scaffold:skill-migrating",
			Status: OK,
			Detail: ".agents/skills/migrating-fleet-context/ carries repository customizations",
		})
	default:
		checks = append(checks, Check{
			Name:   "scaffold:skill-migrating",
			Status: Warn,
			Detail: ".agents/skills/migrating-fleet-context/ is missing",
			Remedy: "run 'agents update' or 'agents init' to refresh infrastructure skills",
		})
	}

	return checks
}

func checkGitleaks(lookPath func(string) (string, error)) Check {
	if lookPath == nil {
		return Check{Name: "gitleaks", Status: Warn, Detail: "gitleaks lookup is unavailable", Remedy: "brew install gitleaks"}
	}
	if _, err := lookPath("gitleaks"); err != nil {
		return Check{Name: "gitleaks", Status: Warn, Detail: "gitleaks is not available on PATH", Remedy: "brew install gitleaks"}
	}
	return Check{Name: "gitleaks", Status: OK, Detail: "gitleaks is available on PATH"}
}

// LaneHealth reports catch-all lanes. It is advisory and never changes Git.
func LaneHealth(recs []record.Record, th Thresholds, now time.Time) []Check {
	type laneStat struct {
		modules, sessions map[string]struct{}
		first, last       time.Time
	}
	byLane := map[string]*laneStat{}
	cutoff := now.Add(-th.Window)
	for _, rec := range recs {
		if rec.Lane == "" || rec.When.Before(cutoff) {
			continue
		}
		stat := byLane[rec.Lane]
		if stat == nil {
			stat = &laneStat{modules: map[string]struct{}{}, sessions: map[string]struct{}{}, first: rec.When, last: rec.When}
			byLane[rec.Lane] = stat
		}
		module := rec.Cwd
		if before, _, ok := strings.Cut(module, "/"); ok {
			module = before
		}
		stat.modules[module] = struct{}{}
		stat.sessions[rec.SessionID] = struct{}{}
		if rec.When.Before(stat.first) {
			stat.first = rec.When
		}
		if rec.When.After(stat.last) {
			stat.last = rec.When
		}
	}
	lanes := make([]string, 0, len(byLane))
	for lane := range byLane {
		lanes = append(lanes, lane)
	}
	sort.Strings(lanes)
	var checks []Check
	for _, lane := range lanes {
		stat := byLane[lane]
		var reasons []string
		if len(stat.modules) > th.Modules {
			modules := make([]string, 0, len(stat.modules))
			for module := range stat.modules {
				modules = append(modules, module)
			}
			sort.Strings(modules)
			reasons = append(reasons, fmt.Sprintf("%d modules (%s)", len(modules), strings.Join(modules, ", ")))
		}
		if days := int(stat.last.Sub(stat.first) / (24 * time.Hour)); days > th.Days {
			reasons = append(reasons, fmt.Sprintf("%d days", days))
		}
		if len(stat.sessions) > th.Sessions {
			reasons = append(reasons, fmt.Sprintf("%d sessions", len(stat.sessions)))
		}
		if len(reasons) > 0 {
			checks = append(checks, Check{Name: "lane:" + lane, Status: Warn, Detail: strings.Join(reasons, "; "), Remedy: "consider separate branches; doctor never changes lanes"})
		}
	}
	return checks
}

// rootChecks reports whether the checkout this binary was stamped to still
// exists.
//
// Nothing else did, and that was measured before this was written rather than
// argued from the code. A binary stamped to a worktree, with core.hooksPath
// agreeing with the stamp, produces output BYTE-IDENTICAL before and after that
// worktree is deleted -- 2691 bytes both times, and the deleted path appears
// nowhere in it. git-hooks:global compares core.hooksPath against HooksDir as
// strings, so two paths that agree with each other pass whether or not either
// exists.
//
// It fails rather than warns because of what the silence costs: githook treats
// a missing extras directory as "no personal hooks" and carries on at exit 0,
// so the whole personal hook chain stops running and every check still says the
// machine is fine.
func rootChecks(deps Dependencies) []Check {
	// An unstamped binary is a different situation and not this check's to
	// report: a test binary, or `go run`, has no root to have lost.
	if deps.Root == "" {
		return nil
	}
	remedy := "rebuild from the main checkout: cd <checkout> && make agents"
	info, err := os.Stat(deps.Root)
	switch {
	case err != nil:
		return []Check{{
			Name: "root:exists", Status: Fail,
			Detail: fmt.Sprintf("the stamped checkout %s does not exist", deps.Root),
			Remedy: remedy,
		}}
	case !info.IsDir():
		return []Check{{
			Name: "root:exists", Status: Fail,
			Detail: fmt.Sprintf("the stamped checkout %s is not a directory", deps.Root),
			Remedy: remedy,
		}}
	}
	return []Check{{Name: "root:exists", Status: OK, Detail: "the stamped checkout exists"}}
}
