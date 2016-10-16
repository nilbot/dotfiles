#!/usr/bin/env bash

if [[ "$(uname)" == "Darwin" ]];then
    echo "installing prerequisite softwares using brew..."
    /usr/bin/ruby -e "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/master/install)"
    echo "installing packages..."
    source brew/brew.tap
    for pkg in $(cat brew/brew.list); do
        brew install $pkg
    done
    for pkg in $(cat brew/brew.cask.list); do
        brew cask install $pkg
    done
elif [[ "$(uname -r)" == *"ARCH" ]]; then
    echo "installing prerequisite softwares using pacman and AUR..."
    pacman -S --noconfirm base-devel
    cat <<'EOF' >> /etc/pacman.conf
[archlinuxfr]
SigLevel = Never
Server = https://repo.archlinux.fr/$arch
EOF
    pacman -Syy && pacman -S --noconfirm yaourt
    pacman -S --noconfirm gnupg python ruby git go shellcheck
else
    echo "Your platform is not yet supported. Install the softwares manually please."
    exit 127
fi
exit 0
