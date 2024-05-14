#!/bin/bash

if [[ "$(uname)" == "Linux" && $(echo $UID) != 0 ]]; then
		ruby -e "$(curl -fsSL https://raw.githubusercontent.com/Linuxbrew/install/master/install)"
elif [[ "$(uname)" == "Darwin" ]]; then
		echo "installing prerequisite softwares using brew..."
		/usr/bin/ruby -e "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/master/install)"
		echo "installing packages..."
		while read -r pkg
		do
				brew install "$pkg"
		done < brew/brew-frameworks.list
		while read -r pkg
		do
				brew install "$pkg"
		done < brew/brew-utils.list
		while read -r pkg
		do
				brew cask install "$pkg"
		done < brew/brew-cask.list
fi
