#!/usr/bin/env bash
# recover my machine from snapshots
archive=$HOME/archive
cd $archive
cat *.tar.gz* | tar -C $HOME -xzf -
echo "done"
# recover my machine from snapshots done

