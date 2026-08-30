package imgmeta

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

const (
	jpegSOS = 0xDA
	jpegEOI = 0xD9
	jpegCOM = 0xFE
)

type jpegSeg struct {
	marker  byte
	payload []byte
}

// jpegSplit walks the marker segments that precede the first SOS (or EOI) and
// returns them together with the offset at which entropy-coded data begins.
// Everything from that offset to EOF is copied verbatim on write.
func jpegSplit(data []byte) (segs []jpegSeg, tailOff int, err error) {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return nil, 0, fmt.Errorf("not a JPEG file")
	}
	p := 2
	for {
		if p+1 >= len(data) {
			return nil, 0, fmt.Errorf("truncated JPEG before image data")
		}
		if data[p] != 0xFF {
			return nil, 0, fmt.Errorf("bad JPEG marker at offset %d", p)
		}
		m := data[p+1]
		for m == 0xFF { // skip fill bytes
			p++
			if p+1 >= len(data) {
				return nil, 0, fmt.Errorf("truncated JPEG marker")
			}
			m = data[p+1]
		}
		switch {
		case m == jpegSOS || m == jpegEOI:
			return segs, p, nil
		case m == 0x01 || (m >= 0xD0 && m <= 0xD7):
			p += 2 // standalone marker, no payload
			continue
		}
		if p+4 > len(data) {
			return nil, 0, fmt.Errorf("truncated JPEG segment header")
		}
		length := int(binary.BigEndian.Uint16(data[p+2 : p+4]))
		if length < 2 || p+2+length > len(data) {
			return nil, 0, fmt.Errorf("bad JPEG segment length")
		}
		segs = append(segs, jpegSeg{marker: m, payload: append([]byte(nil), data[p+4:p+2+length]...)})
		p += 2 + length
	}
}

func jpegRead(data []byte, key string) (string, error) {
	segs, _, err := jpegSplit(data)
	if err != nil {
		return "", err
	}
	prefix := []byte(key + "=")
	for _, s := range segs {
		if s.marker == jpegCOM && bytes.HasPrefix(s.payload, prefix) {
			return string(s.payload[len(prefix):]), nil
		}
	}
	return "", ErrTagAbsent
}

func jpegWrite(data []byte, key, value string) ([]byte, error) {
	segs, tailOff, err := jpegSplit(data)
	if err != nil {
		return nil, err
	}
	prefix := []byte(key + "=")
	payload := append(append([]byte(nil), prefix...), value...)
	if len(payload)+2 > 0xFFFF {
		return nil, fmt.Errorf("metadata value too large for a JPEG COM segment")
	}

	var b bytes.Buffer
	b.Write([]byte{0xFF, 0xD8})

	writeSeg := func(marker byte, p []byte) {
		var hdr [4]byte
		hdr[0], hdr[1] = 0xFF, marker
		binary.BigEndian.PutUint16(hdr[2:], uint16(len(p)+2))
		b.Write(hdr[:])
		b.Write(p)
	}

	for _, s := range segs {
		if s.marker == jpegCOM && bytes.HasPrefix(s.payload, prefix) {
			continue // drop stale copy of our tag
		}
		writeSeg(s.marker, s.payload)
	}
	writeSeg(jpegCOM, payload)

	b.Write(data[tailOff:]) // SOS/EOI .. EOF, untouched
	return b.Bytes(), nil
}

func jpegReadAll(data []byte) (map[string]string, error) {
	segs, _, err := jpegSplit(data)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, s := range segs {
		if s.marker != jpegCOM {
			continue
		}
		if i := bytes.IndexByte(s.payload, '='); i > 0 {
			out[string(s.payload[:i])] = string(s.payload[i+1:])
		}
	}
	return out, nil
}
