#!/usr/bin/env bash
#
# Acceptance test for the `agents` simplification, run against the real
# repository and the real binary. It is the check CI cannot fully make: the git
# hook chain and the guard run only on a machine with git configured, and the
# point of this change is partly that the chain still works after the tool
# stopped installing hook entries.
#
# It changes nothing outside a temporary HOME unless --install is passed; see
# the USE OF --install note below.
#
# Usage: docs/../script/accept-agents-simplification.sh [--install]

set -euo pipefail

REPO_ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
INSTALL=0
[ "${1-}" = "--install" ] && INSTALL=1

pass=0
fail=0
step() { printf '\n=== %s\n' "$1"; }
ok()   { printf '  PASS  %s\n' "$1"; pass=$((pass + 1)); }
bad()  { printf '  FAIL  %s\n' "$1"; fail=$((fail + 1)); }
check() { if [ "$2" = "$3" ]; then ok "$1"; else bad "$1 (got '$2', want '$3')"; fi; }

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

step "1. the module builds, formats and vets clean"
( cd "$REPO_ROOT/agents" && gofmt -l . ) | grep -q . && bad "gofmt reported files" || ok "gofmt clean"
( cd "$REPO_ROOT/agents" && go vet ./... ) && ok "go vet clean" || bad "go vet failed"

step "2. the full suites pass"
( cd "$REPO_ROOT/agents" && go test -count=1 ./... >/dev/null ) && ok "agents suite" || bad "agents suite"
( cd "$REPO_ROOT/bootstrap.d" && go test -count=1 ./... >/dev/null ) && ok "bootstrap.d suite" || bad "bootstrap.d suite"

step "3. the binary is built from this checkout"
go build -C "$REPO_ROOT/agents" -o "$WORK/agents" . && ok "built" || bad "build failed"

step "4. the command surface is the reduced one"
listing=$("$WORK/agents" help 2>&1)
for want in init wire doctor version help; do
  echo "$listing" | grep -q "agents $want" && ok "help lists $want" || bad "help omits $want"
done
# guard is audience Git/CI: it is deliberately absent from the listing a person
# reads and present in --all, which is the same split the previous tool had.
echo "$listing" | grep -q "agents guard" && bad "the default listing shows an automated command" || ok "the default listing hides guard"
alllisting=$("$WORK/agents" help --all 2>&1)
echo "$alllisting" | grep -q "agents guard" && ok "--all shows guard" || { bad "--all omits guard"; echo "$alllisting" | head -8; }
for gone in trace save drift layout ls update hook; do
  if echo "$listing" | grep -qE "agents (trace|save|drift|layout|ls|update|hook)\b"; then
    bad "help still lists $gone"
  else
    ok "help does not list $gone"
  fi
done

step "5. a fresh repository reaches a working state"
FRESH="$WORK/fresh"
mkdir -p "$FRESH" && git -C "$FRESH" init -q .
# `init` exits 1 by design -- wiring is written but a human still has to trust
# the directory -- so the code has to be captured without `set -e` ending the run
# on it.
code=0
( cd "$FRESH" && "$WORK/agents" init >/dev/null 2>&1 ) || code=$?
check "init exits advisory (1)" "$code" "1"
for path in .agents/skills/recording-what-you-learn/SKILL.md AGENTS.md CLAUDE.md .agents/AGENTS.md docs/qna/README.md .claude/skills; do
  [ -e "$FRESH/$path" ] && ok "created $path" || bad "missing $path"
done
# The skill that ships must not name a command this binary does not have.
if grep -qE 'agents (layout|trace|save|drift|update|ls|hook)\b' "$FRESH/.agents/skills/recording-what-you-learn/SKILL.md"; then
  bad "the installed skill names a deleted command"
else
  ok "the installed skill names no deleted command"
fi

step "6. doctor is clean on that repository, and exits 0"
if ( cd "$FRESH" && PATH="$WORK:$PATH" "$WORK/agents" doctor >"$WORK/doctor.out" 2>&1 ); then
  ok "doctor exits 0"
else
  bad "doctor did not exit 0"; cat "$WORK/doctor.out"
fi
grep -q "FAIL" "$WORK/doctor.out" && bad "doctor reported a failure" || ok "no check failed"

step "7. wire removes a retired entry and leaves a foreign one"
CONF="$FRESH/.claude/settings.json"
mkdir -p "$(dirname "$CONF")"
cat > "$CONF" <<'JSON'
{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/opt/homebrew/bin/agents hook stop --harness claude-code"}]}],"Notification":[{"hooks":[{"type":"command","command":"/my/own/notify.sh"}]}]}}
JSON
( cd "$FRESH" && "$WORK/agents" wire >/dev/null 2>&1 ) && ok "wire ran" || bad "wire failed"
grep -q "agents hook" "$CONF" && bad "a retired entry survived" || ok "the retired entry is gone"
grep -q "/my/own/notify.sh" "$CONF" && ok "the foreign hook survived" || bad "the foreign hook was dropped"

step "8. doctor reports a stale entry before wire clears it"
cat > "$CONF" <<'JSON'
{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/opt/homebrew/bin/agents hook stop --harness claude-code"}]}]}}
JSON
( cd "$FRESH" && PATH="$WORK:$PATH" "$WORK/agents" doctor >"$WORK/doctor2.out" 2>&1 ) && code=0 || code=$?
check "doctor exits 1 on a stale entry" "$code" "1"
grep -q "run \`agents wire\`" "$WORK/doctor2.out" && ok "the remedy names wire" || bad "the remedy does not name wire"
( cd "$FRESH" && "$WORK/agents" wire >/dev/null 2>&1 )
[ -e "$CONF" ] && bad "a config holding only our entry should have been removed" || ok "the emptied config was removed"

step "9. the git hook chain still runs after the reduction"
if [ "$INSTALL" = "1" ]; then
  bash "$REPO_ROOT/git/install-hooks.sh" install --adopt-owned "$REPO_ROOT" "$HOME" "$WORK/agents" >/dev/null 2>&1 \
    && ok "hook chain installed against this binary" || bad "hook installation failed"
  CHAIN_HOME="$HOME"
else
  # Without --install the live chain is left alone, so the installer runs
  # against a disposable COPY of the checkout. It always writes into
  # <dotfiles-root>/git/hooks.d, and the live one points at an earlier binary,
  # which the installer correctly refuses to repoint without --adopt-owned.
  printf '  NOTE  --install not given; using a disposable checkout so the live chain is untouched\n'
  CHAIN_ROOT="$WORK/chainroot"
  CHAIN_HOME="$WORK/chainhome"
  mkdir -p "$CHAIN_ROOT/git" "$CHAIN_HOME/bin"
  cp -R "$REPO_ROOT/git/." "$CHAIN_ROOT/git/"
  cp "$WORK/agents" "$CHAIN_HOME/bin/agents"
  # --adopt-owned because the copy carries the live symlinks, which point at an
  # earlier binary. Adopting is safe here precisely because the root is
  # disposable; on the live checkout the same flag is what repoints the real
  # chain, and that is what --install does.
  if bash "$CHAIN_ROOT/git/install-hooks.sh" install --adopt-owned "$CHAIN_ROOT" "$CHAIN_HOME" "$CHAIN_HOME/bin/agents" >/dev/null 2>&1; then
    ok "hook chain installed into a disposable checkout"
  else
    bad "hook installation failed"
    bash "$CHAIN_ROOT/git/install-hooks.sh" install --adopt-owned "$CHAIN_ROOT" "$CHAIN_HOME" "$CHAIN_HOME/bin/agents" 2>&1 | head -3
  fi
fi
COMMITTER="$WORK/commitrepo"
mkdir -p "$COMMITTER"
git -C "$COMMITTER" init -q .
git -C "$COMMITTER" -c user.email=t@example.com -c user.name=T commit -q --allow-empty -m "baseline"

# The chain lives under the dotfiles root the installer was given -- that is
# where `git/hooks.d` is -- and pointing core.hooksPath at a directory that does
# not exist makes git skip every hook in silence, which would let this step pass
# while proving nothing. So the path is asserted to exist first.
CHAIN_HOOKS="$REPO_ROOT/git/hooks.d"
[ "$INSTALL" = "1" ] || CHAIN_HOOKS="$CHAIN_ROOT/git/hooks.d"
if [ -x "$CHAIN_HOOKS/pre-commit" ] || [ -L "$CHAIN_HOOKS/pre-commit" ]; then
  ok "the hook chain is installed at $CHAIN_HOOKS"
else
  bad "no hook chain at $CHAIN_HOOKS; the commit below would run no hook at all"
fi

# A hook must actually RUN, not merely be installed. The commit-msg hook strips
# AI attribution, so a message carrying a footer and coming out without one is
# proof the hook executed -- a plain successful commit proves nothing, because
# git treats a missing hook as success.
printf 'x\n' > "$COMMITTER/x.txt"
git -C "$COMMITTER" add x.txt
if git -C "$COMMITTER" -c user.email=t@example.com -c user.name=T \
     -c core.hooksPath="$CHAIN_HOOKS" commit -q -m "chore: exercise the hook chain" 2>"$WORK/commit.err"; then
  ok "a commit runs through the installed hook chain"
else
  bad "the commit failed"; cat "$WORK/commit.err"
fi

printf 'y\n' > "$COMMITTER/y.txt"
git -C "$COMMITTER" add y.txt
# The message goes in a file: `commit -F -` reads stdin, and a heredoc beside a
# stderr redirect is not reliably delivered to it.
cat > "$WORK/msg.txt" <<'MSG'
chore: attribution must be stripped

🤖 Generated with [Claude Code](https://claude.ai/code)
MSG
if git -C "$COMMITTER" -c user.email=t@example.com -c user.name=T \
     -c core.hooksPath="$CHAIN_HOOKS" commit -q -F "$WORK/msg.txt" 2>"$WORK/commit2.err"; then
  ok "the attribution commit was accepted"
else
  bad "the attribution commit failed"; cat "$WORK/commit2.err"
fi
if git -C "$COMMITTER" log -1 --pretty=%B | grep -q "Generated with"; then
  bad "the commit-msg hook did not run: the attribution footer survived"
else
  ok "the commit-msg hook ran: the attribution footer was stripped"
fi

# The repository's global instruction file quotes this second URL form and says
# the URL varies by version, so both are exercised. It was measured carrying
# through unchanged while only the other form was covered.
printf 'z\n' > "$COMMITTER/z.txt"
git -C "$COMMITTER" add z.txt
cat > "$WORK/msg2.txt" <<'MSG'
chore: the other documented form

🤖 Generated with [Claude Code](https://claude.com/claude-code)
MSG
git -C "$COMMITTER" -c user.email=t@example.com -c user.name=T \
  -c core.hooksPath="$CHAIN_HOOKS" commit -q -F "$WORK/msg2.txt" 2>/dev/null || true
if git -C "$COMMITTER" log -1 --pretty=%B | grep -q "Generated with"; then
  bad "the second documented attribution URL form survived"
else
  ok "both documented attribution URL forms are stripped"
fi

step "10. the generated README block matches the binary (CI checks this)"
( cd "$REPO_ROOT/agents" && go test -count=1 -run 'TestReadmeCommandBlockIsCurrent' . >/dev/null ) \
  && ok "README block is current" || bad "README block is stale"

printf '\n===============================\n'
printf 'acceptance: %d passed, %d failed\n' "$pass" "$fail"
printf '===============================\n'
[ "$fail" -eq 0 ]
