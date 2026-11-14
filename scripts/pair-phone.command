#!/bin/bash
# A NEW optional launcher, not an installer or replacement for any Desktop app.
set -euo pipefail
umask 077
project="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd -- "$project"
if [[ ! -f private/invite.private.json ]]; then
    printf '%s\n' 'Create private/invite.private.json with public_origin (your HTTPS origin) first.' 'See docs/PAIRING.md. No credentials are discovered by this launcher.'
    read -r -p 'Press Return to close. ' _
    exit 1
fi
# Compile/check before the owner opens a short-lived pairing window.
while true; do
    if python3 tools/show-invite.py --preflight; then break; else code=$?; fi
    if [[ "$code" != 2 ]]; then exit 1; fi
    read -r -p 'Enlarge this terminal, then Return to check again (q to quit): ' reply
    [[ "$reply" == q || "$reply" == Q ]] && exit 0
done
printf '%s\n' 'The fresh private link and a QR will be printed in this terminal; do not record or publish it.'
printf '%s\n' 'Keep Windows, the gateway and phone awake and connected.' 'The original app, profiles and existing Desktop launcher will not be changed.' 'First open the native phone app Paste link field.'
read -r -p 'Press Return only when the PHONE is ready to paste: ' _
printf '%s\n' 'Now open a FRESH genuine native owner pairing window and transfer its screenshot privately.' 'The native invitation has a short lifetime; do not wait after generating it.'
read -r -p 'Fresh screenshot path (relative to this project or absolute; no shell quotes): ' screenshot
if [[ -z "$screenshot" || ! -f "$screenshot" ]]; then
    printf '%s\n' 'Screenshot file not found.'
    exit 1
fi
read -r -p 'Copy the resulting private link to the Mac clipboard? [y/N] ' consent
# A new gitignored destination on each attempt; do not spend invite lifetime editing settings.
output="private/invites/session-$(date +%Y%m%dT%H%M%S)-${RANDOM}"
set -- tools/make-https-invite.py "$screenshot" --config private/invite.private.json --output-dir "$output"
if [[ "$consent" == y || "$consent" == Y ]]; then set -- "$@" --copy; fi
if ! python3 "$@"; then
    printf '%s\n' 'Invite was not verified. Do not use partial output; inspect privately.'
    read -r -p 'Press Return to close. ' _
    exit 1
fi
if [[ -x /usr/bin/afplay && -f /System/Library/Sounds/Glass.aiff ]]; then
    /usr/bin/afplay /System/Library/Sounds/Glass.aiff || true
fi
printf '\033[2J\033[H'
if ! python3 tools/show-invite.py "$output/invite.txt"; then
    printf '%s\n' 'Private output was created, but terminal display needs attention. Inspect the diagnostic above.'
fi
printf '%s\n' 'SCAN OR PASTE NOW before the native expiry. Then clear sensitive clipboard/terminal output.'
read -r -p 'Press Return to close after scanning/pasting: ' _
