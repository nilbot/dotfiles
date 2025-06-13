# >>> conda initialize >>>
# !! Contents within this block are managed by 'conda init' !!
# if test -f $HOME/sdk/mambaforge/bin/conda
#     eval $HOME/sdk/mambaforge/bin/conda "shell.fish" "hook" $argv | source
# end

# if test -f "$HOME/sdk/mambaforge/etc/fish/conf.d/mamba.fish"
#     source "$HOME/sdk/mambaforge/etc/fish/conf.d/mamba.fish"
# end
# <<< conda initialize <<<

# >>> mamba initialize >>>
# !! Contents within this block are managed by 'mamba init' !!
if test -x /opt/homebrew/bin/micromamba
    set -gx MAMBA_EXE /opt/homebrew/bin/micromamba
    set -gx MAMBA_ROOT_PREFIX "$HOME/sdk/mambaforge"
    $MAMBA_EXE shell hook --shell fish --root-prefix $MAMBA_ROOT_PREFIX | source
end
# <<< mamba initialize <<<

# >>> mojo >>>
if test -d "$HOME/.modular"
    set -gx MODULAR_HOME $HOME/.modular
    fish_add_path "$MODULAR_HOME/bin"
    fish_add_path "$MODULAR_HOME/pkg/packages.modular.com_mojo/bin"
end
# <<< mojo <<<

# >>> eve frontier foundry >>>
if test -d "$HOME/.foundry/bin"
    fish_add_path -a /Users/nilbot/.foundry/bin
end
