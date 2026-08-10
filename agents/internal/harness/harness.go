// Package harness isolates everything that differs between coding-agent
// runtimes: what a hook payload is called, which events exist, what the
// generated config looks like, and what each runtime can and cannot tell us.
//
// Harness identity is always passed in explicitly. It is never inferred from
// the environment: Codex publishes no identifying variables of its own, and a
// Codex hook launched from a Claude Code session inherits ~14 CLAUDE_CODE_* and
// ANTHROPIC_* variables. Detection has false positives available and no true
// positives.
package harness

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/nilbot/dotfiles/agents/internal/pointer"
)

// Semantic event names. These are what the command line and the record use;
// each adapter maps them to its own vendor spelling for wiring.
const (
	SessionStart  = "session-start"
	SubagentStart = "subagent-start"
	SubagentStop  = "subagent-stop"
	Stop          = "stop"
)

// Payload is the entire subset of a hook payload this tool will decode.
//
// It is deliberately exhaustive: encoding/json discards keys with no
// destination field, so anything absent here -- last_assistant_message,
// tool_input, tool_response -- cannot reach any writer, whatever a future
// harness decides to send.
type Payload struct {
	HookEventName string `json:"hook_event_name"`
	SessionID     string `json:"session_id"`

	// The per-turn identifier, under both spellings in use. Codex sends
	// turn_id; Claude Code sends prompt_id and has no turn_id at all (measured
	// 2026-08-07, fixtures/2026-08-07-claude-code-hook-payloads). Build picks
	// whichever is present rather than each adapter restating it.
	TurnID   string `json:"turn_id"`
	PromptID string `json:"prompt_id"`

	AgentID             string `json:"agent_id"`
	AgentType           string `json:"agent_type"`
	Cwd                 string `json:"cwd"`
	TranscriptPath      string `json:"transcript_path"`
	AgentTranscriptPath string `json:"agent_transcript_path"`
	Source              string `json:"source"`
}

// Event maps a semantic event to one harness's spelling of it.
type Event struct {
	Semantic string
	Vendor   string
	Matcher  string // emitted only when non-empty; empty means match everything
}

// Capabilities states what a harness can supply, so the record format does not
// have to pretend they are equal.
type Capabilities struct {
	Description bool // supplies a human label for a subagent
}

// Trace is everything a harness can determine from a payload on its own,
// knowing nothing about repositories, machines, or lanes.
type Trace struct {
	Event           string
	SessionID       string
	TurnID          string
	AgentID         string
	AgentType       string
	Description     string
	Transcript      string
	PointerVerified bool
	Cwd             string // absolute, as the harness reported it
}

type Adapter interface {
	Name() string
	Capabilities() Capabilities
	Events() []Event

	// Describe returns a human label for a subagent, or "" when the harness
	// cannot supply one. transcript is the already-resolved path, because the
	// only harness that can answer reads a sidecar next to it.
	Describe(p Payload, transcript string) string

	// WireConfigPath is the generated config file for a repo.
	WireConfigPath(repoRoot string) string

	// Wire writes that config, merging into whatever is already there.
	Wire(repoRoot, binary string) error

	// TrustSteps are the manual steps left after wiring. No harness lets a
	// freshly wired repo's hooks fire unattended, and defeating that gate is an
	// explicit non-goal.
	TrustSteps(repoRoot string) []string
}

var registry = map[string]Adapter{}

func register(a Adapter) { registry[a.Name()] = a }

func Get(name string) (Adapter, bool) {
	a, ok := registry[strings.ToLower(strings.TrimSpace(name))]
	return a, ok
}

// All returns every adapter in a stable order.
func All() []Adapter {
	names := []string{"claude-code", "codex"}
	var out []Adapter
	for _, n := range names {
		if a, ok := registry[n]; ok {
			out = append(out, a)
		}
	}
	return out
}

// Decode reads one hook payload. It is the single place where hook JSON becomes
// Go values, which is what makes the redaction guarantee auditable.
func Decode(r io.Reader) (Payload, error) {
	var p Payload
	if err := json.NewDecoder(r).Decode(&p); err != nil {
		return Payload{}, fmt.Errorf("decode hook payload: %w", err)
	}
	return p, nil
}

// turnID returns the per-turn identifier under whichever spelling the harness
// used. turn_id wins when both are set, so a harness that adds the other name
// later does not silently change which value is recorded.
func turnID(p Payload) string {
	if p.TurnID != "" {
		return p.TurnID
	}
	return p.PromptID
}

// Build assembles the harness-determined part of a record.
func Build(a Adapter, semantic string, p Payload) Trace {
	tr := Trace{
		Event:     semantic,
		SessionID: p.SessionID,
		TurnID:    turnID(p),
		Cwd:       p.Cwd,
	}

	// The key whose presence in a transcript path verifies the pointer: the
	// agent for subagent events, the session otherwise.
	key := p.SessionID
	if semantic == SubagentStart || semantic == SubagentStop {
		tr.AgentID = p.AgentID
		tr.AgentType = p.AgentType
		key = p.AgentID
	}

	tr.Transcript, tr.PointerVerified = pointer.Resolve(
		[]string{p.AgentTranscriptPath, p.TranscriptPath}, key,
	)
	if a.Capabilities().Description {
		tr.Description = a.Describe(p, tr.Transcript)
	}
	return tr
}

// hookCommand renders the invocation that a harness will run. Harness identity
// is on the command line because it cannot be read from the environment.
func HookCommand(binary, harnessName, semantic string) string {
	return fmt.Sprintf("%s hook %s --harness %s", quotePOSIXWord(binary), semantic, harnessName)
}

func quotePOSIXWord(word string) string {
	if word != "" && strings.IndexFunc(word, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && !strings.ContainsRune("_@%+=:,./-", r)
	}) == -1 {
		return word
	}
	return "'" + strings.ReplaceAll(word, "'", "'\"'\"'") + "'"
}

// ParseHookCommand recognizes only commands generated by agents. It accepts
// the historical safe-unquoted spelling and the current POSIX-quoted spelling.
func ParseHookCommand(command string) (binary, harnessName, semantic string, ok bool) {
	words, ok := splitShellWords(command)
	if !ok || len(words) != 5 || words[1] != "hook" || words[3] != "--harness" {
		return "", "", "", false
	}
	binary, semantic, harnessName = words[0], words[2], words[4]
	if !filepath.IsAbs(binary) || filepath.Base(binary) != "agents" || !knownSemantic(semantic) || !knownHarness(harnessName) {
		return "", "", "", false
	}
	current := HookCommand(binary, harnessName, semantic)
	legacy := fmt.Sprintf("%s hook %s --harness %s", binary, semantic, harnessName)
	if command != current && (command != legacy || quotePOSIXWord(binary) != binary) {
		return "", "", "", false
	}
	return binary, harnessName, semantic, true
}

func IsOwnedHookCommand(command string) bool {
	_, _, _, ok := ParseHookCommand(command)
	return ok
}

func knownSemantic(semantic string) bool {
	switch semantic {
	case SessionStart, SubagentStart, SubagentStop, Stop:
		return true
	default:
		return false
	}
}

func knownHarness(name string) bool {
	switch name {
	case "claude-code", "codex":
		return true
	default:
		return false
	}
}

func splitShellWords(command string) ([]string, bool) {
	var words []string
	var word strings.Builder
	started := false
	quote := byte(0)
	for i := 0; i < len(command); i++ {
		c := command[i]
		if quote != 0 {
			if c == quote {
				quote = 0
				continue
			}
			if quote == '"' && c == '\\' {
				i++
				if i >= len(command) {
					return nil, false
				}
				word.WriteByte(command[i])
				continue
			}
			word.WriteByte(c)
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
			started = true
		case '\\':
			i++
			if i >= len(command) {
				return nil, false
			}
			word.WriteByte(command[i])
			started = true
		case ' ', '\t', '\r', '\n':
			if started {
				words = append(words, word.String())
				word.Reset()
				started = false
			}
		default:
			word.WriteByte(c)
			started = true
		}
	}
	if quote != 0 {
		return nil, false
	}
	if started {
		words = append(words, word.String())
	}
	return words, true
}

type configSnapshot struct {
	exists bool
	info   os.FileInfo
	perm   os.FileMode
}

type skillsSnapshot struct {
	exists bool
	info   os.FileInfo
}

var beforeWirePublish = func() {}

// openHarnessDir opens one repository-controlled child below the repository
// root without allowing that child to redirect writes through a symlink. The
// returned Root remains bound to the checked directory if a pathname above it
// is renamed after this function returns.
func openHarnessDir(repoRoot, name string) (*os.Root, string, error) {
	root, err := os.OpenRoot(repoRoot)
	if err != nil {
		return nil, "", err
	}
	defer root.Close()

	path := filepath.Join(repoRoot, name)
	for {
		info, err := root.Lstat(name)
		if os.IsNotExist(err) {
			if err := root.Mkdir(name, 0o755); err != nil && !os.IsExist(err) {
				return nil, "", err
			}
			continue
		}
		if err != nil {
			return nil, "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, "", fmt.Errorf("%s is a symlink: refusing to wire outside the repository", path)
		}
		if !info.IsDir() {
			return nil, "", fmt.Errorf("%s is not a directory: move it aside before wiring", path)
		}

		child, err := root.OpenRoot(name)
		if err != nil {
			return nil, "", err
		}
		opened, err := child.Stat(".")
		if err != nil {
			child.Close()
			return nil, "", err
		}
		if !os.SameFile(info, opened) {
			child.Close()
			return nil, "", fmt.Errorf("%s changed while it was being opened: retry after ensuring it is a real directory", path)
		}
		return child, path, nil
	}
}

func isSingleLinkRegular(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return info.Mode().IsRegular() && ok && stat.Nlink == 1
}

// acquireWireLock serializes this tool's writers without ever deleting a
// repository-controlled lock pathname. The small persistent inode is harmless;
// keeping it avoids a check-then-remove cleanup race that could delete a
// replacement object.
func acquireWireLock(dir *os.Root, dirPath string) (*os.File, error) {
	const name = ".agents-wire.lock"
	path := filepath.Join(dirPath, name)
	for {
		info, err := dir.Lstat(name)
		if os.IsNotExist(err) {
			file, err := dir.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
			if os.IsExist(err) {
				continue
			}
			if err != nil {
				return nil, err
			}
			opened, err := file.Stat()
			if err != nil || !isSingleLinkRegular(opened) {
				_ = file.Close()
				return nil, fmt.Errorf("%s is not a safe wire lock", path)
			}
			if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
				_ = file.Close()
				return nil, fmt.Errorf("another wire operation owns %s: %w", path, err)
			}
			return file, nil
		}
		if err != nil {
			return nil, err
		}
		if !isSingleLinkRegular(info) {
			return nil, fmt.Errorf("%s is not a single-link regular lock file", path)
		}
		file, err := dir.OpenFile(name, os.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if err != nil {
			return nil, err
		}
		opened, err := file.Stat()
		if err != nil || !isSingleLinkRegular(opened) || !os.SameFile(info, opened) {
			_ = file.Close()
			return nil, fmt.Errorf("%s changed while it was being opened", path)
		}
		if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			_ = file.Close()
			return nil, fmt.Errorf("another wire operation owns %s: %w", path, err)
		}
		return file, nil
	}
}

func releaseWireLock(file *os.File) {
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	_ = file.Close()
}

// readHooksJSON reads only a verified, single-link regular config leaf. The
// nonblocking, no-follow open makes FIFOs and last-component symlinks errors
// instead of hangs or reads from another inode.
func readHooksJSON(dir *os.Root, dirPath, name string) (map[string]any, configSnapshot, error) {
	settings := map[string]any{}
	path := filepath.Join(dirPath, name)
	info, err := dir.Lstat(name)
	if os.IsNotExist(err) {
		return settings, configSnapshot{perm: 0o644}, nil
	}
	if err != nil {
		return nil, configSnapshot{}, err
	}
	if !isSingleLinkRegular(info) {
		return nil, configSnapshot{}, fmt.Errorf("%s is not a single-link regular file: refusing to read or replace it", path)
	}

	file, err := dir.OpenFile(name, os.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, configSnapshot{}, err
	}
	opened, err := file.Stat()
	if err != nil || !isSingleLinkRegular(opened) || !os.SameFile(info, opened) {
		_ = file.Close()
		return nil, configSnapshot{}, fmt.Errorf("%s changed while it was being opened: refusing to wire it", path)
	}
	b, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if readErr != nil {
		return nil, configSnapshot{}, readErr
	}
	if closeErr != nil {
		return nil, configSnapshot{}, closeErr
	}
	if err := json.Unmarshal(b, &settings); err != nil {
		return nil, configSnapshot{}, fmt.Errorf("%s is not valid JSON; fix or remove it: %w", path, err)
	}
	return settings, configSnapshot{exists: true, info: opened, perm: opened.Mode().Perm()}, nil
}

// renderHooksJSON merges our entries while preserving every unrelated key and
// foreign hook. Silently dropping an audit hook would be serious misbehaviour.
func renderHooksJSON(settings map[string]any, harnessName string, events []Event, binary string) ([]byte, error) {
	if settings == nil {
		return nil, fmt.Errorf("generated hook config root must be a JSON object, not null")
	}
	var hooks map[string]any
	if raw, present := settings["hooks"]; present {
		var ok bool
		hooks, ok = raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("generated hook config hooks must be a JSON object")
		}
	} else {
		hooks = map[string]any{}
	}

	for _, ev := range events {
		var groups []any
		if raw, present := hooks[ev.Vendor]; present {
			var ok bool
			groups, ok = raw.([]any)
			if !ok {
				return nil, fmt.Errorf("generated hook config event %s must be a JSON array", ev.Vendor)
			}
		}
		kept := stripOurs(groups)

		entry := map[string]any{
			"hooks": []any{map[string]any{
				"type":    "command",
				"command": HookCommand(binary, harnessName, ev.Semantic),
			}},
		}
		if ev.Matcher != "" {
			entry["matcher"] = ev.Matcher
		}
		hooks[ev.Vendor] = append(kept, entry)
	}
	settings["hooks"] = hooks
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// atomicWriteHooks publishes through the verified directory handle. The final
// rename replaces a racing leaf rather than following it; a pre-existing config
// must still be the exact verified inode and metadata observed while reading.
func atomicWriteHooks(dir *os.Root, dirPath, name string, data []byte, snapshot configSnapshot, validateSkills func() error) error {
	tmpName := ".agents-wire-tmp-" + rand.Text()
	file, err := dir.OpenFile(tmpName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
		_ = dir.Remove(tmpName)
	}()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Chmod(snapshot.perm); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}

	beforeWirePublish()
	current, err := dir.Lstat(name)
	if snapshot.exists {
		if err != nil || !isSingleLinkRegular(current) || !os.SameFile(snapshot.info, current) ||
			current.Mode() != snapshot.info.Mode() || current.Size() != snapshot.info.Size() ||
			!current.ModTime().Equal(snapshot.info.ModTime()) {
			return fmt.Errorf("%s changed while wiring: refusing to replace it", filepath.Join(dirPath, name))
		}
	} else if !os.IsNotExist(err) {
		if err != nil {
			return err
		}
		return fmt.Errorf("%s appeared while wiring: refusing to replace it", filepath.Join(dirPath, name))
	}
	if err := validateSkills(); err != nil {
		return err
	}
	if !snapshot.exists {
		// Link is an atomic create-if-absent publish. Rename would silently
		// overwrite a config that appeared after the absence check.
		return dir.Link(tmpName, name)
	}
	// A non-cooperating writer can still change an existing destination after
	// the final identity check. The per-harness lock closes that race between all
	// agents writers; portable os.Root has no compare-and-rename primitive for a
	// foreign process that ignores the lock.
	return dir.Rename(tmpName, name)
}

func preflightSkills(dir *os.Root, dirPath string) (skillsSnapshot, error) {
	const name = "skills"
	target := filepath.Join("..", ".agents", "skills")
	info, err := dir.Lstat(name)
	if os.IsNotExist(err) {
		return skillsSnapshot{}, nil
	}
	if err != nil {
		return skillsSnapshot{}, err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return skillsSnapshot{}, fmt.Errorf("%s exists and is not the managed skills symlink; move it aside before wiring", filepath.Join(dirPath, name))
	}
	got, err := dir.Readlink(name)
	if err != nil {
		return skillsSnapshot{}, err
	}
	if got != target {
		return skillsSnapshot{}, fmt.Errorf("%s points to %s, not the managed skills target; move it aside before wiring", filepath.Join(dirPath, name), got)
	}
	return skillsSnapshot{exists: true, info: info}, nil
}

func validateSkills(dir *os.Root, dirPath string, snapshot skillsSnapshot) error {
	const name = "skills"
	target := filepath.Join("..", ".agents", "skills")
	info, err := dir.Lstat(name)
	if err != nil || !snapshot.exists || info.Mode()&os.ModeSymlink == 0 || !os.SameFile(snapshot.info, info) {
		return fmt.Errorf("%s changed while wiring: refusing to publish hooks", filepath.Join(dirPath, name))
	}
	if got, err := dir.Readlink(name); err != nil || got != target {
		return fmt.Errorf("%s changed while wiring: refusing to publish hooks", filepath.Join(dirPath, name))
	}
	return nil
}

// wireRepository keeps every mutation below a verified harness directory and
// validates config and skills ownership before publishing either one.
func wireRepository(repoRoot, harnessDir, configName, harnessName string, events []Event, binary string) error {
	dir, dirPath, err := openHarnessDir(repoRoot, harnessDir)
	if err != nil {
		return err
	}
	defer dir.Close()
	lock, err := acquireWireLock(dir, dirPath)
	if err != nil {
		return err
	}
	defer releaseWireLock(lock)

	skills, err := preflightSkills(dir, dirPath)
	if err != nil {
		return err
	}
	settings, snapshot, err := readHooksJSON(dir, dirPath, configName)
	if err != nil {
		return err
	}
	out, err := renderHooksJSON(settings, harnessName, events, binary)
	if err != nil {
		return err
	}

	if !skills.exists {
		if err := dir.Symlink(filepath.Join("..", ".agents", "skills"), "skills"); err != nil {
			return err
		}
		skills, err = preflightSkills(dir, dirPath)
		if err != nil {
			return err
		}
	}
	if err := atomicWriteHooks(dir, dirPath, configName, out, snapshot, func() error {
		return validateSkills(dir, dirPath, skills)
	}); err != nil {
		return err
	}
	return nil
}

// stripOurs removes previously generated entries, at the level of individual
// hooks rather than whole groups, so a group that mixes a foreign hook with
// ours keeps the foreign one.
func stripOurs(groups []any) []any {
	var kept []any
	for _, g := range groups {
		gm, ok := g.(map[string]any)
		if !ok {
			kept = append(kept, g)
			continue
		}
		rawInner, present := gm["hooks"]
		inner, ok := rawInner.([]any)
		if !present || !ok {
			// An unknown foreign shape is not ours to normalize or discard.
			kept = append(kept, g)
			continue
		}
		var innerKept []any
		for _, h := range inner {
			hm, _ := h.(map[string]any)
			if cmd, _ := hm["command"].(string); IsOwnedHookCommand(cmd) {
				continue
			}
			innerKept = append(innerKept, h)
		}
		if len(innerKept) == 0 {
			continue
		}
		gm["hooks"] = innerKept
		kept = append(kept, gm)
	}
	return kept
}
