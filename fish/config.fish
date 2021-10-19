source ~/.config/fish/alias.fish
source ~/.config/fish/myenv.fish
if test -d ~/etc/extras.secret
    source ~/etc/extras.secret/gitconfig.extra.fish
end

# Configure Jump
status --is-interactive; and source (jump shell fish | psub)

# Fish syntax highlighting
set -g fish_color_autosuggestion '555'  'brblack'
set -g fish_color_cancel -r
set -g fish_color_command --bold
set -g fish_color_comment red
set -g fish_color_cwd green
set -g fish_color_cwd_root red
set -g fish_color_end brmagenta
set -g fish_color_error brred
set -g fish_color_escape 'bryellow'  '--bold'
set -g fish_color_history_current --bold
set -g fish_color_host normal
set -g fish_color_match --background=brblue
set -g fish_color_normal normal
set -g fish_color_operator bryellow
set -g fish_color_param cyan
set -g fish_color_quote yellow
set -g fish_color_redirection brblue
set -g fish_color_search_match 'bryellow'  '--background=brblack'
set -g fish_color_selection 'white'  '--bold'  '--background=brblack'
set -g fish_color_user brgreen
set -g fish_color_valid_path --underline

# Install Starship
starship init fish | source


if test -d "$HOME"/sdk/miniforge3
    set CONDA_BIN "$HOME"/sdk/miniforge3/bin/conda 
else if test -d /opt/homebrew/Caskroom/miniforge/base
    set CONDA_BIN /opt/homebrew/Caskroom/miniforge/base/bin/conda
else if test -d "$HOME"/miniconda3
    set CONDA_BIN "$HOME"/miniconda3/bin/conda
end
if test -n "$CONDA_BIN"
    eval $CONDA_BIN "shell.fish" "hook" $argv | source
end


# Mamba
if test -d "$HOME/sdk/micromamba" -a -x "$HOME/bin/micromamba"
    set -gx MAMBA_EXE "$HOME/bin/micromamba"
    set -gx MAMBA_ROOT_PREFIX "$HOME/sdk/micromamba"
    eval "$MAMBA_EXE" shell hook --shell fish --prefix "$MAMBA_ROOT_PREFIX" | source
end
