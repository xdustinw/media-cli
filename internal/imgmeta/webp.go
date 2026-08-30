package imgmeta

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// media-cli's private RIFF chunk id. Unknown chunks are ignored by WebP
// decoders, so the pixel data (and its hash) is unaffected.
const webpTagFourCC = "mcTG"

type riffChunk struct {
	id   string
	body []byte
}

func webpParse(data []byte) ([]riffChunk, error) {
	if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return nil, fmt.Errorf("not a WebP file")
	}
	declared := int(binary.LittleEndian.Uint32(data[4:8])) + 8
	if declared < len(data) {
		data = data[:declared] // ignore trailing slack
	}

	var chunks []riffChunk
	p := 12
	for p+8 <= len(data) {
		id := string(data[p : p+4])
		size := int(binary.LittleEndian.Uint32(data[p+4 : p+8]))
		start := p + 8
		if start+size > len(data) {
			return nil, fmt.Errorf("truncated WebP chunk %q", id)
		}
		chunks = append(chunks, riffChunk{id: id, body: append([]byte(nil), data[start:start+size]...)})
		p = start + size
		if size%2 == 1 {
			p++ // padding byte
		}
	}
	return chunks, nil
}

func webpSerialize(chunks []riffChunk) []byte {
	var body bytes.Buffer
	var sz [4]byte
	for _, c := range chunks {
		body.WriteString(c.id)
		binary.LittleEndian.PutUint32(sz[:], uint32(len(c.body)))
		body.Write(sz[:])
		body.Write(c.body)
		if len(c.body)%2 == 1 {
			body.WriteByte(0)
		}
	}
	var out bytes.Buffer
	out.WriteString("RIFF")
	binary.LittleEndian.PutUint32(sz[:], uint32(body.Len()+4))
	out.Write(sz[:])
	out.WriteString("WEBP")
	out.Write(body.Bytes())
	return out.Bytes()
}

func webpRead(data []byte, key string) (string, error) {
	chunks, err := webpParse(data)
	if err != nil {
		return "", err
	}
	prefix := []byte(key + "=")
	for _, c := range chunks {
		if c.id == webpTagFourCC && bytes.HasPrefix(c.body, prefix) {
			return string(c.body[len(prefix):]), nil
		}
	}
	return "", ErrTagAbsent
}

func webpWrite(data []byte, key, value string) ([]byte, error) {
	chunks, err := webpParse(data)
	if err != nil {
		return nil, err
	}
	prefix := key + "="
	kept := make([]riffChunk, 0, len(chunks)+1)
	for _, c := range chunks {
		if c.id == webpTagFourCC && bytes.HasPrefix(c.body, []byte(prefix)) {
			continue
		}
		kept = append(kept, c)
	}
	kept = append(kept, riffChunk{id: webpTagFourCC, body: []byte(prefix + value)})
	return webpSerialize(kept), nil
}

func webpReadAll(data []byte) (map[string]string, error) {
	chunks, err := webpParse(data)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, c := range chunks {
		if c.id != webpTagFourCC {
			continue
		}
		if i := bytes.IndexByte(c.body, '='); i > 0 {
			out[string(c.body[:i])] = string(c.body[i+1:])
		}
	}
	return out, nil
}
