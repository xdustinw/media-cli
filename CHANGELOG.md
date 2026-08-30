# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-08-29

### Added

- **Project scaffold**: Cobra command tree (`mc`), Viper config (`MC_` env
  prefix, optional `media-cli.yaml`), `slog` logging (`--verbose` / `--debug`),
  and a `version` command from an embedded `cmd/version.txt` (overridable at
  build time via `-ldflags -X .../cmd.buildVersion`).
- **Vendored FFmpeg** (`third_party/ffmpeg/<goos>_<goarch>/`): statically linked
  libav* built via cgo — no `ffmpeg` binary needed. Only linux/amd64 is checked
  in; `scripts/build-ffmpeg.sh` builds any target (host or cross), fetching the
  pinned FFmpeg source when `../ref/ffmpeg` is absent. Audio/video use stream
  copy; a curated set of still-image decoders is enabled for image hashing.
- **`mc hash [file|folder] [-y] [-f] [-r]`**: computes a metadata-independent MD5
  for each video (`.mp4 .mkv .mov .m4v .webm .avi`) and image (`.jpg .jpeg .jpe
  .jfif .png .apng .gif .webp`) file. Given a folder, only its own files are
  processed unless `-r` / `--recursive` is passed. The target defaults to the
  current directory.
  - Video: hash of the encoded video+audio streams, ignoring container metadata
    (equivalent to `ffmpeg -map 0:v? -map 0:a? -c copy -f hash -hash md5 -`) —
    pure stream copy, never decoded. Parsers (`AVFMT_FLAG_NOPARSE`), the
    stream-info probe (when the header already types every stream) and the
    packet interleaver are skipped — ~16% faster on large files, hash
    unchanged for normally muxed inputs.
  - Image: hash of the decoded pixels, ignoring EXIF / XMP / ICC / text chunks.
  - FFmpeg's own `AV_LOG_ERROR` chatter about individual malformed packets
    (`Invalid NAL unit size`, `missing picture in access unit`, …) is silenced
    by default — it does not affect the hash and the CLI reports real failures
    itself. `-v` / `--debug` brings the FFmpeg log back.
  - Prints `<hash> - <path>`, shows a preview, then on confirmation (or `-y`)
    writes the freeform tag `mc.hash=<hash>` and renames each file to
    `<name>.<first 6 of hash>.<ext>`. If the name already ends with a
    `.<6-hex>` slot, it is replaced rather than a second one appended.
  - Tag writes never alter pixels or streams: video is remuxed with stream copy;
    images get a native text record (PNG `tEXt`, JPEG `COM`, GIF comment, WebP
    `mcTG` chunk) added via `internal/imgmeta`.
  - By default a file that already has a valid `mc.hash` tag is trusted and not
    re-hashed — the stored value is used directly, making re-runs on large,
    already-processed folders near-instant (and rename-only files are moved, not
    remuxed). `-f` / `--force` re-computes every hash and compares it with the
    stored tag, flagging any mismatch.
  - The per-file preview (hash + planned name, `+`/`»`/`~`/`=` glyph) is printed
    as each file is processed, so progress shows on large folders.
- **`mc list [folder] [flags]`**: folder listing (folder defaults to the current
  directory; only its own files unless `-r` / `--recursive` is passed). Each file
  row is
  `filename`, `size` (human readable), `mc.hash`, `rating`, `artist`, `comment`.
  `mc.hash` is read from container metadata (video) or the imgmeta record
  (images).
  - `--format=toon|json` nests the rows under their folders
    (`"tmp/" → "video/" → files[N]{...}`); `--format=csv` is one flat table with
    absolute paths.
  - `--meta=title,make,...` adds metadata columns.
  - `--select='name=sample* and rating>=4 and size>1g and modifiedAt>2026-08-01'`
    filters — fields `name/path/size/modifiedAt/rating/kind/format/artist/comment`
    or any metadata key, operators `= != > < >= <=` (`=` does a case-insensitive
    `*`/`?` wildcard match, OS-independent), size suffixes `k/m/g/t`, `and`/`or`.
  - `--sort-by='rating desc, size desc, name'` — multi-key, optional `desc`.
- **`mc set '<key=value,...>' [folder] [--select=<expr>] [-y] [-r]`**: writes
  chosen metadata onto the media files in a folder (its own files only unless
  `-r` / `--recursive` is passed; folder defaults to the current directory) that
  match `--select`.
  Video is remuxed with stream copy (pixels/streams and the `mc.hash` value
  unchanged, only container metadata rewritten); image tags go into the same
  native text store `mc.hash` uses (and shares the video remux's parser/probe
  skips). On MP4/MOV the metadata is written as standard iTunes-style atoms
  (`©nam`/`©ART`/`©cmt`/…) so Windows Explorer and QuickTime display it — use
  canonical field names (`artist`, `comment`, `title`, `genre`, `date`); keys
  the MP4 muxer does not recognise are not retained and `mc set` warns.
  Values may contain spaces; a `"…"` wrap keeps a comma inside a value.
  Only files matched by `--select` are probed deeply, and only when the filter
  needs it. Previews `key: <current> -> <new>` per file and confirms unless
  `-y`. Parser in `internal/tag`, workflow in `internal/setcmd`;
  `ffmpeg.WriteTags` / `imgmeta.WriteMany` / `imgmeta.ReadAll` added.
- **`mc info <file> [--format=toon|json]`**: full dump of one file — path, size,
  modified time; container/codec details per stream; every metadata entry
  (image EXIF / PNG text included, Windows XP* tags decoded, binary thumbnail
  blobs dropped); and an `mc_metadata` section listing the file's imgmeta tags.
- **Processing summary**: `mc hash`, `mc list`, `mc set` and `mc info` each print
  a one-line wrap-up to stderr when they finish —
  `processed <n> file(s) in <duration> (<rate> MB/s)` (the rate is omitted when
  it cannot be computed; runs over a minute are shown as `2m 5s`). Shared
  `media.Summary` helper.
- **`internal/ffmpeg.Inspect`** (cgo `mc_probe`): container + stream + metadata
  probe, optionally decoding the first frame for image EXIF. New support
  packages: `internal/mediainfo` (derives rating/authors/tags, size formatting),
  `internal/query` (`--select` / `--sort-by` parser + evaluator),
  `internal/render` (TOON/JSON/CSV, ordered output). TOON output uses
  `github.com/toon-format/toon-go`.
- **`mc update [-y] [--preview]`**: checks GitHub releases for a newer version
  and replaces the running binary in place — resolving the executable through
  symlinks so it updates wherever `mc` sits on `PATH`. Unix does an atomic
  rename; Windows renames the running `mc.exe` to `mc-<version>.exe` first.
  Skips the download when already current; `-y` skips the prompt. Logic in
  `internal/selfupdate`.
  - Considers stable releases by default. A newer preview (pre-release) is always
    reported; when there is no stable update it is offered interactively, and
    when a stable update is offered the preview is noted for a `--preview`
    re-run. `--preview` targets the newest preview directly; with `-y` a preview
    is only taken alongside `--preview`. Backed by `selfupdate.Releases` /
    `selfupdate.LatestReleases` (all releases listed and ranked locally),
    replacing the single `releases/latest` lookup.
- **License compliance**: FFmpeg is LGPL-2.1-or-later and statically linked, so
  the repo carries `third_party/ffmpeg/COPYING.LGPLv2.1` / `COPYING.LGPLv3` /
  `LICENSE.md`, a root `THIRD-PARTY-NOTICES.md` (notice + corresponding-source
  pointer + LGPL §6 relink steps), a License section in the README, and
  `mc version` now prints the bundled FFmpeg version and license. Releases
  attach `THIRD-PARTY-NOTICES.md`. The build enables no GPL/nonfree parts.
- **Release workflow** (`.github/workflows/release.yml`): builds
  `mc-linux-amd64`, `mc-darwin-arm64` and `mc-windows-amd64.exe` — each on a
  matching runner building its own static FFmpeg from the pinned commit (Windows
  cross-compiled from Linux with mingw-w64 and fully statically linked via
  `-extldflags=-static`, so the `.exe` has no `zlib1.dll` / libgcc dependency) —
  then publishes one GitHub Release: a rolling `preview` pre-release on `main`
  pushes, a versioned release on `v*` tags, plus a manual `workflow_dispatch`
  trigger. Intel macOS is not built (build from source).
