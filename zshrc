# use antigen
source $HOME/dotfiles/antigen/antigen.zsh

# conf antigen
antigen use oh-my-zsh
antigen bundle <<EOBUNDLES

command-not-found
colored-man

zsh-users/zsh-completions

git
pip

zsh-users/zsh-syntax-highlighting

phing
composer
git-extras
npm
brew
EOBUNDLES

# theme
antigen theme sorin

is_linux () {
    	[[ $('uname') == 'Linux' ]];
}

is_osx () {
	[[ $('uname') == 'Darwin' ]]
}

if is_osx; then
	antigen bundle osx
fi

antigen bundle zsh-users/zsh-history-substring-search

if is_osx; then
	bindkey '^[[A' history-substring-search-up
	bindkey '^[[B' history-substring-search-down
elif is_linux; then
	zmodload zsh/terminfo
	bindkey "$terminfo[kcuu1]" history-substring-search-up
	bindkey "$terminfo[kcud1]" history-substring-search-down

	bindkey -M emacs '^P' history-substring-search-up
	bindkey -M emacs '^N' history-substring-search-down

	bindkey -M vicmd 'k' history-substring-search-up
	bindkey -M vicmd 'j' history-substring-search-down
fi

# antigen apply
antigen apply

# User configuration


export GREP_OPTIONS='--color=always'
export GOPATH="$HOME/gopath"
export GOROOT="/usr/local/opt/go/libexec"
export PLAN9="/usr/local/plan9"
export PATH="$PATH:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:/opt/X11/bin:/usr/local/sbin:$GOROOT/bin:$GOPATH/bin:$PLAN9/bin"

# export MANPATH="/usr/local/man:$MANPATH"
alias goapp="$HOME/devel/google-cloud-sdk/platform/google_appengine/goapp"


source '/Users/nilbot/devel/google-cloud-sdk/path.zsh.inc'

