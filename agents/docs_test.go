package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/nilbot/dotfiles/agents/internal/scaffold"
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
	targets := []string{"README.md", filepath.Join("agents", "README.md"), "CLAUDE.md", filepath.Join("claude", "CLAUDE.md")}
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

// The migrating-fleet-context skill is the one deliverable of the two-tier work
// that is prose rather than code, and it shipped contradicting the tooling it
// drives: no fleet mode, one procedure for four router states, an unconditional
// `rm -f CLAUDE.md`, and a commit with no approval gate.
//
// TestLivingDocumentsNameOnlyRealCommands already scans this file, but only in
// one direction -- it catches a command that does not exist, never a required
// command that is absent. That asymmetry is exactly why the omissions survived
// review. This is the other direction, the way TestHarnessSkillCoversAgentCommands
// is the other direction for claude/skills/agents-tool.
//
// See docs/journal/2026-09-01-why-the-migration-skill-shipped-hollow.md and
// Amendment 1 of docs/design/2026-08-29-two-tier-context-and-llm-migration-architecture.md.
func TestMigrationSkillCoversItsSpecifiedProtocol(t *testing.T) {
	root := task18RepoRoot(t)
	rel := filepath.Join(".agents", "skills", "migrating-fleet-context", "SKILL.md")
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("the migration skill is missing: %v", err)
	}
	text := string(data)

	// Required by the amended design section 7. Each entry names the section
	// that requires it, so a future edit that drops one can find out why.
	required := []struct{ substr, why string }{
		{"agents ls", "7.1.2 target discovery over the registered fleet"},
		{"agents drift --json", "7.1.2 single-repository inspection"},
		{"agents drift --all --json", "7.6 fleet inspection, which returns an array"},
		{"agents update --apply", "7.1.1 self-currency check before trusting itself"},
		{"agents doctor", "7.1.7 verification gate"},
		{"clean_current", "7.3 router state table"},
		{"clean_legacy", "7.3 router state table"},
		{"drifted", "7.3 router state table"},
		{"missing", "7.3 router state table"},
		{"upstream", "7.3 named merge sources"},
		{"base", "7.3 named merge sources"},
		{"local", "7.3 named merge sources"},
		{"stop and ask", "7.5 unclassifiable blocks are not a judgement call"},
		{"traceability", "7.5 evidence for zero rule dropping"},
		{"gh pr create", "7.1.9 the migration ends in a pull request"},
	}
	lower := strings.ToLower(text)
	for _, r := range required {
		if !strings.Contains(lower, strings.ToLower(r.substr)) {
			t.Errorf("%s does not mention %q, required by %s", rel, r.substr, r.why)
		}
	}

	// Forbidden: the unconditional symlink replacement. On the pre-2026-08-19
	// topology (AGENTS.md -> CLAUDE.md, content in CLAUDE.md) this deletes the
	// only real file and leaves AGENTS.md -> CLAUDE.md -> AGENTS.md, a symlink
	// loop with every line of repository context gone. playground/desktop_pet
	// was in exactly that state on 2026-09-01.
	if strings.Contains(text, "rm -f CLAUDE.md") {
		t.Errorf("%s still carries the unconditional `rm -f CLAUDE.md`; design 7.4 requires "+
			"stat-ing both root paths and preserving content before the symlink", rel)
	}

	// Forbidden: a command that relocates out of the immutable archive. Prose
	// forbidding the move is fine and expected; a `git mv` with an archive
	// source is not.
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "git mv") && strings.Contains(line, "docs/archive/") {
			t.Errorf("%s relocates out of docs/archive/, which is immutable: %q", rel, strings.TrimSpace(line))
		}
	}
}

// The skill exists twice: the repository's own copy and the embedded asset the
// binary scaffolds into every other repository. Nothing bound them together,
// so they could diverge silently and the fleet would be migrated by whichever
// copy the reader happened to open.
func TestMigrationSkillMatchesEmbeddedAsset(t *testing.T) {
	root := task18RepoRoot(t)
	repoCopy, err := os.ReadFile(filepath.Join(root, ".agents", "skills", "migrating-fleet-context", "SKILL.md"))
	if err != nil {
		t.Fatalf("repository copy: %v", err)
	}
	asset, err := os.ReadFile(filepath.Join(root, "agents", "internal", "scaffold",
		"assets", "skills", "migrating-fleet-context", "SKILL.md"))
	if err != nil {
		t.Fatalf("embedded asset: %v", err)
	}
	if !bytes.Equal(repoCopy, asset) {
		t.Errorf(".agents/skills/migrating-fleet-context/SKILL.md and its embedded asset differ; "+
			"they are scaffolded into every other repository from the asset, so they must be identical "+
			"(repo copy %d bytes, asset %d bytes)", len(repoCopy), len(asset))
	}
}

// The skill pastes the canonical router so a migrating agent can restore it
// without a second tool. That is a second copy of DefaultAgentsMD, and the two
// ship in the same binary -- so nothing except this test stops a change to one
// from silently leaving the other behind, telling every migrated repository to
// adopt a router the tool then reports as drifted.
func TestMigrationSkillPastesTheCanonicalRouter(t *testing.T) {
	root := task18RepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, ".agents", "skills",
		"migrating-fleet-context", "SKILL.md"))
	if err != nil {
		t.Fatalf("the migration skill is missing: %v", err)
	}

	const fence = "```markdown\n# Agent context\n"
	i := strings.Index(string(data), fence)
	if i < 0 {
		t.Fatal("the skill no longer pastes a canonical router block; if that is deliberate, " +
			"delete this test, and if it is not, restore the block")
	}
	body := string(data)[i+len("```markdown\n"):]
	j := strings.Index(body, "\n```")
	if j < 0 {
		t.Fatal("unterminated router code fence in the skill")
	}
	pasted := body[:j+1]

	if pasted != scaffold.DefaultAgentsMD {
		t.Errorf("the router pasted into the skill does not match scaffold.DefaultAgentsMD\n"+
			"pasted %d bytes, canonical %d bytes", len(pasted), len(scaffold.DefaultAgentsMD))
	}
}
