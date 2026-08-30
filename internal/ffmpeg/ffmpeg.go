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
	// Keep libav* quiet by default; the CLI reports its own errors.
	C.av_log_set_level(C.AV_LOG_ERROR)
}

// SetVerbose raises libav* logging to info/verbose (debug=true) level. Call it
// from the CLI surface when --verbose / --debug is set.
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
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))

	out := make([]C.char, hashHexLen+1)
	errbuf := make([]C.char, errBufLen)

	rc := C.mc_stream_hash(cPath,
		&out[0], C.size_t(len(out)),
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
// and sets the freeform global tag key=value. dst must not already exist and
// its extension selects the container. Equivalent to
// `ffmpeg -i <src> -map 0 -c copy -map_metadata 0 -metadata key=value <dst>`
// (with -movflags use_metadata_tags for MP4/MOV).
func WriteTag(src, dst, key, value string) error {
	cSrc := C.CString(src)
	cDst := C.CString(dst)
	cKey := C.CString(key)
	cVal := C.CString(value)
	defer C.free(unsafe.Pointer(cSrc))
	defer C.free(unsafe.Pointer(cDst))
	defer C.free(unsafe.Pointer(cKey))
	defer C.free(unsafe.Pointer(cVal))

	errbuf := make([]C.char, errBufLen)
	rc := C.mc_write_tag(cSrc, cDst, cKey, cVal, &errbuf[0], C.size_t(len(errbuf)))
	if rc < 0 {
		return avError("write tag "+src, rc, &errbuf[0])
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
