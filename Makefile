.PHONY: agents

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
# The -X stamp names the checkout this binary was built from. Without it the
# binary falls back to the historical ~/dotfiles, where doctor reports three
# failures against a healthy machine and the git hook chain silently runs none
# of the personal hooks. Whoever builds the binary is the only party that knows
# the answer, so both builders have to say it -- ./bootstrap's devtools phase
# carries the same flag, and bootstrap.d's makefile test pins this one so the
# two cannot drift apart unnoticed.

agents:
	mkdir -p "$(HOME)/bin"
	cd "$(CURDIR)/agents" && go build -trimpath -ldflags "-X main.dotfilesRoot=$(CURDIR)" -o "$(HOME)/bin/agents" .
	@echo "built $(HOME)/bin/agents"
