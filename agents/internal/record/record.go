// Package record defines the one thing this tool writes into a tracked
// repository: a pointer to where a transcript lives, and enough provenance to
// know whether that pointer means anything from here.
//
// It never writes what the transcript says.
package record

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ForbiddenFields names the payload fields that must never reach a tracked
// file. All three routinely quote command output, which routinely contains
// credentials -- the captured Codex fixtures carry encrypted task blobs in
// tool_input.message. The guarantee is structural: Record has no field able to
// hold them, and the payload decoder (package harness) names only the fields it
// wants, so everything else is discarded at the JSON boundary.
var ForbiddenFields = []string{
	"last_assistant_message",
	"tool_input",
	"tool_response",
}

// Record is one line of <store>/traces/YYYY-MM-DD.jsonl, where <store> is
// repo.StoreDir. Machine-local: see Writer for why it is not tracked.
//
// Adding a field here is a decision about what this repository publishes.
// Before adding one, check it against ForbiddenFields and against spec 3.2.
type Record struct {
	When    time.Time `json:"when"`
	Harness string    `json:"harness"`
	Machine string    `json:"machine"`
	Event   string    `json:"event"`

	// Lane and Cwd exist to make retrieval mechanical rather than semantic.
	// Cwd is repo-relative and identifies the module in a multi-module repo.
	Lane string `json:"lane"`
	Cwd  string `json:"cwd"`

	SessionID string `json:"session_id"`
	TurnID    string `json:"turn_id,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
	AgentType string `json:"agent_type,omitempty"`

	// Description is empty for harnesses that do not supply one. Codex is such
	// a harness; the adapter declares the gap rather than the format pretending
	// both harnesses are equal.
	Description string `json:"description"`

	Transcript      string `json:"transcript"`
	PointerVerified bool   `json:"pointer_verified"`
}

// Line renders the record as exactly one JSONL line, newline included.
func (r Record) Line() ([]byte, error) {
	type alias Record // avoid recursing through a custom marshaller later
	out := struct {
		When string `json:"when"`
		alias
	}{
		When:  r.When.UTC().Format(time.RFC3339),
		alias: alias(r),
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Writer appends records under the machine-local store, resolved by
// repo.StoreDir.
//
// Not under .agents/. A record names a transcript that only one machine can
// open, so tracking it publishes a pointer with no reader: measured on this
// repository, 48% of tracked records were unreachable on the machine that
// wrote them. The index stays as local forensics, which is a job it can do.
type Writer struct{ storeDir string }

func NewWriter(storeDir string) *Writer { return &Writer{storeDir: storeDir} }

// Append adds one record to the UTC-dated file for its timestamp.
//
// Partitioning is by UTC so that records written from two timezones land in
// the same file. That mattered when the files were tracked and merged with
// merge=union; it survives because a reader still wants one file per day.
func (w *Writer) Append(r Record) error {
	dir := filepath.Join(w.storeDir, "traces")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	line, err := r.Line()
	if err != nil {
		return err
	}

	path := filepath.Join(dir, r.When.UTC().Format("2006-01-02")+".jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	// One write syscall per record. Concurrent agents appending to the same
	// file rely on O_APPEND atomicity, which holds for writes of this size.
	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("append record: %w", err)
	}
	return nil
}
