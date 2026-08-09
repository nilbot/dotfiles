// Package guard checks the exact content selected for a commit.
//
// It is deliberately the one agents subsystem that fails closed: commit is
// where tracked content is decided, so a scan that did not run is not a pass.
package guard

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
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

// The scanner consumes configuration embedded at build time, never a file from
// the repository being checked or the caller's HOME. A test pins these bytes
// exactly to git/gitleaks.toml so the shipped canonical file cannot drift.
//
//go:embed gitleaks.toml
var embeddedGitleaksConfig []byte

var gitleaksConfigBytes = defaultGitleaksConfigBytes

func defaultGitleaksConfigBytes() ([]byte, error) {
	if len(embeddedGitleaksConfig) == 0 {
		return nil, errors.New("embedded gitleaks config is unavailable; rebuild agents")
	}
	return append([]byte(nil), embeddedGitleaksConfig...), nil
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
					Detail:   fmt.Sprintf("staged path %s contains a control character (%s); rename it before committing", quoteASCII(path), quoteASCII(string(r))),
				})
			}
		} else {
			otherPaths = append(otherPaths, path)
		}
	}

	config := ""
	cleanupConfig := func() {}
	defer func() { cleanupConfig() }()
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
			return nil, fmt.Errorf("cannot inspect conflicted staged agent path %s; resolve the index and retry", quoteASCII(path))
		}
		entry := pathEntries[0]
		if entry.Mode != "100644" && entry.Mode != "100755" {
			return nil, fmt.Errorf("cannot inspect staged agent path %s with non-regular Git mode %s", quoteASCII(path), entry.Mode)
		}
		blob, err := stagedBlob(repoRoot, entry.OID)
		if err != nil {
			return nil, fmt.Errorf("cannot read staged agent blob for %s", quoteASCII(path))
		}
		if config == "" {
			config, cleanupConfig, err = snapshotTrustedConfig()
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
	if hasBlockingFinding(findings) {
		findings = appendMixedCommitFinding(findings, agentPaths, otherPaths)
		sortFindings(findings)
		return findings, nil
	}

	generated, err := checkGenerated(repoRoot, agentPaths, entries)
	if err != nil {
		return nil, errors.New("cannot validate staged generated indexes; inspect the quoted staged paths and run `agents index`")
	}
	findings = append(findings, generated...)

	findings = appendMixedCommitFinding(findings, agentPaths, otherPaths)
	sortFindings(findings)
	return findings, nil
}

func hasBlockingFinding(findings []Finding) bool {
	for _, finding := range findings {
		if finding.Blocking {
			return true
		}
	}
	return false
}

func appendMixedCommitFinding(findings []Finding, agentPaths, otherPaths []string) []Finding {
	if len(agentPaths) == 0 || len(otherPaths) == 0 {
		return findings
	}
	return append(findings, Finding{
		Rule:     "mixed-commit",
		Blocking: false,
		Detail: fmt.Sprintf("this commit touches %d agent path(s) and %d other path(s); "+
			"`agents save` commits .agents/ on its own", len(agentPaths), len(otherPaths)),
	})
}

func snapshotTrustedConfig() (snapshot string, cleanup func(), err error) {
	data, err := gitleaksConfigBytes()
	if err != nil {
		return "", func() {}, errors.New("trusted embedded gitleaks config is unavailable; rebuild agents")
	}
	if len(data) == 0 {
		return "", func() {}, errors.New("trusted embedded gitleaks config is empty; rebuild agents")
	}

	dir, err := os.MkdirTemp("", "agents-gitleaks-config-")
	if err != nil {
		return "", func() {}, errors.New("could not create a private gitleaks config snapshot")
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	if err := os.Chmod(dir, 0o700); err != nil {
		cleanup()
		return "", func() {}, errors.New("could not secure the private gitleaks config snapshot")
	}
	snapshot = filepath.Join(dir, "gitleaks.toml")
	out, err := os.OpenFile(snapshot, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		cleanup()
		return "", func() {}, errors.New("could not create the private gitleaks config snapshot")
	}
	if _, err := out.Write(data); err != nil {
		_ = out.Close()
		cleanup()
		return "", func() {}, errors.New("could not write the private gitleaks config snapshot")
	}
	if err := out.Close(); err != nil {
		cleanup()
		return "", func() {}, errors.New("could not finish the private gitleaks config snapshot")
	}
	return snapshot, cleanup, nil
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
		if !validRuleID(result.RuleID) || result.StartLine < 1 {
			return nil, errors.New("gitleaks returned an incomplete finding report")
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

func validRuleID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			continue
		}
		if i > 0 && (c == '-' || c == '_' || c == '.') {
			continue
		}
		return false
	}
	return true
}

func stagedPaths(repoRoot string) ([]string, error) {
	// Do not filter statuses: type changes, unmerged entries, and any Git status
	// added in the future all need to reach the index-mode checks below.
	out, err := gitOutput(repoRoot,
		"-c", "diff.ignoreSubmodules=none",
		"diff", "--cached", "--name-only",
		"--no-renames", "--no-ext-diff", "--no-textconv", "--ignore-submodules=none", "-z")
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
		"GIT_DIR":                          true,
		"GIT_WORK_TREE":                    true,
		"GIT_OBJECT_DIRECTORY":             true,
		"GIT_ALTERNATE_OBJECT_DIRECTORIES": true,
		"GIT_QUARANTINE_PATH":              true,
		"GIT_COMMON_DIR":                   true,
		"GIT_NAMESPACE":                    true,
		"GIT_NO_REPLACE_OBJECTS":           true,
	}
	result := make([]string, 0, len(env))
	for _, pair := range env {
		key, _, ok := strings.Cut(pair, "=")
		if ok && (forbidden[key] || strings.HasPrefix(key, "GIT_CONFIG_")) {
			continue
		}
		result = append(result, pair)
	}
	return append(result, "GIT_NO_REPLACE_OBJECTS=1")
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

	created := map[string]bool{scratch: true}
	// The renderer never consumes the staged generated file, but its destination
	// still owns a filesystem identity in the tree we are modelling. Reserve it
	// before materialising sources so a Git-distinct source component that this
	// host aliases to the destination cannot occupy that identity first.
	generatedDestination := filepath.Join(scratch, filepath.FromSlash(target.index))
	if err := writeScratchFile(scratch, generatedDestination, nil, created); err != nil {
		return nil, fmt.Errorf("cannot reserve generated target %s in the scratch filesystem", quoteASCII(target.index))
	}
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
		// Preserve the cross-platform collision policy even when this checkout is
		// on a case-sensitive filesystem. The scratch reservation above covers
		// every additional alias relation implemented by the current host.
		if aliasesGeneratedTarget(entry.Path, target.index) {
			return nil, fmt.Errorf("source path %s aliases the generated target %s", quoteASCII(entry.Path), quoteASCII(target.index))
		}
		blob, err := stagedBlob(repoRoot, entry.OID)
		if err != nil {
			return nil, fmt.Errorf("cannot read source blob for %q", entry.Path)
		}
		destination := filepath.Join(scratch, filepath.FromSlash(entry.Path))
		rel, err := filepath.Rel(scratch, destination)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("source path %s escapes the scratch tree", quoteASCII(entry.Path))
		}
		if filepath.ToSlash(filepath.Clean(filepath.FromSlash(entry.Path))) != entry.Path {
			return nil, fmt.Errorf("source path %s has a non-canonical filesystem form", quoteASCII(entry.Path))
		}
		if err := writeScratchFile(scratch, destination, blob, created); err != nil {
			return nil, fmt.Errorf("source path %s aliases another staged path in the scratch filesystem", quoteASCII(entry.Path))
		}
	}
	return target.render(scratch)
}

func aliasesGeneratedTarget(source, generated string) bool {
	if strings.EqualFold(source, generated) {
		return true
	}
	return len(source) > len(generated) &&
		source[len(generated)] == '/' &&
		strings.EqualFold(source[:len(generated)], generated)
}

func writeScratchFile(scratch, destination string, blob []byte, created map[string]bool) error {
	rel, err := filepath.Rel(scratch, destination)
	if err != nil {
		return err
	}
	parts := strings.Split(rel, string(filepath.Separator))
	current := scratch
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		if created[current] {
			continue
		}
		if _, err := os.Lstat(current); err == nil {
			return errors.New("filesystem alias")
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := os.Mkdir(current, 0o700); err != nil {
			return err
		}
		created[current] = true
	}
	if created[destination] {
		return errors.New("duplicate scratch destination")
	}
	if _, err := os.Lstat(destination); err == nil {
		return errors.New("filesystem alias")
	} else if !os.IsNotExist(err) {
		return err
	}
	f, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(blob); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	created[destination] = true
	return nil
}

func quoteASCII(value string) string {
	return strconv.QuoteToASCII(value)
}
