#!/usr/bin/env bash
## snapshot my machine
cd $HOME
today=`date -j "+-%a_%b_%d_%Hh%Mm%Ss_%Z_%Y"`
archive="$HOME/archive"
mkdir -p $archive
echo "tarring dotfiles"
tar czf $archive/dotfiles$today.tar.gz dotfiles
echo "tarring etc"
tar czf $archive/etc$today.tar.gz etc
echo "tarring configs"
tar czf $archive/config$today.tar.gz .ackrc .appcfg* .aws .config .exercism.json .dropbox .git* .gnupg .gsutil .iterm* .lego .license .oh* .rclone.conf .profile .slackcat .ssh .tooling .travis .vscode .wegorc .zshrc
echo "tarring src"
tar czf - src | split -b 200m - $archive/src$today.tar.gz.
echo "tarring devel"
tar czf - devel | split -b 200m - $archive/devel$today.tar.gz.
echo "tarring utilities"
tar czf $archive/util$today.tar.gz bin game themes
echo "tarring documents"
tar czf - Desktop Downloads Documents Movies Music Pictures | split -b 200m - $archive/dirty_secrets$today.tar.gz.

echo "moving tar snapshots to archive"
rclone sync $archive ucdrive:/archive/mac --log-file=$HOME/upload_archive.log

echo "done"
# snapshot my machine done

