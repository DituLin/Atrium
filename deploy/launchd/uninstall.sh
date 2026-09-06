#!/usr/bin/env bash
# Remove the Atrium LaunchAgent. The data directory is never touched: photos,
# pairings and the admin token survive an uninstall.

set -euo pipefail

LABEL="${ATRIUM_LABEL:-com.atrium.core}"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"

usage() {
    cat <<USAGE
Usage: uninstall.sh [--label NAME]

Stops and removes the LaunchAgent named NAME (default $LABEL).
The data directory and its contents are left untouched.
USAGE
}

while [ $# -gt 0 ]; do
    case "$1" in
        --label)   LABEL="$2"; PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"; shift 2 ;;
        -h|--help) usage; exit 0 ;;
        *)         echo "unknown argument: $1" >&2; usage; exit 2 ;;
    esac
done

DOMAIN="gui/$(id -u)"

if launchctl print "$DOMAIN/$LABEL" >/dev/null 2>&1; then
    launchctl bootout "$DOMAIN/$LABEL"
    echo "stopped $LABEL"
else
    echo "$LABEL is not loaded"
fi

if [ -f "$PLIST" ]; then
    rm -f "$PLIST"
    echo "removed $PLIST"
else
    echo "no plist at $PLIST"
fi

echo "the data directory was not removed"
