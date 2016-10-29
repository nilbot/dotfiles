.PHONY: all bin dotfiles etc

all: prerequisite links themes extra omz

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

themes:

extra:
	mkdir -p $(CURDIR)/extras.secret/

omz:
	rm -rf $(HOME)/.oh-my-zsh
	git clone https://github.com/robbyrussell/oh-my-zsh.git $(HOME)/.oh-my-zsh
	rm -rf $(HOME)/.oh-my-zsh/custom
	ln -sfn $(CURDIR)/zsh/custom $(HOME)/.oh-my-zsh/custom
	ln -sfn $(CURDIR)/zsh/zshrc $(HOME)/.zshrc
	ln -sfn $(CURDIR)/zsh/zshenv $(HOME)/.zshenv
	sudo chsh -s $(shell which zsh) $(shell whoami)
vim:
	cd vim && git submodule update --init --recursive
	ln -sf $(CURDIR)/neovim/vimrc $(HOME)/.vimrc
neovim:
	cd neovim && git submodule update --init --recursive
	mkdir -p $(HOME)/.config && ln -sfn $(CURDIR)/neovim $(HOME)/.config/nvim
	ln -sf $(CURDIR)/neovim/vimrc $(HOME)/.config/nvim/init.vim

