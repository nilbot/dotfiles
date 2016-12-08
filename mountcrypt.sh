#!/bin/bash

if [[ $(uname) == "Darwin" ]];then
	ENCFS="/usr/local/bin/encfs"
	ENCDIR="$HOME/.crypt"
	DECDIR="$HOME/crypt"
	security find-generic-password -ga encfs 2>&1 >/dev/null | cut -d'"' -f2 | "$ENCFS" -S "$ENCDIR" "$DECDIR"
fi
