.PHONY: all bin dotfiles etc

all: links themes vim extra omz

INSTALL_DIR=$(shell find $(CURDIR))

links:
	for dir in $(shell find $(INSTALL_DIR)/dot -type d -name ".*"); \
	do \
		target=$$(basename $$dir) \
		ln -sfn $$dir $(HOME)/$$target \
	done;
	ln -sfn $(INSTALL_DIR)/gnupg/gpg.conf $(HOME)/.gnupg/gpg.conf;
	ln -sfn $(INSTALL_DIR)/gnupg/gpg-agent.conf $(HOME)/.gnupg/gpg-agent.conf;

themes:

vim:

extra:


omz:
	git clone https://github.com/robbyrussell/oh-my-zsh.git $(HOME)/.oh-my-zsh
	ln -sfn $(INSTALL_DIR)/zsh/zshrc $(HOME)/.zshrc
	ln -sfn $(INSTALL_DIR)/zsh/zshenv $(HOME)/.zshenv