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
| [`mc hash`](#mc-hash) | Hash media by content, then tag and rename each file |
| [`mc list`](#mc-list) | List a folder's media with size and metadata, filter & sort |
| [`mc set`](#mc-set) | Write chosen metadata onto matching files in a folder |
| [`mc info`](#mc-info) | Dump everything known about one file |
| [`mc update`](#mc-update) | Update `mc` to the latest release |
| [`mc version`](#mc-version) | Print version and bundled-FFmpeg info |

Global flags: `-v/--verbose`, `--debug`, `--config <path>`.

### `mc hash`

```
mc hash [file or folder] [-y] [-f] [-r]   # target defaults to the current directory
```

Hash media by content, then write the hash into the file and rename it. Given a
folder, only its own files are processed; pass `-r` / `--recursive` to descend
into subfolders.

```bash
mc hash                       # hash the current directory
mc hash movie.mp4             # hash one file, confirm changes
mc hash -r ~/Videos           # include subfolders
mc hash ~/Photos -y          # no confirmation prompt
mc hash ~/Photos -f          # re-hash even files that already have mc.hash
```

| | |
| --- | --- |
| **Video** `.mp4 .mkv .mov .m4v .webm .avi` | MD5 of the encoded video+audio streams (stream copy, no decoding) — container metadata ignored |
| **Images** `.jpg .jpeg .png .gif .webp` (+ `.jpe .jfif .apng`) | MD5 of the decoded pixels — EXIF / XMP / ICC / comments ignored |

As each file is processed it prints a preview line — `+` write & rename, `»`
rename only (tag already correct), `~` re-tag stale, `=` already current — so
progress is visible on large folders:

```
Preview (mc.hash):
  + trip/IMG_1.jpg  6cb96fb6…98e5  ->  trip/IMG_1.6cb96f.jpg
  = trip/IMG_2.fbc6ec.jpg  fbc6ec44…409b
```

Then, after `[y/N]` (or with `-y`), for each pending file it:

1. writes the tag `mc.hash=<hash>` into the file — pixels and streams are left
   byte-for-byte untouched;
2. renames it to `<name>.<first 6 of hash>.<ext>` (replacing an existing
   `.<6-hex>` slot rather than appending a second one).

**By default a file that already carries a valid `mc.hash` tag is trusted and
not re-hashed** — the stored value is used directly, so re-running on a large,
already-processed folder is near-instant (and files that only need renaming are
moved, not remuxed). Pass `-f` / `--force` to re-compute every hash and compare
it with the stored tag; a mismatch is flagged (`~`, "stale mc.hash … replaced")
and the tag rewritten.

### `mc list`

```
mc list [folder] [--meta=<fields>] [--select=<expr>] [--sort-by=<keys>] [--format=toon|json|csv] [-r]
```

Lists `[folder]` (default: current directory). Only that folder's own files are
listed; pass `-r` / `--recursive` to descend into subfolders. Each file row is
`filename`, `size`, `mc.hash`, `rating`, `artist`, `comment` (+ any `--meta`
columns). `toon` and `json` nest the rows under their folders; `csv` is one flat
table with absolute paths.

```bash
mc list ~/Photos
mc list -r ~/Photos --meta=make,model,DateTimeOriginal
mc list -r ~/Videos --select='rating>=4 and size>1g' --sort-by='rating desc, size desc'
mc list -r ~/Photos --format=csv > inventory.csv       # flat, absolute paths
```

```
$ mc list tmp
6 file(s)
"tmp/":
  "img/":
    files[2]{filename,size,mc.hash,rating,artist,comment}:
      photo.jpg,84KB,4358a46e…d7,4,,summer trip
  "video/":
    files[1]{filename,size,mc.hash,rating,artist,comment}:
      clip.mp4,97MB,8f9b6e8b…37,,Adam Yu,
```

- `--meta` – extra metadata columns, comma separated.
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
- Video metadata is written as standard MP4/MOV atoms (`©nam`, `©ART`, `©cmt`,
  …), so **Windows Explorer and QuickTime show it**. Use canonical field names —
  `artist`, `comment`, `title`, `genre`, `date`. Keys the MP4 muxer doesn't
  recognise (e.g. a bare `rating` on a video) aren't retained; `mc set` warns
  when that happens.
- Image tags go into the file's native text area (PNG `tEXt`, JPEG `COM`, GIF
  comment, WebP chunk), the same store `mc.hash` uses. `mc list` / `mc info`
  read them back.
- `--select` uses the same expression as [`mc list`](#mc-list). **Pass it** —
  without it every media file in the folder is updated.
- The change is previewed (`key: <current> -> <new>` per file) and confirmed
  unless `-y`.
- `-r` / `--recursive` – descend into subfolders (off by default).

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
