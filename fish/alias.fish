# apps... but better
if type -q (command -v bit)
    alias git=bit
end
if type -q (command -v nvim)
    alias vim=nvim
end
if type -q (command -v lsd)
    alias ls=lsd
end
if type -q (command -v fd)
    alias find=fd
end
if type -q (command -v micromamba)
    alias mamba=micromamba
    alias conda=micromamba
end
if type -q (command -v uv)
    alias pip='uv pip'
    alias venv='uv venv'
end

# git
abbr -a gs git status -sb
abbr -a ga git add
abbr -a gc git commit
abbr -a gcm git commit -m
abbr -a gca git commit --amend
abbr -a gcl git clone
abbr -a gco git checkout
abbr -a gp git push
abbr -a gpl git pull
abbr -a gl git l
abbr -a gd git diff
abbr -a gds git diff --staged
abbr -a gr git rebase -i HEAD~15
abbr -a gf git fetch
abbr -a gfc git findcommit
abbr -a gfm git findmessage
abbr -a gco git checkout

# ls
alias l='ls -l'
alias la='ls -a'
alias lla='ls -la'
alias lt='ls --tree'

# brew
if type -q (command -v brew)
    alias bubo='brew update && brew outdated'
    alias bubc='brew upgrade && brew cleanup'
    alias bubu='bubo && bubc'
end

# macOS
## tailscale cli (deprecated because using standalone tailscale)
# if type -q /Applications/Tailscale.app/Contents/MacOS/Tailscale
#     alias tailscale="/Applications/Tailscale.app/Contents/MacOS/Tailscale"
# end
