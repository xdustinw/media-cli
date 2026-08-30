# Development

## Overview

`mc` is a Cobra CLI that links the FFmpeg C libraries via **cgo**. There is no
`ffmpeg` binary at build or run time — `internal/ffmpeg` calls `libavformat` /
`libavcodec` / `libavutil` directly, statically linked from
`third_party/ffmpeg/<goos>_<goarch>/`.

- **Video hashing** feeds demuxed packets through FFmpeg's real `hash` muxer, so
  `mc hash` on a video matches `ffmpeg -i f -map 0:v? -map 0:a? -f hash -hash md5 -`.
- **Image hashing** decodes to pixels and MD5s the raw plane data (plus format
  and dimensions), so EXIF / XMP / ICC / text chunks never affect it.
- **Tag writes** never touch pixel or stream data: video is remuxed with stream
  copy (`-movflags use_metadata_tags` for MP4/MOV); images get a native text
  record inserted by `internal/imgmeta` (PNG `tEXt`, JPEG `COM`, GIF comment,
  WebP private `mcTG` RIFF chunk). That reader/writer is a matched pair — values
  are not guaranteed to round-trip through other tools.

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

### `scripts/build-ffmpeg.sh`

Builds the static FFmpeg libraries into `third_party/ffmpeg/<goos>_<goarch>/`
and strips everything `mc` does not need (no programs, no `avfilter` /
`swscale` / `swresample` / `avdevice`, no encoders, decoders limited to a
curated still-image set, network disabled).

**License-relevant:** the script passes neither `--enable-gpl` nor
`--enable-nonfree`, and enables no external codec libraries — only `--enable-zlib`.
This keeps FFmpeg at **LGPL-2.1-or-later** and the resulting `mc` binary freely
redistributable. Do not add `--enable-gpl` / `--enable-nonfree` (or libx264,
libx265, …) without updating `THIRD-PARTY-NOTICES.md` and the project's license
posture accordingly.

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

`internal/ffmpeg/ffmpeg.go` has one `#cgo` block per `GOOS,GOARCH`, each pointing
at `third_party/ffmpeg/<goos>_<goarch>/{include,lib}` with the platform's system
libraries appended (`-latomic -lpthread` on Linux, `-liconv` on macOS,
`-lbcrypt -lws2_32 -lsecur32 -lole32 -luser32` on Windows). Building for a
platform whose libraries are absent fails at link with the missing `.a` path.

The C bridge is `internal/ffmpeg/bridge.c` / `bridge.h`.

## Versioning

`cmd.Version()` returns the `-ldflags` override when set, otherwise the trimmed
contents of the embedded `cmd/version.txt`:

```bash
go build -ldflags "-X 'github.com/xdustinw/media-cli/cmd.buildVersion=v1.2.3'" .
```

Per `CLAUDE.md`, a "version update" refreshes `CHANGELOG.md` and writes
`cmd/version.txt` as `{version}+{last 9 of commit}.{YYYY-MM-DD}`.

## Releases (CI)

`.github/workflows/release.yml` builds four binaries — each on a matching runner,
building its own static FFmpeg from the pinned `FFMPEG_COMMIT` so content hashes
are identical across platforms — then publishes one GitHub Release.

| Asset | Runner | Build |
| --- | --- | --- |
| `mc-linux-amd64` | `ubuntu-latest` | native |
| `mc-darwin-arm64` | `macos-latest` | native |
| `mc-darwin-amd64` | `macos-13` | native |
| `mc-windows-amd64.exe` | `ubuntu-latest` | mingw-w64 cross |

`go test ./...` runs on the three native targets before their build (the Windows
cross build is not executed in CI).

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
| `internal/imgmeta/` | pixel-preserving image metadata read/write (PNG/JPEG/GIF/WebP) |
| `internal/hashcmd/` | `mc hash` workflow |
| `internal/listcmd/`, `internal/infocmd/` | `mc list` / `mc info` workflows |
| `internal/mediainfo/` | filesystem + probe → one record; rating/authors/tags derivation |
| `internal/query/` | `--select` / `--sort-by` parser and evaluator |
| `internal/render/` | TOON / JSON / CSV output, order-preserving |
| `internal/selfupdate/` | `mc update`: release lookup + in-place binary replacement |
| `internal/media/` | file discovery, video/image classification, safe rename |
| `internal/toon/` | TOON preview encoder (hash/update previews) |
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
- Any file mutation shows a TOON preview and asks for confirmation unless `-y`.
