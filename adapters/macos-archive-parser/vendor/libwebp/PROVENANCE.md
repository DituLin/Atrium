# Pinned libwebp 1.6.0

The unmodified `COPYING`, `PATENTS` and `AUTHORS` accompany this distribution.

- Upstream source: https://storage.googleapis.com/downloads.webmproject.org/releases/webp/libwebp-1.6.0.tar.gz
- Source archive SHA-256:
  `93a852c2b3efafee3723efd4636de855b46f9fe1efddd607e1f42f60fc8f2136`.
- `include/decode.h` and `include/types.h` are unmodified public headers from that
  source archive. The decoder reports version `0x010600`; public decoder ABI is
  `0x0210`.
- These **arm64** prebuilt binaries came from the existing bundled Pillow 12.3.0
  `PIL/.dylibs` directory and retain their original bytes and signatures:
  - `libwebp.7.dylib`: SHA-256
    `60603ce66bd2d195344a911486c29456558c6219d6b42d6689572802e0d0765c`.
  - `libsharpyuv.0.dylib`: SHA-256
    `c6269fbda2e9be2f4e2a595f78e1c558f9a77d3133730515176f30eda8b3c3bd`.
- libwebp loads `@loader_path/libsharpyuv.0.dylib`; both otherwise depend only on
  `/usr/lib/libSystem.B.dylib`. The original Mach-O install IDs under `/DLC/PIL/`
  identify the libraries, but are not runtime paths used by this helper.

The build copies these files to the helper's fixed sibling `lib/` directory as
`atrium-libwebp-1.6.0.dylib` and `libsharpyuv.0.dylib`. Both files are checked against
compiled SHA-256 pins before loading libwebp by its absolute path. No caller
argument, document path, Python runtime path or environment chooses these files.
The same signing policy as the libjpeg bundle applies: preserve these bytes, or
sign first, update the reviewed artifact pins, and rebuild the helper. Missing or
modified dependencies fail as `parser_unavailable`, without an ImageIO fallback.

The external scaled output is four bytes. This is **not** a total memory bound:
lossless WebP may allocate source-size internal decoding structures. The source
pixel/byte limits, one-helper concurrency and process deadline still apply.
