# Pinned libjpeg-turbo 3.1.4.1

This software is based in part on the work of the Independent JPEG Group.
The unmodified `LICENSE.md` and `README.ijg` accompany this distribution.

- Upstream source/tag: https://github.com/libjpeg-turbo/libjpeg-turbo/tree/3.1.4.1
- Official source archive SHA-256:
  `a7da42b640377c2a9a9665e2c4b0ea60cd5599afb48c2521e6df0c9dc9d15a25`
- `include/jpeglib.h`, `include/jmorecfg.h`, and `include/jerror.h` are unmodified
  public headers from that source archive.
- `include/jconfig.h` is a local generated public ABI configuration: libjpeg ABI
  62, turbo version 3010401, 8-bit default samples, memory source/destination and
  arithmetic coding support. It contains no codec implementation changes.
- `libjpeg.62.4.0.dylib` is the existing **arm64** prebuilt library distributed
  in the host's bundled Pillow 12.3.0 runtime (its `PIL/.dylibs` directory), copied
  without byte changes. It is not loaded from that runtime in deployment.
  SHA-256: `0faad4d3807f74c20771bd9a9a50cd26142914ee500650dd4e3f0012c009ee58`.
  Its only external dependency is `/usr/lib/libSystem.B.dylib`.

The build copies this exact artifact atomically next to the helper as
`lib/atrium-libjpeg-3.1.4.1.dylib`, preserves its signature/bytes, and compiles its
SHA-256 into the helper. The helper derives that fixed sibling path from its own
executable location, checks regular-file/size/hash, and loads only that library.
No document path, command argument, environment variable, scratch path, Python
runtime, Homebrew installation or library search path selects the codec.

Release signing must preserve the vendored library's bytes/signature, or sign the
library first and rebuild the helper against the final pinned artifact. Re-signing
or modifying a bundled library after building the helper deliberately produces
`parser_unavailable`. The current build supports arm64 only; an x86_64 distribution
needs its own pinned, licensed artifact and actual native verification. The build
fails on other architectures rather than silently substituting a system codec.
