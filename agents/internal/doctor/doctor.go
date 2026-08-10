// Package doctor observes the installation and repo context without repairing it.
package doctor

import (
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/nilbot/dotfiles/agents/internal/memory"
	"github.com/nilbot/dotfiles/agents/internal/record"
	"github.com/nilbot/dotfiles/agents/internal/repo"
	"github.com/nilbot/dotfiles/agents/internal/safeio"
	"github.com/nilbot/dotfiles/agents/internal/scaffold"
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
}

type GitResult struct {
	Output string
	Code   int
}

type Dependencies struct {
	LookPath              func(string) (string, error)
	Git                   func(dir string, args ...string) GitResult
	LegacyHooksPath       func(string) (string, error)
	CodexConfig           string
	HooksDir              string
	AttributesLink        string
	AttributesSource      string
	AttributesConfigValue string
	GlobalGitConfig       string
	SharedGitConfig       string
}

func DefaultThresholds() Thresholds {
	return Thresholds{
		Window:             30 * 24 * time.Hour,
		Modules:            3,
		Days:               14,
		Sessions:           20,
		RecordingFreshness: 7 * 24 * time.Hour,
	}
}

func DefaultDependencies() Dependencies {
	home, _ := os.UserHomeDir()
	dotfiles := filepath.Join(home, "dotfiles")
	return Dependencies{
		LookPath:              exec.LookPath,
		Git:                   runGit,
		LegacyHooksPath:       repo.LegacyHooksPath,
		CodexConfig:           filepath.Join(home, ".codex", "config.toml"),
		HooksDir:              filepath.Join(dotfiles, "git", "hooks.d"),
		AttributesLink:        filepath.Join(home, ".gitattributes"),
		AttributesSource:      filepath.Join(dotfiles, "git", "gitattributes"),
		AttributesConfigValue: "~/.gitattributes",
		GlobalGitConfig:       filepath.Join(home, ".gitconfig"),
		SharedGitConfig:       filepath.Join(dotfiles, "git", "gitconfig.symlink"),
	}
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

func Run(repoRoot, agentsDir, thisMachine, binary string, th Thresholds, now time.Time) ([]Check, error) {
	return RunWithDeps(repoRoot, agentsDir, thisMachine, binary, th, now, DefaultDependencies())
}

func RunWithDeps(repoRoot, agentsDir, thisMachine, binary string, th Thresholds, now time.Time, deps Dependencies) ([]Check, error) {
	traceResult, err := trace.Query(agentsDir, trace.Filter{}, now)
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
	for _, adapter := range harness.All() {
		checks = append(checks, checkRecording(adapter, traceResult.Records, wiringTimes[adapter.Name()], th.RecordingFreshness, now))
	}
	checks = append(checks, checkGitleaks(deps.LookPath))
	checks = append(checks, checkGitHooks(repoRoot, binary, deps)...)
	checks = append(checks, checkGitAttributes(repoRoot, deps))
	checks = append(checks, checkMachine(thisMachine))
	if traceResult.Skipped > 0 {
		checks = append(checks, Check{Name: "trace-index", Status: Warn, Detail: fmt.Sprintf("%d unreadable line(s) skipped", traceResult.Skipped), Remedy: "repair malformed JSONL or merge conflict markers; preserve valid lines"})
	} else {
		checks = append(checks, Check{Name: "trace-index", Status: OK, Detail: "all trace index lines are readable"})
	}
	checks = append(checks, checkPointers(traceResult.Records, thisMachine)...)
	checks = append(checks, checkMemorySources(agentsDir, thisMachine)...)
	checks = append(checks, checkScaffoldInstruction(repoRoot))
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
	return Check{Name: name, Status: OK, Detail: "all required generated hooks are exact"}, info.ModTime(), keys
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

func checkGitHooks(repoRoot, binary string, deps Dependencies) []Check {
	if deps.Git == nil {
		return []Check{{Name: "git-hooks:global", Status: Fail, Detail: "Git diagnostic runner is unavailable"}}
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

	local := deps.Git(repoRoot, "config", "--local", "--get-all", "core.hooksPath")
	localValues := configValues(local.Output)
	switch {
	case local.Code == 1:
		checks = append(checks, Check{Name: "git-hooks:local", Status: OK, Detail: "no repository-local core.hooksPath override"})
	case local.Code != 0:
		checks = append(checks, Check{Name: "git-hooks:local", Status: Fail, Detail: "repository-local core.hooksPath could not be read", Remedy: "inspect repository or linked-worktree Git configuration"})
	case len(localValues) == 0:
		checks = append(checks, Check{Name: "git-hooks:local", Status: Fail, Detail: "repository-local core.hooksPath returned an empty value"})
	default:
		checks = append(checks, Check{Name: "git-hooks:local", Status: Warn, Detail: fmt.Sprintf("repository-local core.hooksPath override is set (%d value(s))", len(localValues)), Remedy: "the global agents hooks are shadowed here; chain them from the local hook directory if desired"})
	}

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
	".agents/reports/traces/*.jsonl merge=union",
	".agents/** linguist-generated=true",
}

const globalTraceAttribute = ".agents/reports/traces/*.jsonl merge=union"

func checkGitAttributes(repoRoot string, deps Dependencies) Check {
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
	source, err := safeio.ReadRegular(deps.AttributesSource)
	if err != nil || !hasExactLine(source, globalTraceAttribute) {
		return Check{Name: "git-attributes", Status: Fail, Detail: "global attributes source lacks the exact trace merge rule", Remedy: "restore the tracked attributes source"}
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

func checkPointers(recs []record.Record, thisMachine string) []Check {
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
			if _, err := os.Stat(rec.Transcript); err != nil {
				localUnreachable++
			}
		}
	}
	if localUnreachable > 0 {
		checks = append(checks, Check{Name: "pointers:local-unreachable", Status: Warn, Detail: fmt.Sprintf("%d verified local pointer(s) are unreachable", localUnreachable), Remedy: "cache reachable transcripts before harness cleanup"})
	}
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

func checkMemorySources(agentsDir, thisMachine string) []Check {
	entries, err := memory.List(filepath.Join(agentsDir, "memory"))
	if err != nil {
		return []Check{{Name: "memory", Status: Fail, Detail: "memory entries could not be read safely", Remedy: "fix memory frontmatter, then run `agents index`"}}
	}
	remote := map[string]int{}
	unknown := 0
	unclassified := 0
	for _, entry := range entries {
		for _, source := range entry.Sources {
			if source.Machine == "" {
				unknown++
			} else if thisMachine == "" {
				unclassified++
			} else if source.Machine != thisMachine {
				remote[source.Machine]++
			}
		}
	}
	if len(remote) == 0 && unknown == 0 && unclassified == 0 {
		return []Check{{Name: "memory", Status: OK, Detail: "memory source dependencies are locally classified"}}
	}
	machines := make([]string, 0, len(remote))
	for machine := range remote {
		machines = append(machines, machine)
	}
	sort.Strings(machines)
	parts := make([]string, 0, len(machines)+1)
	for _, machine := range machines {
		parts = append(parts, fmt.Sprintf("%s=%d", machine, remote[machine]))
	}
	if unknown > 0 {
		parts = append(parts, fmt.Sprintf("unknown=%d", unknown))
	}
	if unclassified > 0 {
		parts = append(parts, fmt.Sprintf("unclassified=%d", unclassified))
	}
	return []Check{{Name: "memory", Status: Warn, Detail: "remote or unclassified source dependencies: " + strings.Join(parts, ", "), Remedy: "the entry claim must stand alone; visit the source machine only for corroborating detail"}}
}

func checkMachine(thisMachine string) Check {
	if thisMachine == "" {
		return Check{Name: "machine-id", Status: Warn, Detail: "machine identity is missing or unreadable", Remedy: "restore the machine-local identity; doctor does not create it"}
	}
	return Check{Name: "machine-id", Status: OK, Detail: "machine identity is readable"}
}

func checkScaffoldInstruction(repoRoot string) Check {
	b, err := safeio.ReadRegular(filepath.Join(repoRoot, "CLAUDE.md"))
	if err != nil || !strings.Contains(string(b), scaffold.DoctorInstruction) {
		return Check{Name: "scaffold:doctor-instruction", Status: Warn, Detail: "CLAUDE.md lacks the doctor instruction used by new scaffolds", Remedy: "review and add the current doctor instruction manually; existing user files are never migrated"}
	}
	return Check{Name: "scaffold:doctor-instruction", Status: OK, Detail: "CLAUDE.md carries the doctor instruction"}
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
