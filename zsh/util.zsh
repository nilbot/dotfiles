# socks proxy
function socks() {
    export SOCKS_SERVER="127.0.0.1:58124"
    export http_proxy="socks://$SOCKS_SERVER" https_proxy="socks://$SOCKS_SERVER"
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

# nvim instead of vim
if [ -e /usr/local/bin/nvim ]; then
    alias vim='nvim'
fi

# my dl work related aliases
function pytorch-daemon() {
    nvidia-docker run -d --name torch-jupyter -p 8888:8888 -p 6006:6006 --ipc=host -v `pwd`:/workspace -e PASSWORD=$PASSWORD nilbot/pytorch-book:v0.3.0t
}
