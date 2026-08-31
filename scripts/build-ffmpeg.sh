#!/usr/bin/env bash
#
# Builds the static FFmpeg libraries that internal/ffmpeg links against and
# installs them under third_party/ffmpeg/<goos>_<goarch>/. No ffmpeg binary is
# produced or needed.
#
# Usage:
#   scripts/build-ffmpeg.sh [ffmpeg-source-dir]
#
# Source resolution order: the argument, $FFMPEG_SRC, ../ref/ffmpeg, or a
# shallow clone of FFmpeg pinned to $FFMPEG_COMMIT.
#
# Target platform: $TARGET_GOOS / $TARGET_GOARCH (default: the host's `go env`).
# Building for windows/amd64 from Linux uses the mingw-w64 cross toolchain.
#
# Build deps: C toolchain, make, pkg-config, nasm, zlib headers. For the Windows
# cross build also: gcc-mingw-w64-x86-64.
set -euo pipefail

FFMPEG_COMMIT="${FFMPEG_COMMIT:-b32f8d1c2377079302d23f82d555d13deda68c57}"
FFMPEG_GIT="${FFMPEG_GIT:-https://github.com/FFmpeg/FFmpeg.git}"

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

goos="${TARGET_GOOS:-$(go env GOOS)}"
goarch="${TARGET_GOARCH:-$(go env GOARCH)}"
prefix="$repo_root/third_party/ffmpeg/${goos}_${goarch}"

jobs="$( (nproc 2>/dev/null || sysctl -n hw.logicalcpu 2>/dev/null || echo 2) )"

# --- resolve FFmpeg source -------------------------------------------------
src="${1:-${FFMPEG_SRC:-}}"
if [[ -z "$src" && -x "$repo_root/../ref/ffmpeg/configure" ]]; then
	src="$repo_root/../ref/ffmpeg"
fi
if [[ -z "$src" ]]; then
	src="$repo_root/third_party/.ffmpeg-src"
	if [[ ! -x "$src/configure" ]]; then
		echo ">> cloning FFmpeg @ ${FFMPEG_COMMIT}"
		rm -rf "$src"
		git init -q "$src"
		git -C "$src" remote add origin "$FFMPEG_GIT"
		git -C "$src" fetch -q --depth 1 origin "$FFMPEG_COMMIT"
		git -C "$src" checkout -q FETCH_HEAD
	fi
fi
if [[ ! -x "$src/configure" ]]; then
	echo "error: no FFmpeg source at $src" >&2
	exit 1
fi

# --- configure flags -----------------------------------------------------
# Still-image decoders needed by `mc hash` for images. vp8 backs lossy WebP.
image_decoders="mjpeg,mjpegb,jpegls,ljpeg,jpeg2000,png,apng,webp,vp8,gif,bmp,\
tiff,targa,pcx,sgi,sunrast,psd,xpm,xbm,xwd,xface,pam,pbm,pgm,pgmyuv,ppm,pfm,phm,\
pgx,qoi,hdr,exr,dpx,pictor,gem,cri,vbn"

# Audio/video decoders for `mc concat`'s re-encode path (mismatched inputs).
av_decoders="h264,hevc,mpeg4,mpeg2video,mpeg1video,vp9,msmpeg4v3,\
aac,ac3,eac3,mp3,mp2,opus,vorbis,flac,alac,\
pcm_s16le,pcm_s24le,pcm_s32le,pcm_f32le,pcm_u8,pcm_s16be,pcm_s24be"

# Native (LGPL) encoders `mc concat` re-encodes mismatched inputs with. There is
# no h264/hevc encoder here — matching an h264 source falls back to mpeg4/aac.
av_encoders="mpeg4,mpeg2video,mjpeg,aac,ac3,flac,pcm_s16le"

cfg=(
	--prefix="$prefix"
	--disable-shared --enable-static --enable-pic
	--disable-programs --disable-doc
	--disable-htmlpages --disable-manpages --disable-podpages --disable-txtpages
	--disable-avdevice --disable-avfilter
	--enable-swscale --enable-swresample
	--disable-encoders --disable-decoders --disable-filters
	--enable-decoder="$image_decoders,$av_decoders"
	--enable-encoder="$av_encoders"
	--disable-network --disable-autodetect --enable-zlib
	--disable-debug
)

case "$goos" in
	darwin)
		# Avoid pulling in system media frameworks.
		cfg+=(--disable-videotoolbox --disable-audiotoolbox --disable-appkit
		      --disable-avfoundation --disable-coreimage --disable-securetransport)
		;;
	windows)
		cfg+=(--target-os=mingw32 --arch=x86_64 --enable-cross-compile
		      --cross-prefix=x86_64-w64-mingw32- --pkg-config=pkg-config
		      --disable-pthreads --enable-w32threads)
		;;
esac

# --- build -------------------------------------------------------------
build_dir="$(mktemp -d)"
trap 'rm -rf "$build_dir"' EXIT

echo ">> configuring ${goos}/${goarch} (source: $src)"
( cd "$build_dir" && "$src/configure" "${cfg[@]}" )

echo ">> building (-j$jobs)"
make -C "$build_dir" -j"$jobs"

echo ">> installing into $prefix"
rm -rf "$prefix"
make -C "$build_dir" install
rm -rf "$prefix/share"

echo ">> done"
find "$prefix/lib" -name '*.a' -printf '   %p (%s bytes)\n' 2>/dev/null ||
	find "$prefix/lib" -name '*.a' -exec ls -l {} +
echo ">> pkg-config Libs.private (system libraries this platform needs):"
grep -h '^Libs' "$prefix"/lib/pkgconfig/*.pc || true
