#!/usr/bin/env bash

if [[ "$(uname)" == "Darwin" ]];then
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
elif [[ "$(uname -r)" == *"ARCH" ]]; then
    echo "installing prerequisite softwares using pacman and AUR..."
    pacman -S --noconfirm base-devel
    cat <<'EOF' >> /etc/pacman.conf
[archlinuxfr]
SigLevel = Never
Server = https://repo.archlinux.fr/$arch
EOF
    pacman -Syy && pacman -S --noconfirm yaourt
    pacman -S --noconfirm gnupg python ruby git go shellcheck zsh
else
    echo "Your platform is not yet supported. Install the softwares manually please."
    exit 127
fi
exit 0
