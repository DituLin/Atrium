#!/bin/sh
# Build only committed source. Destination must not exist; no deployment occurs.
set -eu
umask 077
out=${1:?Usage: build-release.sh /absolute/new/package [git-sha]}
case "$out" in /*) ;; *) echo 'absolute output required' >&2; exit 2;; esac
if [ -e "$out" ] || [ -L "$out" ]; then
 echo 'output already exists' >&2
 exit 2
fi
[ "$(uname -s)/$(uname -m)" = Darwin/arm64 ] || { echo 'Darwin arm64 required' >&2; exit 2; }
repo=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
sha=$(git -C "$repo" rev-parse --verify "${2:-HEAD}^{commit}")
work=$(mktemp -d /private/tmp/atrium-release.XXXXXX)
trap 'rm -rf "$work"' EXIT HUP INT TERM
git -C "$repo" archive "$sha" | tar -x -C "$work"
# Runtime bindings are explicit, never inferred from a Codex tools directory.
: "${ATRIUM_RELEASE_NODE:?absolute runtime node required}"
: "${ATRIUM_RELEASE_OPENCLAW:?absolute openclaw package required}"
: "${ATRIUM_RELEASE_PROVIDER:?absolute provider package required}"
/usr/bin/python3 "$work/deploy/release/runtime-pins.py" "$work/runtime-pins.json"
mkdir "$out"
printf '%s\n' "$sha" > "$out/.atrium-release-owned"
cd "$work"
PATH="$(dirname "$ATRIUM_RELEASE_NODE"):$PATH" make web
mkdir -p "$out/bin" "$out/native" "$out/scripts"
for name in atrium home-mcp atrium-brain-host atrium-spool-read; do
 GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "-X github.com/DituLin/Atrium/internal/version.Commit=$sha -X github.com/DituLin/Atrium/internal/version.Version=$sha" -o "$out/bin/$name" "./cmd/$name"
done
sh adapters/macos-archive-parser/build.sh "$out/native/helper"
/usr/bin/python3 - "$out" <<'PY'
import pathlib, shutil, sys
out=pathlib.Path(sys.argv[1])
shutil.copytree('adapters/openclaw', out/'openclaw-plugin', ignore=shutil.ignore_patterns('node_modules','evidence','testdata','*.test.*','__pycache__'))
for name in ['patch-archive-ingress.mjs','archive-ingress-pins.json']:
 shutil.copy2(pathlib.Path('scripts')/name,out/'scripts'/name)
for name in ['manifest.py','schema-guard.py']:
 shutil.copy2(pathlib.Path('deploy/release')/name,out/'scripts'/name)
shutil.copy2('runtime-pins.json',out/'runtime-pins.json')
shutil.copytree('internal/store/migrations',out/'migrations')
PY
# Sign only bin/* and native/helper with the existing trusted signer, never
# --deep / codec dylibs. Then rerun this manifest script to hash signed bytes.
/usr/bin/python3 "$out/scripts/manifest.py" "$out"
printf 'Built %s at %s; sign executables, regenerate and verify manifest before activation.\n' "$sha" "$out"
