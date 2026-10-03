#!/bin/sh
set -eu
export LC_ALL=C LANG=C
cd "$(dirname "$0")"
out=${1:?Usage: build.sh /absolute/output/helper [--fixture-renderer]}
flags=
case "${2:-}" in "") ;; --fixture-renderer) flags=-DATRIUM_FIXTURE_RENDER;; *) exit 2;; esac
case "$out" in /*) ;; *) echo 'Output must be an absolute path' >&2; exit 2;; esac
[ "$(uname -m)" = arm64 ] || { echo 'Pinned native codec is arm64 only' >&2; exit 2; }
codec=vendor/libjpeg-turbo/libjpeg.62.4.0.dylib
jpeg_hash=$(shasum -a 256 "$codec" | cut -d ' ' -f 1)
[ "$jpeg_hash" = 0faad4d3807f74c20771bd9a9a50cd26142914ee500650dd4e3f0012c009ee58 ] || { echo 'Pinned codec mismatch' >&2; exit 3; }
webp=vendor/libwebp/libwebp.7.dylib
yuv=vendor/libwebp/libsharpyuv.0.dylib
webp_hash=$(shasum -a 256 "$webp" | cut -d ' ' -f 1)
yuv_hash=$(shasum -a 256 "$yuv" | cut -d ' ' -f 1)
[ "$webp_hash" = 60603ce66bd2d195344a911486c29456558c6219d6b42d6689572802e0d0765c ] || { echo 'Pinned codec mismatch' >&2; exit 3; }
[ "$yuv_hash" = c6269fbda2e9be2f4e2a595f78e1c558f9a77d3133730515176f30eda8b3c3bd ] || { echo 'Pinned codec mismatch' >&2; exit 3; }
# Include source, build settings, codec bytes/ABI, compiler and SDK. Runtime OS
# build, Vision revision and settings complete the engine identity.
build=$( { printf '%s\n' "$flags"; cat main.m png_integrity.h jpeg_integrity.h webp_framing.h webp_integrity.h build.sh vendor/libjpeg-turbo/include/*.h vendor/libwebp/include/*.h "$codec" "$webp" "$yuv"; xcrun clang --version; xcrun --show-sdk-version; } | shasum -a 256 | cut -d ' ' -f 1 )
[ "${#build}" = 64 ] || exit 3
libdir=$(dirname "$out")/lib
mkdir -p "$libdir"
libtmp=
trap 'rm -f "$libtmp"' EXIT
copy_codec() {
    libtmp=$(mktemp "$libdir/.atrium-codec.XXXXXX")
    cp "$1" "$libtmp"
    chmod 0555 "$libtmp"
    mv -f "$libtmp" "$libdir/$2"
}
copy_codec "$codec" atrium-libjpeg-3.1.4.1.dylib
copy_codec "$webp" atrium-libwebp-1.6.0.dylib
copy_codec "$yuv" libsharpyuv.0.dylib
mkdir -p "$libdir/licenses/libjpeg-turbo" "$libdir/licenses/libwebp"
cp vendor/libjpeg-turbo/LICENSE.md vendor/libjpeg-turbo/README.ijg vendor/libjpeg-turbo/PROVENANCE.md "$libdir/licenses/libjpeg-turbo/"
cp vendor/libwebp/COPYING vendor/libwebp/PATENTS vendor/libwebp/AUTHORS vendor/libwebp/PROVENANCE.md "$libdir/licenses/libwebp/"
xcrun clang $flags -fobjc-arc -fno-modules -Wall -Wextra -Werror -O2 -I vendor/libjpeg-turbo/include -DATRIUM_BUILD=\""$build"\" -DATRIUM_JPEG_SHA256=\""$jpeg_hash"\" -DATRIUM_WEBP_SHA256=\""$webp_hash"\" -DATRIUM_YUV_SHA256=\""$yuv_hash"\" main.m -framework Foundation -framework Vision -framework PDFKit -framework CoreGraphics -framework ImageIO -lz -o "$out"
