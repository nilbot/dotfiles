# apps... but better
if type -q (which bit)
    abbr -a git bit
end
if type -q (which nvim)
    abbr -a vim nvim
end
if type -q (which lsd)
    abbr -a ls lsd
end
if type -q (which fd)
    abbr -a find fd
end
if type -q (which micromamba)
    abbr -a mamba micromamba
    abbr -a conda micromamba
end
if type -q (which uv)
    abbr -a pip 'uv pip'
    abbr -a venv 'uv venv'
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
abbr -a l 'ls -l'
abbr -a la 'ls -a'
abbr -a lla 'ls -la'
abbr -a lt 'ls --tree'

# misc
abbr -a reload 'exec fish'

# brew
if type -q (which brew)
    abbr -a bubo 'brew update && brew outdated'
    abbr -a bubc 'brew upgrade && brew cleanup'
    abbr -a bubu 'bubo && bubc'
end

# macOS
## tailscale cli (deprecated because using standalone tailscale)
# if type -q /Applications/Tailscale.app/Contents/MacOS/Tailscale
#     abbr -a tailscale "/Applications/Tailscale.app/Contents/MacOS/Tailscale"
# end
