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
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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

// writeHooksJSON merges our hook entries into a config file both harnesses
// share the schema of, preserving every key and every foreign hook.
//
// Merging rather than owning matters: this file also carries settings that have
// nothing to do with us (permissions, effort level, a project's own hooks), and
// silently dropping someone's audit hook would be a serious misbehaviour.
func writeHooksJSON(path, harnessName string, events []Event, binary string) error {
	settings := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &settings); err != nil {
			return fmt.Errorf("%s is not valid JSON; fix or remove it: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}

	for _, ev := range events {
		groups, _ := hooks[ev.Vendor].([]any)
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

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o644)
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
		inner, _ := gm["hooks"].([]any)
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

// linkSkills points a harness's skills directory at the tracked one. It never
// replaces a real directory -- only a stale symlink -- because a real directory
// there is somebody's content.
func linkSkills(link string) error {
	target := filepath.Join("..", ".agents", "skills")
	if fi, err := os.Lstat(link); err == nil {
		if fi.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("%s exists and is not a symlink; move it aside to wire skills", link)
		}
		if err := os.Remove(link); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return err
	}
	return os.Symlink(target, link)
}
