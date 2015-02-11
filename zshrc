# use antigen
source $HOME/dotfiles/antigen/antigen.zsh

# conf antigen
antigen-use oh-my-zsh

antigen-bundle command-not-found
antigen-bundle colored-man

antigen-bundle zsh-users/zsh-completions

antigen-bundle git
antigen-bundle pip

antigen-bundle zsh-users/zsh-syntax-highlighting

antigen-bundle phing
antigen-bundle composer
antigen-bundle git-extras
antigen-bundle npm
antigen-bundle brew


# theme
antigen-theme sorin

is_linux () {
    	[[ $('uname') == 'Linux' ]];
}

is_osx () {
	[[ $('uname') == 'Darwin' ]]
}

if is_osx; then
	antigen-bundle osx
fi

antigen-bundle zsh-users/zsh-history-substring-search

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
antigen-apply

# User configuration


export GREP_OPTIONS='--color=always'
export GOPATH="$HOME/go"
export GOROOT="/usr/local/opt/go/libexec"
export PLAN9="/usr/local/plan9"
export PATH="$PATH:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:/opt/X11/bin:/usr/local/sbin:$GOROOT/bin:$GOPATH/bin:$PLAN9/bin"

# export MANPATH="/usr/local/man:$MANPATH"
alias goapp="$HOME/devel/google-cloud-sdk/platform/google_appengine/goapp"


source '/Users/nilbot/devel/google-cloud-sdk/path.zsh.inc'

