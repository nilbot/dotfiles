.PHONY: all dep links editors tmux extra omz bins dotfiles agents githooks

all: dep links editors tmux extra

dep:
	sudo -v || if [ -z $$? ]; then sudo ./super-install-dep.sh; fi
	./user-install-dep.sh

links: bins dotfiles githooks

binaries := $(wildcard bin/*.bin)

bins:
	mkdir -p $(HOME)/bin $(HOME)/src $(HOME)/pkg
	@for f in $(binaries); do \
		tgt="$$(basename $$f)"; \
		ln -sfn $(CURDIR)/$$f $(HOME)/bin/$$tgt; \
		done

# The agents binary lives in dotfiles and is invoked by absolute path from
# generated harness configs. Nothing is vendored per-repo.
agents:
	mkdir -p "$(HOME)/bin"
	cd "$(CURDIR)/agents" && go build -trimpath -o "$(HOME)/bin/agents" .
	@echo "built $(HOME)/bin/agents"

# Global Git hooks, for every repository on this machine. Preflight runs before
# the build and the installer repeats it before linking; core.hooksPath is the
# installer's final write.
githooks:
	bash "$(CURDIR)/git/install-hooks.sh" preflight "$(CURDIR)" "$(HOME)" "$(HOME)/bin/agents"
	$(MAKE) --no-print-directory agents HOME="$(HOME)"
	bash "$(CURDIR)/git/install-hooks.sh" install "$(CURDIR)" "$(HOME)" "$(HOME)/bin/agents"

dotfiles:
	mkdir -p $(HOME)/.config $(HOME)/.local
	./softlinks.sh
	ln -sf $(CURDIR)/spacemacs/dotspacemacs $(HOME)/.spacemacs;
	ln -sf $(CURDIR)/git/gitignore_global $(HOME)/.gitignore;
	ln -sf $(CURDIR)/tmux/tmux.conf $(HOME)/.tmux.conf;
# ~/.gitconfig is a machine-local FILE, not a symlink into this repo. It only
# includes the shared config, so that `git config --global ...` -- run by you, by
# git after "Please tell me who you are", or by 1Password's signing setup -- writes
# here instead of into published content.
	@if [ -L $(HOME)/.gitconfig ]; then \
		echo "removing legacy ~/.gitconfig symlink into this repo"; \
		rm -f $(HOME)/.gitconfig; \
	fi
# The template names the checkout with @DOTFILES_ROOT@; substituting it is what
# makes the include resolve from a checkout that is not ~/dotfiles. A plain cp
# would seed a path git cannot open, and git ignores a missing include in silence.
	@if [ ! -e $(HOME)/.gitconfig ]; then \
		sed 's|@DOTFILES_ROOT@|$(CURDIR)|g' $(CURDIR)/git/gitconfig.local.template > $(HOME)/.gitconfig; \
		echo "created ~/.gitconfig (machine-local; includes $(CURDIR)/git/gitconfig.shared)"; \
	else \
		echo "~/.gitconfig exists and is a regular file; leaving it alone"; \
	fi
# ~/.claude is owned by the Claude Code harness (plugins/, projects/, sessions/,
# settings.json). Only skills/ comes from this repo. Symlinking the whole directory
# put a stray ~/.claude/claude inside it.
	mkdir -p $(HOME)/.claude
	ln -sfn $(CURDIR)/claude/skills $(HOME)/.claude/skills;

editors:
	rm -rf $(HOME)/.vim $(HOME)/.emacs.d
	git clone --recursive https://github.com/jessfraz/.vim.git $(HOME)/.vim && cd $(HOME)/.vim && git submodule update --init
	ln -sf $(HOME)/.vim/vimrc $(HOME)/.vimrc
	git clone https://github.com/syl20bnr/spacemacs.git $(HOME)/.emacs.d

tmux:
	mkdir -p $(HOME)/.tmux
	git clone https://github.com/tmux-plugins/tpm $(HOME)/.tmux/plugins/tpm

extra:
	ln -sfn $(HOME)/crypt/extras.secret $(CURDIR)/extras.secret

omz:
	rm -rf $(HOME)/.oh-my-zsh
	git clone https://github.com/robbyrussell/oh-my-zsh.git $(HOME)/.oh-my-zsh
	@for d in $$(find zsh/custom/themes/ -maxdepth 1 ! -path zsh/custom/themes/ -type d); do \
		ln -sfn $(CURDIR)/$$d $(HOME)/.oh-my-zsh/custom/themes/$$(basename $$d); \
		ln -s $(CURDIR)/$$d/$$(basename $$d).zsh $(HOME)/.oh-my-zsh/custom/themes/$$(basename $$d).zsh-theme; \
		done
	@for d in $$(find zsh/custom/plugins/ -maxdepth 1 ! -path zsh/custom/plugins/ -type d); do \
		ln -sfn $(CURDIR)/$$d $(HOME)/.oh-my-zsh/custom/plugins/$$(basename $$d); \
		done
	ln -sfn $(CURDIR)/zsh/zshrc $(HOME)/.zshrc
	ln -sfn $(CURDIR)/zsh/zshenv $(HOME)/.zshenv
	ln -sfn $(CURDIR)/zsh/zprofile $(HOME)/.zprofile
	sudo -v || if [ -z $$? ]; then sudo chsh -s $(shell which zsh) $(shell whoami); fi

# The fish, fishshell and starship targets are gone; ./bootstrap owns those
# paths now. `rm -rf $(HOME)/.config/fish` used to delete a symlink and relink
# it, which was idempotent. Since ~/.config/fish became a real directory it
# would instead destroy the seeded stub and fisher's functions/, completions/,
# conf.d/, fish_plugins and fish_variables -- none of which is in git.
