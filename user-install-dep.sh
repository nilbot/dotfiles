#!/bin/bash
if [[ "$(uname -r)" == *"ARCH" ]]; then
	yaourt -S --noconfirm the_platinum_searcher
fi
