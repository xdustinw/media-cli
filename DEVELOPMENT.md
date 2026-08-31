# Development

## Overview

`mc` is a Cobra CLI that links the FFmpeg C libraries via **cgo**. There is no
`ffmpeg` binary at build or run time — `internal/ffmpeg` calls `libavformat` /
`libavcodec` / `libavutil` directly, statically linked from
`third_party/ffmpeg/<goos>_<goarch>/`.

- **Video hashing** (`mc_stream_hash`) is pure stream copy — packets are demuxed
  and fed straight to FFmpeg's real `hash` muxer, never decoded — so `mc hash`
  on a video matches
  `ffmpeg -i f -map 0:v? -map 0:a? -c copy -f hash -hash md5 -`. The cost is
  one MD5 pass over the streams' bytes; a few things around it are trimmed for
  large files, each leaving the hashed byte stream identical for a normally
  muxed file:
  - `AVFMT_FLAG_NOPARSE` — the bitstream parsers only re-frame packets;
    concatenated the bytes are unchanged.
  - `avformat_find_stream_info` is skipped when the container header already
    types every stream (mp4/mkv/mov/webm/...); it only reads/buffers data we
    then stream through anyway.
  - `av_write_frame` instead of `av_interleaved_write_frame` — the interleaver
    buffers and re-copies every packet to DTS-sort it; a normally muxed file is
    already in order.

  ~16 % faster on a 97 MB h264/aac mp4 (`go test ./internal/ffmpeg -bench StreamHash`).

  `StreamHashLimit(path, maxBytes)` stops feeding packets once `maxBytes` of
  payload has been copied (`mc_stream_hash`'s `max_bytes` arg) — the `hash -m
  ffmpeg-10m` default and `*-10m` methods fingerprint only a bounded prefix,
  bounding work on very large files. `mc hash`'s non-`ffmpeg` methods
  (`internal/hashcmd/method.go`) skip metadata entirely: they hash raw file
  bytes (md5/sha) or a bounded stream prefix and only rename the file.
- **Image hashing** decodes to pixels and MD5s the raw plane data (plus format
  and dimensions), so EXIF / XMP / ICC / text chunks never affect it.
- **Tag writes** never touch pixel or stream data. MP4/MKV container metadata
  cannot be patched in place, so `ffmpeg.WriteTags` **remuxes the whole file**
  with stream copy — a read + write pass over every byte, comparable in cost to
  hashing it, and unavoidable for video. It reuses the pre-computed hash
  (nothing is re-hashed after confirmation) and applies the same parser/probe
  skips as `mc_stream_hash`. Its `movFreeform` argument selects the MP4/MOV
  metadata style: `mc hash` passes `true` (the `mdta` key box, so the
  non-standard `mc.hash` key survives); `mc set` passes `false` (iTunes `©`
  atoms, which the mp4 muxer only keeps for known fields but which Windows
  Explorer and QuickTime read).
  Images are cheap: `internal/imgmeta` inserts a native text record (PNG `tEXt`,
  JPEG `COM`, GIF comment, WebP private `mcTG` chunk) without re-encoding. That
  reader/writer is a matched pair — values are not guaranteed to round-trip
  through other tools.
- **FFmpeg logging** is set to `AV_LOG_FATAL` in `internal/ffmpeg`'s `init()`.
  libav*'s `AV_LOG_ERROR` messages about individual malformed packets are noise
  for a hashing tool and do not change the result; genuine open/read failures
  come back as return codes the CLI surfaces itself. `mc <cmd> -v` / `--debug`
  raises the level via `ffmpeg.SetVerbose`.

## Build from source

Requires Go 1.22+, a C compiler, and `CGO_ENABLED=1`.

```bash
git clone https://github.com/xdustinw/media-cli.git
cd media-cli
go build -o mc .        # links third_party/ffmpeg/linux_amd64/*.a
```

Only the **linux/amd64** FFmpeg libraries are checked in. For any other platform,
build them first, then `go build`:

```bash
scripts/build-ffmpeg.sh                        # for the host GOOS/GOARCH
TARGET_GOOS=windows scripts/build-ffmpeg.sh    # cross-build (needs mingw-w64)
```

The FFmpeg build takes roughly 15–25 minutes (the h264/hevc decoders dominate).

### `scripts/build-ffmpeg.sh`

Builds the static FFmpeg libraries into `third_party/ffmpeg/<goos>_<goarch>/`
and strips everything `mc` does not need (no programs, no `avfilter` /
`avdevice`, network disabled). What is enabled:

- a curated still-image decoder set (for `mc hash` on images);
- a curated a/v decoder set — `h264 hevc mpeg4 mpeg2video vp9 aac ac3 mp3 opus
  vorbis flac …` — and the native encoders `mpeg4 mpeg2video mjpeg aac ac3 flac
  pcm_s16le`, plus `libswscale` and `libswresample`, for `mc concat`'s
  re-encode path.

There is **no H.264/H.265 encoder** — that would need GPL `libx264`/`libx265`.
`mc concat` re-encodes mismatched inputs to MPEG-4 + AAC instead.

**License-relevant:** the script passes neither `--enable-gpl` nor
`--enable-nonfree`, and enables no external codec libraries — only `--enable-zlib`.
Every enabled encoder/decoder is LGPL, so FFmpeg stays **LGPL-2.1-or-later** and
the `mc` binary stays freely redistributable. Do not add `--enable-gpl` /
`--enable-nonfree` (or libx264, libx265, libfdk-aac, …) without updating
`THIRD-PARTY-NOTICES.md` and the project's license posture accordingly.

| Input | Purpose |
| --- | --- |
| `$1` / `$FFMPEG_SRC` | FFmpeg source tree to build from |
| `$TARGET_GOOS`, `$TARGET_GOARCH` | target platform (default: host `go env`) |
| `$FFMPEG_COMMIT` | commit to clone when no local source is found |

Source resolution: the argument → `$FFMPEG_SRC` → `../ref/ffmpeg` → a shallow
clone of FFmpeg at `$FFMPEG_COMMIT` into `third_party/.ffmpeg-src/`.

`windows` targets are cross-compiled with the `x86_64-w64-mingw32-` toolchain.

**System dependencies:** `nasm`, `pkg-config`, a C toolchain, `make`, zlib
headers (`zlib1g-dev`). For the Windows cross build also `gcc-mingw-w64-x86-64`
and `libz-mingw-w64-dev`.

### cgo wiring

`internal/ffmpeg/ffmpeg.go` has one `#cgo` block per `GOOS,GOARCH`, each linking
`libavformat` / `libavcodec` / `libswscale` / `libswresample` / `libavutil` from
`third_party/ffmpeg/<goos>_<goarch>/lib` with the platform's system
libraries appended (`-latomic -lpthread` on Linux, `-liconv` on macOS,
`-lbcrypt -lws2_32 -lsecur32 -lole32 -luser32` on Windows). Building for a
platform whose libraries are absent fails at link with the missing `.a` path.

The Windows binary is built with `-ldflags -extldflags=-static` so zlib, libgcc,
libwinpthread and libssp are linked in and the `.exe` has no DLL dependencies
beyond the OS's own (`kernel32`, `bcrypt`, …).

The C bridge is `internal/ffmpeg/bridge.c` / `bridge.h`: stream/pixel hashing,
tag remux, `Inspect` probe, `mc_split` (segment muxer), `mc_concat_copy`
(timestamp-continuous stream-copy join) and `mc_transcode` (decode → swscale /
swresample → MPEG-4 / AAC encode, used by `mc concat` for mismatched inputs).

## Versioning

`cmd.Version()` returns the `-ldflags` override when set, otherwise the trimmed
contents of the embedded `cmd/version.txt`:

```bash
go build -ldflags "-X 'github.com/xdustinw/media-cli/cmd.buildVersion=v1.2.3'" .
```

Per `CLAUDE.md`, a "version update" refreshes `CHANGELOG.md` and writes
`cmd/version.txt` as `{version}+{last 9 of commit}.{YYYY-MM-DD}`.

## Releases (CI)

`.github/workflows/release.yml` builds three binaries — each on a matching
runner, building its own static FFmpeg from the pinned `FFMPEG_COMMIT` so content
hashes are identical across platforms — then publishes one GitHub Release.

| Asset | Runner | Build |
| --- | --- | --- |
| `mc-linux-amd64` | `ubuntu-latest` | native |
| `mc-darwin-arm64` | `macos-latest` | native |
| `mc-windows-amd64.exe` | `ubuntu-latest` | mingw-w64 cross, `-extldflags=-static` (no DLL deps) |

`go test ./...` runs on the two native targets before their build (the Windows
cross build is not executed in CI). Intel macOS is not built — contributors on
that platform run `scripts/build-ffmpeg.sh` then `go build`.

| Trigger | Result |
| --- | --- |
| push to `main` (incl. a merged PR) | rotates a single `preview` pre-release for the latest `main` |
| push a tag `vX.Y.Z` | permanent release `vX.Y.Z`, version stamped via `-ldflags` |
| **Run workflow** button (Actions tab, `workflow_dispatch`) | same as a `main` push, on demand |

Cut a versioned release once your change is on `main`:

```bash
git switch main && git pull
git tag -a v0.1.0 -m "v0.1.0"
git push origin v0.1.0        # the tag push starts the release job
```

Or run it without pushing: `gh workflow run release.yml --ref main`
(`gh run watch` to follow). GitHub only recognises the workflow once it is on
the default branch.

Asset names (`mc-<goos>-<goarch>[.exe]`) must match `matrix.asset` in the
workflow and `AssetName()` in `internal/selfupdate` — `mc update` downloads by
that exact name.

## Testing

```bash
go test ./...
go vet ./...
```

- `internal/imgmeta`, `internal/toon`, `internal/media`, `internal/selfupdate`,
  `internal/query`, `internal/render`, `internal/mediainfo` are pure-Go units.
- `internal/ffmpeg`, `internal/hashcmd`, `internal/listcmd`, `internal/infocmd`
  tests generate PNG/JPEG/GIF fixtures in-process; video tests use samples under
  `tmp/` and skip when absent.
- CLI tests capture output with `bytes.Buffer` (see `cmd/*_test.go`).

## Licensing

`mc` is MIT. Its binaries statically link FFmpeg (LGPL-2.1-or-later), built with
no GPL/nonfree parts. Compliance is handled by:

- `third_party/ffmpeg/COPYING.LGPLv2.1`, `COPYING.LGPLv3`, `LICENSE.md` — the
  license texts, committed.
- `THIRD-PARTY-NOTICES.md` — the prominent notice, corresponding-source pointer
  (upstream URL + pinned commit + this build script) and LGPL §6 relinking
  instructions (the vendored `.a` + headers + open Go source make relinking a
  `go build` away).
- `mc version` prints the FFmpeg version and the LGPL notice.
- Releases attach `THIRD-PARTY-NOTICES.md` and reference it in the notes.

## Repo layout

| Path | Purpose |
| --- | --- |
| `cmd/` | Cobra commands — thin: parsing, wiring, error presentation |
| `internal/ffmpeg/` | cgo bridge: stream & pixel hashing, tag remux, `Inspect` probe |
| `internal/imgmeta/` | pixel-preserving image metadata read/write/enumerate (PNG/JPEG/GIF/WebP) |
| `internal/hashcmd/`, `internal/setcmd/` | `mc hash` (see `method.go`) / `mc set` workflows |
| `internal/listcmd/`, `internal/infocmd/` | `mc list` / `mc info` workflows |
| `internal/copycmd/` | `mc copy` / `mc move`: name-hash duplicate resolution |
| `internal/dedupecmd/` | `mc dedupe`: delete duplicate hash-named files across folders |
| `internal/deletecmd/` | `mc delete`: remove files matching a required `--select` |
| `internal/splitcmd/`, `internal/concatcmd/` | `mc split` (keyframe cut) / `mc concat` (stream-copy or re-encode join) |
| `internal/mediainfo/` | filesystem + probe + imgmeta → one record; rating/authors/tags derivation |
| `internal/query/` | `--select` / `--sort-by` parser and evaluator |
| `internal/tag/` | `mc set` `key=value,…` parser |
| `internal/render/` | TOON / JSON / CSV output, order-preserving |
| `internal/selfupdate/` | `mc update`: release lookup + in-place binary replacement |
| `internal/media/` | file discovery/walk, video/image classification, safe rename, copy/move |
| `internal/toon/` | small TOON encoder for the `mc update` / `mc copy` previews |
| `internal/config/`, `internal/logging/` | Viper / slog setup |
| `scripts/build-ffmpeg.sh` | vendored FFmpeg builder |
| `third_party/ffmpeg/<goos>_<goarch>/` | static FFmpeg libs + headers (only `linux_amd64` committed) |
| `third_party/ffmpeg/COPYING.*`, `LICENSE.md` | FFmpeg license texts |
| `THIRD-PARTY-NOTICES.md` | bundled-component notices (FFmpeg LGPL, zlib) |

## Architectural conventions (`CLAUDE.md`)

- `cmd/` stays lightweight; logic lives in `internal/`.
- Return errors from packages; handle them at the CLI surface.
- `slog` for logging; stdout for output, stderr for logs/errors.
- `context.Context` for cancellation (Ctrl-C).
- Any file mutation shows a preview and asks for confirmation unless `-y`
  (`mc hash` streams the preview per file; `mc update` uses a TOON block).
