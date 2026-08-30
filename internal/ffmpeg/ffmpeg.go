// Package ffmpeg is a thin cgo wrapper over a vendored, statically linked build
// of the FFmpeg libraries. No ffmpeg binary is required at build or run time.
//
// The static libraries live under third_party/ffmpeg/<goos>_<goarch>/. Only
// linux/amd64 is committed; other targets are produced in CI by
// scripts/build-ffmpeg.sh before `go build`. To build locally for a platform
// whose libraries are not present, run that script first.
package ffmpeg

/*
#cgo linux,amd64   CFLAGS:  -I${SRCDIR}/../../third_party/ffmpeg/linux_amd64/include
#cgo linux,amd64   LDFLAGS: ${SRCDIR}/../../third_party/ffmpeg/linux_amd64/lib/libavformat.a ${SRCDIR}/../../third_party/ffmpeg/linux_amd64/lib/libavcodec.a ${SRCDIR}/../../third_party/ffmpeg/linux_amd64/lib/libavutil.a -lm -lz -latomic -lpthread

#cgo linux,arm64   CFLAGS:  -I${SRCDIR}/../../third_party/ffmpeg/linux_arm64/include
#cgo linux,arm64   LDFLAGS: ${SRCDIR}/../../third_party/ffmpeg/linux_arm64/lib/libavformat.a ${SRCDIR}/../../third_party/ffmpeg/linux_arm64/lib/libavcodec.a ${SRCDIR}/../../third_party/ffmpeg/linux_arm64/lib/libavutil.a -lm -lz -latomic -lpthread

#cgo darwin,arm64  CFLAGS:  -I${SRCDIR}/../../third_party/ffmpeg/darwin_arm64/include
#cgo darwin,arm64  LDFLAGS: ${SRCDIR}/../../third_party/ffmpeg/darwin_arm64/lib/libavformat.a ${SRCDIR}/../../third_party/ffmpeg/darwin_arm64/lib/libavcodec.a ${SRCDIR}/../../third_party/ffmpeg/darwin_arm64/lib/libavutil.a -lm -lz -liconv

#cgo darwin,amd64  CFLAGS:  -I${SRCDIR}/../../third_party/ffmpeg/darwin_amd64/include
#cgo darwin,amd64  LDFLAGS: ${SRCDIR}/../../third_party/ffmpeg/darwin_amd64/lib/libavformat.a ${SRCDIR}/../../third_party/ffmpeg/darwin_amd64/lib/libavcodec.a ${SRCDIR}/../../third_party/ffmpeg/darwin_amd64/lib/libavutil.a -lm -lz -liconv

#cgo windows,amd64 CFLAGS:  -I${SRCDIR}/../../third_party/ffmpeg/windows_amd64/include
#cgo windows,amd64 LDFLAGS: ${SRCDIR}/../../third_party/ffmpeg/windows_amd64/lib/libavformat.a ${SRCDIR}/../../third_party/ffmpeg/windows_amd64/lib/libavcodec.a ${SRCDIR}/../../third_party/ffmpeg/windows_amd64/lib/libavutil.a -lm -lz -lbcrypt -lws2_32 -lsecur32 -lole32 -luser32

#include <stdlib.h>
#include <libavutil/avutil.h>
#include <libavformat/avformat.h>
#include <libavutil/log.h>
#include "bridge.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

const (
	hashHexLen = 32 // md5
	errBufLen  = 256
)

// ErrTagAbsent is returned by ReadTag when the requested key is not present.
var ErrTagAbsent = errors.New("metadata tag not present")

// BuildInfo describes the statically linked FFmpeg libraries, for attribution
// in `mc version` (FFmpeg is LGPL-2.1-or-later — see THIRD-PARTY-NOTICES.md).
func BuildInfo() string {
	return fmt.Sprintf("FFmpeg %s / libavformat %d.%d.%d (LGPL-2.1-or-later)",
		C.GoString(C.av_version_info()),
		C.LIBAVFORMAT_VERSION_MAJOR, C.LIBAVFORMAT_VERSION_MINOR, C.LIBAVFORMAT_VERSION_MICRO)
}

func init() {
	// Keep libav* silent by default. The CLI reports genuine failures through
	// its own error path; FFmpeg's AV_LOG_ERROR chatter about individual
	// malformed packets (e.g. "Invalid NAL unit size", "missing picture in
	// access unit") is noise for a hashing tool and does not affect the result.
	// `mc <cmd> -v` / `--debug` brings it back.
	C.av_log_set_level(C.AV_LOG_FATAL)
}

// SetVerbose raises libav* logging when --verbose / --debug is set: info-level
// for --verbose, verbose-level for --debug.
func SetVerbose(debug bool) {
	if debug {
		C.av_log_set_level(C.AV_LOG_VERBOSE)
	} else {
		C.av_log_set_level(C.AV_LOG_INFO)
	}
}

// avError renders a negative AVERROR code plus the C-side message.
func avError(op string, code C.int, errbuf *C.char) error {
	msg := C.GoString(errbuf)
	if msg == "" {
		msg = "ffmpeg error"
	}
	return fmt.Errorf("%s: %s (%d)", op, msg, int(code))
}

// StreamHash returns the lowercase hex MD5 of the file's video and audio
// elementary streams, independent of any container metadata. It is equivalent
// to `ffmpeg -i <path> -map 0:v? -map 0:a? -c copy -f hash -hash md5 -`.
func StreamHash(path string) (string, error) {
	return StreamHashLimit(path, 0)
}

// StreamHashLimit is StreamHash bounded to roughly the first maxBytes of copied
// packet payload (maxBytes <= 0 hashes the whole file). It lets the fast
// "*-10m" hash methods cap work on very large files at the cost of only
// fingerprinting a prefix of the streams.
func StreamHashLimit(path string, maxBytes int64) (string, error) {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))

	out := make([]C.char, hashHexLen+1)
	errbuf := make([]C.char, errBufLen)

	rc := C.mc_stream_hash(cPath,
		&out[0], C.size_t(len(out)), C.int64_t(maxBytes),
		&errbuf[0], C.size_t(len(errbuf)))
	if rc < 0 {
		return "", avError("hash "+path, rc, &errbuf[0])
	}
	return C.GoString(&out[0]), nil
}

// ImageHash returns the lowercase hex MD5 of a still image's decoded pixel
// data, independent of every form of embedded metadata (EXIF, XMP, ICC, text
// chunks, comments). Animated inputs fold in every frame.
func ImageHash(path string) (string, error) {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))

	out := make([]C.char, hashHexLen+1)
	errbuf := make([]C.char, errBufLen)

	rc := C.mc_image_hash(cPath,
		&out[0], C.size_t(len(out)),
		&errbuf[0], C.size_t(len(errbuf)))
	if rc < 0 {
		return "", avError("image hash "+path, rc, &errbuf[0])
	}
	return C.GoString(&out[0]), nil
}

// WriteTag remuxes src to dst (stream copy, all streams and metadata preserved)
// and sets the freeform global tag key=value. On MP4/MOV the mdta key box is
// used so a non-standard key (e.g. mc.hash) survives.
func WriteTag(src, dst, key, value string) error {
	return WriteTags(src, dst, []string{key}, []string{value}, true)
}

// WriteTags is WriteTag for several keys at once (one remux). keys and values
// are parallel slices. dst must not already exist and its extension selects the
// container. Equivalent to `ffmpeg -i <src> -map 0 -c copy -map_metadata 0
// -metadata k1=v1 -metadata k2=v2 <dst>`.
//
// movFreeform selects the MP4/MOV metadata style: true keeps arbitrary keys via
// the mdta box (round-trips through mc, invisible to Windows); false writes the
// iTunes ilst atoms that Windows Explorer and QuickTime read, at the cost of
// dropping keys the mp4 muxer does not recognise. It is ignored for other
// containers.
func WriteTags(src, dst string, keys, values []string, movFreeform bool) error {
	if len(keys) != len(values) {
		return fmt.Errorf("write tags %s: %d keys but %d values", src, len(keys), len(values))
	}
	cSrc := C.CString(src)
	cDst := C.CString(dst)
	defer C.free(unsafe.Pointer(cSrc))
	defer C.free(unsafe.Pointer(cDst))

	ck := make([]*C.char, len(keys)+1) // +1 keeps &ck[0] valid when empty
	cv := make([]*C.char, len(values)+1)
	for i := range keys {
		ck[i] = C.CString(keys[i])
		cv[i] = C.CString(values[i])
	}
	defer func() {
		for i := range keys {
			C.free(unsafe.Pointer(ck[i]))
			C.free(unsafe.Pointer(cv[i]))
		}
	}()

	movFF := C.int(0)
	if movFreeform {
		movFF = 1
	}

	errbuf := make([]C.char, errBufLen)
	rc := C.mc_write_tags(cSrc, cDst,
		(**C.char)(unsafe.Pointer(&ck[0])), (**C.char)(unsafe.Pointer(&cv[0])),
		C.int(len(keys)), movFF, &errbuf[0], C.size_t(len(errbuf)))
	if rc < 0 {
		return avError("write tags "+src, rc, &errbuf[0])
	}
	return nil
}

// ReadTag returns the value of the freeform global tag key, or ErrTagAbsent.
func ReadTag(path, key string) (string, error) {
	cPath := C.CString(path)
	cKey := C.CString(key)
	defer C.free(unsafe.Pointer(cPath))
	defer C.free(unsafe.Pointer(cKey))

	out := make([]C.char, 512)
	errbuf := make([]C.char, errBufLen)
	rc := C.mc_read_tag(cPath, cKey, &out[0], C.size_t(len(out)), &errbuf[0], C.size_t(len(errbuf)))
	switch {
	case rc == 1:
		return "", ErrTagAbsent
	case rc < 0:
		return "", avError("read tag "+path, rc, &errbuf[0])
	default:
		return C.GoString(&out[0]), nil
	}
}
