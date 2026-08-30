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
	read    func(data []byte, key string) (string, error)
	readAll func(data []byte) (map[string]string, error)
	write   func(data []byte, key, value string) ([]byte, error)
}

func codecFor(path string) (codec, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".apng":
		return codec{read: pngRead, readAll: pngReadAll, write: pngWrite}, true
	case ".jpg", ".jpeg", ".jpe", ".jfif":
		return codec{read: jpegRead, readAll: jpegReadAll, write: jpegWrite}, true
	case ".gif":
		return codec{read: gifRead, readAll: gifReadAll, write: gifWrite}, true
	case ".webp":
		return codec{read: webpRead, readAll: webpReadAll, write: webpWrite}, true
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

// ReadAll returns every mc-written tag in the image at path.
func ReadAll(path string) (map[string]string, error) {
	c, ok := codecFor(path)
	if !ok {
		return nil, ErrUnsupportedFormat
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return c.readAll(data)
}

// Write reads src, inserts or replaces key=value, and writes the result to dst.
// Pixel data is copied verbatim. dst and src may not be the same path.
func Write(src, dst, key, value string) error {
	return WriteMany(src, dst, []string{key}, []string{value})
}

// WriteMany inserts or replaces each keys[i]=values[i] and writes the result to
// dst. Pixel data is copied verbatim.
func WriteMany(src, dst string, keys, values []string) error {
	if len(keys) != len(values) {
		return errors.New("imgmeta: keys and values length mismatch")
	}
	c, ok := codecFor(src)
	if !ok {
		return ErrUnsupportedFormat
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	for i := range keys {
		if data, err = c.write(data, keys[i], values[i]); err != nil {
			return err
		}
	}
	return os.WriteFile(dst, data, 0o644)
}
