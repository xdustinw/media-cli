# media-cli (`mc`)

Content-addressable media operations. `mc` computes a **metadata-independent**
hash for your video and image files — change a rating, title, EXIF field or
embedded comment and the hash stays the same — then tags and renames each file
by its content.

FFmpeg is linked straight into the binary; there is nothing else to install.

## Install

Grab the binary for your platform from the
[latest release](https://github.com/xdustinw/media-cli/releases/latest).

**Linux**
```bash
wget -O mc https://github.com/xdustinw/media-cli/releases/latest/download/mc-linux-amd64
chmod +x ./mc && ./mc version
```

**macOS** (Apple Silicon)
```bash
wget -O mc https://github.com/xdustinw/media-cli/releases/latest/download/mc-darwin-arm64
chmod +x ./mc && ./mc version
# if Gatekeeper complains: xattr -d com.apple.quarantine ./mc
```
Intel Macs: no pre-built binary — [build from source](#option-b--build-from-source).

**Windows** (PowerShell)
```powershell
Invoke-WebRequest https://github.com/xdustinw/media-cli/releases/latest/download/mc-windows-amd64.exe -OutFile mc.exe
.\mc.exe version
```

### Run it from anywhere

Move `mc` into a folder on your `PATH`:

```bash
# Linux / macOS
mkdir -p ~/.local/bin && mv mc ~/.local/bin/
# add to ~/.bashrc / ~/.zshrc if needed:  export PATH="$HOME/.local/bin:$PATH"
```

```powershell
# Windows
mkdir "$env:LOCALAPPDATA\Programs\Media-Cli"; move mc.exe "$env:LOCALAPPDATA\Programs\Media-Cli\"
setx PATH "$env:PATH;$env:LOCALAPPDATA\Programs\Media-Cli"    # restart the terminal
```

### Stay up to date

```bash
mc update            # checks GitHub, asks, replaces the binary in place
mc update -y         # no questions
mc update --preview  # take the newest preview (pre-release) build
```

`mc update` finds the running binary wherever it is on your `PATH` (Windows keeps
the old `mc.exe` as `mc-<version>.exe`).

## Commands

| Command | Description |
| --- | --- |
| [`mc hash`](#mc-hash) | Fingerprint media by content and rename each file (optionally tag it) |
| [`mc list`](#mc-list) | List a folder's media with size and metadata, filter & sort |
| [`mc set`](#mc-set) | Write chosen metadata onto matching files in a folder |
| [`mc copy`](#mc-copy--mc-move) / [`mc move`](#mc-copy--mc-move) | Bring files into a folder, resolving name-hash duplicates |
| [`mc info`](#mc-info) | Dump everything known about one file |
| [`mc update`](#mc-update) | Update `mc` to the latest release |
| [`mc version`](#mc-version) | Print version and bundled-FFmpeg info |

Global flags: `-v/--verbose`, `--debug`, `--config <path>`.

### `mc hash`

```
mc hash [file or folder] [-m <method>] [-y] [-f] [-r]   # target defaults to the current directory
```

Fingerprint media by content and rename each file to
`<name>.<first 6 of hash>.<ext>` (replacing a short hash already in the name).
Given a folder, only its own files are processed; pass `-r` / `--recursive` to
descend into subfolders.

```bash
mc hash                       # current directory, default method (ffmpeg-10m)
mc hash movie.mp4             # one file, confirm changes
mc hash -r ~/Videos           # include subfolders
mc hash ~/Photos -y           # no confirmation prompt
mc hash ~/Videos -m ffmpeg    # full metadata-independent hash + write mc.hash
mc hash ~/Videos -m md5-10m   # fastest: md5 of the first 10 MB of file bytes
```

**`-m` / `--method`** — how the fingerprint is computed:

| method | hashes | speed | writes `mc.hash`? |
| --- | --- | --- | --- |
| `ffmpeg-10m` *(default)* | md5 of the first ~10 MB of the video+audio stream | fast | no |
| `ffmpeg` | md5 of the whole video+audio stream (video) or decoded pixels (images); metadata ignored | slow | **yes** |
| `md5` / `sha` | md5 / sha-256 of the raw file bytes (metadata included) | medium | no |
| `md5-10m` / `sha-10m` | md5 / sha-256 of the first 10 MB of file bytes | fastest | no |

Only `ffmpeg` reads or writes file metadata; every other method just renames.
`ffmpeg` and `ffmpeg-10m` treat two files that differ only in metadata as
identical; `md5` / `sha` do not (they see the raw bytes).

As each file is processed it prints a preview line — `+` add hash to name, `»`
replace a different hash already in the name (or, for `ffmpeg`, rename only), `~`
re-tag stale, `=` already current:

```
Preview (ffmpeg-10m, rename only):
  + trip/IMG_1.jpg  6cb96fb66986b44f505831c54f69598e  ->  trip/IMG_1.6cb96f.jpg
  = trip/IMG_2.fbc6ec.jpg  fbc6ec44...
```

For the `ffmpeg` method, after `[y/N]` (or `-y`) each pending file gets the tag
`mc.hash=<hash>` written into it (pixels and streams left byte-for-byte
untouched) and is renamed. **A file that already carries a valid `mc.hash` tag
is trusted and not re-hashed**; pass `-f` / `--force` to re-compute and compare
(a mismatch is flagged `~` "stale mc.hash … replaced" and rewritten). `-f` has
no effect on the rename-only methods, which always re-read the file.

### `mc list`

```
mc list [folder] [--meta=<fields>] [--select=<expr>] [--sort-by=<keys>] [--format=toon|json|csv] [-r]
```

Lists `[folder]` (default: current directory). Only that folder's own files are
listed; pass `-r` / `--recursive` to descend into subfolders. Each file row is
`filename`, `size`, `artist`, `comment` (+ any `--meta` columns). `mc.hash` and
`rating` are **not** shown by default — they're usually empty; add them back with
`--meta=mc.hash,rating`. `toon` and `json` nest the rows under their folders;
`csv` is one flat table with absolute paths.

```bash
mc list ~/Photos
mc list -r ~/Photos --meta=mc.hash,rating,make,model
mc list -r ~/Videos --select='rating>=4 and size>1g' --sort-by='rating desc, size desc'
mc list -r ~/Photos --format=csv > inventory.csv       # flat, absolute paths
```

```
$ mc list tmp --meta=mc.hash,rating
6 file(s)
"tmp/":
  "img/":
    files[2]{filename,size,artist,comment,mc.hash,rating}:
      photo.jpg,84KB,,summer trip,4358a46e…d7,4
  "video/":
    files[1]{filename,size,artist,comment,mc.hash,rating}:
      clip.mp4,97MB,Adam Yu,,8f9b6e8b…37,
```

- `--meta` – extra metadata columns, comma separated (e.g. `mc.hash`, `rating`, `title`).
- `--select` – keep matching files. Fields: `name`, `path`, `size`,
  `modifiedAt`, `rating`, `kind`, `format`, `artist`, `comment`, or any metadata
  key. Operators `= != > < >= <=`; `=` matches case-insensitively and supports
  `*` / `?` wildcards (`name=*trip*`), the same on every OS. Sizes take
  `k`/`m`/`g`/`t` suffixes; dates are `YYYY-MM-DD`. Combine with `and` / `or`.
- `--sort-by` – comma-separated keys, each optionally `desc` (default `name`);
  folders are always alphabetical.
- `--format` – `toon` (default), `json`, or `csv`.
- `-r` / `--recursive` – descend into subfolders (off by default).

Every command that walks files (`hash`, `list`, `set`, `info`) prints a one-line
summary to stderr when it finishes, e.g.
`processed 12 file(s) in 3.4s (28.1 MB/s)` (runs over a minute read as
`2m 5s`).

### `mc set`

```
mc set '<key=value,...>' [folder] [--select=<expr>] [-y] [-r]
```

Writes metadata onto the media files in `[folder]` — default the current
directory — that match `--select`. Only that folder's own files are considered;
pass `-r` / `--recursive` to descend into subfolders.

```bash
mc set 'rating=3,artist=Adam' -r ~/Photos --select='name=3*Adam*'
mc set 'title=Trip 2026,comment=family' ~/Videos --select='name=DSC*' -y
mc set 'artist="Doe, Jane"' ~/Photos --select='artist=old-name'   # quote to keep a comma
```

- Video files are **remuxed with stream copy** — pixels and streams are
  untouched, so the `mc hash` value does not change; only the container metadata
  is rewritten.
- On MP4/MOV, when **every** key you set is a standard field (`artist`, `comment`,
  `title`, `genre`, `date`, `album`, …) the metadata is written as the classic
  `©nam` / `©ART` / `©cmt` atoms that **Windows Explorer and QuickTime show**.
  As soon as a **non-standard** key is involved (`rating`, `tags`, anything
  custom) that file's MP4/MOV metadata switches to the freeform `mdta` box so
  nothing is dropped — it round-trips through `mc list` / `mc info` / ffmpeg and
  QuickTime, but may not appear in Windows Explorer. `mc set` prints a note when
  it makes that switch. MKV and image formats take any key with no caveat.
- Image tags go into the file's native text area (PNG `tEXt`, JPEG `COM`, GIF
  comment, WebP chunk), the same store `mc.hash` uses. `mc list` / `mc info`
  read them back.
- `--select` uses the same expression as [`mc list`](#mc-list). **Pass it** —
  without it every media file in the folder is updated.
- The change is previewed (`key: <current> -> <new>` per file) and confirmed
  unless `-y`.
- `-r` / `--recursive` – descend into subfolders (off by default).

### `mc copy` / `mc move`

```
mc copy <source> <target> [-m <mode>] [-y]
mc move <source> <target> [-m <mode>] [-y]
```

Bring every file from `<source>` (a file or folder) into `<target>`,
recursively — each file lands at `<target>/<path relative to source>`. `move`
removes a source file once its target is written; `copy` leaves it.

Before writing anything, each source file's `.<6-hex>` short hash (from
[`mc hash`](#mc-hash)) is looked up among the short hashes of the files
**already anywhere under `<target>`**. Each match is a *duplicate* and you decide
what to do with it:

| choice | effect |
| --- | --- |
| `o` / `overwrite` | copy the source bytes over the matching target file (it keeps its folder **and** name) |
| `s` / `skip-duplicate` | leave the target; don't bring the source in (`move` leaves the source too) |
| `r` / `rename` | rename the matching target file to the source's name (folder unchanged); bytes untouched |

```bash
mc copy ~/incoming ~/library                 # ask per duplicate
mc move ~/incoming ~/library -m rename        # one choice for all duplicates
mc move ~/incoming ~/library -y               # no prompts; duplicates -> overwrite
```

`-m` / `--mode` applies one choice to every duplicate; without it you're asked
per file. `-y` skips the final confirmation and makes duplicates default to
`overwrite`. A source file whose destination path is already taken by a
*non-duplicate* is skipped (never overwritten). The full plan is shown as a TOON
preview before anything happens.

### `mc info`

```
mc info <file> [--format=toon|json]
```

Prints path, size and modified time; container format, duration and bitrate;
every stream's codec and parameters; and all embedded metadata, including image
EXIF and PNG text.

```bash
mc info clip.mkv
mc info photo.jpg --format=json
```

### `mc update`

```
mc update [-y] [--preview]
```

Update `mc` to the latest GitHub release; exits cleanly when already current.
It replaces the running binary wherever it lives on your `PATH` (following
symlinks). On Windows the current `mc.exe` is kept as `mc-<version>.exe`.
See [Stay up to date](#stay-up-to-date).

Only **stable** releases are used by default. If a newer **preview**
(pre-release) is also published, `mc update` reports it: when there is no stable
update to offer, it prompts to install the preview instead; when a stable update
*is* offered, the preview is noted so you can re-run with `--preview`.
`--preview` goes straight to the newest preview build. With `-y`, a preview is
installed only when `--preview` is also given.

### `mc version`

```
mc version
```

Print the `mc` version, the statically linked FFmpeg version, and the license
notice.

## Configuration

Flags > `MC_*` environment variables > `media-cli.yaml` > defaults.

| Setting | Env | Default |
| --- | --- | --- |
| `media.extensions` | `MC_MEDIA_EXTENSIONS` | video + image extensions above |
| `hash.metadata_key` | `MC_HASH_METADATA_KEY` | `mc.hash` |
| `hash.name_length` | `MC_HASH_NAME_LENGTH` | `6` |
| `assume_yes` | `MC_ASSUME_YES` | `false` |

## Building & contributing

See [DEVELOPMENT.md](DEVELOPMENT.md) for building from source, the vendored
FFmpeg setup, the release pipeline and the repo layout.

## License

`mc` is released under the [MIT License](LICENSE).

The binaries statically link **FFmpeg** (`libavformat` / `libavcodec` /
`libavutil`), which is licensed under **LGPL-2.1-or-later**. FFmpeg is built with
no GPL or nonfree components, so the combined binary is freely redistributable.
See [THIRD-PARTY-NOTICES.md](THIRD-PARTY-NOTICES.md) for the full notice,
corresponding-source pointer and relinking instructions; the license texts are
in [`third_party/ffmpeg/`](third_party/ffmpeg/).
