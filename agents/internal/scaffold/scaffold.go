// Package scaffold creates the tracked .agents/ layout in a repository and the
// two thin files that make a harness notice it.
package scaffold

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/nilbot/dotfiles/agents/internal/repo"
)

// ErrLocalInLinkedWorktree refuses --local where it would silently delete work.
//
// --local's only mechanism is an exclude entry, and a linked worktree shares
// info/exclude with its main checkout. Measured: already-tracked .agents/ files
// stay visible, but every new one is ignored everywhere -- `git check-ignore`
// matches it, `git status --untracked-files=all` omits it, `git add .` skips it.
// The handoffs, memory notes and traces this tool exists to preserve would be
// dropped without a message. A warning in scrollback is not a defence against
// that, so this is an error.
var ErrLocalInLinkedWorktree = errors.New("--local is not supported inside a linked worktree: info/exclude is shared with the main checkout, so excluding /.agents/ here would hide every new file under .agents/ from git in every worktree of this repo, including files already written. Run `agents init --local` in the main checkout instead, or drop --local here")

// DoctorInstruction is part of newly generated context only. Create never
// rewrites an existing instruction file, so restoring this marker is not a migration.
const DoctorInstruction = "Run `agents doctor` early and report any warnings before relying on this context."

// DefaultAgentsMD is the root instruction file for coding agent harnesses.
const DefaultAgentsMD = `# Agent context

Durable context for this repo lives in ` + "`docs/`" + `. Read it before assuming;
it is the record, and this file is only the pointer to it.

- ` + "`docs/qna/`" + ` — answers indexed by the question you would ask again
- ` + "`docs/journal/`" + ` — dated record of what happened
- ` + "`docs/design/`" + ` — the design still in force

## Repository Architecture & Guidelines
- Domain engineering guidelines, commenting standards, and safety constraints 
  are defined in ` + "`.agents/AGENTS.md`" + `.
- Repo-specific procedures and skills are located in ` + "`.agents/skills/`" + `.

## Machine Wiring
` + "`.agents/`" + ` holds machine wiring and local skills. A hook cannot install itself 
and a missing hook fails silently, so an empty or stale ` + "`.agents/`" + ` means the setup 
is broken rather than that there is nothing to say — report it rather than 
working around it.

` + DoctorInstruction + `

Recording is covered by the global instruction and the ` + "`recording-what-you-learn`" + ` 
skill; it is not repo-specific and is not restated here.
`

// gitattributesLines are tracked on purpose: they are a statement about how this
// repository merges and renders, which belongs to the repository.
//
// merge=union used to be here and is gone with the thing it protected. It
// existed because two branches appending to the same tracked daily trace file
// produced conflict markers that are not valid JSON, which a line-oriented
// reader silently drops. The index is machine-local now, so nothing tracked
// appends concurrently and the attribute has nothing to defend.
var gitattributesLines = []string{
	".agents/** linguist-generated=true",
}

// excludeLines are machine-specific generated paths. They go in
// .git/info/exclude rather than the repo's tracked .gitignore: an ignore list
// belongs to the repository's maintainers, not to this tool.
var excludeLines = []string{
	"/.claude/settings.json",
	"/.claude/.agents-wire.lock",
	"/.claude/skills",
	"/.codex/hooks.json",
	"/.codex/.agents-wire.lock",
	"/.codex/skills",
	"/.agents/hooks.json",
	"/.agents/.agents-wire.lock",
}

// dirs is what .agents/ is for after the 2026-08-19 redesign: machine wiring
// and repo-specific procedure, nothing else.
//
// memory/ and reports/handoff/ held knowledge and were retired with the
// apparatus that filled them. reports/{specs,plans,analysis} went with them for
// the same reason -- they duplicated a docs/ directory every repository already
// has, which is the tier error the redesign exists to correct.
var dirs = []string{
	"skills",
}

// Create is idempotent. Running it on an initialized repo must change nothing.
func Create(root string, local bool) error {
	// First, before anything is written: a refusal that leaves a half-scaffolded
	// repo behind is a worse outcome than the one it is refusing.
	if local {
		linked, err := repo.IsLinkedWorktree(root)
		if err != nil {
			return err
		}
		if linked {
			return ErrLocalInLinkedWorktree
		}
	}

	agents := filepath.Join(root, ".agents")
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(agents, d), 0o755); err != nil {
			return err
		}
	}
	// git does not track empty directories, and an .agents/ that vanishes on
	// clone is worse than one with placeholder files.
	for _, d := range dirs {
		keep := filepath.Join(agents, d, ".gitkeep")
		if _, err := os.Stat(keep); os.IsNotExist(err) {
			if err := os.WriteFile(keep, nil, 0o644); err != nil {
				return err
			}
		}
	}

	if err := writeIfAbsent(filepath.Join(root, "AGENTS.md"), DefaultAgentsMD); err != nil {
		return err
	}
	if err := linkIfAbsent(filepath.Join(root, "CLAUDE.md"), "AGENTS.md"); err != nil {
		return err
	}

	if err := appendMissingLines(filepath.Join(root, ".gitattributes"), gitattributesLines); err != nil {
		return err
	}

	lines := excludeLines
	if local {
		// --local: the whole directory stays out of the repo, for repos where
		// committing agent artifacts is not acceptable. Same layout either way.
		lines = append(append([]string{}, excludeLines...), "/.agents/")
	}
	// Ask git where the exclude file is rather than assuming <root>/.git/info:
	// in a linked worktree .git is a regular file and that path cannot exist.
	exclude, err := repo.InfoExcludePath(root)
	if err != nil {
		return err
	}
	return appendMissingLines(exclude, lines)
}

func writeIfAbsent(path, content string) error {
	if _, err := os.Lstat(path); err == nil {
		return nil
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func linkIfAbsent(path, target string) error {
	if _, err := os.Lstat(path); err == nil {
		return nil
	}
	return os.Symlink(target, path)
}

// appendMissingLines adds only the lines that are not already present, so
// running init twice does not duplicate anything and a hand-edited file keeps
// its edits.
func appendMissingLines(path string, want []string) error {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	have := map[string]bool{}
	for _, l := range strings.Split(string(existing), "\n") {
		have[strings.TrimSpace(l)] = true
	}

	var add []string
	for _, l := range want {
		if !have[l] {
			add = append(add, l)
		}
	}
	if len(add) == 0 {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	var b strings.Builder
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		b.WriteString("\n")
	}
	for _, l := range add {
		b.WriteString(l + "\n")
	}
	_, err = f.WriteString(b.String())
	return err
}
