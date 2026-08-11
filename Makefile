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

agents:
	mkdir -p "$(HOME)/bin"
	cd "$(CURDIR)/agents" && go build -trimpath -o "$(HOME)/bin/agents" .
	@echo "built $(HOME)/bin/agents"
