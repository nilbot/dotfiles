# >>> fisher plugin manager >>>
function install_fisher
    set --local plugins (read --null <$HOME/dotfiles/fish/fishfile)
    curl -sL https://git.io/fisher | source && fisher install jorgebucaran/fisher $plugins
end
# <<< fisher plugin manager <<<

# wsl systemd and ssh hack with genie, need to be run first
switch (uname -r)
case "*microsoft*"
    if ! set -q INSIDE_GENIE; and test -x /usr/bin/genie
        exec /usr/bin/genie -s
    end
    if set -q SSH_CLIENT; and set -q INSIDE_GENIE
        eval (systemctl show-environment | awk -F '=' '{print "set -x "$1" "$2";"}')
    end
case "*"
    # do nothing
end

# base
set base_env /usr/local/bin /usr/local/sbin /usr/bin /bin /usr/sbin /sbin
fish_add_path $base_env
if type -q /usr/local/go/bin/go # manual go installation on linux machines
    fish_add_path /usr/local/go/bin
end


# user base
set user_base_env $HOME/bin $HOME/.config/bin
fish_add_path $user_base_env

# brew base
switch (uname -s)
    case Linux
        set brew_bin_path /home/linuxbrew/.linuxbrew/bin
    case Darwin
        set brew_bin_path /opt/homebrew/bin
    case "*"
        set brew_bin_path /usr/local/bin
end
fish_add_path $brew_bin_path

# pyenv
command -sq pyenv
if test $status -eq 0
    set -gx PYENV_ROOT $HOME/.pyenv
    fish_add_path $PYENV_ROOT/bin
    status is-login; and pyenv init --path | source
    status is-interactive; and pyenv init - | source
end

# fzf
command -q fzf
if test $status -eq 0
    fzf --fish | source
end

# go
set -gx GOPATH $HOME

# rust
if test -d $HOME/.cargo/bin
    fish_add_path $HOME/.cargo/bin
end

# gcloud
if test -d $HOME/sdk/google-cloud-sdk
    # source $HOME/sdk/google-cloud-sdk/path.fish.inc
    # NOT USING official google cloud sdk fish path script
    # Why? The author claims they don't really understand fish.
    # and apparently sourcing this file doesn't work across sessions.
    fish_add_path $HOME/sdk/google-cloud-sdk/bin
end
if test (uname) = "Darwin"
    set -x CLOUDSDK_PYTHON (which python)
end

# plan9
set -gx PLAN9 /usr/local/plan9
fish_add_path -maP $PLAN9/bin

# postgres (on macOS)
if test -d /Applications/Postgres.app/Contents/Versions/latest/bin
    fish_add_path /Applications/Postgres.app/Contents/Versions/latest/bin
end

# TODO(n)
# java
# scala
if test -d "$HOME/Library/Application Support/Coursier/bin"
    fish_add_path "$HOME/Library/Application Support/Coursier/bin"
end
# android
if test (uname) = "Darwin"
    set -gx JAVA_HOME (/usr/libexec/java_home)
end
if test -d $HOME/Library/Android
    set -gx ANDROID_SDK_ROOT $HOME/Library/Android/sdk
    set -gx ANDROID_NDK_ROOT $HOME/Library/Android/ndk
    fish_add_path $ANDROID_SDK_ROOT/platform-tools
end

# cocoapods
if test (uname -s) = "Darwin"
    set -gx GEM_HOME $HOME/.gem
    fish_add_path $GEM_HOME/bin
end

# dart; not required when installed via homebrew
if test -d /usr/local/opt/dart
    fish_add_path /usr/local/opt/dart/libexec/bin
end
if test -d $HOME/.pub-cache/bin
    fish_add_path $HOME/.pub-cachee/bin
end

# dotnet
if test -d /usr/local/share/dotnet
    fish_add_path /usr/local/share/dotnet
end

# flutter; not required when installed via homebrew
if test -d $HOME/sdk/flutter/bin
    fish_add_path $HOME/sdk/flutter/bin
end

# misc. bin
if test -d $HOME/.local/bin
    fish_add_path $HOME/.local/bin
end

# misc. functions
function space
    du -ch | grep total
end

function prunelinks
    find . -type l -exec test ! -e {} \; -delete
end

# github download latest release
# this function downloads the first link
#   - usage: github-dl owner repo
function github-dl
    set org $argv[1]; set repo $argv[2];
    curl -s https://api.github.com/repos/"$org"/"$repo"/releases/latest \
    | grep browser_download_url \
    | cut -d '"' -f 4 \
    | head -n 1 \
    | wget -qi -
end

# func readenv for .env files
function readenv --on-variable PWD
    if test -r .env
        cat .env | while read -l line
            # split on the first =, and set the variable, but only if the line is not empty and the starting character is not #
            if test -n $line; and test (string sub -s 1 -l 1 $line) != "#"
                set -l kv (string split -m 1 = -- $line)
                set -gx $kv # this will set the variable named by $kv[1] to the rest of $kv
            end
        end
   end
end

# bit is git client, but better
set -Ux COMP_POINT 1

# gpg
set -gx GPG_TTY (tty)

# tmux shell var
if test -n (which fish)
    set -gx TMUX_SHELL_VAR (which fish)
end

# remove fish-variables
# it turns out that fish-variables resides in local,
# and it's not cross-platform compatible (eg. macOS vs linux)
function fish_reset_all
    set --local fish_path_prefix $HOME/dotfiles/fish
    echo '' > $fish_path_prefix/fish_plugins
    echo '' > $fish_path_prefix/fish_variables
    rm -rf $fish_path_prefix/{functions,completions,conf.d}/
    install_fisher
    reload
end

# vscode open in temrinal
# macOS only for now
if test -d "/Applications/Visual Studio Code.app/"
    fish_add_path "/Applications/Visual Studio Code.app/Contents/Resources/app/bin"
end

# rbenv (homebrew)
command -sq rbenv
if test $status -eq 0
    status --is-interactive; and source (rbenv init -| psub)
end

# Background: tidb, tiup, macOS, arm64 setup
# include tidb bin path
if test -d $HOME/.tiup/bin
    set TIUP_BIN_PATH $HOME/.tiup/bin
    fish_add_path $TIUP_BIN_PATH
end

# proxy
# deprecated: using clash is way better than setting these up and cleaning up
# set prefix http https ftp rsync all
# set postfix _proxy
# set prefix_u (string upper $prefix)
# set postfix_u (string upper $postfix)
# set host 127.0.0.1
# set port 1080
# set protocol_header socks5h://

# function proxon
#     for name in $prefix$postfix
#         set -gx $name $protocol_header$host:$port
#     end
#     for name in $prefix_u$postfix_u
#         set -gx $name $protocol_header$host:$port
#     end
#     networksetup -setsocksfirewallproxy wi-fi $host $port
#     # networksetup -setwebproxy wi-fi $host $port
#     # networksetup -setsecurewebproxy wi-fi $host $port
#     networksetup -setsocksfirewallproxystate wi-fi on
#     # networksetup -setwebproxystate wi-fi on
#     # networksetup -setsecurewebproxystate wi-fi on
# end
# function proxoff
#     for name in $prefix$postfix
#         set -e $name
#     end
#     for name in $prefix_u$postfix_u
#         set -e $name
#     end
#     networksetup -setsocksfirewallproxystate wi-fi off
#     networksetup -setwebproxystate wi-fi off
#     networksetup -setsecurewebproxystate wi-fi off
# end


# bun
if test -d $HOME/.bun
    set -gx BUN_INSTALL $HOME/.bun
    fish_add_path $BUN_INSTALL/bin
end


# Add LM Studio CLI (lms)
if test -d $HOME/.cache/lm-studio
    set -gx LMSTUDIO_INSTALL $HOME/.cache/lm-studio
    fish_add_path $LMSTUDIO_INSTALL/bin
end

# Add Modular CLI (modular / magic)
if test -d $HOME/.modular
    set -gx MODULAR_INSTALL $HOME/.modular
    fish_add_path $MODULAR_INSTALL/bin
end
