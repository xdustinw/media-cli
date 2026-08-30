// Package imgmeta reads and writes a single freeform key/value tag inside still
// image files, without ever touching pixel data. It exists because FFmpeg's
// muxers cannot inject metadata into images without re-encoding.
//
// Each format stores the tag in its native text facility:
//
//	PNG / APNG  tEXt chunk           (keyword = key)
//	JPEG        COM marker segment   (payload = "key=value")
//	GIF         Comment Extension    (payload = "key=value")
//	WebP        RIFF "XMP " chunk?   -> uses an "mcTG" chunk ("key=value")
//
// The reader and writer are a matched pair; values written here round-trip
// through Read here, not necessarily through other tools.
package imgmeta

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ErrTagAbsent is returned by Read when the key is not present.
var ErrTagAbsent = errors.New("image metadata tag not present")

// ErrUnsupportedFormat is returned for image extensions with no writer.
var ErrUnsupportedFormat = errors.New("unsupported image format for metadata")

type codec struct {
	read  func(data []byte, key string) (string, error)
	write func(data []byte, key, value string) ([]byte, error)
}

func codecFor(path string) (codec, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".apng":
		return codec{read: pngRead, write: pngWrite}, true
	case ".jpg", ".jpeg", ".jpe", ".jfif":
		return codec{read: jpegRead, write: jpegWrite}, true
	case ".gif":
		return codec{read: gifRead, write: gifWrite}, true
	case ".webp":
		return codec{read: webpRead, write: webpWrite}, true
	default:
		return codec{}, false
	}
}

// Supported reports whether path's extension has a metadata reader/writer.
func Supported(path string) bool {
	_, ok := codecFor(path)
	return ok
}

// Read returns the value stored under key in the image at path.
func Read(path, key string) (string, error) {
	c, ok := codecFor(path)
	if !ok {
		return "", ErrUnsupportedFormat
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return c.read(data, key)
}

// Write reads src, inserts or replaces key=value, and writes the result to dst.
// Pixel data is copied verbatim. dst and src may not be the same path.
func Write(src, dst, key, value string) error {
	c, ok := codecFor(src)
	if !ok {
		return ErrUnsupportedFormat
	}
	in, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	out, err := c.write(in, key, value)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, out, 0o644)
}
