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

# --adopt-owned repoints links this installer wrote for an EARLIER binary.
# Without it every existing link must already name the binary it was handed,
# which is what keeps a foreign hook from being overwritten. It is the one flag
# an upgrade needs: a package manager deletes the versioned path a pinned link
# points at, git runs a dangling hook as if no hook existed, and the default
# then refuses to repair what the upgrade broke. See
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

preflight() {
	if [ ! -d "$hooks_dir" ] || [ -L "$hooks_dir" ]; then
		refuse "hooks directory '$hooks_dir' must be an existing real directory"
	fi
	if [ ! -f "$attributes_source" ] || [ -L "$attributes_source" ]; then
		refuse "tracked attributes source '$attributes_source' must be a regular file"
	fi
	validate_global_config_target
	inspect_global_hooks_path
	check_exact_symlink_or_absent "$attributes_link" "$attributes_source" "global attributes link"
	for hook in $hook_names; do
		check_exact_symlink_or_absent "$hooks_dir/$hook" "$binary" "owned $hook hook"
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
	if [ -L "$hooks_dir/$hook" ]; then
		# Only reachable under --adopt-owned: without it preflight refused any
		# link that does not already name $binary, and a non-symlink refused
		# outright. The recheck below then confirms every link names $binary.
		if [ "$(readlink "$hooks_dir/$hook")" != "$binary" ]; then
			ln -sfn "$binary" "$hooks_dir/$hook"
			printf 'install-hooks: repointed %s at %s\n' "$hook" "$binary" >&2
		fi
		continue
	fi
	ln -s "$binary" "$hooks_dir/$hook"
done

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
