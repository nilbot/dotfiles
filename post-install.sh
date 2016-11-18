#!/bin/bash

if [[ $(uname) == "Linux" ]]; then
	brew install git go ncurses vim emacs
	go get -u github.com/monochromegane/the_platinum_searcher/...
fi
if [[ $(name -v) =~ "Ubuntu" ]]; then

elif [[ $(uname -r) =~ "ARCH" ]]; then

fi
