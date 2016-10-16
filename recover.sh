#!/usr/bin/env bash
# recover my machine from snapshots
archive=$HOME/archive
cd "$archive" || exit 1
cat ./*.tar.gz* | tar -C "$HOME" -xzf -
echo "done"
# recover my machine from snapshots done
