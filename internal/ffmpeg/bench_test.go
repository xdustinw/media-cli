package ffmpeg

import (
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkStreamHash measures the stream-copy video hash against a sample
// under ../../tmp/video; skipped when absent. Run with:
//
//	go test ./internal/ffmpeg -run x -bench StreamHash
func BenchmarkStreamHash(b *testing.B) {
	m, _ := filepath.Glob(filepath.Join("..", "..", "tmp", "video", "sample-*.mp4"))
	if len(m) == 0 {
		b.Skip("no sample under tmp/video")
	}
	if fi, err := os.Stat(m[0]); err == nil {
		b.SetBytes(fi.Size())
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := StreamHash(m[0]); err != nil {
			b.Fatal(err)
		}
	}
}
