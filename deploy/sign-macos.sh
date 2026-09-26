#!/bin/sh
# Signs the atrium binary with a stable local code-signing identity.
#
# Why: macOS grants "Network Volumes" access (TCC) per code identity. An
# ad-hoc signed Go binary gets a new identity on every build, so each deploy
# silently loses NAS access until someone clicks Allow on the Mac mini. With a
# stable certificate and identifier the grant survives upgrades: approve once.
#
# The identity is self-signed, lives in a dedicated keychain next to the data
# (never the login keychain), and never leaves this machine.
#
# Usage: sign-macos.sh <binary> [state-dir]
#   state-dir defaults to ~/Atrium/signing and holds the keychain and its
#   generated password (mode 0600).
set -eu

BINARY=${1:?usage: sign-macos.sh <binary> [state-dir]}
STATE=${2:-"$HOME/Atrium/signing"}
NAME="Atrium Core Signing"
IDENTIFIER="io.atrium.core"
KEYCHAIN="$STATE/atrium-signing.keychain-db"
PASSFILE="$STATE/keychain.pass"

umask 077
mkdir -p "$STATE"

if [ ! -f "$KEYCHAIN" ]; then
  echo "creating signing identity \"$NAME\" in $KEYCHAIN"
  openssl rand -hex 24 > "$PASSFILE"
  WORK=$(mktemp -d)
  trap 'rm -rf "$WORK"' EXIT
  cat > "$WORK/cert.cnf" <<EOF
[req]
distinguished_name = dn
x509_extensions = ext
prompt = no
[dn]
CN = $NAME
[ext]
basicConstraints = critical,CA:false
keyUsage = critical,digitalSignature
extendedKeyUsage = critical,codeSigning
EOF
  openssl req -x509 -newkey rsa:2048 -nodes -days 3650 -config "$WORK/cert.cnf" \
    -keyout "$WORK/key.pem" -out "$WORK/cert.pem" 2>/dev/null
  P12PASS=$(openssl rand -hex 16)
  openssl pkcs12 -export -legacy -inkey "$WORK/key.pem" -in "$WORK/cert.pem" \
    -out "$WORK/identity.p12" -passout "pass:$P12PASS" 2>/dev/null \
    || openssl pkcs12 -export -inkey "$WORK/key.pem" -in "$WORK/cert.pem" \
      -out "$WORK/identity.p12" -passout "pass:$P12PASS"
  security create-keychain -p "$(cat "$PASSFILE")" "$KEYCHAIN"
  security set-keychain-settings "$KEYCHAIN"   # no auto-lock timeout
  security import "$WORK/identity.p12" -k "$KEYCHAIN" -P "$P12PASS" -T /usr/bin/codesign
  security set-key-partition-list -S apple-tool:,apple:,codesign: -s -k "$(cat "$PASSFILE")" "$KEYCHAIN" >/dev/null
fi

security unlock-keychain -p "$(cat "$PASSFILE")" "$KEYCHAIN"
HASH=$(security find-certificate -c "$NAME" -Z "$KEYCHAIN" | awk '/SHA-1 hash:/ {print $3}')
[ -n "$HASH" ] || { echo "signing certificate not found in $KEYCHAIN" >&2; exit 1; }

# codesign only finds identities in keychains on the user search list. Add the
# signing keychain for this one command and always put the list back exactly,
# one argument per line (paths may contain spaces).
ORIGINAL=$(security list-keychains -d user | sed -e 's/^[[:space:]]*"//' -e 's/"[[:space:]]*$//')
set_search_list() {
  set --
  while IFS= read -r entry; do
    [ -n "$entry" ] && set -- "$@" "$entry"
  done <<EOF
$ORIGINAL
EOF
  security list-keychains -d user -s "$@" ${EXTRA:+"$EXTRA"}
}
restore_search_list() { EXTRA="" set_search_list; }
trap restore_search_list EXIT INT TERM
EXTRA="$KEYCHAIN" set_search_list

codesign --force --sign "$HASH" --keychain "$KEYCHAIN" --identifier "$IDENTIFIER" "$BINARY"
restore_search_list
trap - EXIT INT TERM
codesign --verify --strict "$BINARY"
codesign -d -r- "$BINARY" 2>&1 | sed -n 's/^designated => /designated requirement: /p'
