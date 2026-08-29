# Why is root `AGENTS.md` a regular file and `CLAUDE.md` a symlink?

## Context

2026-08-29, adopting the Two-Tier Agent Context architecture across `dotfiles` and the `agents` fleet.

Historically, `dotfiles` had `CLAUDE.md` as a regular file and `AGENTS.md` as a symlink pointing to it (`AGENTS.md -> CLAUDE.md`). When aligning with `agents/internal/scaffold/scaffold.go`, the question arose whether the symlink direction matters, or if `CLAUDE.md` could remain the regular file.

## Answer

`AGENTS.md` must be the authoritative regular file at repository root, with `CLAUDE.md` symlinking to it (`CLAUDE.md -> AGENTS.md`), for three mechanical reasons:

### 1. `safeio.ReadRegular` uses `O_NOFOLLOW`

In `agents/internal/safeio/safeio.go`, file inspection uses `syscall.O_NOFOLLOW`:

```go
fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
```

`safeio.ReadRegular` refuses leaf symlinks to guarantee observational security and prevent symlink-following race conditions. When `agents doctor` evaluates candidate instruction files in `doctor.go`:

```go
candidates := []string{
    filepath.Join(repoRoot, "AGENTS.md"),
    filepath.Join(repoRoot, "CLAUDE.md"),
    filepath.Join(repoRoot, ".agents", "AGENTS.md"),
}
```

If `AGENTS.md` is a symlink, `safeio.ReadRegular(AGENTS.md)` returns `syscall.ELOOP / os.PathError` and is skipped. If `AGENTS.md` is the regular file, `checkScaffoldInstruction` verifies it on the first candidate check without symlink indirection.

### 2. Multi-Harness Universal Neutrality

`AGENTS.md` is the universal, multi-harness convention recognized across Antigravity, Codex, Cursor, and modern AI coding tools. `CLAUDE.md` is Anthropic-specific.

Establishing `AGENTS.md` as the canonical file makes the repository harness-agnostic. `CLAUDE.md -> AGENTS.md` provides 100% backwards compatibility for Claude Code without making Anthropic-specific filenames authoritative over other agents.

### 3. Canonical Scaffold Topology

`scaffold.Create` in `agents/internal/scaffold/scaffold.go` creates:

```go
if err := writeIfAbsent(filepath.Join(root, "AGENTS.md"), DefaultAgentsMD); err != nil {
    return err
}
if err := linkIfAbsent(filepath.Join(root, "CLAUDE.md"), "AGENTS.md"); err != nil {
    return err
}
```

If a repository has `AGENTS.md -> CLAUDE.md`, `scaffold.Create` treats `AGENTS.md` as present (skipping write) and `linkIfAbsent(CLAUDE.md, "AGENTS.md")` fails because `CLAUDE.md` exists. Having `AGENTS.md` as regular file matches canonical scaffold generation across all newly initialized repositories.
