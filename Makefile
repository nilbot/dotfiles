.PHONY: agents release

# Provisioning lives in ./bootstrap, not here:
#
#     ./bootstrap plan  workstation    # what it would do, changing nothing
#     ./bootstrap apply workstation    # do it
#     ./bootstrap check                # is this machine still converged
#     ./bootstrap migrate              # what needs migrating, and how
#
# Everything this Makefile used to provision is now a phase. The targets were
# not aliased to ./bootstrap: an alias would keep an interface this work exists
# to remove and leave two apparent entry points.
#
# What remains is a developer convenience for the agents module during
# inner-loop work. `./bootstrap apply workstation` builds the same binary in
# its devtools phase, so this target is a shortcut, not a separate mechanism.
#
# The -X stamp names the checkout this binary was built from, activating Dotfiles
# Operator Mode. Without it, an unstamped binary operates in Standalone Mode
# (DotfilesRoot() == ""), where doctor skips machine-level dotfiles checks and the
# dispatcher skips personal hook chains. Whoever builds the binary is the only party
# that knows the checkout root, so both builders have to say it -- ./bootstrap's
# devtools phase carries the same flag, and bootstrap.d's makefile test pins this one
# so the two cannot drift apart unnoticed.
#
# RUN THIS FROM THE MAIN CHECKOUT, not from a linked worktree. The two builders
# stamp different things by design: this one stamps $(CURDIR), the devtools
# phase stamps the root bootstrap was pointed at. In the main checkout they are
# the same directory. In a worktree they are not, and this target still writes
# the one global $(HOME)/bin/agents -- so it publishes a binary stamped to a
# temporary directory. Delete that worktree later and the stamp names nothing:
# doctor still passes, because it compares paths that agree with each other,
# while the git hook chain finds no extras directory and silently runs none of
# the personal hooks, at exit 0. The stamp deliberately beats
# AGENTS_DOTFILES_ROOT, so the environment cannot rescue it either. Rebuild
# from the main checkout to repair.

agents:
	mkdir -p "$(HOME)/bin"
	cd "$(CURDIR)/agents" && go build -trimpath -ldflags "-X main.dotfilesRoot=$(CURDIR)" -o "$(HOME)/bin/agents" .
	@echo "built $(HOME)/bin/agents"

release:
	@bash script/package-release.sh
