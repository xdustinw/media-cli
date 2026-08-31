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
  pinned FFmpeg source when `../ref/ffmpeg` is absent. Most work is stream copy;
  a curated set of still-image decoders backs image hashing, and — for
  `mc concat`'s re-encode path — a set of a/v decoders plus the native (LGPL)
  MPEG-4 / MPEG-2 / MJPEG / AAC / AC-3 / FLAC encoders and `libswscale` /
  `libswresample`. There is deliberately no H.264/H.265 encoder (would need
  GPL `libx264`/`libx265`); the binary grew ~10 MB for the added components.
- **`mc hash [file|folder ...] [-m <method>] [--select=<expr>] [-y] [-f] [--nr]`**:
  fingerprints each video (`.mp4 .mkv .mov .m4v .webm .avi .wmv .asf .flv .mpg
  .mpeg .ts .m2ts .3gp .ogv .vob …`) and image (`.jpg .jpeg .jpe .jfif .png
  .apng .gif .webp`) file and renames it to
  `<name>.<first 6 of hash>.<ext>` (replacing a short hash already in the name).
  Takes one or more files/folders (default: the current directory); folders are
  scanned **recursively unless `--nr` is passed** (the old `-r` opt-in flag is
  gone — recursive is now the default).
  - `-m` / `--method` selects the fingerprint. The default (no `-m`) tries
    `ffmpeg-10m` and falls back to `md5-10m` for any file ffmpeg can't read
    (e.g. an unusual `.wmv`). The recognised video extension list grew to cover
    `.wmv .asf .flv .mpg .mpeg .m2v .ts .m2ts .mts .3gp .3g2 .ogv .vob .mxf .rm
    .rmvb .divx .f4v` (single source of truth: `media.DefaultExtensions()`,
    which `config` now uses).
    `ffmpeg-10m` md5s the first ~10 MB of the video+audio stream; `ffmpeg` md5s
    the whole stream (or decoded pixels for images) and is the only method that
    also writes the `mc.hash` tag; `md5` / `sha` hash the raw file bytes;
    `md5-10m` / `sha-10m` hash the first 10 MB of raw bytes. Only `ffmpeg` reads
    or writes metadata — the rest just rename. The `*-10m` methods bound work on
    very large files (new `ffmpeg.StreamHashLimit`; `mc_stream_hash` gained a
    `max_bytes` arg).
  - `--select` (fields `name/path/ext/size/modifiedAt/kind`) narrows the file
    set; the matches are listed and confirmed before hashing, unless `-y`.
  - Video (`ffmpeg` method): hash of the encoded video+audio streams, ignoring container metadata
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
  - Shows a per-file preview, then on confirmation (or `-y`) renames each file
    (`ffmpeg` method also writes the freeform tag `mc.hash=<hash>` first). If the
    name already ends with a `.<6-hex>` slot, it is replaced rather than a
    second one appended.
  - Tag writes (`ffmpeg` method) never alter pixels or streams: video is remuxed with stream copy;
    images get a native text record (PNG `tEXt`, JPEG `COM`, GIF comment, WebP
    `mcTG` chunk) added via `internal/imgmeta`.
  - A file whose name already carries a valid 6-hex slot is left untouched and
    never hashed (for the `ffmpeg` method, a valid `mc.hash` tag is trusted the
    same way) — re-runs on large, already-processed folders are near-instant.
    `-f` / `--force` re-hashes them and re-checks, replacing a stale slot / tag.
  - When the renamed-to name already exists in the same folder, `mc hash` asks
    per file whether to `[o]verwrite` it, `[s]kip` (keep both), or `[d]elete`
    the incoming un-hashed file (default); `-y` always deletes it.
  - The per-file preview (hash + planned name, `+`/`»`/`~`/`=` glyph) is printed
    as each file is processed, so progress shows on large folders.
- **`mc list [folder] [flags]`**: folder listing (folder defaults to the current
  directory; only its own files unless `-r` / `--recursive` is passed). Each file
  row is `filename`, `size` (human readable), `artist`, `comment`. `mc.hash` and
  `rating` are not shown by default (usually empty) — add them back with
  `--meta=mc.hash,rating`.
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
  skips). On MP4/MOV, when every key is a standard field (`artist`, `comment`,
  `title`, `genre`, `date`, …) the metadata is written as iTunes-style atoms
  (`©nam`/`©ART`/`©cmt`/…) that Windows Explorer and QuickTime read; setting any
  non-standard key (`rating`, `tags`, custom) switches that file to the freeform
  `mdta` box so nothing is dropped (still read by mc/ffmpeg/QuickTime, may not
  show in Windows Explorer) — `mc set` prints a note. MKV and images take any
  key. Values may contain spaces; a `"…"` wrap keeps a comma inside a value.
  Only files matched by `--select` are probed deeply, and only when the filter
  needs it. Previews `key: <current> -> <new>` per file and confirms unless
  `-y`. Parser in `internal/tag`, workflow in `internal/setcmd`;
  `ffmpeg.WriteTags` / `imgmeta.WriteMany` / `imgmeta.ReadAll` added.
- **`mc copy <source> [<source> ...] <target> [-m <mode>] [--select=<expr>] [-y] [--nr]`**
  and **`mc move …`**: bring every file from one or more sources (files/folders)
  into a target folder, each landing at `<target>/<path relative to that
  source>`. Folders are scanned recursively unless `--nr`. `move` removes a
  source once its target is written. Before writing, each source file's
  `.<6-hex>` short hash (from `mc hash`, name-based — no metadata read) is
  matched against the short hashes of files already anywhere under the target;
  `-m` / `--mode` says what to do with every match: `skip-duplicate` (default —
  leave the target, keep the source; `move` does not delete a skipped source),
  `overwrite` (copy source bytes over the matching target file), or `keep-both`
  (bring the source in too, at `<target>/<rel>`). `--select` narrows the source
  set with a confirmation before the hash compare. When source/target files
  have no short hash, `mc copy`/`mc move` offers to hash them first (`-y` does
  it automatically); declined, the comparison falls back to relative path. `-y`
  skips confirmations. Non-duplicate path collisions are skipped, never
  overwritten. The plan is shown as a TOON preview first. New package
  `internal/copycmd`; `media.CopyFile` / `media.MoveFile` / `media.WalkFiles` /
  `media.DiscoverMany` / `media.Facts`; `hashcmd.HashInPlace`.
  - `mc move` gained `--delete-source`: deletes every matching source file even
    when the target already holds a content duplicate under `skip-duplicate`, so
    the source folder is drained regardless. Honours `--select`.
  - `mc move` across drives: `media.MoveFile` now also recognises Windows'
    `ERROR_NOT_SAME_DEVICE` (not just Unix `EXDEV`) as the signal to copy the
    bytes across instead of renaming, and removes the source only after the
    copied file is confirmed byte-count-identical. Any other rename failure is
    returned unchanged with the source left in place, so a failed move can never
    destroy the original.
- **`mc list-missing <src-folder> <target-folder> [<target-folder> …]
  [--select=<expr>] [-y] [--nr]`** (alias `find-missing`): walks the source
  folder and lists every file whose `.<6-hex>` short hash (from `mc hash`) is on
  no file under any target folder. The source is scanned recursively unless
  `--nr`; targets are always recursive; a non-existent target is treated as
  empty. Unhashed files on either side are offered to `mc hash` first (`-y`
  hashes them); declined, the comparison falls back to the base file name.
  Read-only aside from an accepted hash pass; results print as a TOON table. New
  package `internal/listmissingcmd`.
- **`mc dedupe [folder ...] [-m <method>] [--select=<expr>] [-y] [--nr]`**:
  groups files by the `.<6-hex>` short hash in their name (from `mc hash`, no
  metadata read) across the folders (default: the current directory) and deletes
  all but one copy of each set. Folders are scanned recursively unless `--nr`. `-m` / `--method` chooses the
  keeper: `interactive` (default — pick per set, or skip it), `longer-name`,
  `newer`, or `older`. `--select` filters the files. Files without a short hash
  are offered to `mc hash` first (`-y` hashes them); declined, they are grouped
  by file name. The deletions are shown as a TOON preview and confirmed unless
  `-y`. New package `internal/dedupecmd`.
- **`mc delete [folder ...] --select=<expr> [-y] [--nr]`**: deletes the files
  under the folders (default: the current directory) that match `--select` —
  which is required. Folders are scanned recursively unless `--nr`. The matched
  files are shown as a TOON preview and confirmed unless `-y`. New package
  `internal/deletecmd`.
- **`mc split <file> "<t1>,<t2>,…" [-o <folder>] [-y]`**: cuts a media file at
  the given timestamps (seconds, `MM:SS` or `HH:MM:SS`) into
  `<name>-Part1.<ext>`, `<name>-Part2.<ext>`, … (N timestamps → N+1 parts).
  Stream copy via the FFmpeg segment muxer, so each part begins at the nearest
  keyframe at or after its timestamp. Parts land in `-o`/`--outputFolder` or the
  file's folder. New `ffmpeg.Split` (`mc_split`) and package
  `internal/splitcmd`.
- **`mc concat <file1> <file2> [<file3> …] [-o <outputFile>] [-y]`**: joins media
  files in order. Matching codec/parameters → stream copy with timestamps made
  continuous across inputs (`ffmpeg.ConcatCopy` / `mc_concat_copy`). Mismatched
  inputs → a warning, then every input is re-encoded to MPEG-4 + AAC at the
  first file's geometry / rate and joined (`ffmpeg.Transcode` / `mc_transcode`,
  using `libswscale` + `libswresample`); the output container switches to
  `.mkv` when the first file's extension can't hold MPEG-4/AAC. Default output
  `<file1>-<file2>-…-combined.<ext>`. New package `internal/concatcmd`.
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
    re-run. `--preview` targets the newest preview on GitHub and offers it
    **regardless of the installed version** (so it can switch a stable install
    onto the preview channel, or re-install the current preview); with `-y` that
    install runs without a prompt. Backed by `selfupdate.Releases` /
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
