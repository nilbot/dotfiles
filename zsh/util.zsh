# socks proxy
function socks() {
    local_socks5=http://127.0.0.1:58173
    export http_proxy=$local_socks5 https_proxy=$local_socks5
}
function usocks() unset http_proxy https_proxy

# calculate space for current folder
function space() du -ch | grep total

# visual studio code (mac osx only)
function code() { VSCODE_CWD="$PWD" open -n -b "com.microsoft.VSCode" --args $* ;}

# findutils
if [[ -e /usr/local/bin/gfind  ]]; then
	alias find='gfind'
fi

if [[ $(hostname) == "airbot.modouwifi.com" ]]; then
    socks
fi
