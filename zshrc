# use antigen
source $HOME/dotfiles/antigen/antigen.zsh

# conf antigen
antigen use oh-my-zsh
antigen bundles <<EOBUNDLES

zsh-users/zsh-completions

git
git-extras

zsh-users/zsh-syntax-highlighting

npm

osx
brew

EOBUNDLES

# theme
antigen theme sorin

# antigen apply
antigen apply

# User configuration


export GREP_OPTIONS='--color=always'
export GOPATH="$HOME/go"
export GOROOT="/usr/local/opt/go/libexec"
export PLAN9="/usr/local/plan9"
export PATH="$PATH:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:/opt/X11/bin:/usr/local/sbin:$GOROOT/bin:$GOPATH/bin:$PLAN9/bin"

# export MANPATH="/usr/local/man:$MANPATH"
alias goapp="$HOME/devel/google-cloud-sdk/platform/google_appengine/goapp"


source "$HOME/devel/google-cloud-sdk/path.zsh.inc"

