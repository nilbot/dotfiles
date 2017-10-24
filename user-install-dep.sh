#!/bin/bash

if [[ "$(uname)" == "Linux" -a $(echo $UID) != 0 ]]; then
		ruby -e "$(curl -fsSL https://raw.githubusercontent.com/Linuxbrew/install/master/install)"
elif [[ "$(uname)" == "Darwin" ]]; then
		echo "installing prerequisite softwares using brew..."
		/usr/bin/ruby -e "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/master/install)"
		echo "installing packages..."
		source brew/brew.tap
		while read -r pkg
		do
				brew install "$pkg"
		done < brew/brew.list
		while read -r pkg
		do
				brew cask install "$pkg"
		done < brew/brew.cask.list
fi
