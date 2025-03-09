#!/usr/bin/env bash
set -e
CURRENT_FILE_CALLED_PATH=$0
RELATIVE_PATH=$(dirname "$CURRENT_FILE_CALLED_PATH")
ABS_PATH=$(cd "$RELATIVE_PATH" && pwd)

echo "Creating filebrowser host location at $HOME/share..."
mkdir -p $HOME/share/inbox


echo "Setting up filebrowser to start at boot..."
echo "This script will create a launch agent to start filebrowser at boot."

cat $ABS_PATH/dev.nilbot.filebrowser.plist | sed "s|!ABS_PATH|${ABS_PATH}|g" > $HOME/Library/LaunchAgents/dev.nilbot.filebrowser.plist



launchctl unload $HOME/Library/LaunchAgents/dev.nilbot.filebrowser.plist
launchctl load $HOME/Library/LaunchAgents/dev.nilbot.filebrowser.plist

echo "Filebrowser will now start at boot."
echo "To remove it, run:"
echo "launchctl unload $HOME/Library/LaunchAgents/dev.nilbot.filebrowser.plist"
echo "rm $HOME/Library/LaunchAgents/dev.nilbot.filebrowser.plist"
