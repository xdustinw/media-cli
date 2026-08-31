package ffmpeg

// #include <stdlib.h>
// #include "bridge.h"
import "C"

import (
	"fmt"
	"unsafe"
)

// Split cuts inFile at cutTimesSec (a comma-separated list of seconds, e.g.
// "80,150,290") into files named by outPattern, which must contain a single
// "%d" for the 1-based part number. Stream copy; cuts land on the nearest
// keyframe at or after each time. N cut points produce N+1 parts.
func Split(inFile, outPattern, cutTimesSec string) error {
	cIn := C.CString(inFile)
	cPat := C.CString(outPattern)
	cCuts := C.CString(cutTimesSec)
	defer C.free(unsafe.Pointer(cIn))
	defer C.free(unsafe.Pointer(cPat))
	defer C.free(unsafe.Pointer(cCuts))

	errbuf := make([]C.char, errBufLen)
	rc := C.mc_split(cIn, cPat, cCuts, &errbuf[0], C.size_t(len(errbuf)))
	if rc < 0 {
		return avError("split "+inFile, rc, &errbuf[0])
	}
	return nil
}

// ConcatCopy joins inFiles into outFile by stream copy, with timestamps made
// continuous across inputs. The caller must have verified the inputs share
// codec/parameters.
func ConcatCopy(inFiles []string, outFile string) error {
	if len(inFiles) == 0 {
		return fmt.Errorf("concat: no inputs")
	}
	cOut := C.CString(outFile)
	defer C.free(unsafe.Pointer(cOut))

	arr := make([]*C.char, len(inFiles)+1)
	for i, f := range inFiles {
		arr[i] = C.CString(f)
	}
	defer func() {
		for i := range inFiles {
			C.free(unsafe.Pointer(arr[i]))
		}
	}()

	errbuf := make([]C.char, errBufLen)
	rc := C.mc_concat_copy(
		(**C.char)(unsafe.Pointer(&arr[0])), C.int(len(inFiles)), cOut,
		&errbuf[0], C.size_t(len(errbuf)))
	if rc < 0 {
		return avError("concat", rc, &errbuf[0])
	}
	return nil
}

// Transcode re-encodes inFile to outFile as MPEG-4 video + AAC audio at the
// given geometry and audio rate. w <= 0 drops video; sampleRate <= 0 drops
// audio.
func Transcode(inFile, outFile string, w, h, fpsNum, fpsDen, sampleRate, channels int) error {
	cIn := C.CString(inFile)
	cOut := C.CString(outFile)
	defer C.free(unsafe.Pointer(cIn))
	defer C.free(unsafe.Pointer(cOut))

	errbuf := make([]C.char, errBufLen)
	rc := C.mc_transcode(cIn, cOut,
		C.int(w), C.int(h), C.int(fpsNum), C.int(fpsDen),
		C.int(sampleRate), C.int(channels),
		&errbuf[0], C.size_t(len(errbuf)))
	if rc < 0 {
		return avError("transcode "+inFile, rc, &errbuf[0])
	}
	return nil
}
