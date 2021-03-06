.PHONY: all dep links editors tmux extra omz bins dotfiles fish

all: dep links editors tmux extra omz

dep:
	sudo -v || if [ -z $$? ]; then sudo ./super-install-dep.sh; fi
	./user-install-dep.sh

links: bins dotfiles

binaries := $(wildcard bin/*.bin)

bins:
	mkdir -p $(HOME)/bin $(HOME)/src $(HOME)/pkg
	@for f in $(binaries); do \
		tgt="$$(basename $$f)"; \
		ln -sfn $(CURDIR)/$$f $(HOME)/bin/$$tgt; \
		done

dotfiles:
	ln -sf $(CURDIR)/spacemacs/dotspacemacs $(HOME)/.spacemacs;
	ln -sf $(CURDIR)/git/gitconfig.symlink $(HOME)/.gitconfig;
	ln -sf $(CURDIR)/git/gitignore_global.symlink $(HOME)/.gitignore;
	ln -sf $(CURDIR)/tmux/tmux.conf $(HOME)/.tmux.conf;

editors:
	rm -rf $(HOME)/.vim $(HOME)/.emacs.d
	git clone https://github.com/jessfraz/.vim.git $(HOME)/.vim && cd $(HOME)/.vim && git submodule update --init
	ln -sf $(HOME)/.vim/vimrc $(HOME)/.vimrc
	mkdir -p $(HOME)/.config/nvim && ln -sf $(CURDIR)/nvim/init.vim $(HOME)/.config/nvim/init.vim
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

fish: starship fishshell

starship:
	ln -s $(CURDIR)/starship.toml $(HOME)/.config/starship.toml
fishshell:
	rm -rf $(HOME)/.config/fish
	mkdir -p $(HOME)/.config/fish
	@for f in $$(find fish -maxdepth 1 -type f); do \
		ln -s $(CURDIR)/$$f $(HOME)/.config/fish/$$(basename $$f); \
		done
	sudo -v || if [ -z $$? ]; then sudo chsh -s $(shell which fish) $(shell whoami); fi

