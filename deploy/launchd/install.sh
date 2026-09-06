#!/usr/bin/env bash
# Install Atrium as a per-user LaunchAgent.
#
# The service starts after the user logs in, which is what the NAS mount needs:
# an SMB share mounted in a user session is not visible to a LaunchDaemon.
# See deploy/README.md for the auto-login and FileVault trade-offs.

set -euo pipefail

LABEL="${ATRIUM_LABEL:-com.atrium.core}"
BINARY="${ATRIUM_BINARY:-/usr/local/bin/atrium}"
CONFIG="${ATRIUM_CONFIG:-$HOME/.config/atrium/config.yaml}"
DATA_DIR="${ATRIUM_DATA_DIR:-$HOME/Library/Application Support/Atrium}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEMPLATE="$SCRIPT_DIR/com.atrium.core.plist.tmpl"
AGENTS_DIR="$HOME/Library/LaunchAgents"
PLIST="$AGENTS_DIR/$LABEL.plist"

usage() {
    cat <<USAGE
Usage: install.sh [--binary PATH] [--config PATH] [--data-dir PATH] [--label NAME]

Environment variables ATRIUM_BINARY, ATRIUM_CONFIG, ATRIUM_DATA_DIR and
ATRIUM_LABEL set the same values. Defaults:

  binary    $BINARY
  config    $CONFIG
  data dir  $DATA_DIR
  label     $LABEL
USAGE
}

while [ $# -gt 0 ]; do
    case "$1" in
        --binary)   BINARY="$2"; shift 2 ;;
        --config)   CONFIG="$2"; shift 2 ;;
        --data-dir) DATA_DIR="$2"; shift 2 ;;
        --label)    LABEL="$2"; PLIST="$AGENTS_DIR/$LABEL.plist"; shift 2 ;;
        -h|--help)  usage; exit 0 ;;
        *)          echo "unknown argument: $1" >&2; usage; exit 2 ;;
    esac
done

if [ ! -f "$TEMPLATE" ]; then
    echo "template not found: $TEMPLATE" >&2
    exit 1
fi

if [ ! -x "$BINARY" ]; then
    echo "atrium binary not found or not executable: $BINARY" >&2
    echo "build it with 'make build' and copy bin/atrium to $BINARY" >&2
    exit 1
fi

if [ ! -f "$CONFIG" ]; then
    echo "config not found: $CONFIG" >&2
    echo "create it with: $BINARY init --config $CONFIG" >&2
    exit 1
fi

mkdir -p "$AGENTS_DIR" "$DATA_DIR/logs"

# The substitution uses a temporary file so a failure never leaves a half
# written plist that launchd would refuse to load.
TMP_PLIST="$(mktemp "${TMPDIR:-/tmp}/atrium-plist.XXXXXX")"
trap 'rm -f "$TMP_PLIST"' EXIT

sed \
    -e "s|@LABEL@|$LABEL|g" \
    -e "s|@BINARY@|$BINARY|g" \
    -e "s|@CONFIG@|$CONFIG|g" \
    -e "s|@DATA_DIR@|$DATA_DIR|g" \
    "$TEMPLATE" > "$TMP_PLIST"

plutil -lint "$TMP_PLIST" >/dev/null
mv "$TMP_PLIST" "$PLIST"
trap - EXIT
chmod 0644 "$PLIST"

DOMAIN="gui/$(id -u)"

# bootout first so a re-install picks up the new plist.
if launchctl print "$DOMAIN/$LABEL" >/dev/null 2>&1; then
    echo "stopping the running service"
    launchctl bootout "$DOMAIN/$LABEL" 2>/dev/null || true
fi

launchctl bootstrap "$DOMAIN" "$PLIST"
launchctl enable "$DOMAIN/$LABEL"
launchctl kickstart -k "$DOMAIN/$LABEL"

cat <<DONE

Installed $LABEL

  plist     $PLIST
  binary    $BINARY
  config    $CONFIG
  data dir  $DATA_DIR
  logs      $DATA_DIR/logs/atrium.log

Check it with:
  launchctl print $DOMAIN/$LABEL | head -20
  $BINARY admin diag --config $CONFIG

Uninstall with deploy/launchd/uninstall.sh
DONE
