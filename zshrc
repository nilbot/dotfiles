# use antigen
# source $HOME/dotfiles/antigen/antigen.zsh

# conf antigen
# antigen use prezto
# antigen bundles <<EOBUNDLES

# zsh-users/zsh-completions

# git
# git-extras

# zsh-users/zsh-syntax-highlighting

# npm

# osx
# brew

# EOBUNDLES

# theme
# antigen theme sorin

# antigen apply
# antigen apply

# Use prezto
if [[ ! -d $HOME/.zprezto ]]; then
  ln -s $HOME/dotfiles/prezto $HOME/.zprezto
  ln -s $HOME/dotfiles/zshenv $HOME/.zshenv
  ln -s $HOME/dotfiles/zlogin $HOME/.zlogin
  ln -s $HOME/dotfiles/zlogout $HOME/.zlogout
  ln -s $HOME/dotfiles/zpreztorc $HOME/.zpreztorc
  ln -s $HOME/dotfiles/zprofile $HOME/.zprofile
fi

if [[ -s "${ZDOTDIR:-$HOME}/.zprezto/init.zsh" ]]; then
  source "${ZDOTDIR:-$HOME}/.zprezto/init.zsh"
fi

# User configuration


export GREP_OPTIONS='--color=always'
export GOPATH="$HOME/_workspace"
export GOROOT="/usr/local/opt/go/libexec"
export PLAN9="/usr/local/plan9"
export PATH="$PATH:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:/opt/X11/bin:/usr/local/sbin:$GOROOT/bin:$GOPATH/bin:$PLAN9/bin"

# socks proxy
function socks() export http_proxy=socks5://127.0.0.1:1080 https_proxy=socks5://127.0.0.1:1080
function usocks() unset http_proxy https_proxy

# export MANPATH="/usr/local/man:$MANPATH"
alias goapp="$HOME/devel/google-cloud-sdk/platform/google_appengine/goapp"


source "$HOME/devel/google-cloud-sdk/path.zsh.inc"

