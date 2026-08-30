package ffmpeg

// #include <stdlib.h>
// #include <libavutil/mem.h>
// #include "bridge.h"
import "C"

import (
	"sort"
	"strconv"
	"strings"
	"time"
	"unsafe"
)

// KV is one ordered metadata entry.
type KV struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// FormatInfo describes the container.
type FormatInfo struct {
	Name     string        `json:"name"`
	LongName string        `json:"long_name,omitempty"`
	Duration time.Duration `json:"-"`
	BitRate  int64         `json:"bit_rate,omitempty"`
	Streams  int           `json:"streams"`
}

// StreamInfo describes one stream.
type StreamInfo struct {
	Index         int           `json:"index"`
	Type          string        `json:"type"`
	Codec         string        `json:"codec"`
	CodecLong     string        `json:"codec_long,omitempty"`
	Profile       string        `json:"profile,omitempty"`
	BitRate       int64         `json:"bit_rate,omitempty"`
	Duration      time.Duration `json:"-"`
	Width         int           `json:"width,omitempty"`
	Height        int           `json:"height,omitempty"`
	PixFmt        string        `json:"pix_fmt,omitempty"`
	FPS           string        `json:"fps,omitempty"`
	SAR           string        `json:"sar,omitempty"`
	SampleRate    int           `json:"sample_rate,omitempty"`
	Channels      int           `json:"channels,omitempty"`
	ChannelLayout string        `json:"channel_layout,omitempty"`
	SampleFmt     string        `json:"sample_fmt,omitempty"`
	Metadata      []KV          `json:"metadata,omitempty"`
}

// Probe is the parsed result of mc_probe.
type Probe struct {
	Format   FormatInfo   `json:"format"`
	Streams  []StreamInfo `json:"streams"`
	Metadata []KV         `json:"metadata"`
}

// Meta returns the first metadata value whose key matches name case-insensitively
// (checking container metadata first, then each stream's).
func (p *Probe) Meta(name string) (string, bool) {
	name = strings.ToLower(name)
	for _, kv := range p.Metadata {
		if strings.ToLower(kv.Key) == name {
			return kv.Value, true
		}
	}
	for _, s := range p.Streams {
		for _, kv := range s.Metadata {
			if strings.ToLower(kv.Key) == name {
				return kv.Value, true
			}
		}
	}
	return "", false
}

// MetaKeys returns every distinct metadata key (container + streams), sorted.
func (p *Probe) MetaKeys() []string {
	seen := map[string]struct{}{}
	for _, kv := range p.Metadata {
		seen[kv.Key] = struct{}{}
	}
	for _, s := range p.Streams {
		for _, kv := range s.Metadata {
			seen[kv.Key] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Inspect opens path and returns its container, stream and metadata details.
// When deep is true the first video frame is decoded so image EXIF / PNG text
// surfaces in Metadata (slower; use it for `mc info` and image listings).
func Inspect(path string, deep bool) (*Probe, error) {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))

	errbuf := make([]C.char, errBufLen)
	var cOut *C.char
	d := C.int(0)
	if deep {
		d = 1
	}
	rc := C.mc_probe(cPath, d, &cOut, &errbuf[0], C.size_t(len(errbuf)))
	if rc < 0 {
		return nil, avError("probe "+path, rc, &errbuf[0])
	}
	report := C.GoString(cOut)
	C.av_free(unsafe.Pointer(cOut))
	return parseProbe(report), nil
}

func parseProbe(report string) *Probe {
	p := &Probe{}
	streams := map[int]*StreamInfo{}

	for _, line := range strings.Split(report, "\n") {
		if line == "" {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key, val := line[:eq], unescape(line[eq+1:])

		switch {
		case strings.HasPrefix(key, "format."):
			applyFormat(&p.Format, key[len("format."):], val)
		case strings.HasPrefix(key, "metadata."):
			p.Metadata = append(p.Metadata, KV{key[len("metadata."):], val})
		case strings.HasPrefix(key, "stream."):
			rest := key[len("stream."):]
			dot := strings.IndexByte(rest, '.')
			if dot < 0 {
				continue
			}
			idx, err := strconv.Atoi(rest[:dot])
			if err != nil {
				continue
			}
			s := streams[idx]
			if s == nil {
				s = &StreamInfo{Index: idx}
				streams[idx] = s
			}
			field := rest[dot+1:]
			if md, ok := strings.CutPrefix(field, "metadata."); ok {
				s.Metadata = append(s.Metadata, KV{md, val})
			} else {
				applyStream(s, field, val)
			}
		}
	}

	idxs := make([]int, 0, len(streams))
	for i := range streams {
		idxs = append(idxs, i)
	}
	sort.Ints(idxs)
	for _, i := range idxs {
		p.Streams = append(p.Streams, *streams[i])
	}
	return p
}

func applyFormat(f *FormatInfo, field, val string) {
	switch field {
	case "name":
		f.Name = val
	case "long_name":
		f.LongName = val
	case "duration_us":
		if n, err := strconv.ParseInt(val, 10, 64); err == nil {
			f.Duration = time.Duration(n) * time.Microsecond
		}
	case "bit_rate":
		f.BitRate, _ = strconv.ParseInt(val, 10, 64)
	case "nb_streams":
		f.Streams, _ = strconv.Atoi(val)
	}
}

func applyStream(s *StreamInfo, field, val string) {
	switch field {
	case "type":
		s.Type = val
	case "codec":
		s.Codec = val
	case "codec_long":
		s.CodecLong = val
	case "profile":
		s.Profile = val
	case "bit_rate":
		s.BitRate, _ = strconv.ParseInt(val, 10, 64)
	case "duration_us":
		if n, err := strconv.ParseInt(val, 10, 64); err == nil {
			s.Duration = time.Duration(n) * time.Microsecond
		}
	case "width":
		s.Width, _ = strconv.Atoi(val)
	case "height":
		s.Height, _ = strconv.Atoi(val)
	case "pix_fmt":
		s.PixFmt = val
	case "fps":
		s.FPS = val
	case "sar":
		s.SAR = val
	case "sample_rate":
		s.SampleRate, _ = strconv.Atoi(val)
	case "channels":
		s.Channels, _ = strconv.Atoi(val)
	case "channel_layout":
		s.ChannelLayout = val
	case "sample_fmt":
		s.SampleFmt = val
	}
}

func unescape(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case '\\':
				b.WriteByte('\\')
			default:
				b.WriteByte(s[i+1])
			}
			i++
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
