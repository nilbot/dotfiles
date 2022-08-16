# apps... but better
if type -q (which bit)
    alias git=bit
end
if type -q (which nvim)
    alias vim=nvim
end
if type -q (which lsd)
    alias ls=lsd
end
if type -q (which gfind)
    alias find=gfind
end

# git
abbr -a gs  git status -sb
abbr -a ga  git add
abbr -a gc  git commit
abbr -a gcm git commit -m
abbr -a gca git commit --amend
abbr -a gcl git clone
abbr -a gco git checkout
abbr -a gp  git push
abbr -a gpl git pull
abbr -a gl  git l
abbr -a gd  git diff
abbr -a gds git diff --staged
abbr -a gr  git rebase -i HEAD~15
abbr -a gf  git fetch
abbr -a gfc git findcommit
abbr -a gfm git findmessage
abbr -a gco git checkout

# ls
alias l='ls -l'
alias la='ls -a'
alias lla='ls -la'
alias lt='ls --tree'

# misc
alias reload='exec fish'

# brew
if type -q (which brew)
    alias bubo='brew update && brew outdated'
    alias bubc='brew upgrade && brew cleanup'
    alias bubu='bubo && bubc'
end

# macOS
## tailscale cli
if type -q /Applications/Tailscale.app/Contents/MacOS/Tailscale
    alias tailscale="/Applications/Tailscale.app/Contents/MacOS/Tailscale"
end
