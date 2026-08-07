#!/usr/bin/env bash
set -e
cd $HOME && /opt/homebrew/bin/filebrowser -r $HOME/share/inbox/ -d $HOME/share/filebrowser.db -a 0.0.0.0 -p 23333
