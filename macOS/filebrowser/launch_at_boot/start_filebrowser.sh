#!/usr/bin/env bash
set -e
cd $HOME && /opt/homebrew/bin/filebrowser -r $HOME/share/inbox/ -d $HOME/share/filebrowser.db -a $(ifconfig | grep "inet " | grep -Fv 127.0.0.1 | grep -Fv 100. | awk '{print $2}') -p 23333
