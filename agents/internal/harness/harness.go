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
	HookEventName       string `json:"hook_event_name"`
	SessionID           string `json:"session_id"`
	TurnID              string `json:"turn_id"`
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

// Build assembles the harness-determined part of a record.
func Build(a Adapter, semantic string, p Payload) Trace {
	tr := Trace{
		Event:     semantic,
		SessionID: p.SessionID,
		TurnID:    p.TurnID,
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
