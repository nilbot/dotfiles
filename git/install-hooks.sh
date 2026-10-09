#!/usr/bin/env bash

set -eu

usage() {
	printf '%s\n' "usage: install-hooks.sh [--adopt-owned] <preflight|install> <dotfiles-root> <home> <agents-binary>" >&2
	exit 2
}

refuse() {
	printf 'install-hooks: refusing: %s\n' "$1" >&2
	exit 1
}

# --adopt-owned replaces hook names this installer wrote -- a symlink it
# installed for an EARLIER binary, or an entry naming a different checkout or
# written by an older form of this script. Without it every name must be absent
# or already hold the exact entry this run would write, which is what keeps a
# foreign hook from being overwritten. It is the one flag an upgrade needs: a
# package manager deletes the versioned path a pinned symlink points at, git
# runs a dangling hook as if no hook existed, and the default then refuses to
# repair what the upgrade broke. See
# docs/qna/why-does-a-brew-upgrade-stop-my-commit-guard.md.
adopt_owned=0
if [ "${1-}" = "--adopt-owned" ]; then
	adopt_owned=1
	shift
elif [ "${2-}" = "--adopt-owned" ]; then
	# Accepted on either side of the mode, because `install --adopt-owned`
	# reads as the verb and its modifier while `--adopt-owned install` reads as
	# the option and the verb, and a remedy the human has to edit before it
	# runs is not a remedy. Rebuilt rather than re-split: these paths contain
	# spaces, so "$@" is the only safe way to carry them.
	adopt_owned=1
	first=$1
	shift 2
	set -- "$first" "$@"
fi

if [ "$#" -ne 4 ]; then
	usage
fi

mode=$1
root=$2
install_home=$3
binary=$4

case "$mode" in
	preflight|install) ;;
	*) usage ;;
esac
for value in "$root" "$install_home" "$binary"; do
	case "$value" in
		/*) ;;
		*) refuse "all paths must be absolute" ;;
	esac
done

hooks_dir=$root/git/hooks.d
chain_record=$hooks_dir/chain.env
attributes_source=$root/git/gitattributes
attributes_link=$install_home/.gitattributes
hook_names='pre-commit commit-msg post-merge post-checkout'
config_correct=0

# The explicit home is authoritative. This keeps direct invocations from
# querying or writing an unrelated ambient user's global Git configuration.
HOME=$install_home
export HOME
global_config=$install_home/.gitconfig
if [ "${GIT_CONFIG_GLOBAL+x}" = x ] && [ "$GIT_CONFIG_GLOBAL" != "$global_config" ]; then
	refuse "GIT_CONFIG_GLOBAL must equal the machine-local primary '$global_config'; no files were changed"
fi
GIT_CONFIG_GLOBAL=$global_config
export GIT_CONFIG_GLOBAL

global_config_link_count() {
	if count=$(stat -f '%l' "$global_config" 2>/dev/null); then
		printf '%s\n' "$count"
		return 0
	fi
	if count=$(stat -c '%h' -- "$global_config" 2>/dev/null); then
		printf '%s\n' "$count"
		return 0
	fi
	refuse "cannot inspect link count for primary global config '$global_config'; no files were changed"
}

validate_global_config_target() {
	if [ -L "$global_config" ]; then
		refuse "primary global config '$global_config' must be a non-symlink machine-local regular file; preserve or move the symlink aside deliberately, then retry"
	fi
	if [ ! -e "$global_config" ]; then
		return 0
	fi
	if [ ! -f "$global_config" ]; then
		refuse "primary global config '$global_config' must be a machine-local regular file; preserve or move it aside deliberately, then retry"
	fi
	count=$(global_config_link_count)
	case "$count" in
		''|*[!0-9]*) refuse "primary global config '$global_config' has an unreadable link count; no files were changed" ;;
	esac
	if [ "$count" -ne 1 ]; then
		refuse "primary global config '$global_config' has multiple hard links and may be shared; copy it to a private file deliberately, then retry"
	fi
	if [ ! -w "$global_config" ]; then
		refuse "primary global config '$global_config' is not writable; preserve it or repair its ownership and permissions deliberately, then retry"
	fi
}

inspect_global_hooks_path() {
	config_correct=0
	set +e
	config_output=$(git config --global --includes --show-origin --get-all core.hooksPath 2>&1)
	config_status=$?
	set -e
	case "$config_status" in
		1) return 0 ;;
		0) ;;
		*) refuse "could not inspect global core.hooksPath; no files were changed" ;;
	esac

	line_count=$(printf '%s\n' "$config_output" | awk 'END { print NR }')
	if [ "$line_count" -ne 1 ]; then
		refuse "global core.hooksPath has multiple values; preserve them or run 'git config --global --unset-all core.hooksPath' deliberately, then retry"
	fi
	tab=$(printf '\t')
	case "$config_output" in
		*"$tab"*) ;;
		*) refuse "global core.hooksPath origin could not be parsed; no files were changed" ;;
	esac
	origin=${config_output%%"$tab"*}
	configured_path=${config_output#*"$tab"}
	expected_origin=file:$global_config
	if [ "$configured_path" != "$hooks_dir" ] || [ "$origin" != "$expected_origin" ]; then
		refuse "global core.hooksPath is already configured as '$configured_path' from '$origin'; preserve it or run 'git config --global --unset-all core.hooksPath' deliberately, then retry"
	fi
	config_correct=1
}

check_exact_symlink_or_absent() {
	path=$1
	want=$2
	label=$3
	if [ -L "$path" ]; then
		got=$(readlink "$path") || refuse "cannot inspect $label at '$path'"
		if [ "$got" = "$want" ]; then
			return 0
		fi
		if owned_link_target "$got"; then
			if [ "$adopt_owned" -eq 1 ]; then
				return 0
			fi
			# Adopting here would be a regression, not a repair. The link is
			# already a stable path that survives upgrades, and $want is the
			# version-specific keg it resolves into -- the shape a caller gets
			# from `realpath "$(command -v agents)"`, which is how the previous
			# advice produced exactly the links an upgrade then broke. Say so
			# instead of offering a flag that undoes the good state.
			if ! is_keg_agents_path "$got" && is_keg_agents_path "$want" && resolve_path "$got" >/dev/null 2>&1; then
				refuse "$label at '$path' already points to '$got', which is the stable path and survives a package upgrade; '$want' is the version-specific keg path that the next upgrade removes. Re-run with '$got' instead of '$want'."
			fi
			refuse "$label at '$path' points to '$got', not '$want'; that is a link this installer wrote for an earlier binary, so re-run with --adopt-owned to repoint it"
		fi
		refuse "$label at '$path' points to '$got', not '$want'; move it aside deliberately, then retry"
	fi
	if [ -e "$path" ]; then
		refuse "$label at '$path' already exists and is not the exact intended symlink; preserve or move it aside deliberately, then retry"
	fi
}

# check_hook_or_absent is the hook-name half of preflight, and the conversion
# table in one place. What each shape it can find means:
#
#   nothing                       write the entry (install does)
#   a symlink this installer made refuse without --adopt-owned; replace with it
#   a symlink to anything else    refuse, with or without the flag
#   our entry, current bytes      a success state: a second install changes
#                                 nothing, inodes included
#   our entry, other bytes        refuse without --adopt-owned; replace with it
#   any other regular file        refuse, with or without the flag
#
# "Ours" for an entry is entry_is_generated, decided from the file alone -- a
# missing or malformed record is one of the states the repair command exists
# for, so reading chain.env first would refuse the installer's own entries as
# foreign.
check_hook_or_absent() {
	path=$1
	hook=$2
	if [ -L "$path" ]; then
		check_owned_symlink_or_absent "$path" "$hook"
		return 0
	fi
	if [ ! -e "$path" ]; then
		return 0
	fi
	if [ -f "$path" ] && entry_is_generated "$path" "$hook"; then
		if entry_is_current "$path" "$hook"; then
			return 0
		fi
		if [ "$adopt_owned" -eq 1 ]; then
			return 0
		fi
		refuse "'$path' is an entry this installer wrote for another checkout or from an older generated form; re-run with --adopt-owned to replace it"
	fi
	refuse "'$path' already exists and is not an entry this installer wrote; preserve or move it aside deliberately, then retry"
}

# check_owned_symlink_or_absent is what a hook name holding a symlink may be.
#
# An exact symlink STOPPED BEING A SUCCESS STATE here, and that is the whole
# point of the conversion: this machine is wired that way, and a success state
# would leave four symlinks in place forever. Every refusal the old exact-link
# rule carried is still here -- the stable-path refusal for a keg target, and
# the foreign-link refusal -- but "it already points at the binary you handed
# me" is now the state --adopt-owned exists to replace.
check_owned_symlink_or_absent() {
	path=$1
	hook=$2
	label="owned $hook hook"
	got=$(readlink "$path") || refuse "cannot inspect $label at '$path'"
	if [ "$got" = "$binary" ] || owned_link_target "$got"; then
		# Adopting here would be a regression, not a repair. The link is already
		# a stable path that survives upgrades, and $binary is the
		# version-specific keg it resolves into -- the shape a caller gets from
		# `realpath "$(command -v agents)"`, which is how the previous advice
		# produced exactly the links an upgrade then broke. Say so instead of
		# offering a flag that undoes the good state.
		if ! is_keg_agents_path "$got" && is_keg_agents_path "$binary" && resolve_path "$got" >/dev/null 2>&1; then
			refuse "$label at '$path' already points to '$got', which is the stable path and survives a package upgrade; '$binary' is the version-specific keg path that the next upgrade removes. Re-run with '$got' instead of '$binary'."
		fi
		if [ "$adopt_owned" -eq 1 ]; then
			return 0
		fi
		refuse "$label at '$path' is a symlink this installer wrote (it points to '$got'); four symlinks have to become four entries, so re-run with --adopt-owned to replace it"
	fi
	refuse "$label at '$path' points to '$got', not '$binary'; move it aside deliberately, then retry"
}

# entry_is_generated decides ownership from the entry alone, never from
# chain.env: a missing or malformed record is one of the states the repair
# command exists for, so a rule that read the record first would refuse the
# installer's own entries as foreign.
#
# The three tests are the design's, and they are all inside the file: line 1,
# the generated header on line 2 (the path is not compared with this checkout --
# an entry from a different checkout is still ours to recognise), and a last
# line that execs githook for the hook name the file itself carries.
entry_is_generated() {
	path=$1
	hook=$2
	[ "$(sed -n '1p' "$path")" = '#!/bin/sh' ] || return 1
	case "$(sed -n '2p' "$path")" in
		'# Written by git/install-hooks.sh for '*'.') ;;
		*) return 1 ;;
	esac
	case "$(tail -n 1 "$path")" in
		'exec '*" githook $hook "*) return 0 ;;
	esac
	return 1
}

# entry_is_current is ownership plus currency: the bytes on disk are exactly
# what this run would write. That is the difference between a second install --
# which must change nothing, inodes included -- and an entry for another
# checkout or an older form, which --adopt-owned is required to replace.
entry_is_current() {
	path=$1
	hook=$2
	[ -f "$path" ] && [ "$(cat "$path")" = "$(generate_entry "$hook")" ]
}

# resolve_path follows symlinks to the real file. realpath is in GNU coreutils
# and in macOS; readlink -f covers systems where it is not. Failing both is a
# refusal rather than a guess, because what gets recorded in a hook is what git
# executes for every commit on this machine.
resolve_path() {
	if command -v realpath >/dev/null 2>&1; then
		realpath -- "$1"
		return $?
	fi
	if readlink -f -- "$1" >/dev/null 2>&1; then
		readlink -f -- "$1"
		return $?
	fi
	return 1
}

# is_keg_agents_path is true for a Homebrew keg for agents. Every stable
# Homebrew path -- bin/agents, opt/agents/bin/agents -- resolves into one, and
# Homebrew repoints them at the new keg on upgrade. A hook pinned to the keg
# itself instead dangles as soon as cleanup removes it, and git runs a dangling
# hook as if no hook existed: the commit guard stops running and nothing says so.
is_keg_agents_path() {
	case "$1" in
		*/Cellar/agents/*/bin/agents) return 0 ;;
	esac
	return 1
}

# owned_link_target is true when an existing link is one this installer could
# have written: a keg path for agents, current or already removed by an
# upgrade. A foreign file, or a link to any other program, is never adopted --
# with or without the flag.
owned_link_target() {
	if is_keg_agents_path "$1"; then
		return 0
	fi
	resolved=$(resolve_path "$1" 2>/dev/null) || return 1
	is_keg_agents_path "$resolved"
}

# chain_recorded_binary reports the binary the chain already names, or the empty
# string when there is none.
#
# It exists ONLY for the keg-path guard in preflight: installing through a
# version-specific keg path over a chain that names a stable path re-pins the
# hooks to a directory the next upgrade removes. It is never used to decide
# ownership -- a missing or malformed record is one of the states the repair
# command exists for, and ownership is read from the entry itself.
chain_recorded_binary() {
	if [ -f "$chain_record" ]; then
		sed -n 's/^binary=//p' "$chain_record" | head -n 1
		return 0
	fi
	if [ -L "$hooks_dir/pre-commit" ]; then
		readlink "$hooks_dir/pre-commit"
		return 0
	fi
	printf '\n'
}

validate_binary() {
	if [ -L "$binary" ]; then
		# A symlink is accepted only when it resolves into a Homebrew keg for
		# agents, which is what makes an upgrade survivable: brew repoints its
		# stable paths at the new keg, so the hooks keep resolving to the
		# current binary with nothing to re-run. The path RECORDED is the one
		# passed in -- not the keg it resolves to -- so the next upgrade moves
		# it. Anything else is still refused: the resolved file, not the link,
		# is what git executes on every commit here.
		resolved=$(resolve_path "$binary") || resolved=
		if [ -z "$resolved" ] || ! is_keg_agents_path "$resolved"; then
			refuse "agents binary '$binary' must be an executable regular file, or a symlink that resolves into a Homebrew keg for agents; '$binary' resolves to '${resolved:-nothing}'"
		fi
		if [ ! -f "$resolved" ] || [ ! -x "$resolved" ]; then
			refuse "agents binary '$binary' must be an executable regular file, or a symlink that resolves into a Homebrew keg for agents; '$binary' resolves to '$resolved', which is not executable"
		fi
		return 0
	fi
	if [ ! -f "$binary" ] || [ ! -x "$binary" ]; then
		refuse "agents binary '$binary' must be an executable regular file, or a symlink that resolves into a Homebrew keg for agents"
	fi
}

# generate_entry prints the entry for one hook name. The text is the design's
# §3.1 script with two substitutions -- the checkout in its header, and the hook
# name on the last line -- and nothing else. The record is PARSED, never
# sourced, which is why the allow-list and the one `fail` path have to be
# exactly this: the entry runs at every commit, with the committing user's
# privileges, so a record carrying `: > /tmp/pwned` must be rejected and not
# executed, and a value the record does not define must never be guessed at.
generate_entry() {
	hook=$1
	printf '%s\n' '#!/bin/sh'
	printf '# Written by git/install-hooks.sh for %s.\n' "$root"
	cat <<'ENTRY'
# The checkout is read from our own header, so a broken record is still repairable.
{ IFS= read -r _shebang; IFS= read -r _header; } < "$0"
repair_root=${_header#*for }
repair_root=${repair_root%.}
fail() {
  printf 'agents: %s\n' "$1" >&2
  if [ -n "$repair_root" ]; then
    printf 'agents: repair with: bash %s/git/install-hooks.sh install --adopt-owned %s "$HOME" "$(command -v agents)"\n' "$repair_root" "$repair_root" >&2
  fi
  exit 1
}
ENTRY
	# The one branch on the entry's own hook name. A missing binary is a lost
	# banner for post-merge and post-checkout, and reporting it at exit 0 is
	# what keeps `git checkout` and `git switch` working; for the two guard
	# names it blocks the commit, which is the whole point of a guard. Every
	# record, format and path error still goes through fail, for all four: a
	# broken record is a broken guard whatever the hook is.
	if is_observational_hook "$hook"; then
		cat <<'ENTRY'
skip() {
  printf 'agents: %s\n' "$1" >&2
  if [ -n "$repair_root" ]; then
    printf 'agents: repair with: bash %s/git/install-hooks.sh install --adopt-owned %s "$HOME" "$(command -v agents)"\n' "$repair_root" "$repair_root" >&2
  fi
  exit 0
}
ENTRY
	fi
	cat <<'ENTRY'
chain=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd) || fail "cannot locate the chain directory"
[ -n "$repair_root" ] || repair_root=$chain
format=; binary=; checkout=
while IFS='=' read -r key value; do
  case "$key" in
    ''|\#*) ;;
    format)   format=$value ;;
    binary)   binary=$value ;;
    checkout) checkout=$value ;;
    *) fail "unknown key '$key' in $chain/chain.env" ;;
  esac
done < "$chain/chain.env" || fail "cannot read $chain/chain.env"
[ "$format" = 1 ] || fail "$chain/chain.env is not format 1"
case "$binary" in /*) ;; *) fail "binary in $chain/chain.env is not an absolute path" ;; esac
ENTRY
	if is_observational_hook "$hook"; then
		printf '%s\n' '[ -f "$binary" ] && [ -x "$binary" ] || skip "the commit guard is not installed: $binary is missing or not executable"'
	else
		printf '%s\n' '[ -f "$binary" ] && [ -x "$binary" ] || fail "the commit guard is not installed: $binary is missing or not executable"'
	fi
	cat <<'ENTRY'
case "$checkout" in -|/*) ;; *) fail "checkout in $chain/chain.env is neither - nor an absolute path" ;; esac
ENTRY
	printf 'exec "$binary" githook %s --checkout "$checkout" "$@"\n' "$hook"
}

# is_observational_hook is true for the two hook names that report rather than
# guard: a missing banner there must not fail a `git checkout` or a `git switch`.
is_observational_hook() {
	case "$1" in
		post-merge|post-checkout) return 0 ;;
	esac
	return 1
}

# generate_record prints chain.env: a comment line, then format, binary and
# checkout, one key=value per line. Values are literal -- no quoting, no
# expansion, nothing after the first = is interpreted -- which is what lets a
# path containing a space be recorded at all.
generate_record() {
	printf '%s\n' '# Written by git/install-hooks.sh. Re-run the installer to change it.'
	printf 'format=1\n'
	printf 'binary=%s\n' "$binary"
	printf 'checkout=%s\n' "$root"
}

# publish_entry writes one entry atomically: a temporary file in the same
# directory, its final mode set before it is visible, then mv into place. A hook
# name that exists but is not executable is skipped by Git with a hint and exit
# 0, so no entry may ever be seen in a partial or non-executable state -- ln -sfn
# was atomic, and its replacement has to be too.
#
# A name already holding exactly these bytes is left alone, so a second install
# preserves the inode it installed instead of replacing a working chain.
publish_entry() {
	hook=$1
	path=$hooks_dir/$hook
	if entry_is_current "$path" "$hook"; then
		return 0
	fi
	tmp=$hooks_dir/.$hook.install-hooks.$$
	generate_entry "$hook" > "$tmp" ||
		{ rm -f "$tmp"; refuse "could not write a temporary entry for '$hook' in '$hooks_dir'; no global key was written"; }
	chmod 0755 "$tmp" ||
		{ rm -f "$tmp"; refuse "could not set the mode of the temporary entry for '$hook'; no global key was written"; }
	if [ -e "$path" ] || [ -L "$path" ]; then
		printf 'install-hooks: replaced %s with a generated entry\n' "$hook" >&2
	fi
	mv -f "$tmp" "$path" ||
		{ rm -f "$tmp"; refuse "could not publish the entry for '$hook'; no global key was written"; }
}

# publish_record writes chain.env the same way. It is written after the entries
# and before the global key: an entry whose record is missing fails closed and
# says so, which is the state a half-finished install leaves.
publish_record() {
	if [ -f "$chain_record" ] && [ "$(cat "$chain_record")" = "$(generate_record)" ]; then
		return 0
	fi
	tmp=$hooks_dir/.chain.env.install-hooks.$$
	generate_record > "$tmp" ||
		{ rm -f "$tmp"; refuse "could not write a temporary record at '$tmp'; no global key was written"; }
	chmod 0644 "$tmp" ||
		{ rm -f "$tmp"; refuse "could not set the mode of the temporary record at '$tmp'; no global key was written"; }
	mv -f "$tmp" "$chain_record" ||
		{ rm -f "$tmp"; refuse "could not publish '$chain_record'; no global key was written"; }
}

preflight() {
	if [ ! -d "$hooks_dir" ] || [ -L "$hooks_dir" ]; then
		refuse "hooks directory '$hooks_dir' must be an existing real directory"
	fi
	if [ ! -f "$attributes_source" ] || [ -L "$attributes_source" ]; then
		refuse "tracked attributes source '$attributes_source' must be a regular file"
	fi
	validate_global_config_target
	inspect_global_hooks_path
	# A string comparison of two paths, never a stat: preflight runs before a
	# binary exists, and must stay blind to whether the one it is handed is
	# real. Only install validates it.
	if is_keg_agents_path "$binary"; then
		recorded=$(chain_recorded_binary)
		if [ -n "$recorded" ] && ! is_keg_agents_path "$recorded"; then
			refuse "the hook chain already names '$recorded', which is the stable path and survives a package upgrade; '$binary' is the version-specific keg path that the next upgrade removes. Re-run with '$recorded' instead of '$binary'."
		fi
	fi
	check_exact_symlink_or_absent "$attributes_link" "$attributes_source" "global attributes link"
	for hook in $hook_names; do
		check_hook_or_absent "$hooks_dir/$hook" "$hook"
	done
}

preflight
if [ "$mode" = preflight ]; then
	printf '%s\n' "install-hooks: preflight passed"
	exit 0
fi

validate_binary

if [ ! -L "$attributes_link" ]; then
	ln -s "$attributes_source" "$attributes_link"
fi
for hook in $hook_names; do
	publish_entry "$hook"
done
publish_record

# Recheck immediately before activating the chain. The global key is written
# last, so a partial install cannot make Git execute an incomplete hooks dir.
preflight
validate_binary
if [ "$config_correct" -eq 0 ]; then
	git config --global core.hooksPath "$hooks_dir"
fi
printf '%s\n' "install-hooks: installed global Git hooks"
if is_keg_agents_path "$binary"; then
	# The install is valid, so this is a note and not a refusal: the hooks work
	# until the next cleanup. Naming the stable path here is the difference
	# between a machine that survives its next upgrade and one that silently
	# stops guarding commits.
	printf 'install-hooks: note: %s is a version-specific keg path that a package upgrade removes; re-run with the stable path, e.g. %s\n' \
		"$binary" "$(command -v agents 2>/dev/null || printf 'agents')" >&2
fi
