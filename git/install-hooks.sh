#!/usr/bin/env bash

set -eu

usage() {
	printf '%s\n' "usage: install-hooks.sh <preflight|install> <dotfiles-root> <home> <agents-binary>" >&2
	exit 2
}

refuse() {
	printf 'install-hooks: refusing: %s\n' "$1" >&2
	exit 1
}

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
		if [ "$got" != "$want" ]; then
			refuse "$label at '$path' points to '$got', not '$want'; move it aside deliberately, then retry"
		fi
		return 0
	fi
	if [ -e "$path" ]; then
		refuse "$label at '$path' already exists and is not the exact intended symlink; preserve or move it aside deliberately, then retry"
	fi
}

validate_binary() {
	if [ ! -f "$binary" ] || [ ! -x "$binary" ] || [ -L "$binary" ]; then
		refuse "agents binary '$binary' must be an executable regular file"
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
	if [ ! -L "$hooks_dir/$hook" ]; then
		ln -s "$binary" "$hooks_dir/$hook"
	fi
done

# Recheck immediately before activating the chain. The global key is written
# last, so a partial install cannot make Git execute an incomplete hooks dir.
preflight
validate_binary
if [ "$config_correct" -eq 0 ]; then
	git config --global core.hooksPath "$hooks_dir"
fi
printf '%s\n' "install-hooks: installed global Git hooks"
