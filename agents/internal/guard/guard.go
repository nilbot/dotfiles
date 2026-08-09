// Package guard checks the exact content selected for a commit.
//
// It is deliberately the one agents subsystem that fails closed: commit is
// where tracked content is decided, so a scan that did not run is not a pass.
package guard

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/nilbot/dotfiles/agents/internal/handoff"
	"github.com/nilbot/dotfiles/agents/internal/memory"
	"github.com/nilbot/dotfiles/agents/internal/safetext"
)

const agentsPrefix = ".agents/"

// Finding is one actionable result from the staged-content guard. Detail is
// always authored by this package; scanner matches and stderr are never copied
// into it.
type Finding struct {
	Path     string
	Line     int
	Rule     string
	Blocking bool
	Detail   string
}

// gitleaksConfigPath is indirect only so tests can point the subprocess at the
// checked-in config without depending on the test runner's home directory.
// Production always resolves the config from dotfiles, never from the repo
// being scanned.
var gitleaksConfigPath = defaultGitleaksConfigPath

func defaultGitleaksConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory for gitleaks config: %w", err)
	}
	return filepath.Join(home, "dotfiles", "git", "gitleaks.toml"), nil
}

type indexEntry struct {
	Mode  string
	OID   string
	Stage int
	Path  string
}

// Staged checks staged .agents/ blobs, generated indexes, and mixed commits.
// Every repository read goes through Git's index; the working tree is never an
// input and is never written.
func Staged(repoRoot string) ([]Finding, error) {
	changed, err := stagedPaths(repoRoot)
	if err != nil {
		return nil, err
	}
	entries, err := indexEntries(repoRoot)
	if err != nil {
		return nil, err
	}
	byPath := make(map[string][]indexEntry, len(entries))
	for _, e := range entries {
		byPath[e.Path] = append(byPath[e.Path], e)
	}

	var agentPaths, otherPaths []string
	var findings []Finding
	for _, path := range changed {
		if strings.HasPrefix(path, agentsPrefix) {
			agentPaths = append(agentPaths, path)
			if r, ok := safetext.ControlRune(path); ok {
				findings = append(findings, Finding{
					Rule:     "unsafe-path",
					Blocking: true,
					Detail:   fmt.Sprintf("staged path %q contains a control character (%q); rename it before committing", path, r),
				})
			}
		} else {
			otherPaths = append(otherPaths, path)
		}
	}

	config := ""
	for _, path := range agentPaths {
		if _, unsafe := safetext.ControlRune(path); unsafe {
			continue
		}
		pathEntries := byPath[path]
		if len(pathEntries) == 0 {
			// A deletion has no staged blob to inspect.
			continue
		}
		if len(pathEntries) != 1 || pathEntries[0].Stage != 0 {
			return nil, fmt.Errorf("cannot inspect conflicted staged agent path %q; resolve the index and retry", path)
		}
		entry := pathEntries[0]
		if entry.Mode != "100644" && entry.Mode != "100755" {
			return nil, fmt.Errorf("cannot inspect staged agent path %q with non-regular Git mode %s", path, entry.Mode)
		}
		blob, err := stagedBlob(repoRoot, entry.OID)
		if err != nil {
			return nil, fmt.Errorf("cannot read staged agent blob for %q", path)
		}
		if config == "" {
			config, err = trustedConfig()
			if err != nil {
				return nil, err
			}
		}
		scanned, err := scanBlob(path, blob, config)
		if err != nil {
			return nil, err
		}
		findings = append(findings, scanned...)
	}

	generated, err := checkGenerated(repoRoot, agentPaths, entries)
	if err != nil {
		return nil, err
	}
	findings = append(findings, generated...)

	if len(agentPaths) > 0 && len(otherPaths) > 0 {
		findings = append(findings, Finding{
			Rule:     "mixed-commit",
			Blocking: false,
			Detail: fmt.Sprintf("this commit touches %d agent path(s) and %d other path(s); "+
				"`agents save` commits .agents/ on its own", len(agentPaths), len(otherPaths)),
		})
	}
	sortFindings(findings)
	return findings, nil
}

func trustedConfig() (string, error) {
	path, err := gitleaksConfigPath()
	if err != nil {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("trusted gitleaks config is unavailable; restore %q", path)
	}
	info, statErr := f.Stat()
	_, readErr := io.Copy(io.Discard, f)
	closeErr := f.Close()
	if statErr != nil || readErr != nil || closeErr != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("trusted gitleaks config is not a readable regular file; restore %q", path)
	}
	return path, nil
}

type scannerFinding struct {
	RuleID    string `json:"RuleID"`
	StartLine int    `json:"StartLine"`
}

func scanBlob(path string, blob []byte, config string) ([]Finding, error) {
	bin, err := exec.LookPath("gitleaks")
	if err != nil {
		return nil, errors.New("gitleaks is required to scan staged .agents/ content; install it with `brew install gitleaks`")
	}
	cmd := exec.Command(bin,
		"stdin",
		"--config", config,
		"--redact",
		"--no-banner",
		"--no-color",
		"--log-level", "error",
		"--report-format", "json",
		"--report-path", "-",
	)
	cmd.Stdin = bytes.NewReader(blob)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard
	runErr := cmd.Run()
	exit := 0
	if runErr != nil {
		var ee *exec.ExitError
		if !errors.As(runErr, &ee) {
			return nil, errors.New("could not execute gitleaks for staged .agents/ content")
		}
		exit = ee.ExitCode()
	}
	if exit != 0 && exit != 1 {
		return nil, fmt.Errorf("gitleaks could not scan staged .agents/ content (exit %d)", exit)
	}

	report := bytes.TrimSpace(stdout.Bytes())
	if len(report) == 0 {
		if exit == 0 {
			return nil, nil
		}
		return nil, errors.New("gitleaks reported a finding but returned no parseable report")
	}
	var raw []scannerFinding
	if err := json.Unmarshal(report, &raw); err != nil {
		return nil, errors.New("gitleaks returned a malformed finding report")
	}
	if exit == 0 {
		if len(raw) == 0 {
			return nil, nil
		}
		return nil, errors.New("gitleaks returned findings with a clean exit status")
	}
	if len(raw) == 0 {
		return nil, errors.New("gitleaks reported a finding but returned an empty report")
	}

	findings := make([]Finding, 0, len(raw))
	for _, result := range raw {
		if result.RuleID == "" || result.StartLine < 1 {
			return nil, errors.New("gitleaks returned an incomplete finding report")
		}
		if _, unsafe := safetext.ControlRune(result.RuleID); unsafe {
			return nil, errors.New("gitleaks returned an unsafe rule identifier")
		}
		findings = append(findings, Finding{
			Path:     path,
			Line:     result.StartLine,
			Rule:     result.RuleID,
			Blocking: true,
			Detail:   "gitleaks detected a secret in staged content",
		})
	}
	return findings, nil
}

func stagedPaths(repoRoot string) ([]string, error) {
	// Include U explicitly. An unmerged path can otherwise disappear from this
	// list even though ls-files correctly exposes its stage 1/2/3 entries.
	out, err := gitOutput(repoRoot, "diff", "--cached", "--name-only", "--diff-filter=ACMRDU", "-z")
	if err != nil {
		return nil, errors.New("cannot read staged path list")
	}
	return splitNUL(out), nil
}

func indexEntries(repoRoot string) ([]indexEntry, error) {
	out, err := gitOutput(repoRoot, "ls-files", "--stage", "-z")
	if err != nil {
		return nil, errors.New("cannot read Git index")
	}
	var entries []indexEntry
	for _, record := range splitNUL(out) {
		meta, path, ok := strings.Cut(record, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 || path == "" {
			return nil, errors.New("Git returned a malformed index entry")
		}
		stage, err := strconv.Atoi(fields[2])
		if err != nil {
			return nil, errors.New("Git returned a malformed index stage")
		}
		entries = append(entries, indexEntry{Mode: fields[0], OID: fields[1], Stage: stage, Path: path})
	}
	return entries, nil
}

func stagedBlob(repoRoot, oid string) ([]byte, error) {
	return gitOutput(repoRoot, "cat-file", "blob", oid)
}

func gitOutput(repoRoot string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoRoot
	cmd.Env = sanitizeGitEnv(os.Environ())
	cmd.Stderr = io.Discard
	return cmd.Output()
}

func sanitizeGitEnv(env []string) []string {
	forbidden := map[string]bool{
		"GIT_DIR":              true,
		"GIT_WORK_TREE":        true,
		"GIT_INDEX_FILE":       true,
		"GIT_OBJECT_DIRECTORY": true,
		"GIT_COMMON_DIR":       true,
		"GIT_NAMESPACE":        true,
	}
	result := make([]string, 0, len(env))
	for _, pair := range env {
		key, _, ok := strings.Cut(pair, "=")
		if ok && forbidden[key] {
			continue
		}
		result = append(result, pair)
	}
	return result
}

func splitNUL(b []byte) []string {
	parts := bytes.Split(b, []byte{0})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) != 0 {
			out = append(out, string(part))
		}
	}
	return out
}

func sortFindings(findings []Finding) {
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Path != findings[j].Path {
			return findings[i].Path < findings[j].Path
		}
		if findings[i].Line != findings[j].Line {
			return findings[i].Line < findings[j].Line
		}
		return findings[i].Rule < findings[j].Rule
	})
}

type generatedTarget struct {
	trigger string
	index   string
	render  func(string) ([]byte, error)
}

func checkGenerated(repoRoot string, agentPaths []string, entries []indexEntry) ([]Finding, error) {
	targets := []generatedTarget{
		{
			trigger: ".agents/memory/",
			index:   ".agents/memory/INDEX.md",
			render: func(scratch string) ([]byte, error) {
				list, err := memory.List(filepath.Join(scratch, ".agents", "memory"))
				if err != nil {
					return nil, err
				}
				return memory.RenderIndex(list), nil
			},
		},
		{
			trigger: ".agents/reports/handoff/",
			index:   ".agents/reports/handoff/INDEX.md",
			render: func(scratch string) ([]byte, error) {
				list, err := handoff.List(filepath.Join(scratch, ".agents"))
				if err != nil {
					return nil, err
				}
				return handoff.RenderIndex(list), nil
			},
		},
	}

	var findings []Finding
	for _, target := range targets {
		if !hasPathPrefix(agentPaths, target.trigger) {
			continue
		}
		want, err := renderStagedIndex(repoRoot, target, entries)
		if err != nil {
			return nil, fmt.Errorf("cannot render staged sources for %s: %w", target.index, err)
		}
		entry, ok, err := stageZeroEntry(entries, target.index)
		if err != nil {
			return nil, err
		}
		if !ok {
			findings = append(findings, Finding{
				Path:     target.index,
				Rule:     "generated-file",
				Blocking: true,
				Detail:   "a source changed but the generated index is not staged; run `agents index` and stage it, or use `agents save`",
			})
			continue
		}
		got, err := stagedBlob(repoRoot, entry.OID)
		if err != nil {
			return nil, fmt.Errorf("cannot read staged generated index %q", target.index)
		}
		if !bytes.Equal(want, got) {
			findings = append(findings, Finding{
				Path:     target.index,
				Rule:     "generated-file",
				Blocking: true,
				Detail:   "staged content differs from what `agents index` produces; do not hand-edit generated files",
			})
		}
	}
	return findings, nil
}

func hasPathPrefix(paths []string, prefix string) bool {
	for _, path := range paths {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func stageZeroEntry(entries []indexEntry, path string) (indexEntry, bool, error) {
	var found []indexEntry
	for _, entry := range entries {
		if entry.Path == path {
			found = append(found, entry)
		}
	}
	if len(found) == 0 {
		return indexEntry{}, false, nil
	}
	if len(found) != 1 || found[0].Stage != 0 {
		return indexEntry{}, false, fmt.Errorf("generated index %q is conflicted; resolve it and retry", path)
	}
	if found[0].Mode != "100644" && found[0].Mode != "100755" {
		return indexEntry{}, false, fmt.Errorf("generated index %q has non-regular Git mode %s", path, found[0].Mode)
	}
	return found[0], true, nil
}

// renderStagedIndex materialises source blobs into an isolated temporary tree,
// then uses the same pure List/RenderIndex APIs as the writers. The user's
// worktree and index remain read-only even when parsing or rendering fails.
func renderStagedIndex(repoRoot string, target generatedTarget, entries []indexEntry) ([]byte, error) {
	scratch, err := os.MkdirTemp("", "agents-guard-index-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(scratch)

	for _, entry := range entries {
		if !strings.HasPrefix(entry.Path, target.trigger) || entry.Path == target.index {
			continue
		}
		if entry.Stage != 0 {
			return nil, fmt.Errorf("source path %q is conflicted", entry.Path)
		}
		if entry.Mode != "100644" && entry.Mode != "100755" {
			return nil, fmt.Errorf("source path %q has non-regular Git mode %s", entry.Path, entry.Mode)
		}
		if _, unsafe := safetext.ControlRune(entry.Path); unsafe {
			return nil, fmt.Errorf("source path %q contains a control character", entry.Path)
		}
		// memory.WriteIndex refuses this case before rendering because writing
		// INDEX.md would destroy the entry on a case-insensitive filesystem.
		if target.index == ".agents/memory/INDEX.md" &&
			path.Dir(entry.Path) == path.Dir(target.index) &&
			strings.EqualFold(filepath.Base(entry.Path), "INDEX.md") {
			return nil, fmt.Errorf("memory source %q collides with the generated index name", entry.Path)
		}
		blob, err := stagedBlob(repoRoot, entry.OID)
		if err != nil {
			return nil, fmt.Errorf("cannot read source blob for %q", entry.Path)
		}
		destination := filepath.Join(scratch, filepath.FromSlash(entry.Path))
		rel, err := filepath.Rel(scratch, destination)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("source path %q escapes the scratch tree", entry.Path)
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(destination, blob, 0o600); err != nil {
			return nil, err
		}
	}
	return target.render(scratch)
}
