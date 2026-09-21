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
	// global/AGENTS.md is the machine-global instruction file: one source,
	// symlinked to every harness's path. It replaced claude/CLAUDE.md on
	// 2026-09-21, and this list has to follow it -- the entry is what puts a file
	// in front of the "names only real commands" check, and a path that no
	// longer exists is skipped in silence.
	targets := []string{
		"README.md",
		filepath.Join("agents", "README.md"),
		"CLAUDE.md",
		filepath.Join("global", "AGENTS.md"),
	}
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
	if len(known) < 5 {
		t.Fatalf("the tree walk found only %d commands; this check would prove little", len(known))
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
// TestInstalledSkillMatchesTheBinary guards the one skill this tool installs.
//
// The skill is written once and then belongs to the repository, so `doctor`
// reports a divergence as OK rather than as a fault. What must not drift is the
// text the binary *installs*: a repository initialized by this build has to get
// the bytes this build carries, or `doctor` on a freshly initialized repository
// would immediately report customizations nobody made.
func TestInstalledSkillMatchesTheBinary(t *testing.T) {
	root := newRepo(t)
	if err := scaffold.Create(root, false); err != nil {
		t.Fatal(err)
	}
	const name = "recording-what-you-learn"
	current, found, err := scaffold.SkillCurrent(root, name)
	if err != nil {
		t.Fatal(err)
	}
	if found == "" {
		t.Fatalf("init did not write .agents/skills/%s/", name)
	}
	if !current {
		t.Errorf("the installed skill is not the text this binary carries (digest %s)", found)
	}
}

// TestInstalledSkillNamesNoDeletedCommand is the regression for a shipped
// blocker: the text `init` installed told an agent to run `agents layout path`
// and to read `.agents/layout.json`, and by the time it shipped, neither
// existed. Every freshly initialized repository got it.
//
// What let that through was deleting the two tests that guarded it --
// TestRecordingSkillNamesRolesNotDocsPaths and the frozen-v1 asset tests --
// because they were written against the layout schemas that went away. The
// property they protected did not go away with them, so it is restated here in
// the terms that still apply: the text this binary installs names only commands
// this binary has, and only paths this repository actually creates.
func TestInstalledSkillNamesNoDeletedCommand(t *testing.T) {
	root := newRepo(t)
	if err := scaffold.Create(root, false); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".agents", "skills", "recording-what-you-learn", "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("init did not install the skill: %v", err)
	}
	text := string(data)

	known := map[string]bool{}
	rootCommand().Walk(func(p []string, _ *Command) { known[strings.Join(p, " ")] = true })

	// Every `agents <subcommand>` span in the skill must resolve against the
	// command tree. This is the check the deleted tests performed by hand for
	// one text; doing it from the tree means it cannot go stale when a command
	// is removed.
	for _, span := range agentSpanPattern.FindAllStringSubmatch(text, -1) {
		if !known[span[1]] {
			t.Errorf("the installed skill names `agents %s`, which is not a command in this tree", span[1])
		}
	}
	// And it must not name a manifest this tool no longer writes.
	if strings.Contains(text, "layout.json") {
		t.Error("the installed skill tells the reader to consult .agents/layout.json, which this tool no longer writes")
	}
}

// agentSpanPattern captures the command path in a backticked `agents ...` span,
// stopping at a flag, a quote, or a closing backtick.
var agentSpanPattern = regexp.MustCompile("`agents ([a-z][a-z0-9 -]*[a-z0-9])")

// A living document may not name a command the tool no longer has, unless it
// says the command is gone.
//
// This is the gap that let a green gate sit on top of stale documentation. The
// scan above covers the READMEs, the instruction files and the skill trees, and
// the design and Q&A stores were outside it -- so the stores an agent is told to
// read before asserting, and the store the repository calls the design still in
// force, could describe commands that no longer exist with nothing to notice.
//
// The exemption is a marker on the same line, because a document has a
// legitimate reason to name a deleted command: to record that it was deleted.
// The markers are deliberately few and literal. A document that wants to discuss
// a removal writes "removed" or "deleted" or "no longer" beside it, which is
// also what makes the sentence useful to a reader -- a bare `agents layout` in a
// living document tells them to run something that will fail.
func TestLivingDocumentsNameNoDeletedCommand(t *testing.T) {
	root := task18RepoRoot(t)

	// Commands this tool had and no longer has. Each is a whole name: matching
	// on `layout` as free text would flag every discussion of layout as a
	// concept, which several documents legitimately contain.
	deleted := []string{
		"agents drift", "agents layout", "agents trace", "agents save",
		"agents ls", "agents update", "agents hook",
	}
	// A line carrying one of these says the command is gone, which is the one
	// thing a living document may say about it.
	historical := []string{"removed", "deleted", "no longer", "retired", "gone", "archived"}

	// A document may declare itself a record rather than live design, and then
	// its references are history. The marker is deliberately explicit and has to
	// appear near the top: an implicit claim ("this is a spec, it must be
	// historical") is how a store of eighty documents ends up describing a tool
	// that does not exist. A document that carries this is announcing that it no
	// longer describes the present, which is also the thing a reader needs told.
	const recordMarker = "SUPERSEDED:"

	// A document that carries a dated follow-up has declared that part of it is
	// history, and its original body may keep the names it was written with.
	//
	// This is the Q&A store's own contract rather than an exemption invented to
	// make the check pass. An entry is a dated answer; the way this repository
	// records that an answer stopped being true is to leave the answer alone and
	// append what changed. Demanding a marker on the same physical line as the
	// original reference fought that convention: it forced the correction to be
	// woven through the original prose, which erases the boundary between what
	// was believed and what replaced it -- the one thing the store exists to
	// preserve. Line-local markers were also demonstrably fragile: a follow-up
	// that said "neither `agents update` nor `agents drift` exists any more" was
	// itself flagged, because "any more" is not on the marker list.
	//
	// A file with no follow-up gets no exemption, so a document that simply went
	// stale is still caught. The check's teeth are in the requirement to have
	// recorded the change, not in where the words sit.
	const followUpMarker = "## Follow-up, "

	flagged := 0
	for _, rel := range append(livingDocuments(t, root), docsStores(t, root)...) {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		body := string(data)
		if declaresItselfARecord(body, recordMarker) {
			continue
		}
		hasFollowUp := strings.Contains(body, followUpMarker)
		for n, line := range strings.Split(string(data), "\n") {
			lower := strings.ToLower(line)
			// Only an inline code span is a claim about a command: prose that
			// says "layout" is talking about a concept, and several documents
			// legitimately do.
			// Longest match wins, so `agents layout show` is reported once as
			// itself rather than also as the `agents layout` inside it.
			var worst string
			for _, m := range commandSpan.FindAllStringSubmatch(line, -1) {
				if isDeletedCommand(m[1], deleted) && len(m[1]) > len(worst) {
					worst = m[1]
				}
			}
			if worst == "" {
				continue
			}
			saysGone := hasFollowUp
			for _, marker := range historical {
				if strings.Contains(lower, marker) {
					saysGone = true
					break
				}
			}
			if !saysGone {
				flagged++
				t.Errorf("%s:%d names `agents %s` without saying it is gone:\n  %s", rel, n+1, worst, strings.TrimSpace(line))
			}
		}
	}
	if flagged == 0 {
		t.Logf("no living document names a deleted command outside a removal note")
	}
}

// declaresItselfARecord reports whether a document says, near its top, that it
// is a record rather than a description of the present.
//
// The window is the first 25 lines because a reader has to meet the marker
// before the content that depends on it; a marker buried at the end is a
// disclaimer nobody sees in time.
func declaresItselfARecord(body, marker string) bool {
	lines := strings.Split(body, "\n")
	if len(lines) > 25 {
		lines = lines[:25]
	}
	return strings.Contains(strings.Join(lines, "\n"), marker)
}

// isDeletedCommand reports whether a span names one of the retired commands.
// The comparison is on whole words, so `agents layout` matches the deleted
// `agents layout` entry and not a surviving command that merely starts with it.
func isDeletedCommand(span string, deleted []string) bool {
	span = strings.TrimSpace(span)
	for _, d := range deleted {
		name := strings.TrimPrefix(d, "agents ")
		if span == name || strings.HasPrefix(span, name+" ") {
			return true
		}
	}
	return false
}

// docsStores are the two stores an agent is told to read: the Q&A answers and
// the design still in force.
func docsStores(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	for _, dir := range []string{"docs/qna", "docs/design"} {
		_ = filepath.WalkDir(filepath.Join(root, dir), func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(p, ".md") {
				if rel, relErr := filepath.Rel(root, p); relErr == nil {
					out = append(out, rel)
				}
			}
			return nil
		})
	}
	return out
}
