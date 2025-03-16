#!/bin/bash

SYSTEM=`uname -s`
MACHINE=`uname -m`
CURDIR=`pwd`

if test $SYSTEM = "Darwin"; then
	rm -rf $HOME/.config/alacritty
	mkdir -p $HOME/.config/alacritty
	ln -s $CURDIR/macOS/alacritty/alacritty.toml $HOME/.config/alacritty/alacritty.toml
fi

if test $SYSTEM = "Darwin"; then
	rm -rf $HOME/.config/ghostty
	ln -s $CURDIR/macOS/ghostty $HOME/.config/ghostty
fi

