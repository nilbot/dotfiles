package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nilbot/dotfiles/agents/internal/githook"
	"github.com/nilbot/dotfiles/agents/internal/safeio"
)

// The chain is the directory the global core.hooksPath names: one record and
// four entries Git executes. Doctor reads it and never writes it.
//
// It reads the chain WHEREVER that setting points, and holds no checkout root
// of its own. Stage 1 wrote it inside a checkout, at <checkout>/git/hooks.d;
// stage 2 moved it to the machine-owned ~/.config/agents/hooks.d, where a moved
// or deleted checkout cannot take the guard with it. Nothing here assumes
// either location: the record's `checkout` is the machine's statement of where
// the personal stages live, which is a different fact from where the chain
// itself sits.

const (
	chainRecordName         = "chain.env"
	chainGeneratedHeaderFor = "# Written by git/install-hooks.sh for "
)

// chainRecord is the parsed record. Its three keys are the whole format.
type chainRecord struct {
	Format   string
	Binary   string
	Checkout string
}

// parseChainRecord reads the record under the same allow-list the entries
// enforce: one key=value per line, comments and blank lines skipped, nothing
// after the first `=` interpreted, an unknown key refused by name.
//
// The wording mirrors the generated entry in git/install-hooks.sh, because the
// person who reads a failed commit and the person who runs doctor should be
// looking at the same sentence.
func parseChainRecord(path string, contents []byte) (chainRecord, error) {
	var record chainRecord
	for _, line := range strings.Split(strings.ReplaceAll(string(contents), "\r\n", "\n"), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			return chainRecord{}, fmt.Errorf("unknown key %q in %s", line, path)
		}
		switch key {
		case "format":
			record.Format = value
		case "binary":
			record.Binary = value
		case "checkout":
			record.Checkout = value
		default:
			return chainRecord{}, fmt.Errorf("unknown key %q in %s", key, path)
		}
	}
	switch {
	case record.Format != "1":
		return chainRecord{}, fmt.Errorf("%s is not format 1", path)
	case !filepath.IsAbs(record.Binary):
		return chainRecord{}, fmt.Errorf("binary in %s is not an absolute path", path)
	case record.Checkout != "-" && !filepath.IsAbs(record.Checkout):
		return chainRecord{}, fmt.Errorf("checkout in %s is neither - nor an absolute path", path)
	}
	return record, nil
}

// chainEntryProblem reports what is wrong with one entry's contents, or "" when
// nothing is. The shape is the installer's ownership rule -- line 1, the
// generated header on line 2, and a last line that execs githook for the hook
// name the file itself carries.
func chainEntryProblem(hook string, contents []byte) string {
	lines := strings.Split(strings.TrimSuffix(string(contents), "\n"), "\n")
	if len(lines) < 3 || lines[0] != "#!/bin/sh" {
		return hook + " does not carry the generated header"
	}
	header := lines[1]
	if !strings.HasPrefix(header, chainGeneratedHeaderFor) || !strings.HasSuffix(header, ".") {
		return hook + " does not carry the generated header"
	}
	last := lines[len(lines)-1]
	if !strings.Contains(last, "exec ") || !strings.Contains(last, " githook "+hook+" ") {
		return hook + " does not exec githook " + hook + " on its last line"
	}
	return ""
}

// hasGeneratedHeader is the weaker test chain:unmanaged needs: a file that
// looks like an entry this installer writes, whatever name it sits under.
func hasGeneratedHeader(contents []byte) bool {
	lines := strings.Split(strings.TrimSuffix(string(contents), "\n"), "\n")
	if len(lines) < 2 || lines[0] != "#!/bin/sh" {
		return false
	}
	return strings.HasPrefix(lines[1], chainGeneratedHeaderFor) && strings.HasSuffix(lines[1], ".")
}

// chainChecks is the machine-level family. It reports what is installed and
// where, and every check in it reads the chain the global core.hooksPath names
// rather than a path compiled into this binary.
func chainChecks(repoRoot, binary string, deps Dependencies) []Check {
	hooksPath, chainDir, installed := resolveChain(repoRoot, deps)
	checks := []Check{hooksPath}
	if !installed {
		// A machine that never had a chain is not a broken machine: the report
		// says so once, and the two checks that need no chain still run.
		// §8 row 15 pins this: making the absence an error would fail doctor on
		// every machine that never installed the hooks.
		return append(checks, checkChainLocal(repoRoot, deps.Git), checkChainLegacy(repoRoot, deps))
	}
	record, recordErr := readChainRecord(chainDir)
	// The remedy names the installer, and the checkout it lives in comes from
	// the RECORD. A machine-owned chain has no checkout in its path, so the
	// record's `checkout` key is the only statement of which checkout supplies
	// the stages -- and of where git/install-hooks.sh is. A record that will
	// not parse leaves the remedy as the sentence that names no path: printing
	// a guessed command is worse than printing none.
	remedyCheckout := ""
	if recordErr == nil && record.Checkout != "-" {
		remedyCheckout = record.Checkout
	}
	checks = append(checks,
		checkChainEntries(chainDir, remedyCheckout, deps),
		checkChainRecord(chainDir, record, recordErr, remedyCheckout, deps),
		checkChainUnmanaged(chainDir, remedyCheckout, deps),
		checkChainLocal(repoRoot, deps.Git),
		checkChainLegacy(repoRoot, deps),
		checkChainRunning(record, recordErr, binary, remedyCheckout, deps),
		checkChainCheckout(record, recordErr, remedyCheckout, deps),
	)
	return checks
}

// resolveChain answers where the chain is, and reports the setting it read.
//
// The value must come from the machine-local primary global config: a
// core.hooksPath set by an included or shared file is a different finding even
// when the path is right, which is the rule the installer enforces on its own
// side too.
func resolveChain(repoRoot string, deps Dependencies) (Check, string, bool) {
	const name = "chain:hooks-path"
	if deps.Git == nil {
		return Check{Name: name, Status: Fail, Detail: "Git diagnostic runner is unavailable"}, "", false
	}
	result := deps.Git(repoRoot, "config", "--global", "--includes", "--null", "--show-origin", "--get-all", "core.hooksPath")
	values, parseErr := configOriginValues(result.Output)
	switch {
	case result.Code == 1:
		return Check{Name: name, Status: OK, Detail: "global core.hooksPath is unset; no hook chain is installed"}, "", false
	case result.Code != 0:
		return Check{Name: name, Status: Fail, Detail: "global core.hooksPath could not be read", Remedy: "inspect global Git configuration"}, "", false
	case parseErr != nil || len(values) != 1:
		return Check{Name: name, Status: Fail, Detail: fmt.Sprintf("global core.hooksPath has %d values", len(values)), Remedy: "resolve the global values deliberately"}, "", false
	case values[0].Origin != "file:"+deps.GlobalGitConfig:
		return Check{Name: name, Status: Fail, Detail: fmt.Sprintf("global core.hooksPath comes from %s, not the machine-local %s", values[0].Origin, deps.GlobalGitConfig), Remedy: "preserve included settings and restore the reviewed primary global setting deliberately"}, "", false
	}
	chainDir := values[0].Value
	if !filepath.IsAbs(chainDir) {
		// Git resolves a relative core.hooksPath against the top level of the
		// working tree. Resolving it the same way keeps every check below about
		// the directory Git actually reads.
		chainDir = filepath.Join(repoRoot, chainDir)
	}
	return Check{Name: name, Status: OK, Detail: "global core.hooksPath is " + values[0].Value}, chainDir, true
}

func readChainRecord(chainDir string) (chainRecord, error) {
	path := filepath.Join(chainDir, chainRecordName)
	contents, err := safeio.ReadRegular(path)
	if err != nil {
		return chainRecord{}, fmt.Errorf("cannot read %s", path)
	}
	return parseChainRecord(path, contents)
}

func checkChainEntries(chainDir, remedyCheckout string, deps Dependencies) Check {
	const name = "chain:entries"
	remedy := hookInstallerRemedy(remedyCheckout, deps, false)
	if info, err := os.Stat(chainDir); err != nil || !info.IsDir() {
		// The one silence §4 names: Git looks in a directory that is not there,
		// runs nothing, and reports nothing. doctor is the only thing that can
		// say so.
		return Check{Name: name, Status: Fail, Detail: fmt.Sprintf("the chain directory %s is gone, so Git runs no hook and reports nothing", chainDir), Remedy: remedy}
	}
	for _, hook := range installedHookNames {
		path := filepath.Join(chainDir, hook)
		info, err := os.Lstat(path)
		switch {
		case err != nil:
			return Check{Name: name, Status: Fail, Detail: hook + " is missing from " + chainDir, Remedy: remedy}
		case info.Mode()&os.ModeSymlink != 0:
			// The shape an unconverted machine still has. Git runs whatever the
			// link resolves to; doctor wants the entry, and --adopt-owned is
			// what converts it.
			return Check{Name: name, Status: Fail, Detail: hook + " is a symlink, not a generated entry", Remedy: remedy}
		case !info.Mode().IsRegular():
			return Check{Name: name, Status: Fail, Detail: hook + " is not a regular file", Remedy: remedy}
		}
		contents, info, err := safeio.ReadRegularInfo(path)
		if err != nil {
			return Check{Name: name, Status: Fail, Detail: hook + " could not be read", Remedy: remedy}
		}
		if info.Mode().Perm()&0o111 == 0 {
			// Git prints a hint and runs nothing at exit 0, so a
			// non-executable entry is a guard that is off with only a hint
			// saying so.
			return Check{Name: name, Status: Fail, Detail: hook + " is not executable: Git skips it with a hint and runs nothing", Remedy: remedy}
		}
		if problem := chainEntryProblem(hook, contents); problem != "" {
			return Check{Name: name, Status: Fail, Detail: problem, Remedy: remedy}
		}
	}
	return Check{Name: name, Status: OK, Detail: "all four entries are present, executable and carry the generated header"}
}

func checkChainRecord(chainDir string, record chainRecord, recordErr error, remedyCheckout string, deps Dependencies) Check {
	const name = "chain:record"
	remedy := hookInstallerRemedy(remedyCheckout, deps, false)
	if recordErr != nil {
		return Check{Name: name, Status: Fail, Detail: recordErr.Error(), Remedy: remedy}
	}
	info, err := os.Stat(record.Binary)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return Check{Name: name, Status: Fail, Detail: fmt.Sprintf("the record names %s, which is missing or not an executable regular file", record.Binary), Remedy: remedy}
	}
	return Check{Name: name, Status: OK, Detail: fmt.Sprintf("%s records format 1, binary %s, checkout %s", chainRecordName, record.Binary, record.Checkout)}
}

// checkChainRunning stays a failure, and that is deliberate: it compares the
// binary the record names with the executable that is running. Reporting both
// as facts with no failure would make doctor print ok while the binary you
// invoke is not the binary your commits run -- the disagreement the 2026-08-20
// upgrade incident was made of.
func checkChainRunning(record chainRecord, recordErr error, binary, remedyCheckout string, deps Dependencies) Check {
	const name = "chain:running"
	remedy := hookInstallerRemedy(remedyCheckout, deps, false)
	if recordErr != nil {
		return Check{Name: name, Status: Fail, Detail: "the record cannot be read, so the binary Git runs cannot be compared with " + binary, Remedy: remedy}
	}
	recorded, err := os.Stat(record.Binary)
	if err != nil {
		return Check{Name: name, Status: Fail, Detail: fmt.Sprintf("the binary the record names, %s, cannot be inspected", record.Binary), Remedy: remedy}
	}
	running, err := os.Stat(binary)
	if err != nil {
		return Check{Name: name, Status: Fail, Detail: fmt.Sprintf("the running binary %s cannot be inspected", binary), Remedy: "rebuild and reinstall agents"}
	}
	if !os.SameFile(recorded, running) {
		return Check{Name: name, Status: Fail, Detail: fmt.Sprintf("the chain runs %s, but the binary that is running is %s", record.Binary, binary), Remedy: remedy}
	}
	return Check{Name: name, Status: OK, Detail: "the chain runs " + record.Binary + ", which is the running binary"}
}

// checkChainCheckout reports the personal stages the record points at. It is a
// warning and not a failure, where root:exists was a failure: the checkout
// being gone stops the personal stages, and the built-in guard still runs.
//
// It reports the COUNT rather than mere presence: this repository's git/hooks/
// carries two tracked files, so "the directory is not empty" is true of any
// checkout of it, and zero is the state the check exists to notice.
func checkChainCheckout(record chainRecord, recordErr error, remedyCheckout string, deps Dependencies) Check {
	const name = "chain:checkout"
	remedy := hookInstallerRemedy(remedyCheckout, deps, false)
	if recordErr != nil {
		return Check{Name: name, Status: Warn, Detail: "the record cannot be read, so the checkout it names cannot be reported", Remedy: remedy}
	}
	if record.Checkout == "-" {
		return Check{Name: name, Status: OK, Detail: "the record declares no personal stages"}
	}
	info, err := os.Stat(record.Checkout)
	if err != nil || !info.IsDir() {
		return Check{Name: name, Status: Warn, Detail: fmt.Sprintf("the recorded checkout %s is gone, so the personal stages no longer run", record.Checkout), Remedy: remedy}
	}
	count := personalStageCount(record.Checkout)
	if count == 0 {
		return Check{Name: name, Status: Warn, Detail: fmt.Sprintf("the recorded checkout %s supplies no personal stages: 0 executable files named <anything>.<hook> under git/hooks", record.Checkout), Remedy: remedy}
	}
	return Check{Name: name, Status: OK, Detail: fmt.Sprintf("the recorded checkout %s supplies %d personal stage(s)", record.Checkout, count)}
}

// personalStageCount counts what `agents githook` would actually run: regular
// files under <checkout>/git/hooks named <anything>.<hook> that are executable,
// which is the rule internal/githook applies.
func personalStageCount(checkout string) int {
	dir := filepath.Join(checkout, "git", "hooks")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, entry := range entries {
		name := entry.Name()
		matches := false
		for _, hook := range installedHookNames {
			if strings.HasSuffix(name, "."+hook) {
				matches = true
				break
			}
		}
		if !matches {
			continue
		}
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.IsDir() || info.Mode().Perm()&0o111 == 0 {
			continue
		}
		count++
	}
	return count
}

// checkChainUnmanaged reports the complementary case to chain:entries: files in
// the chain directory that are not one of the four managed names, and that a
// person has to deal with.
//
// Two shapes: a symlink left by an earlier installer that no longer resolves --
// it dangles forever and no other check names it -- and a generated entry under
// a name Git never runs, which looks installed and is not.
func checkChainUnmanaged(chainDir, remedyCheckout string, deps Dependencies) Check {
	const name = "chain:unmanaged"
	entries, err := os.ReadDir(chainDir)
	if err != nil {
		return Check{Name: name, Status: Warn, Detail: "the chain directory could not be read", Remedy: "inspect " + chainDir}
	}
	var dangling, lookalikes []string
	for _, entry := range entries {
		hookName := entry.Name()
		if isManagedHookName(hookName) {
			continue
		}
		path := filepath.Join(chainDir, hookName)
		info, err := os.Lstat(path)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if _, err := os.Stat(path); err != nil {
				dangling = append(dangling, hookName)
			}
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if contents, err := safeio.ReadRegular(path); err == nil && hasGeneratedHeader(contents) {
			lookalikes = append(lookalikes, hookName)
		}
	}
	if len(dangling) == 0 && len(lookalikes) == 0 {
		return Check{Name: name, Status: OK, Detail: "no unowned hook links dangle and no look-alike entry sits under another name"}
	}
	sort.Strings(dangling)
	sort.Strings(lookalikes)
	var findings []string
	if len(dangling) > 0 {
		findings = append(findings, "unowned hook link(s) dangle and will never run: "+strings.Join(dangling, ", "))
	}
	if len(lookalikes) > 0 {
		findings = append(findings, "generated entry(ies) sit under a name Git never runs: "+strings.Join(lookalikes, ", "))
	}
	return Check{
		Name:   name,
		Status: Warn,
		Detail: strings.Join(findings, "; "),
		Remedy: "delete them, or repoint them deliberately: " + hookInstallerRemedy(remedyCheckout, deps, false),
	}
}

// checkChainLocal reports a repository-local core.hooksPath, which bypasses the
// chain by choice rather than by fault.
func checkChainLocal(repoRoot string, git func(dir string, args ...string) GitResult) Check {
	const name = "chain:local"
	if git == nil {
		return Check{Name: name, Status: Fail, Detail: "Git diagnostic runner is unavailable"}
	}
	local := git(repoRoot, "config", "--local", "--get-all", "core.hooksPath")
	localValues := configValues(local.Output)
	switch {
	case local.Code == 1:
		return Check{Name: name, Status: OK, Detail: "no repository-local core.hooksPath override"}
	case local.Code != 0:
		return Check{Name: name, Status: Fail, Detail: "repository-local core.hooksPath could not be read", Remedy: "inspect repository or linked-worktree Git configuration"}
	case len(localValues) == 0:
		return Check{Name: name, Status: Fail, Detail: "repository-local core.hooksPath returned an empty value"}
	default:
		return Check{Name: name, Status: Warn, Detail: fmt.Sprintf("repository-local core.hooksPath override is set (%d value(s))", len(localValues)), Remedy: "the global agents hooks are shadowed here; chain them from the local hook directory if desired"}
	}
}

// checkChainLegacy reports the exact retired dispatcher when one is still
// installed in the repository's own hooks directory.
func checkChainLegacy(repoRoot string, deps Dependencies) Check {
	const name = "chain:legacy"
	if deps.LegacyHooksPath == nil {
		return Check{Name: name, Status: Fail, Detail: "repository legacy hooks directory could not be resolved", Remedy: "inspect the repository Git directory"}
	}
	dir, err := deps.LegacyHooksPath(repoRoot)
	if err != nil || !filepath.IsAbs(dir) {
		return Check{Name: name, Status: Fail, Detail: "repository legacy hooks directory could not be resolved", Remedy: "inspect the repository Git directory"}
	}
	var found []string
	for _, name := range installedHookNames {
		if githook.IsRetiredShim(filepath.Join(dir, name)) {
			found = append(found, name)
		}
	}
	if len(found) > 0 {
		return Check{Name: name, Status: Warn, Detail: "exact retired legacy dispatcher remains for " + strings.Join(found, ", "), Remedy: "remove only the exact retired shim after preserving foreign hooks"}
	}
	return Check{Name: name, Status: OK, Detail: "no exact retired legacy dispatcher detected"}
}
