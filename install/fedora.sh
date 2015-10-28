if [ `uname -r | grep -o fc2 | wc -l` ]; then
    # install some essential packages for brew 
    sudo dnf -y groupinstall "Development Tools"
    sudo dnf -y install gcc-g++ patch 

    # install linuxbrew
    git clone git@github.com:Homebrew/linuxbrew.git $HOME/.linuxbrew
    export BREWHOME=$HOME/.linuxbrew
    export PATH=$BREWHOME/bin:$PATH
    ln -s $(which gcc) $BREWHOME/bin/gcc-5
    ln -s $(which g++) $BREWHOME/bin/g++-5
    
    brew install pkg-config
	source $HOME/dotfiles/install/brew.sh

    mkdir -p $HOME/.config/nvim
    ln -s $HOME/dotfiles/install/nvim.fedora $HOME/.config/nvim/init.vim
fi
