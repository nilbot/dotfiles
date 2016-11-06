#!/bin/bash

if [[ "$(uname)" == "Darwin" ]];then
		xcode-select --install
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
elif [[ "$(uname -v)" =~ "Ubuntu" ]];then
		apt-get update && apt-get upgrade -y
		apt-get install -y build-essential
		apt-get install -y ruby zsh
else
		echo "Your platform is not yet supported. Install the softwares manually please."
		exit 127
fi
exit 0
