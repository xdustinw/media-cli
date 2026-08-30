package imgmeta

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
)

var pngMagic = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

type pngChunk struct {
	typ  string
	data []byte
}

func pngParse(data []byte) ([]pngChunk, error) {
	if !bytes.HasPrefix(data, pngMagic) {
		return nil, fmt.Errorf("not a PNG file")
	}
	var chunks []pngChunk
	p := len(pngMagic)
	for p+8 <= len(data) {
		length := binary.BigEndian.Uint32(data[p : p+4])
		typ := string(data[p+4 : p+8])
		start := p + 8
		end := start + int(length)
		if end+4 > len(data) {
			return nil, fmt.Errorf("truncated PNG chunk %q", typ)
		}
		chunks = append(chunks, pngChunk{typ: typ, data: append([]byte(nil), data[start:end]...)})
		p = end + 4 // skip CRC
		if typ == "IEND" {
			break
		}
	}
	if len(chunks) == 0 || chunks[len(chunks)-1].typ != "IEND" {
		return nil, fmt.Errorf("PNG missing IEND")
	}
	return chunks, nil
}

func pngSerialize(chunks []pngChunk) []byte {
	var b bytes.Buffer
	b.Write(pngMagic)
	var lenbuf [4]byte
	for _, c := range chunks {
		binary.BigEndian.PutUint32(lenbuf[:], uint32(len(c.data)))
		b.Write(lenbuf[:])
		crc := crc32.NewIEEE()
		b.WriteString(c.typ)
		crc.Write([]byte(c.typ))
		b.Write(c.data)
		crc.Write(c.data)
		binary.BigEndian.PutUint32(lenbuf[:], crc.Sum32())
		b.Write(lenbuf[:])
	}
	return b.Bytes()
}

// textKeyword returns the keyword of a tEXt/zTXt/iTXt chunk (bytes before the
// first NUL), or "" if the type is not a text chunk.
func textKeyword(c pngChunk) string {
	if c.typ != "tEXt" && c.typ != "zTXt" && c.typ != "iTXt" {
		return ""
	}
	if i := bytes.IndexByte(c.data, 0); i >= 0 {
		return string(c.data[:i])
	}
	return ""
}

func pngRead(data []byte, key string) (string, error) {
	chunks, err := pngParse(data)
	if err != nil {
		return "", err
	}
	for _, c := range chunks {
		if c.typ != "tEXt" || textKeyword(c) != key {
			continue
		}
		if i := bytes.IndexByte(c.data, 0); i >= 0 {
			return string(c.data[i+1:]), nil
		}
	}
	return "", ErrTagAbsent
}

func pngWrite(data []byte, key, value string) ([]byte, error) {
	chunks, err := pngParse(data)
	if err != nil {
		return nil, err
	}

	newChunk := pngChunk{typ: "tEXt", data: append(append([]byte(key), 0), value...)}

	out := make([]pngChunk, 0, len(chunks)+1)
	inserted := false
	for _, c := range chunks {
		// Drop any prior text chunk carrying the same keyword.
		if textKeyword(c) == key {
			continue
		}
		// Place the tag just before the first IDAT (or before IEND if none).
		if !inserted && (c.typ == "IDAT" || c.typ == "IEND") {
			out = append(out, newChunk)
			inserted = true
		}
		out = append(out, c)
	}
	if !inserted {
		// Should not happen (IEND is guaranteed), but stay safe.
		out = append(out[:len(out)-1], newChunk, out[len(out)-1])
	}
	return pngSerialize(out), nil
}

func pngReadAll(data []byte) (map[string]string, error) {
	chunks, err := pngParse(data)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, c := range chunks {
		if c.typ != "tEXt" {
			continue
		}
		if i := bytes.IndexByte(c.data, 0); i >= 0 {
			out[string(c.data[:i])] = string(c.data[i+1:])
		}
	}
	return out, nil
}
