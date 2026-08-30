package imgmeta

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func sampleImage() image.Image {
	m := image.NewRGBA(image.Rect(0, 0, 16, 12))
	for y := 0; y < 12; y++ {
		for x := 0; x < 16; x++ {
			m.Set(x, y, color.RGBA{uint8(x * 16), uint8(y * 20), 0x40, 0xff})
		}
	}
	return m
}

func encPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, sampleImage()); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func encJPEG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, sampleImage(), &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func encGIF(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := gif.Encode(&b, sampleImage(), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// synthWebP builds a minimal simple-format WebP (RIFF/WEBP with one VP8L chunk
// of arbitrary bytes). It is not a decodable image but exercises RIFF handling.
func synthWebP() []byte {
	body := []byte("WEBP")
	vp8l := []byte{0x2f, 0x00, 0x00, 0x00, 0x00} // 5 bytes, odd -> pad
	chunk := append([]byte("VP8L"), 0x05, 0x00, 0x00, 0x00)
	chunk = append(chunk, vp8l...)
	chunk = append(chunk, 0x00) // pad
	body = append(body, chunk...)
	out := []byte("RIFF")
	sz := len(body)
	out = append(out, byte(sz), byte(sz>>8), byte(sz>>16), byte(sz>>24))
	out = append(out, body...)
	return out
}

func TestRoundTripAllFormats(t *testing.T) {
	const key = "mc.hash"
	cases := map[string][]byte{
		"a.png":  encPNG(t),
		"a.jpg":  encJPEG(t),
		"a.gif":  encGIF(t),
		"a.webp": synthWebP(),
	}

	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, name)
			if err := os.WriteFile(src, data, 0o644); err != nil {
				t.Fatal(err)
			}

			if _, err := Read(src, key); err != ErrTagAbsent {
				t.Fatalf("expected ErrTagAbsent, got %v", err)
			}

			dst := filepath.Join(dir, "out"+filepath.Ext(name))
			if err := Write(src, dst, key, "deadbeefcafe"); err != nil {
				t.Fatalf("write: %v", err)
			}
			got, err := Read(dst, key)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			if got != "deadbeefcafe" {
				t.Fatalf("got %q", got)
			}

			// Rewriting replaces, not duplicates.
			dst2 := filepath.Join(dir, "out2"+filepath.Ext(name))
			if err := Write(dst, dst2, key, "0123456789ab"); err != nil {
				t.Fatalf("rewrite: %v", err)
			}
			got, err = Read(dst2, key)
			if err != nil || got != "0123456789ab" {
				t.Fatalf("rewrite read: %q %v", got, err)
			}
			// Byte growth between one and two writes must be bounded (no pile-up).
			s1, _ := os.Stat(dst)
			s2, _ := os.Stat(dst2)
			if s2.Size() > s1.Size()+8 {
				t.Fatalf("rewrite grew the file: %d -> %d", s1.Size(), s2.Size())
			}
		})
	}
}

func TestUnsupported(t *testing.T) {
	if Supported("x.bmp") {
		t.Fatal("bmp should be unsupported")
	}
	if _, err := Read("x.tiff", "k"); err != ErrUnsupportedFormat {
		t.Fatalf("got %v", err)
	}
}
