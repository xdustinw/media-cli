package hashcmd

import (
	"crypto/md5"
	"crypto/sha256"
	"fmt"
	"hash"
	"io"
	"os"
	"strings"

	"github.com/xdustinw/media-cli/internal/ffmpeg"
	"github.com/xdustinw/media-cli/internal/media"
)

// Method selects how `mc hash` fingerprints a file.
//
//	ffmpeg      md5 of the whole encoded video+audio stream (metadata-independent);
//	            the only method that also writes the mc.hash tag into the file
//	ffmpeg-10m  same, but only the first ~10 MB of stream data (fast; rename only)
//	md5 / sha   md5 / sha-256 of the raw file bytes                  (rename only)
//	md5-10m /
//	sha-10m     md5 / sha-256 of the first 10 MB of raw file bytes   (rename only)
//
// Every method except "ffmpeg" only renames the file to <name>.<first 6 of
// hash>.<ext>; none of them read or write file metadata.
type Method string

const (
	MethodFFmpeg    Method = "ffmpeg"
	MethodFFmpeg10M Method = "ffmpeg-10m"
	MethodMD5       Method = "md5"
	MethodMD510M    Method = "md5-10m"
	MethodSHA       Method = "sha"
	MethodSHA10M    Method = "sha-10m"
)

// MethodAuto is the behaviour when no -m/--method is given: try ffmpeg-10m per
// file and fall back to md5-10m when the ffmpeg attempt errors.
const MethodAuto Method = ""

// DefaultMethod is the concrete method MethodAuto starts from.
const DefaultMethod = MethodFFmpeg10M

// byteCap10M bounds the "*-10m" methods.
const byteCap10M = 10 << 20

// Methods lists every accepted value, in help order.
var Methods = []Method{
	MethodFFmpeg, MethodMD5, MethodSHA,
	MethodFFmpeg10M, MethodMD510M, MethodSHA10M,
}

// ParseMethod validates s. An empty string yields MethodAuto (try ffmpeg-10m,
// fall back to md5-10m per file).
func ParseMethod(s string) (Method, error) {
	if strings.TrimSpace(s) == "" {
		return MethodAuto, nil
	}
	m := Method(strings.ToLower(strings.TrimSpace(s)))
	for _, k := range Methods {
		if m == k {
			return m, nil
		}
	}
	names := make([]string, len(Methods))
	for i, k := range Methods {
		names[i] = string(k)
	}
	return "", fmt.Errorf("unknown method %q (want one of: %s)", s, strings.Join(names, ", "))
}

// resolve computes the digest for path. For MethodAuto it tries ffmpeg-10m and
// falls back to md5-10m when that errors, returning the method that succeeded.
func (m Method) resolve(path string, kind media.Kind) (hash string, used Method, err error) {
	if m != MethodAuto {
		h, e := m.digest(path, kind)
		return h, m, e
	}
	h, ffErr := MethodFFmpeg10M.digest(path, kind)
	if ffErr == nil {
		return h, MethodFFmpeg10M, nil
	}
	if h2, e := MethodMD510M.digest(path, kind); e == nil {
		return h2, MethodMD510M, nil
	}
	// Both failed — report the primary (ffmpeg) failure, the informative one.
	return "", MethodAuto, ffErr
}

// WritesTag reports whether the method also stores the digest as mc.hash
// metadata inside the file. Only the full "ffmpeg" method does.
func (m Method) WritesTag() bool { return m == MethodFFmpeg }

// usesFFmpeg reports whether the digest comes from the FFmpeg libraries (stream
// copy for video, pixel decode for images) rather than raw file bytes.
func (m Method) usesFFmpeg() bool { return m == MethodFFmpeg || m == MethodFFmpeg10M }

// cap returns the byte limit for this method (0 = whole file/stream).
func (m Method) cap() int64 {
	switch m {
	case MethodFFmpeg10M, MethodMD510M, MethodSHA10M:
		return byteCap10M
	default:
		return 0
	}
}

// digest computes this method's hash of path.
func (m Method) digest(path string, kind media.Kind) (string, error) {
	switch m {
	case MethodFFmpeg, MethodFFmpeg10M:
		if kind == media.KindImage {
			// A byte cap is meaningless for a decoded still; hash all pixels.
			return ffmpeg.ImageHash(path)
		}
		return ffmpeg.StreamHashLimit(path, m.cap())
	case MethodMD5, MethodMD510M:
		return fileDigest(path, md5.New(), m.cap())
	case MethodSHA, MethodSHA10M:
		return fileDigest(path, sha256.New(), m.cap())
	default:
		return "", fmt.Errorf("unknown method %q", m)
	}
}

func fileDigest(path string, h hash.Hash, limit int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var r io.Reader = f
	if limit > 0 {
		r = io.LimitReader(f, limit)
	}
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
