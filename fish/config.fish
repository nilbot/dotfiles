source ~/.config/fish/alias.fish
source ~/.config/fish/myenv.fish
if test -d ~/etc/extras.secret
    source ~/etc/extras.secret/gitconfig.extra.fish
end

# Configure Jump
status --is-interactive; and source (jump shell fish | psub)

# Load all saved ssh keys
/usr/bin/ssh-add -A ^/dev/null

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


if test -d "$HOME"/miniforge3
    eval "$HOME"/miniforge3/bin/conda "shell.fish" "hook" $argv | source
else if test -d /opt/homebrew/Caskroom/miniforge/base
    eval /opt/homebrew/Caskroom/miniforge/base/bin/conda "shell.fish" "hook" $argv | source
end

