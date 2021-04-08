#!/bin/bash

SYSTEM=`uname -s`
MACHINE=`uname -m`
CURDIR=`pwd`

if test $SYSTEM = "Darwin"; then
	rm -rf $HOME/.config/alacritty
	mkdir -p $HOME/.config/alacritty
	ln -s $CURDIR/macOS/alacritty/alacritty.yml $HOME/.config/alacritty/alacritty.yml
fi
