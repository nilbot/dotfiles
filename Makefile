.PHONY: all bin dotfiles etc

all: prerequisite links editors extra omz

prerequisite:
	sudo ./super-install-dep.sh
	./user-install-dep.sh

links:
	for dir in $(shell find $(CURDIR) -name ".*" -not -name ".git" -not -name ".gitignore" -not -name ".gitmodules"); do \
		target=$$(basename $$dir); \
		ln -sfn $$dir $(HOME)/$$target; \
	done
	mkdir -p $(HOME)/.gnupg/;
	ln -sfn $(CURDIR)/gnupg/gpg.conf $(HOME)/.gnupg/gpg.conf;
	ln -sfn $(CURDIR)/gnupg/gpg-agent.conf $(HOME)/.gnupg/gpg-agent.conf;
	ln -sf $(CURDIR)/spacemacs/dotspacemacs $(HOME)/.spacemacs;
	ln -sf $(CURDIR)/git/gitconfig.symlink $(HOME)/.gitconfig;
	ln -sf $(CURDIR)/git/gitignore_global.symlink $(HOME)/.gitignore;
	ln -sf $(CURDIR)/tmux/tmux.conf $(HOME)/.tmux.conf;

editors:
	git clone https://github.com/jessfraz/.vim.git $(HOME)/.vim && cd $(HOME)/.vim && git submodule update --init
	ln -sf $(HOME)/.vim/vimrc $(HOME)/.vimrc
	git clone https://github.com/syl20bnr/spacemacs.git $(HOME)/.emacs.d

extra:
	mkdir -p $(CURDIR)/extras.secret/

omz:
	rm -rf $(HOME)/.oh-my-zsh
	git clone https://github.com/robbyrussell/oh-my-zsh.git $(HOME)/.oh-my-zsh
	rm -rf $(HOME)/.oh-my-zsh/custom
	ln -sfn $(CURDIR)/zsh/custom/themes $(HOME)/.oh-my-zsh/custom/themes
	ln -sfn $(CURDIR)/zsh/zshrc $(HOME)/.zshrc
	ln -sfn $(CURDIR)/zsh/zshenv $(HOME)/.zshenv
	sudo chsh -s $(shell which zsh) $(shell whoami)
