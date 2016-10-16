#!/usr/bin/env bash

cd $HOME
if [ -d $HOME/mnt/claritytrec ];then
	mkdir -p mnt/claritytrec
fi
sshfs -o allow_other,defer_permissions insight:. $HOME/mnt/claritytrec
