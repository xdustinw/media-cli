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
| [`mc dedupe`](#mc-dedupe) | Delete duplicate copies of hash-named files across folders |
| [`mc list-missing`](#mc-list-missing) | List a folder's files whose content hash is in no target folder |
| [`mc delete`](#mc-delete) | Delete files under folders that match a `--select` filter |
| [`mc split`](#mc-split) | Cut a media file at timestamps into numbered parts |
| [`mc concat`](#mc-concat) | Join media files into one |
| [`mc info`](#mc-info) | Dump everything known about one file |
| [`mc update`](#mc-update) | Update `mc` to the latest release |
| [`mc version`](#mc-version) | Print version and bundled-FFmpeg info |

Global flags: `-v/--verbose`, `--debug`, `--config <path>`.

### `mc hash`

```
mc hash [file or folder ...] [-m <method>] [--select=<expr>] [-y] [-f] [--nr]
```

Fingerprint media by content and rename each file to
`<name>.<first 6 of hash>.<ext>` (replacing a short hash already in the name).
One or more files/folders may be given (default: current directory); folders are
scanned **recursively** unless `--nr` is passed.

```bash
mc hash                       # current directory, recursively, default method
mc hash movie.mp4 clip.mkv    # several explicit files
mc hash --nr ~/Photos         # this folder only, no subfolders
mc hash ~/Videos -m ffmpeg    # full metadata-independent hash + write mc.hash
mc hash ~/Videos -m md5-10m   # fastest: md5 of the first 10 MB of file bytes
mc hash ~/Photos --select='name=IMG_*'   # only matching files (asks first)
```

**`-m` / `--method`** — how the fingerprint is computed:

| method | hashes | speed | writes `mc.hash`? |
| --- | --- | --- | --- |
| *(default)* | `ffmpeg-10m`, then `md5-10m` for any file ffmpeg can't read | fast | no |
| `ffmpeg-10m` | md5 of the first ~10 MB of the video+audio stream | fast | no |
| `ffmpeg` | md5 of the whole video+audio stream (video) or decoded pixels (images); metadata ignored | slow | **yes** |
| `md5` / `sha` | md5 / sha-256 of the raw file bytes (metadata included) | medium | no |
| `md5-10m` / `sha-10m` | md5 / sha-256 of the first 10 MB of file bytes | fastest | no |

Only `ffmpeg` reads or writes file metadata; every other method just renames.
`ffmpeg` and `ffmpeg-10m` treat two files that differ only in metadata as
identical; `md5` / `sha` do not (they see the raw bytes).

**`--select`** filters the files (`name`, `path`, `ext`, `size`, `modifiedAt`,
`kind`); you're shown the matches and asked to confirm before hashing, unless
`-y`.

As each file is processed it prints a preview line — `+` add hash to name, `»`
replace a different hash already in the name (or, for `ffmpeg`, rename only), `~`
re-tag stale, `=` already current; `[md5-10m]` marks a default-mode fallback:

```
Preview (auto (ffmpeg-10m, md5-10m fallback), rename only):
  + trip/IMG_1.jpg  6cb96fb66986b44f505831c54f69598e  ->  trip/IMG_1.6cb96f.jpg
  = trip/IMG_2.fbc6ec.jpg  fbc6ec44...
```

**A file whose name already carries a valid 6-hex slot is left untouched and
never hashed** (for `ffmpeg`, a file with a valid `mc.hash` tag is trusted the
same way). Pass `-f` / `--force` to re-hash them and re-check — a stale slot is
then replaced (`»` "replaces …"; `ffmpeg` rewrites the tag, `~` "stale mc.hash …
replaced").

For the `ffmpeg` method, after `[y/N]` (or `-y`) each pending file gets the tag
`mc.hash=<hash>` written into it (pixels and streams left byte-for-byte
untouched) and is renamed.

**Name already taken.** If the renamed-to name already exists in the same folder
(a copy that was hashed earlier), you're asked per file: `[o]verwrite` it,
`[s]kip` (keep both, leave this one un-renamed), or `[d]elete` the incoming
un-hashed file (**default** — the hashed copy is already there). With `-y` the
incoming file is always deleted.

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

Every command that processes files (`hash`, `list`, `set`, `info`, `copy`,
`move`, `dedupe`, `delete`, `split`, `concat`) prints a one-line summary to
stderr when it finishes, e.g.
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
mc copy <source> [<source> ...] <target> [-m <mode>] [--select=<expr>] [-y] [-v] [--nr]
mc move <source> [<source> ...] <target> [-m <mode>] [--select=<expr>] [--delete-source] [-y] [-v] [--nr]
```

Bring every file from the source(s) — files and/or folders — into `<target>`,
each landing at `<target>/<path relative to that source>`. Folders are scanned
recursively unless `--nr` is passed. `move` removes a source once its target is
written; `copy` leaves it. A `move` across drives copies the bytes over and
deletes the source only after verifying the copy — a failed move never loses the
original. Once a `move` finishes, any source folder left empty (at any depth,
including a source folder you named) is removed.

Before writing anything, each source file's `.<6-hex>` short hash (from
[`mc hash`](#mc-hash)) is looked up among the short hashes of the files
**already anywhere under `<target>`**. `-m` / `--mode` says what to do with every
match:

| mode | effect |
| --- | --- |
| `s` / `skip-duplicate` *(default)* | leave the target; keep the source where it is (`move` does **not** delete a skipped source) |
| `o` / `overwrite` | copy the source bytes over the matching target file (it keeps its folder and name) |
| `k` / `keep-both` | bring the source in too, at `<target>/<rel>`, alongside the existing file |

```bash
mc copy ~/incoming ~/library                  # skip duplicates (default)
mc move ~/cam1 ~/cam2 ~/library -m keep-both   # merge two folders, keep every copy
mc move ~/incoming ~/library -y                # no prompts
mc copy ~/incoming ~/library --select='name=IMG_*'
mc move ~/incoming ~/library --delete-source   # drain the source even where a copy already exists
```

**`--delete-source`** (`move` only) deletes every matching source file even when
the target already holds a content duplicate and the mode is `skip-duplicate` —
so the source folder ends up empty regardless. It respects `--select`.

**`--select`** filters the source files (`name`, `path`, `ext`, `size`,
`modifiedAt`, `kind`). `-y` skips the confirmation.

**Output.** The preview lists only the files that will actually be copied/moved;
duplicates already at the target are collapsed to a count (and repeated in the
summary line). Pass `-v` to also list the `--select` matches and the full
duplicates table. With `--delete-source` the duplicates are always listed, so
you can see which sources will be removed.

**Unhashed files.** When files in the sources or the target have no
`.<6-hex>` slot, `mc copy` / `mc move` offers to hash them first (`mc hash`,
in place). Decline and the comparison falls back to **relative path** — a
source file is a duplicate only when `<target>/<rel>` already exists. `-y`
hashes them all automatically. The full plan is shown as a TOON preview before
anything happens.

### `mc dedupe`

```
mc dedupe [folder ...] [-k <keep>] [--select=<expr>] [-y] [--nr]
```

Groups files by the `.<6-hex>` short hash in their name (from
[`mc hash`](#mc-hash)) across the given folders (default: the current
directory), then **deletes all but one copy** of each set. Folders are scanned
recursively unless `--nr`.

| `-k` / `--keep` | which copy is kept |
| --- | --- |
| `i` / `interactive` *(default)* | you're shown each set and pick which to keep (`1`-`n`), or `s` to skip that set |
| `l` / `longer-name` | the file with the longest name |
| `n` / `newer` | the most recently modified |
| `o` / `older` | the oldest |
| `f<n>` / `folder<n>` | protect the **n-th folder** given on the command line: its copies are never deleted and a duplicate in any other folder is. A set with no copy in that folder is left alone. |

```bash
mc dedupe ~/Photos ~/Backup/Photos            # review each set by hand
mc dedupe ~/Photos ~/Backup -k newer -y       # keep the newest, no prompts
mc dedupe ~/Photos --select='name=IMG_*' -k longer-name
mc dedupe ~/Library ~/Phone ~/Card -k f1 -y   # keep everything in ~/Library, prune the rest
```

Files without a `.<6-hex>` slot are offered to `mc hash` first (or, with `-y`,
hashed automatically); decline and they're grouped by **file name** instead.
The full list of deletions is shown as a TOON preview and confirmed before
anything is removed, unless `-y`.

### `mc list-missing`

```
mc list-missing <src-folder> <target-folder> [<target-folder> ...] [--select=<expr>] [-y] [--nr]
```

Walks `<src-folder>` and prints every file whose `.<6-hex>` short hash (from
[`mc hash`](#mc-hash)) is **not present on any file** under the target
folder(s) — i.e. what you still need to copy over. The source folder is scanned
recursively unless `--nr`; target folders are always recursive. Nothing is
modified (aside from an accepted hash pass). `find-missing` is an alias.

```bash
mc list-missing ~/Phone/DCIM ~/Photos                    # what's on the phone but not filed
mc list-missing ~/inbox ~/library ~/archive -y           # check against two libraries
mc list-missing ~/inbox ~/library --select='kind=video'
```

Files without a `.<6-hex>` slot (in the source or a target) are offered to
`mc hash` first (or, with `-y`, hashed automatically); decline and the
comparison falls back to the **base file name**. Missing files are printed as a
TOON table (`file`, `hash`, `size`).

### `mc delete`

```
mc delete [folder ...] --select=<expr> [-y] [--nr]
```

Deletes the files under the given folders (default: current directory) that
match `--select` — which is **required**; it is the only thing that decides
what goes. Folders are scanned recursively unless `--nr`.

```bash
mc delete ~/incoming --select='name=IMG_2* or name=IMG_3*'
mc delete . --select='ext=tmp and size<1k'
```

The matched files are shown as a TOON preview and confirmed before deletion,
unless `-y`. Any folder left empty by the deletions (at any depth) is removed
too; the current directory and a filesystem root are never removed.

### `mc split`

```
mc split <file> "<t1>,<t2>,…" [-o <folder>] [-y]
```

Cut `<file>` at the given timestamps into `<name>-Part1.<ext>`,
`<name>-Part2.<ext>`, … — N timestamps make N+1 parts. Timestamps are seconds,
`MM:SS` or `HH:MM:SS` (fractions allowed), comma separated.

```bash
mc split trip.mp4 "1:20,2:30,4:50"
mc split trip.mp4 "90,210" -o ~/clips
```

The streams are **copied, not re-encoded**, so each part starts at the nearest
keyframe at or after its timestamp (cuts are not frame-accurate). Parts go to
`-o` / `--outputFolder`, or the file's own folder.

### `mc concat`

```
mc concat <file1> <file2> [<file3> …] [-o <outputFile>] [-y]
```

Join two or more files, in order, into one output.

```bash
mc concat clip1.mp4 clip2.mp4 clip3.mp4          # -> clip1-clip2-clip3-combined.mp4
mc concat a.mp4 b.mp4 -o joined.mp4
```

- When the inputs share codec and parameters they are **stream-copied** (fast,
  lossless), with timestamps made continuous across inputs.
- When they differ, a warning is shown and **every input is re-encoded** to
  MPEG-4 video + AAC audio at the first file's resolution / frame rate / sample
  rate, then joined. This build has no H.264 encoder, so expect a quality drop;
  the output container switches to `.mkv` if the first file's extension can't
  hold MPEG-4/AAC.
- Without `-o` the result is `<file1>-<file2>-…-combined.<ext>`.

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

**`--preview`** targets the newest preview build on GitHub and offers it
**regardless of the installed version** — use it to move a stable install onto
the preview channel, or to re-install the current preview. With `-y` that
install runs without a prompt.

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
