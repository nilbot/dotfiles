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
	"syscall"
)

var installedHookNames = map[string]struct{}{
	"pre-commit":    {},
	"commit-msg":    {},
	"post-merge":    {},
	"post-checkout": {},
}

const activeHooksEnvironment = "AGENTS_ACTIVE_GIT_HOOKS"

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
	activeHooks := os.Getenv(activeHooksEnvironment)
	if activeHookContains(activeHooks, name) {
		fmt.Fprintf(stderr, "%s: refusing recursive git hook dispatch; inspect the repository hook wrapper\n", name)
		return 1
	}
	childEnvironment := hookEnvironment(os.Environ(), activeHooks, name)
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
		cmd.Env = childEnvironment
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

func activeHookContains(active, name string) bool {
	for _, item := range strings.Split(active, ",") {
		if item == name {
			return true
		}
	}
	return false
}

func hookEnvironment(environment []string, active, name string) []string {
	prefix := activeHooksEnvironment + "="
	out := make([]string, 0, len(environment)+1)
	for _, item := range environment {
		if !strings.HasPrefix(item, prefix) {
			out = append(out, item)
		}
	}
	if active != "" {
		active += ","
	}
	return append(out, prefix+active+name)
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
	_, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("cannot inspect repository hook %s", strconv.QuoteToASCII(path))
	}
	info, err := os.Stat(path)
	if err != nil {
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
	commentStart := trailingCommentStart(lines)
	lastTrailer := commentStart - 1
	for lastTrailer >= 0 && blankLine(lines[lastTrailer]) {
		lastTrailer--
	}
	if lastTrailer < 0 || !trailerLike(lines[lastTrailer]) {
		return msg
	}

	start := lastTrailer
	separatorStart := -1
	for i := lastTrailer; i >= 0; {
		if trailerLike(lines[i]) {
			start = i
			i--
			continue
		}
		if !blankLine(lines[i]) {
			return msg
		}
		runEnd := i
		for i >= 0 && blankLine(lines[i]) {
			i--
		}
		runStart := i + 1
		// The generated marker and co-author trailer convention contains one
		// blank line between its two attribution lines. Other blank runs are
		// the required separator before the trailer block, and stop the scan
		// from crossing into a distinct earlier block.
		if runEnd == runStart && i >= 0 && claudeGeneratedLine.Match(normalizedLine(lines[i])) {
			start = runStart
			continue
		}
		separatorStart = runStart
		break
	}
	if separatorStart < 0 {
		return msg
	}

	removed := false
	keptTrailers := make([][]byte, 0, lastTrailer-start+1)
	for _, line := range lines[start : lastTrailer+1] {
		if aiAttributionLine(line) {
			removed = true
			continue
		}
		if !blankLine(line) {
			keptTrailers = append(keptTrailers, line)
		}
	}
	if !removed {
		return msg
	}

	out := append([][]byte(nil), lines[:separatorStart]...)
	for len(out) > 0 && blankLine(out[len(out)-1]) {
		out = out[:len(out)-1]
	}
	if len(keptTrailers) > 0 {
		out = append(out, blankForMessage(msg))
		out = append(out, keptTrailers...)
	}
	if commentStart < len(lines) {
		out = append(out, lines[lastTrailer+1:commentStart]...)
		out = append(out, lines[commentStart:]...)
	} else if bytes.HasSuffix(msg, []byte{'\n'}) {
		out = append(out, []byte{})
	}
	return bytes.Join(out, []byte{'\n'})
}

func trailerLike(line []byte) bool {
	line = normalizedLine(line)
	return trailerLine.Match(line) || claudeGeneratedLine.Match(line)
}

func trailingCommentStart(lines [][]byte) int {
	i := len(lines) - 1
	for i >= 0 && blankLine(lines[i]) {
		i--
	}
	if i < 0 || !commentLine(lines[i]) {
		return len(lines)
	}
	for i >= 0 && (blankLine(lines[i]) || commentLine(lines[i])) {
		i--
	}
	start := i + 1
	for start < len(lines) && blankLine(lines[start]) {
		start++
	}
	return start
}

func commentLine(line []byte) bool {
	line = bytes.TrimSuffix(line, []byte{'\r'})
	return bytes.HasPrefix(line, []byte{'#'})
}

func blankForMessage(msg []byte) []byte {
	if bytes.Contains(msg, []byte("\r\n")) {
		return []byte{'\r'}
	}
	return []byte{}
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
	return stripFootersInFileBeforeReplace(path, stderr, nil)
}

func stripFootersInFileBeforeReplace(path string, stderr io.Writer, beforeReplace func()) int {
	info, err := os.Lstat(path)
	if err != nil {
		return commitMessageFailure(stderr, path, "cannot inspect message file")
	}
	if !info.Mode().IsRegular() {
		return commitMessageFailure(stderr, path, "message path is not a regular file")
	}

	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
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

	if beforeReplace != nil {
		beforeReplace()
	}
	currentFile, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return commitMessageFailure(stderr, path, "message file changed before rewrite")
	}
	current, statErr := currentFile.Stat()
	currentMsg, readErr := io.ReadAll(currentFile)
	closeErr = currentFile.Close()
	if statErr != nil || readErr != nil || closeErr != nil || !current.Mode().IsRegular() ||
		!os.SameFile(openedInfo, current) || current.Mode() != openedInfo.Mode() ||
		current.Size() != int64(len(msg)) || !bytes.Equal(currentMsg, msg) {
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
	_, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return !info.IsDir() && info.Mode()&0o111 != 0, nil
}
