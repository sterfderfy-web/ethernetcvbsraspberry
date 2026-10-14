#!/bin/bash
# Run as the checkout owner; sudo is used only by the installer.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
command -v git >/dev/null || { echo 'Install git first: sudo apt-get install git'; exit 1; }
[[ -d .git ]] || { echo 'Run update.sh from a git clone of the project'; exit 1; }
[[ -z $(git status --porcelain) ]] || { echo 'Local changes found; commit or stash them before updating'; exit 1; }
[[ $(git branch --show-current) == main ]] || { echo 'Switch to main before updating'; exit 1; }
git fetch origin main
git merge --ff-only origin/main
# Preserve device.env, image settings, network and radios.
if [[ $EUID -eq 0 ]]; then
    bash install.sh --keep-network --keep-radios
else
    sudo bash install.sh --keep-network --keep-radios
fi
echo "Installed revision: $(git rev-parse --short HEAD)"
