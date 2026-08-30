package imgmeta

import (
	"bytes"
	"fmt"
)

// gifPrefixLen returns the length of the GIF header + Logical Screen Descriptor
// + Global Color Table, i.e. the offset of the first data block.
func gifPrefixLen(data []byte) (int, error) {
	if len(data) < 13 || (!bytes.HasPrefix(data, []byte("GIF87a")) && !bytes.HasPrefix(data, []byte("GIF89a"))) {
		return 0, fmt.Errorf("not a GIF file")
	}
	off := 13
	packed := data[10]
	if packed&0x80 != 0 { // Global Color Table present
		gctSize := 3 * (1 << ((packed & 0x07) + 1))
		off += gctSize
	}
	if off > len(data) {
		return 0, fmt.Errorf("truncated GIF header")
	}
	return off, nil
}

// gifSubBlocks reads a run of sub-blocks starting at p, returning the joined
// data and the offset just past the terminating zero-length block.
func gifSubBlocks(data []byte, p int) ([]byte, int, error) {
	var out []byte
	for {
		if p >= len(data) {
			return nil, 0, fmt.Errorf("truncated GIF sub-block")
		}
		n := int(data[p])
		p++
		if n == 0 {
			return out, p, nil
		}
		if p+n > len(data) {
			return nil, 0, fmt.Errorf("truncated GIF sub-block data")
		}
		out = append(out, data[p:p+n]...)
		p += n
	}
}

// gifWalk invokes fn for each top-level block. fn receives the block's start
// offset and its end offset (exclusive). Iteration stops after the trailer.
func gifWalk(data []byte, start int, fn func(kind byte, label byte, s, e int)) error {
	p := start
	for p < len(data) {
		switch data[p] {
		case 0x3B: // trailer
			fn(0x3B, 0, p, p+1)
			return nil
		case 0x21: // extension
			if p+2 > len(data) {
				return fmt.Errorf("truncated GIF extension")
			}
			label := data[p+1]
			_, end, err := gifSubBlocks(data, p+2)
			if err != nil {
				return err
			}
			fn(0x21, label, p, end)
			p = end
		case 0x2C: // image descriptor
			if p+10 > len(data) {
				return fmt.Errorf("truncated GIF image descriptor")
			}
			lctPacked := data[p+9]
			q := p + 10
			if lctPacked&0x80 != 0 {
				q += 3 * (1 << ((lctPacked & 0x07) + 1))
			}
			q++ // LZW minimum code size
			_, end, err := gifSubBlocks(data, q)
			if err != nil {
				return err
			}
			fn(0x2C, 0, p, end)
			p = end
		default:
			return fmt.Errorf("unknown GIF block 0x%02x at %d", data[p], p)
		}
	}
	return fmt.Errorf("GIF missing trailer")
}

func gifCommentPayload(data []byte, s int) []byte {
	// s points at 0x21; comment sub-blocks start at s+2.
	joined, _, err := gifSubBlocks(data, s+2)
	if err != nil {
		return nil
	}
	return joined
}

func gifRead(data []byte, key string) (string, error) {
	start, err := gifPrefixLen(data)
	if err != nil {
		return "", err
	}
	prefix := []byte(key + "=")
	var found string
	var hit bool
	werr := gifWalk(data, start, func(kind, label byte, s, e int) {
		if kind == 0x21 && label == 0xFE && !hit {
			if p := gifCommentPayload(data, s); bytes.HasPrefix(p, prefix) {
				found = string(p[len(prefix):])
				hit = true
			}
		}
	})
	if werr != nil {
		return "", werr
	}
	if !hit {
		return "", ErrTagAbsent
	}
	return found, nil
}

func encodeGIFComment(text []byte) []byte {
	var b bytes.Buffer
	b.Write([]byte{0x21, 0xFE})
	for len(text) > 0 {
		n := len(text)
		if n > 255 {
			n = 255
		}
		b.WriteByte(byte(n))
		b.Write(text[:n])
		text = text[n:]
	}
	b.WriteByte(0x00)
	return b.Bytes()
}

func gifWrite(data []byte, key, value string) ([]byte, error) {
	start, err := gifPrefixLen(data)
	if err != nil {
		return nil, err
	}
	prefix := []byte(key + "=")

	// Collect [start,end) spans of existing comment extensions carrying our key.
	var drop [][2]int
	werr := gifWalk(data, start, func(kind, label byte, s, e int) {
		if kind == 0x21 && label == 0xFE {
			if p := gifCommentPayload(data, s); bytes.HasPrefix(p, prefix) {
				drop = append(drop, [2]int{s, e})
			}
		}
	})
	if werr != nil {
		return nil, werr
	}

	var b bytes.Buffer
	b.Write(data[:start])
	b.Write(encodeGIFComment(append(append([]byte(nil), prefix...), value...)))

	pos := start
	for _, d := range drop {
		b.Write(data[pos:d[0]])
		pos = d[1]
	}
	b.Write(data[pos:])
	return b.Bytes(), nil
}
