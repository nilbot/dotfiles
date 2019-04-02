.PHONY: all dep links editors extra omz bins dotfiles

all: dep links editors extra omz

dep:
	if [ -z $(sudo -v) ]; then sudo ./super-install-dep.sh; fi
	./user-install-dep.sh

links: bins dotfiles

binaries := $(wildcard bin/*.bin)

bins:
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

extra:
	ln -sfn $(HOME)/crypt/extras.secret $(CURDIR)/extras.secret

omz:
	rm -rf $(HOME)/.oh-my-zsh
	git clone https://github.com/robbyrussell/oh-my-zsh.git $(HOME)/.oh-my-zsh
	@for d in $$(find zsh/custom/themes/ -maxdepth 1 ! -path zsh/custom/themes/ -type d); do \
		ln -sfn $(CURDIR)/zsh/custom/themes $(HOME)/.oh-my-zsh/custom/themes/$$(basename $$d); \
		done
	ln -sfn $(CURDIR)/zsh/zshrc $(HOME)/.zshrc
	ln -sfn $(CURDIR)/zsh/zshenv $(HOME)/.zshenv
	ln -sfn $(CURDIR)/zsh/zprofile $(HOME)/.zprofile
	sudo -v || if [ -z $$? ]; then sudo chsh -s $(shell which zsh) $(shell whoami); fi

