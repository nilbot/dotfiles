.PHONY: all bin dotfiles etc

all: prerequisite links themes vim extra omz

prerequisite:
	sudo ./super-install-dep.sh
	./user-install-dep.sh

links:
	for dir in $(shell find $(CURDIR)/dot/ -type d -name ".*" -not -name ".git"); do \
		target=$$(basename $$dir) \
		ln -sfn $$dir $(HOME)/$$target \
	done;
	ln -sfn $(CURDIR)/gnupg/gpg.conf $(HOME)/.gnupg/gpg.conf;
	ln -sfn $(CURDIR)/gnupg/gpg-agent.conf $(HOME)/.gnupg/gpg-agent.conf;
	ln -sf $(CURDIR)/spacemacs/dotspacemacs $(HOME)/.spacemacs;

themes:

vim:

extra:


omz:
	git clone https://github.com/robbyrussell/oh-my-zsh.git $(HOME)/.oh-my-zsh
	ln -sfn $(CURDIR)/zsh/custom $(HOME)/.oh-my-zsh/custom
	ln -sfn $(CURDIR)/zsh/zshrc $(HOME)/.zshrc
	ln -sfn $(CURDIR)/zsh/zshenv $(HOME)/.zshenv
	sudo chsh -s $(shell which zsh) $(shell whoami)
