#!/bin/sh
set -eu
export LC_ALL=C LANG=C
cd "$(dirname "$0")"
out=${1:?Usage: build.sh /absolute/output/helper}
flags=
case "${2:-}" in "") ;; --fixture-renderer) flags=-DATRIUM_FIXTURE_RENDER;; *) exit 2;; esac
case "$out" in /*) ;; *) echo 'Output must be an absolute path' >&2; exit 2;; esac
# Fingerprint includes source, build settings, compiler, and SDK version. Runtime
# OS build, explicit Vision revision and language/settings complete the engine ID.
build=$( { printf '%s\n' "$flags"; cat main.m build.sh; xcrun clang --version; xcrun --show-sdk-version; } | shasum -a 256 | cut -d ' ' -f 1 )
[ "${#build}" = 64 ] || exit 3
xcrun clang $flags -fobjc-arc -fno-modules -Wall -Wextra -Werror -O2 -DATRIUM_BUILD=\""$build"\" main.m -framework Foundation -framework Vision -framework PDFKit -framework CoreGraphics -framework ImageIO -o "$out"
