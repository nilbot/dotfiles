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

// Staged checks staged .agents/ blobs, unsafe paths, and mixed commits, then
// scans the staged content for secrets.
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
			return findings, fmt.Errorf("cannot inspect conflicted staged agent path %s; resolve the index and retry", quoteASCII(path))
		}
		entry := pathEntries[0]
		if entry.Mode != "100644" && entry.Mode != "100755" {
			return findings, fmt.Errorf("cannot inspect staged agent path %s with non-regular Git mode %s", quoteASCII(path), entry.Mode)
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

func quoteASCII(value string) string {
	return strconv.QuoteToASCII(value)
}
