# shellcheck shell=bash
#
# The ONLY file in this tree permitted to mutate the filesystem or invoke a
# package manager. Phase files call these primitives; a test enforces it.
#
# Every primitive is a no-op under BOOTSTRAP_DRY_RUN=1, which is what makes
# `bootstrap plan` and `bootstrap apply` the same code path.

BOOTSTRAP_DRY_RUN=${BOOTSTRAP_DRY_RUN:-0}

log()    { printf '%s\n' "$*"; }
plan()   { printf 'plan  %s\n' "$*"; }
did()    { printf 'ok    %s\n' "$*"; }
warn()   { printf 'warn  %s\n' "$*" >&2; }

# Exit 2 is "block" in spec 1 §6's shared table.
refuse() { printf 'bootstrap: refusing: %s\n' "$*" >&2; exit 2; }

dry_run() { [ "$BOOTSTRAP_DRY_RUN" -eq 1 ]; }

bootstrap_platform() {
	case "$(uname -s)" in
		Darwin) printf 'darwin\n' ;;
		Linux)  printf 'linux\n' ;;
		*)      refuse "unsupported operating system '$(uname -s)'" ;;
	esac
}

do_dir() {
	local target=$1
	if [ -d "$target" ] && [ ! -L "$target" ]; then
		return 0
	fi
	if [ -e "$target" ] || [ -L "$target" ]; then
		refuse "'$target' exists and is not a real directory; move it aside deliberately, then retry"
	fi
	if dry_run; then
		plan "create directory $target"
		return 0
	fi
	mkdir -p "$target"
	did "created directory $target"
}

do_link() {
	local link_source=$1
	local target=$2
	if [ -L "$target" ]; then
		local current=$(readlink "$target") || refuse "cannot read the symlink at '$target'"
		if [ "$current" = "$link_source" ]; then
			return 0
		fi
		refuse "'$target' points to '$current', not '$link_source'; move it aside deliberately, then retry"
	fi
	if [ -e "$target" ]; then
		refuse "'$target' exists and is not a symlink; move it aside deliberately, then retry"
	fi
	do_dir "$(dirname "$target")"
	if dry_run; then
		plan "link $target -> $link_source"
		return 0
	fi
	ln -s "$link_source" "$target"
	did "linked $target -> $link_source"
}

do_seed() {
	local seed_source=$1
	local target=$2
	if [ -L "$target" ]; then
		refuse "'$target' must be a machine-local regular file but is a symlink; run './bootstrap migrate', or move it aside deliberately, then retry"
	fi
	if [ -f "$target" ]; then
		return 0
	fi
	if [ -e "$target" ]; then
		refuse "'$target' exists and is not a regular file; move it aside deliberately, then retry"
	fi
	[ -f "$seed_source" ] || refuse "seed template '$seed_source' is missing"
	do_dir "$(dirname "$target")"
	if dry_run; then
		plan "seed $target from $seed_source"
		return 0
	fi
	cp "$seed_source" "$target"
	did "seeded $target from $seed_source"
}

do_run() {
	if dry_run; then
		plan "run: $*"
		return 0
	fi
	"$@"
}

do_sudo() {
	if dry_run; then
		plan "run (sudo): $*"
		return 0
	fi
	sudo "$@"
}
