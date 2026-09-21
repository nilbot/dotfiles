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

	"github.com/nilbot/dotfiles/agents/internal/githook"
	"github.com/nilbot/dotfiles/agents/internal/harness"
	"github.com/nilbot/dotfiles/agents/internal/repo"
	"github.com/nilbot/dotfiles/agents/internal/safeio"
	"github.com/nilbot/dotfiles/agents/internal/scaffold"
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

type GitResult struct {
	Output string
	Code   int
}

type Dependencies struct {
	LookPath              func(string) (string, error)
	Git                   func(dir string, args ...string) GitResult
	LegacyHooksPath       func(string) (string, error)
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
// RunWithDeps runs every check. It observes only: nothing here writes, and a
// check that would have to mutate to answer is not a check.
//
// It takes a repository root, the running binary's path, and its dependencies,
// and nothing else. A store directory and a machine identity went with the
// session record; the threshold structure went because the only threshold left
// capped a cache that no longer exists, so its value was parsed from a flag and
// then read by nothing. An argument that cannot change any behaviour is worse
// than no argument -- it invites a caller to pass one.
func RunWithDeps(repoRoot, binary string, deps Dependencies) ([]Check, error) {
	var checks []Check
	checks = append(checks, checkBinary(binary, deps.LookPath))
	for _, adapter := range harness.All() {
		checks = append(checks, checkWiring(adapter, repoRoot))
	}
	checks = append(checks, checkAntigravityTrust(deps.AntigravityConfig, repoRoot))
	checks = append(checks, checkGitleaks(deps.LookPath))
	checks = append(checks, rootChecks(deps)...)
	checks = append(checks, checkGitHooks(repoRoot, binary, deps)...)
	checks = append(checks, checkGitAttributes(repoRoot, deps))
	checks = append(checks, checkScaffold(repoRoot)...)
	checks = append(checks, checkSkills(repoRoot)...)
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

// checkWiring reports whether this tool has left anything behind in a harness
// config, and what to do about it.
//
// The question inverted when the tool stopped recording. It used to ask "are
// our entries present for every event we write?", and its remedy was to run
// `wire` again. There is nothing to write any more, so the failure mode is the
// opposite: an entry an earlier version wrote that is still there, calling a
// subcommand this binary no longer answers.
//
// Two findings, kept separate because their remedies differ. An entry this tool
// OWNS is one `agents wire` removes, so that is the remedy. An entry that merely
// LOOKS like ours -- the generated shape under a binary this tool does not own,
// which this repository has produced once for real -- is not ours to delete, so
// telling the operator to run `wire` would print a remedy that provably does
// nothing. Report and repair share one predicate here: the classification below
// is the same one `stripOurs` uses to decide what it may delete.
//
// An absent config is success, not a gap: there is nothing left to write. A
// config that cannot be read or parsed is still a fault, because it is a file
// this tool either wrote or will need to write.
func checkWiring(a harness.Adapter, repoRoot string) Check {
	name := "wiring:" + a.Name()
	path := a.WireConfigPath(repoRoot)
	b, _, err := safeio.ReadRegularInfo(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Check{Name: name, Status: OK, Detail: "no generated config, and none needed"}
		}
		return Check{Name: name, Status: Warn, Detail: "generated config is not a readable regular file", Remedy: "fix or remove it"}
	}
	var cfg map[string]any
	if err := json.Unmarshal(b, &cfg); err != nil {
		return Check{Name: name, Status: Fail, Detail: "generated config is malformed JSON", Remedy: "fix or remove it"}
	}
	commands := commandsIn(a.Name(), cfg)
	if owned := ownedOnly(commands); len(owned) > 0 {
		return Check{
			Name:   name,
			Status: Fail,
			Detail: fmt.Sprintf("%d retired entry(ies) this tool no longer answers: %s", len(owned), strings.Join(owned, ", ")),
			Remedy: "run `agents wire` to remove them; the command they call no longer exists",
		}
	}
	if lookalikes := lookalikeOnly(commands); len(lookalikes) > 0 {
		return Check{
			Name:   name,
			Status: Warn,
			Detail: fmt.Sprintf("%d entry(ies) in the generated shape but under a binary this tool does not own: %s", len(lookalikes), strings.Join(lookalikes, ", ")),
			Remedy: "remove them by hand: `wire` deletes only entries whose binary is this tool, and widening it to delete these would be deleting from a config this tool does not own",
		}
	}
	return Check{Name: name, Status: OK, Detail: "no stale entries"}
}

// ownedOnly keeps the commands `wire` is allowed to delete.
func ownedOnly(commands []string) []string {
	var out []string
	for _, c := range commands {
		if harness.IsOwnedHookCommand(c) {
			out = append(out, c)
		}
	}
	return out
}

// lookalikeOnly keeps the commands in the generated shape that this tool does
// not own, so they can be reported without being deleted.
func lookalikeOnly(commands []string) []string {
	var out []string
	for _, c := range commands {
		if !harness.IsOwnedHookCommand(c) && harness.ResemblesHookCommand(c) {
			out = append(out, c)
		}
	}
	return out
}

// commandsIn returns every command in a harness config that is in this tool's
// generated shape, so the caller can classify each one by whether it may be
// deleted.
//
// It walks EVERY key under the harness's group rather than the events any
// adapter currently declares, because the entries being looked for are the ones
// an older version wrote: an event this binary no longer knows about is exactly
// where a stale entry hides.
//
// It understands both shapes this tool has used. Claude Code and Codex nest a
// command inside a group -- an event maps to `[{"hooks": [{"command": ...}]}]`
// -- while Antigravity puts the command object directly in the list -- an event
// maps to `[{"command": ...}]`. A walk that understood only one shape would
// report the other harness's retired entry as clean, which is the failure this
// check exists to catch, arriving as silence.
func commandsIn(harnessName string, cfg map[string]any) []string {
	var found []string
	var groups map[string]any
	if harnessName == "antigravity" {
		groups, _ = cfg["agents"].(map[string]any)
	} else {
		groups, _ = cfg["hooks"].(map[string]any)
	}
	for _, raw := range groups {
		entries, _ := raw.([]any)
		for _, rawEntry := range entries {
			entry, _ := rawEntry.(map[string]any)
			if cmd, _ := entry["command"].(string); cmd != "" {
				if harness.ResemblesHookCommand(cmd) || harness.IsOwnedHookCommand(cmd) {
					found = append(found, cmd)
				}
				continue
			}
			inner, _ := entry["hooks"].([]any)
			for _, rawHook := range inner {
				hook, _ := rawHook.(map[string]any)
				cmd, _ := hook["command"].(string)
				if harness.ResemblesHookCommand(cmd) || harness.IsOwnedHookCommand(cmd) {
					found = append(found, cmd)
				}
			}
		}
	}
	return found
}

var installedHookNames = []string{"pre-commit", "commit-msg", "post-merge", "post-checkout"}

// checkScaffold reports the state of the three files `init` writes that make
// the two-tier context work.
//
// These checks came back. They were previously computed inside the drift
// inspection, so deleting the drift machinery took them with it -- machinery
// whose removal had nothing to do with whether a repository still has its
// router. The subject of each check still exists and `init` still writes and
// repairs all three, which is what makes the check actionable.
//
// What is NOT restored is the content judgement the old router check made: it
// classified the router as canonical, legacy or diverged, which required the
// catalog of historical router texts. That catalog is gone with the layouts it
// belonged to, and inventing a simpler version would be asserting something
// this tool can no longer know. Presence and shape are what is checked; whether
// the prose is still right is a human judgement.
func checkScaffold(repoRoot string) []Check {
	return []Check{
		checkRouter(repoRoot),
		checkSymlink(repoRoot),
		checkDomain(repoRoot),
	}
}

// checkRouter reports whether the root instruction file is there. Every
// harness reads it, and it is the one file an operator may reasonably delete
// while reorganising a repository.
func checkRouter(repoRoot string) Check {
	const name = "scaffold:router"
	path := filepath.Join(repoRoot, "AGENTS.md")
	info, err := os.Lstat(path)
	switch {
	case os.IsNotExist(err):
		return Check{Name: name, Status: Warn, Detail: "root AGENTS.md is missing",
			Remedy: "run `agents init` to restore it from the embedded template"}
	case err != nil:
		return Check{Name: name, Status: Warn, Detail: "root AGENTS.md cannot be inspected: " + err.Error(),
			Remedy: "check permissions on " + path}
	case info.IsDir():
		return Check{Name: name, Status: Fail, Detail: "root AGENTS.md is a directory",
			Remedy: "replace it with the instruction file; a directory here is read as nothing"}
	}
	return Check{Name: name, Status: OK, Detail: "root AGENTS.md is present"}
}

// checkSymlink is the one that fails silently and does real damage.
//
// CLAUDE.md must be a relative symlink to AGENTS.md. A tar or zip extraction, a
// Windows checkout with core.symlinks=false, or a sync tool can materialise it
// as a regular file whose entire content is the text "AGENTS.md" -- and Claude
// Code then reads that one line as the whole project context, with nothing
// anywhere saying so. The relative form matters too: an absolute link records
// one machine's checkout path and breaks on any other.
func checkSymlink(repoRoot string) Check {
	const name = "scaffold:symlink"
	path := filepath.Join(repoRoot, "CLAUDE.md")
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return Check{Name: name, Status: Warn, Detail: "CLAUDE.md is missing",
			Remedy: "run `agents init`, or create it: ln -s AGENTS.md CLAUDE.md"}
	}
	if err != nil {
		return Check{Name: name, Status: Warn, Detail: "CLAUDE.md cannot be inspected: " + err.Error()}
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return Check{Name: name, Status: Fail, Detail: "CLAUDE.md is not a symlink; a harness reading it sees whatever this file contains",
			Remedy: "remove it and create the relative link: ln -s AGENTS.md CLAUDE.md"}
	}
	target, err := os.Readlink(path)
	if err != nil {
		return Check{Name: name, Status: Warn, Detail: "CLAUDE.md cannot be read as a link: " + err.Error()}
	}
	if target != "AGENTS.md" {
		return Check{Name: name, Status: Fail, Detail: fmt.Sprintf("CLAUDE.md points at %q; an absolute or renamed target breaks on another machine", target),
			Remedy: "recreate it relative to this directory: ln -s AGENTS.md CLAUDE.md"}
	}
	if _, err := os.Stat(path); err != nil {
		return Check{Name: name, Status: Fail, Detail: "CLAUDE.md is a dangling link: AGENTS.md does not resolve",
			Remedy: "run `agents init` to restore AGENTS.md"}
	}
	return Check{Name: name, Status: OK, Detail: "CLAUDE.md is a relative symlink to AGENTS.md"}
}

// checkDomain reports whether the repository's own rules file is there. It is a
// Warn rather than a Fail: a repository may keep its guidance elsewhere, and
// this tool writes a starter file rather than a required one.
func checkDomain(repoRoot string) Check {
	const name = "scaffold:domain"
	path := filepath.Join(repoRoot, ".agents", "AGENTS.md")
	info, err := os.Stat(path)
	switch {
	case os.IsNotExist(err):
		return Check{Name: name, Status: Warn, Detail: ".agents/AGENTS.md is missing",
			Remedy: "run `agents init` to write the starter template, then replace its prose with this repository's own"}
	case err != nil:
		return Check{Name: name, Status: Warn, Detail: ".agents/AGENTS.md cannot be inspected: " + err.Error()}
	case info.IsDir():
		return Check{Name: name, Status: Warn, Detail: ".agents/AGENTS.md is a directory, not a file"}
	}
	return Check{Name: name, Status: OK, Detail: ".agents/AGENTS.md domain context is present"}
}

// checkSkills reports the state of the one skill this tool installs.
//
// Three answers, because they call for three different responses: missing is a
// setup gap with a remedy, present is silent success, and customized is
// reported as OK on purpose. The skill is written once and then belongs to the
// repository -- a repository that has edited it is using the tool as intended,
// and a report that called that a fault would teach people to ignore the
// report. Customized is distinct from missing rather than folded into it for
// exactly that reason: the two are one word apart and opposite in meaning.
func checkSkills(repoRoot string) []Check {
	const name = "recording-what-you-learn"
	const check = "scaffold:skill-recording"
	current, found, err := scaffold.SkillCurrent(repoRoot, name)
	if err != nil {
		return []Check{{Name: check, Status: Warn, Detail: err.Error()}}
	}
	if found == "" {
		return []Check{{
			Name:   check,
			Status: Warn,
			Detail: ".agents/skills/" + name + "/ is missing",
			Remedy: "run 'agents init' to populate the bundled skill",
		}}
	}
	if !current {
		return []Check{{
			Name:   check,
			Status: OK,
			Detail: ".agents/skills/" + name + "/ carries repository customizations",
		}}
	}
	return []Check{{
		Name:   check,
		Status: OK,
		Detail: ".agents/skills/" + name + "/ is present",
	}}
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

	// Untrusted, and reported as OK because the Desktop App -- the harness this
	// repository's hooks target -- executes on open with no trust gate. The
	// consequence has to be stated in the DETAIL rather than carried by the
	// remedy, and that is not a stylistic choice: the caller prints a remedy only
	// for a check that is not ok, so a remedy attached to an ok check is never
	// rendered. It was written that way and measured: the instruction this check
	// exists to give was unreachable in every report it produced.
	return Check{
		Name:   name,
		Status: OK,
		Detail: "Desktop App executes on open (no trust gate); " + repoRoot + " is not in the CLI's trustedWorkspaces, so the CLI loads nothing from it -- " + remedy,
	}
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
		return Check{Name: "git-attributes", Status: Fail, Detail: "global attributes link is missing or not a symlink", Remedy: hookInstallerRemedy(deps, false)}
	}
	linkTarget, err := os.Stat(deps.AttributesLink)
	if err != nil {
		return Check{Name: "git-attributes", Status: Fail, Detail: "global attributes link is broken", Remedy: hookInstallerRemedy(deps, false)}
	}
	sourceInfo, err := os.Stat(deps.AttributesSource)
	if err != nil || !sourceInfo.Mode().IsRegular() || !os.SameFile(linkTarget, sourceInfo) {
		return Check{Name: "git-attributes", Status: Fail, Detail: "global attributes link does not resolve to the tracked source", Remedy: hookInstallerRemedy(deps, false)}
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
		checks = append(checks, Check{Name: "git-hooks:global", Status: Fail, Detail: "global core.hooksPath is unset", Remedy: hookInstallerRemedy(deps, false)})
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

	checks = append(checks, checkInstalledLinks(deps, binary))
	checks = append(checks, checkUnmanagedLinks(deps))
	checks = append(checks, checkLegacyHooks(repoRoot, deps))
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

func checkInstalledLinks(deps Dependencies, binary string) Check {
	binaryInfo, err := os.Stat(binary)
	if err != nil {
		return Check{Name: "git-hooks:links", Status: Fail, Detail: "current binary cannot be inspected", Remedy: "rebuild and reinstall agents"}
	}
	for _, name := range installedHookNames {
		path := filepath.Join(deps.HooksDir, name)
		info, err := os.Lstat(path)
		if err != nil {
			return Check{Name: "git-hooks:links", Status: Fail, Detail: name + " hook link is missing or unreadable", Remedy: hookInstallerRemedy(deps, false)}
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return Check{Name: "git-hooks:links", Status: Fail, Detail: name + " is not an owned symlink", Remedy: "preserve or move the foreign hook deliberately, then " + hookInstallerRemedy(deps, false)}
		}
		resolved, err := os.Stat(path)
		if err != nil || !os.SameFile(binaryInfo, resolved) {
			// The link exists and is ours, but names an older binary -- the
			// shape a package upgrade leaves behind. Repointing it needs the
			// flag, because the default refuses any link it did not just write.
			return Check{Name: "git-hooks:links", Status: Fail, Detail: name + " does not resolve to the current binary", Remedy: hookInstallerRemedy(deps, true)}
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

// checkUnmanagedLinks reports symlinks in the hooks directory that this
// repository does not manage and that no longer resolve. Git ignores names it
// does not know, so they are inert -- and invisible: a link left behind by an
// older install dangles forever and no other check names it. This is the pair
// of links that survived the 2026-09-20 upgrade of this machine while
// git-hooks:links, which sees only the four managed names, stayed silent.
//
// Warn, never fail: a dangling link under a managed name is already a failure
// above, and anything else here is the human's to keep or delete.
func checkUnmanagedLinks(deps Dependencies) Check {
	entries, err := os.ReadDir(deps.HooksDir)
	if err != nil {
		return Check{Name: "git-hooks:unmanaged", Status: Warn, Detail: "hook directory could not be read", Remedy: "inspect " + deps.HooksDir}
	}
	var dangling []string
	for _, entry := range entries {
		name := entry.Name()
		if isManagedHookName(name) {
			continue
		}
		path := filepath.Join(deps.HooksDir, name)
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			dangling = append(dangling, name)
		}
	}
	if len(dangling) == 0 {
		return Check{Name: "git-hooks:unmanaged", Status: OK, Detail: "no unowned hook links dangle"}
	}
	sort.Strings(dangling)
	return Check{
		Name:   "git-hooks:unmanaged",
		Status: Warn,
		Detail: "unowned hook link(s) dangle and will never run: " + strings.Join(dangling, ", "),
		Remedy: "delete them, or repoint them deliberately: " + hookInstallerRemedy(deps, false),
	}
}

// configOriginValue is one git config entry with the file it came from, which
// is what makes "set in the machine's config" distinguishable from "set in this
// repository's".
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

func configValues(output string) []string {
	var values []string
	for _, line := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
		if line != "" {
			values = append(values, line)
		}
	}
	return values
}

func hasExactLine(contents []byte, want string) bool {
	for _, line := range strings.Split(strings.ReplaceAll(string(contents), "\r\n", "\n"), "\n") {
		if line == want {
			return true
		}
	}
	return false
}

// hookInstallerRemedy renders the command that repairs what the git-hooks and
// git-attributes checks report. The text it replaces -- "run the reviewed
// global hook installer" -- named no path, no arguments and no way past the
// installer's own refusal. That matters most in the one case these checks exist
// to catch: a package upgrade deletes the versioned path a pinned hook link
// points at, git runs a dangling hook as if no hook existed, and the installer
// then refuses the link it wrote itself unless it is handed --adopt-owned. So
// the remedy carries the flag and the arguments, and falls back to the old
// sentence only when the checkout paths are unknown.
func hookInstallerRemedy(deps Dependencies, adoptOwned bool) string {
	root := deps.Root
	if root == "" && deps.HooksDir != "" {
		root = filepath.Dir(filepath.Dir(deps.HooksDir))
	}
	home := ""
	if deps.GlobalGitConfig != "" {
		home = filepath.Dir(deps.GlobalGitConfig)
	}
	if root == "" || home == "" {
		return "run the reviewed global hook installer"
	}
	adopt := ""
	if adoptOwned {
		adopt = " --adopt-owned"
	}
	return fmt.Sprintf(`run: bash "%s/git/install-hooks.sh" install%s "%s" "%s" "$(command -v agents)"`,
		root, adopt, root, home)
}

// isManagedHookName reports whether this repository owns the hook name. The
// installer links exactly installedHookNames; anything else in the directory
// belongs to the human.
func isManagedHookName(name string) bool {
	for _, managed := range installedHookNames {
		if name == managed {
			return true
		}
	}
	return false
}

var repoAttributeLines = []string{
	".agents/** linguist-generated=true",
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
