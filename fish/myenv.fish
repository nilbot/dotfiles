# base
set base_env /opt/homebrew/bin /usr/local/bin /usr/local/sbin /usr/bin /bin /usr/sbin /sbin
fish_add_path $base_env
# go
set -gx GOPATH $HOME
fish_add_path $GOPATH/bin
# rust
if test -d $HOME/.cargo/bin
    fish_add_path $HOME/.cargo/bin
end
# gcloud
if test -d $HOME/devel/google-cloud-sdk
    # source $HOME/devel/google-cloud-sdk/path.fish.inc
    # NOT USING official google cloud sdk fish path script
    # Why? The author claims they don't really understand fish.
    # and apparently sourcing this file doesn't work across sessions.
    fish_add_path $HOME/devel/google-cloud-sdk/bin
end
if test (uname) = "Darwin"
    set -x CLOUDSDK_PYTHON /usr/bin/python
end
# plan9
if test (uname -s) = "Darwin" -a (uname -m) = "arm64"
    set -gx PLAN9 /opt/plan9
else
    set -gx PLAN9 /usr/local/plan9
end
fish_add_path -maP $PLAN9/bin

# postgres (on macOS)
if test -d /Applications/Postgres.app/Contents/Versions/latest/bin
    fish_add_path /Applications/Postgres.app/Contents/Versions/latest/bin
end

# TODO(n)
# java
# android
if test (uname) = "Darwin"
    set -gx JAVA_HOME (/usr/libexec/java_home)
end
if test -d $HOME/Library/Android
    set -gx ANDROID_SDK_ROOT $HOME/Library/Android/sdk
    set -gx ANDROID_NDK_ROOT $HOME/Library/Android/ndk
    fish_add_path $ANDROID_SDK_ROOT/platform-tools
end

# dart
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

# miniconda (eval bugged)
if test -d $HOME/miniconda3/bin
    # eval $HOME/miniconda3/bin/conda "shell.fish" "hook" $argv | source
    # fish_add_path $HOME/miniconda3/bin
    # source $HOME/miniconda3/etc/fish/conf.d/conda.fish
end

# flutter
if test -d $HOME/Library/Frameworks/flutter/bin
    fish_add_path $HOME/Library/Frameworks/flutter/bin
end

# perl
if test -d $HOME/perl5
    eval (perl -I$HOME/perl5/lib/perl5 -Mlocal::lib=$HOME/perl5)
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

# bit is git client, but better
set -Ux COMP_POINT 1

# gpg
set -gx GPG_TTY (tty)

# tmux shell var
if test -n (which fish)
    set -gx TMUX_SHELL_VAR (which fish)
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
