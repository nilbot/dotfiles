#!/usr/bin/env bash
set -e
cd $HOME && /opt/homebrew/bin/filebrowser -r $HOME/share/inbox/ -a 127.0.0.1 -p 23333
