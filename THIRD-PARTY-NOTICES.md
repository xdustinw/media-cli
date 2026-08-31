# Third-party notices

`media-cli` (`mc`) is distributed under the MIT License (see `LICENSE`).
Its binaries additionally include the components below. Nothing here changes
`mc`'s own MIT terms, but the FFmpeg libraries carry LGPL obligations that this
distribution satisfies as described.

---

## FFmpeg (libavformat, libavcodec, libswscale, libswresample, libavutil)

- **Homepage:** <https://ffmpeg.org>
- **Copyright:** © the FFmpeg developers
- **License:** GNU Lesser General Public License, version 2.1 or later
  (**LGPL-2.1-or-later**). Full text: `third_party/ffmpeg/COPYING.LGPLv2.1`
  (and `COPYING.LGPLv3`). FFmpeg's own license summary:
  `third_party/ffmpeg/LICENSE.md`.

`mc` links `libavformat`, `libavcodec`, `libswscale`, `libswresample` and
`libavutil` **statically** into its executable. The libraries are built with
neither `--enable-gpl` nor `--enable-nonfree` and enable no external codec
libraries — only FFmpeg's own LGPL decoders and encoders (the MPEG-4 / MPEG-2 /
MJPEG / AAC / AC-3 / FLAC / PCM encoders; there is no H.264/H.265 encoder). The
LGPL-2.1-or-later terms therefore apply unchanged and the result is
redistributable.

### Corresponding source

The exact FFmpeg source these libraries are built from:

- **Upstream:** <https://github.com/FFmpeg/FFmpeg>
- **Commit:** `b32f8d1c2377079302d23f82d555d13deda68c57` (also recorded as
  `FFMPEG_COMMIT` in `scripts/build-ffmpeg.sh` and `.github/workflows/release.yml`)
- **Build configuration:** produced by `scripts/build-ffmpeg.sh` in this
  repository (static libraries; no programs, `avfilter` or `avdevice`; a curated
  decoder/encoder set; no GPL/nonfree; no external codec libraries).

### Relinking (LGPL §6)

You may replace the FFmpeg libraries in any `mc` build and produce a working
binary:

1. The full source of the "work that uses the library" (all of `mc`) is this
   repository.
2. The FFmpeg static libraries and headers `mc` links against are vendored at
   `third_party/ffmpeg/<goos>_<goarch>/` (only `linux_amd64/` is committed;
   other platforms are produced by `scripts/build-ffmpeg.sh`).
3. To relink: modify or rebuild FFmpeg (`scripts/build-ffmpeg.sh`, or drop your
   own `.a` files into that directory) and run `go build .`.

No copyright or permission notices have been removed from the FFmpeg headers or
libraries.

---

## zlib

- **Homepage:** <https://zlib.net>
- **Copyright:** © 1995–2024 Jean-loup Gailly and Mark Adler
- **License:** zlib License (permissive)

FFmpeg is built with `--enable-zlib`; depending on the platform, zlib is linked
dynamically (Linux/macOS system library) or statically (Windows build). zlib's
license permits redistribution in binary form without conditions beyond not
misrepresenting its origin.
