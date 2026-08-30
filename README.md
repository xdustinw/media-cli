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

**macOS** (Apple Silicon — Intel: swap `arm64` → `amd64`)
```bash
wget -O mc https://github.com/xdustinw/media-cli/releases/latest/download/mc-darwin-arm64
chmod +x ./mc && ./mc version
# if Gatekeeper complains: xattr -d com.apple.quarantine ./mc
```

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
mc update        # checks GitHub, asks, replaces the binary in place
mc update -y     # no questions
```

`mc update` finds the running binary wherever it is on your `PATH` (Windows keeps
the old `mc.exe` as `mc-<version>.exe`).

## Commands

| Command | Description |
| --- | --- |
| [`mc hash`](#mc-hash) | Hash media by content, then tag and rename each file |
| [`mc update`](#mc-update) | Update `mc` to the latest release |
| [`mc version`](#mc-version) | Print version and bundled-FFmpeg info |

Global flags: `-v/--verbose`, `--debug`, `--config <path>`.

### `mc hash`

```
mc hash <file or folder> [-y]
```

Hash media by content, then write the hash into the file and rename it.

```bash
mc hash movie.mp4              # hash one file, confirm changes
mc hash ~/Videos              # recurse a folder
mc hash ~/Photos -y           # no confirmation prompt
```

| | |
| --- | --- |
| **Video** `.mp4 .mkv .mov .m4v .webm .avi` | MD5 of the encoded video+audio streams — container metadata ignored |
| **Images** `.jpg .jpeg .png .gif .webp` (+ `.jpe .jfif .apng`) | MD5 of the decoded pixels — EXIF / XMP / ICC / comments ignored |

For each file it prints `<hash> - <path>`, shows a preview, then (after `[y/N]`
or with `-y`):

1. writes the tag `mc.hash=<hash>` into the file — pixels and streams are left
   byte-for-byte untouched;
2. renames it to `<name>.<first 6 of hash>.<ext>`.

Files already hashed and named are skipped. A file whose stored `mc.hash` no
longer matches its content gets a warning and is re-tagged.

### `mc update`

```
mc update [-y]
```

Update `mc` to the latest GitHub release; exits cleanly when already current.
It replaces the running binary wherever it lives on your `PATH` (following
symlinks). On Windows the current `mc.exe` is kept as `mc-<version>.exe`.
See [Stay up to date](#stay-up-to-date).

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
