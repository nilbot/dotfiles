# >>> conda initialize >>>
# !! Contents within this block are managed by 'conda init' !!
if test -f $HOME/sdk/mambaforge/bin/conda
    eval $HOME/sdk/mambaforge/bin/conda "shell.fish" "hook" $argv | source
end

if test -f "$HOME/sdk/mambaforge/etc/fish/conf.d/mamba.fish"
    source "$HOME/sdk/mambaforge/etc/fish/conf.d/mamba.fish"
end
# <<< conda initialize <<<

