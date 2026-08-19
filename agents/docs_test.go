package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const (
	beginMarker = "<!-- BEGIN GENERATED: agents help --render=markdown -->"
	endMarker   = "<!-- END GENERATED -->"
)

// readmeBlock splits README.md around the generated command table and returns
// the three parts. Task 10 reads the surrounding prose through it.
//
// The checkout is located with task18RepoRoot, not by resolving ".." here:
// TestMain chdirs out of the checkout before any test runs, so cwd is not the
// repository, and packageDir's own comment says to go through that helper
// rather than each caller doing its own arithmetic on the path. Eight call
// sites already do.
func readmeBlock(t *testing.T) (before, block, after string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(task18RepoRoot(t), "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	i := strings.Index(text, beginMarker)
	j := strings.Index(text, endMarker)
	if i < 0 || j < 0 || j < i {
		t.Fatalf("README.md is missing the generated-block markers")
	}
	return text[:i+len(beginMarker)], text[i+len(beginMarker) : j], text[j:]
}

// The README block is derived. If it drifts, the fix is to regenerate it, not
// to edit it -- which is the whole reason it is generated.
//
// This is what stops the README from describing a command set the binary does
// not have. The previous arrangement had no command reference at all, which is
// the only reason it had never gone stale.
func TestReadmeCommandBlockIsCurrent(t *testing.T) {
	_, block, _ := readmeBlock(t)
	var want bytes.Buffer
	RenderMarkdown(rootCommand(), &want)
	if strings.TrimSpace(block) != strings.TrimSpace(want.String()) {
		t.Errorf("README command block is stale. Regenerate it:\n"+
			"  agents help --render=markdown\n\ngot:\n%s\nwant:\n%s", block, want.String())
	}
}

// commandSpan matches an inline code span naming an agents command, and only
// that. Prose mentions of the word "agents" are not command references, and
// flagging them would produce findings nobody would act on. The character
// class stops at the first `<`, `=` or `|`, so a span carrying an argument
// placeholder -- `agents handoff draft --lane <lane>` -- is skipped rather
// than half-matched.
var commandSpan = regexp.MustCompile("`agents ([a-z][a-z -]*)`")

// livingDocuments are the files that describe how to use this repository
// today, as opposed to what was true when they were written.
//
// Plans and specs are deliberately excluded. They are dated records: the
// executed bootstrap plan legitimately names `make githooks`, a target that no
// longer exists, and a record silently rewritten to stay true is not a record.
func livingDocuments(t *testing.T, root string) []string {
	t.Helper()
	targets := []string{"README.md", "CLAUDE.md", filepath.Join("claude", "CLAUDE.md")}
	for _, dir := range []string{filepath.Join("claude", "skills"), filepath.Join(".agents", "skills")} {
		_ = filepath.WalkDir(filepath.Join(root, dir), func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(p, ".md") {
				rel, relErr := filepath.Rel(root, p)
				if relErr == nil {
					targets = append(targets, rel)
				}
			}
			return nil
		})
	}
	return targets
}

// No living document may name an `agents` subcommand the registry does not
// define.
//
// This is the direction the README block cannot cover. Generating the table
// keeps the reference complete; it does nothing about a skill or a CLAUDE.md
// that tells its reader to run a command which was renamed two specs ago. The
// reader in that case is usually an agent, and it will run what it is told.
func TestLivingDocumentsNameOnlyRealCommands(t *testing.T) {
	root := task18RepoRoot(t)

	known := map[string]bool{}
	rootCommand().Walk(func(path []string, _ *Command) { known[strings.Join(path, " ")] = true })
	if !known["trace cache prune"] {
		t.Fatal("the tree walk found no three-level command; this check would prove little")
	}

	spans := 0
	for _, rel := range livingDocuments(t, root) {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue // an optional document that does not exist yet
		}
		for _, m := range commandSpan.FindAllStringSubmatch(string(data), -1) {
			spans++
			// Longest match wins: `agents trace cache prune` is a command, and
			// so is the `agents trace` inside it. Checking the longest form
			// first means a real leaf is never reported because its parent
			// happened to match a shorter prefix.
			words := strings.Fields(m[1])
			matched := false
			for n := len(words); n > 0 && !matched; n-- {
				matched = known[strings.Join(words[:n], " ")]
			}
			if !matched {
				t.Errorf("%s names `agents %s`, which the registry does not define", rel, m[1])
			}
		}
	}
	// A pattern that matched nothing would pass every document silently.
	if spans == 0 {
		t.Fatal("no `agents ...` code spans found in any living document; the scan is broken")
	}
}

// A command an agent may invoke must appear in the fleet-wide guidance, or a
// harness never learns to reach for it.
//
// Spec 7 measured this exact shape: the instruction said HOW to write a handoff
// and never THAT one should, and twenty sessions produced none. A generated
// reference answers "what is this command"; only the skill answers "which
// command is this situation", and that is judgment, so it cannot be generated
// -- which is precisely why it needs a check that it stayed complete.
func TestHarnessSkillCoversAgentCommands(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(task18RepoRoot(t),
		"claude", "skills", "agents-tool", "SKILL.md"))
	if err != nil {
		t.Fatalf("the fleet-wide agents skill is missing: %v", err)
	}
	text := string(data)

	var missing []string
	agentFacing := 0
	rootCommand().Walk(func(path []string, c *Command) {
		for _, a := range c.Audience {
			if a != Agent {
				continue
			}
			agentFacing++
			if !strings.Contains(text, "`agents "+strings.Join(path, " ")+"`") {
				missing = append(missing, strings.Join(path, " "))
			}
			return
		}
	})
	if agentFacing == 0 {
		t.Fatal("no agent-facing commands found; this check would prove nothing")
	}
	if len(missing) > 0 {
		t.Errorf("agent-facing commands absent from the skill:\n  %s", strings.Join(missing, "\n  "))
	}
}
