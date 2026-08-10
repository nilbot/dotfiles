// Package githook dispatches the agents binary when Git invokes it through a
// hook-named symlink and preserves hooks that were already installed.
package githook

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var installedHookNames = map[string]struct{}{
	"pre-commit":    {},
	"commit-msg":    {},
	"post-merge":    {},
	"post-checkout": {},
}

// IsHookName reports whether name is one of the hook entrypoints installed by
// this repository. Other Git hook names remain normal CLI basenames.
func IsHookName(name string) bool {
	_, ok := installedHookNames[name]
	return ok
}

// Chain locates the external stages that precede the agents built-in stage.
type Chain struct {
	RepoHooksDir string
	ExtrasDir    string
	// DispatcherPath identifies the multicall executable for recursion checks.
	// Production callers may leave it empty to use os.Executable.
	DispatcherPath string
	SkipBuiltin    bool
}

// Run executes an existing repository hook and then matching personal hooks.
func Run(c Chain, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if !IsHookName(name) {
		fmt.Fprintln(stderr, "agents: unsupported git hook name")
		return 1
	}
	stages, err := externalStages(c, name)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return 1
	}
	for _, stage := range stages {
		cmd := exec.Command(stage, args...)
		cmd.Stdin = stdin
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		cmd.Env = os.Environ()
		if err := cmd.Run(); err != nil {
			if exit, ok := err.(*exec.ExitError); ok {
				fmt.Fprintf(stderr, "%s: hook %s exited %d\n", name, strconv.QuoteToASCII(stage), exit.ExitCode())
				return exit.ExitCode()
			}
			fmt.Fprintf(stderr, "%s: could not execute hook %s\n", name, strconv.QuoteToASCII(stage))
			return 1
		}
	}
	if c.SkipBuiltin {
		return 0
	}
	return builtin(name, args, stderr)
}

func externalStages(c Chain, name string) ([]string, error) {
	var stages []string
	if c.RepoHooksDir != "" {
		path := filepath.Join(c.RepoHooksDir, name)
		include, err := repositoryHookStage(path, c.DispatcherPath)
		if err != nil {
			return nil, err
		}
		if include {
			stages = append(stages, path)
		}
	}
	if c.ExtrasDir == "" {
		return stages, nil
	}
	entries, err := os.ReadDir(c.ExtrasDir)
	if err != nil {
		if os.IsNotExist(err) {
			return stages, nil
		}
		return nil, fmt.Errorf("cannot inspect personal hooks directory %s", strconv.QuoteToASCII(c.ExtrasDir))
	}
	suffix := "." + name
	var extras []string
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), suffix) {
			continue
		}
		path := filepath.Join(c.ExtrasDir, entry.Name())
		isExecutable, err := executable(path)
		if err != nil {
			return nil, fmt.Errorf("cannot inspect personal hook %s", strconv.QuoteToASCII(path))
		}
		if isExecutable {
			extras = append(extras, path)
		}
	}
	sort.Strings(extras)
	return append(stages, extras...), nil
}

var retiredShimFingerprints = map[int64]string{
	428: "e86bca20c7faa344867c0db807c42608ec2963a9adde0a4d9133d57c7d14c43a",
	659: "b4b2cf4da1231db9a379aee9ca0cf714ff7ca5b66a7acbfbe60d8020354e68b0",
}

func repositoryHookStage(path, dispatcherPath string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("cannot inspect repository hook %s", strconv.QuoteToASCII(path))
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		return false, nil
	}

	if dispatcherPath == "" {
		dispatcherPath, _ = os.Executable()
	}
	if dispatcherPath != "" {
		dispatcherInfo, dispatcherErr := os.Stat(dispatcherPath)
		if dispatcherErr == nil && os.SameFile(info, dispatcherInfo) {
			return false, fmt.Errorf("repository hook %s resolves to the agents dispatcher; remove the recursive hook", strconv.QuoteToASCII(path))
		}
	}

	if retiredTemplateShim(path, info.Size()) {
		return false, nil
	}
	return true, nil
}

func retiredTemplateShim(path string, size int64) bool {
	want, ok := retiredShimFingerprints[size]
	if !ok {
		return false
	}
	b, err := os.ReadFile(path)
	if err != nil || int64(len(b)) != size {
		return false
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]) == want
}

var (
	trailerLine         = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*[ \t]*:[ \t]+\S.*$`)
	claudeCoauthorLine  = regexp.MustCompile(`(?i)^co-authored-by[ \t]*:[ \t]*claude(?:[ \t]+code)?[ \t]*<noreply@anthropic\.com>[ \t]*$`)
	claudeGeneratedLine = regexp.MustCompile(`(?i)^(?:🤖[ \t]*)?generated with[ \t]+(?:\[claude code\](?:\(https://claude\.com/claude-code\))?|claude code)[ \t]*$`)
)

// StripFooters removes Claude attribution from the trailing Git trailer block.
// Identical text in the message body is outside that suffix and is preserved.
// If no attribution is removed, the original bytes are returned unchanged.
func StripFooters(msg []byte) []byte {
	if len(msg) == 0 {
		return msg
	}
	lines := bytes.Split(msg, []byte{'\n'})
	last := len(lines) - 1
	for last >= 0 && blankLine(lines[last]) {
		last--
	}
	if last < 0 {
		return msg
	}

	start := last
	for start >= 0 {
		line := normalizedLine(lines[start])
		if len(line) == 0 || trailerLine.Match(line) || claudeGeneratedLine.Match(line) {
			start--
			continue
		}
		break
	}
	start++

	removed := false
	out := make([][]byte, 0, len(lines))
	for i, line := range lines {
		if i >= start && aiAttributionLine(line) {
			removed = true
			continue
		}
		if i >= start && blankLine(line) && len(out) > 0 && blankLine(out[len(out)-1]) {
			continue
		}
		out = append(out, line)
	}
	if !removed {
		return msg
	}
	for len(out) > 0 && blankLine(out[len(out)-1]) {
		out = out[:len(out)-1]
	}
	if len(out) == 0 {
		return nil
	}
	if bytes.HasSuffix(msg, []byte{'\n'}) {
		out = append(out, nil)
	}
	return bytes.Join(out, []byte{'\n'})
}

func normalizedLine(line []byte) []byte {
	return bytes.TrimSpace(bytes.TrimSuffix(line, []byte{'\r'}))
}

func blankLine(line []byte) bool { return len(normalizedLine(line)) == 0 }

func aiAttributionLine(line []byte) bool {
	line = normalizedLine(line)
	return claudeCoauthorLine.Match(line) || claudeGeneratedLine.Match(line)
}

func builtin(name string, args []string, stderr io.Writer) int {
	if name != "commit-msg" {
		return 0
	}
	if len(args) == 0 {
		fmt.Fprintln(stderr, "commit-msg: missing message file argument")
		return 1
	}
	return stripFootersInFile(args[0], stderr)
}

func stripFootersInFile(path string, stderr io.Writer) int {
	info, err := os.Lstat(path)
	if err != nil {
		return commitMessageFailure(stderr, path, "cannot inspect message file")
	}
	if !info.Mode().IsRegular() {
		return commitMessageFailure(stderr, path, "message path is not a regular file")
	}

	file, err := os.Open(path)
	if err != nil {
		return commitMessageFailure(stderr, path, "cannot read message file")
	}
	openedInfo, statErr := file.Stat()
	if statErr != nil || !os.SameFile(info, openedInfo) {
		_ = file.Close()
		return commitMessageFailure(stderr, path, "message file changed while opening")
	}
	msg, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return commitMessageFailure(stderr, path, "cannot read message file")
	}

	out := StripFooters(msg)
	if bytes.Equal(msg, out) {
		return 0
	}
	if info.Mode().Perm()&0o222 == 0 {
		return commitMessageFailure(stderr, path, "message file is not writable")
	}

	temp, err := os.CreateTemp(filepath.Dir(path), ".agents-commit-msg-")
	if err != nil {
		return commitMessageFailure(stderr, path, "cannot create atomic rewrite")
	}
	tempPath := temp.Name()
	keepTemp := true
	defer func() {
		_ = temp.Close()
		if keepTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(info.Mode().Perm()); err != nil {
		return commitMessageFailure(stderr, path, "cannot preserve message file mode")
	}
	if _, err := temp.Write(out); err != nil {
		return commitMessageFailure(stderr, path, "cannot write atomic rewrite")
	}
	if err := temp.Sync(); err != nil {
		return commitMessageFailure(stderr, path, "cannot finish atomic rewrite")
	}
	if err := temp.Close(); err != nil {
		return commitMessageFailure(stderr, path, "cannot finish atomic rewrite")
	}

	current, err := os.Lstat(path)
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(info, current) {
		return commitMessageFailure(stderr, path, "message file changed before rewrite")
	}
	if err := os.Rename(tempPath, path); err != nil {
		return commitMessageFailure(stderr, path, "cannot replace message file")
	}
	keepTemp = false
	return 0
}

func commitMessageFailure(stderr io.Writer, path, reason string) int {
	fmt.Fprintf(stderr, "commit-msg: %s %s\n", reason, strconv.QuoteToASCII(path))
	return 1
}

func executable(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return !info.IsDir() && info.Mode()&0o111 != 0, nil
}
