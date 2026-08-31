// Package concatcmd implements `mc concat`: join media files into one. When the
// inputs share codec/parameters they are stream-copied; otherwise they are
// re-encoded (MPEG-4 + AAC) to match the first file before joining.
package concatcmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/xdustinw/media-cli/internal/ffmpeg"
	"github.com/xdustinw/media-cli/internal/media"
	"github.com/xdustinw/media-cli/internal/toon"
)

// Options configures a Run.
type Options struct {
	Inputs     []string
	OutputFile string // "" => <stem1>-<stem2>-…-combined.<ext>

	AssumeYes bool
	Stdout    io.Writer
	Stderr    io.Writer
	Confirm   func(prompt string) (bool, error)
	Logger    *slog.Logger
}

type shape struct {
	container string
	vCodec    string
	w, h      int
	fpsNum    int
	fpsDen    int
	aCodec    string
	rate      int
	channels  int
	hasVideo  bool
	hasAudio  bool
}

// reencodeExts are containers that can hold MPEG-4 video + AAC audio.
var reencodeExts = map[string]bool{".mp4": true, ".m4v": true, ".mov": true, ".mkv": true, ".avi": true}

// Run probes the inputs, previews the join and (on confirmation) writes it.
func Run(ctx context.Context, o Options) error {
	log := o.Logger
	if log == nil {
		log = slog.Default()
	}
	if len(o.Inputs) < 2 {
		return fmt.Errorf("concat needs at least two input files")
	}

	shapes := make([]shape, len(o.Inputs))
	for i, in := range o.Inputs {
		if fi, err := os.Stat(in); err != nil {
			return err
		} else if fi.IsDir() {
			return fmt.Errorf("%s is a directory", in)
		}
		p, err := ffmpeg.Inspect(in, false)
		if err != nil {
			return fmt.Errorf("probing %s: %w", in, err)
		}
		shapes[i] = shapeOf(p)
	}

	mismatch := firstMismatch(shapes)
	reencode := mismatch != ""

	out := o.OutputFile
	if out == "" {
		out = derivedName(o.Inputs, reencode)
	} else if reencode && !reencodeExts[strings.ToLower(filepath.Ext(out))] {
		return fmt.Errorf("re-encoding is required but %s cannot hold MPEG-4/AAC; use an .mkv or .mp4 output",
			filepath.Ext(out))
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}

	// Preview.
	doc := &toon.Document{}
	doc.AddField("output", out)
	if reencode {
		doc.AddField("mode", "re-encode to MPEG-4 / AAC (inputs differ)")
	} else {
		doc.AddField("mode", "stream copy")
	}
	tbl := toon.Table{Name: "inputs", Columns: []string{"file", "video", "audio"}}
	for i, in := range o.Inputs {
		tbl.Rows = append(tbl.Rows, []string{
			media.RelTo(filepath.Dir(out), in), vDesc(shapes[i]), aDesc(shapes[i]),
		})
	}
	doc.AddTable(tbl)
	fmt.Fprint(o.Stdout, doc.String())
	if reencode {
		fmt.Fprintf(o.Stderr, "  ! inputs differ (%s); re-encoding every file to MPEG-4/AAC at %s's geometry\n",
			mismatch, filepath.Base(o.Inputs[0]))
		fmt.Fprintln(o.Stderr, "    (this build has no H.264 encoder — expect a quality drop)")
	}

	if !o.AssumeYes {
		ok := false
		if o.Confirm != nil {
			var cErr error
			ok, cErr = o.Confirm(fmt.Sprintf("\nWrite %s? [y/N] ", filepath.Base(out)))
			if cErr != nil {
				return cErr
			}
		}
		if !ok {
			fmt.Fprintln(o.Stdout, "Aborted; nothing written.")
			return nil
		}
	}

	start := time.Now()
	inputs := o.Inputs
	var tmps []string
	defer func() {
		for _, t := range tmps {
			_ = os.Remove(t)
		}
	}()

	if reencode {
		t := shapes[0]
		normalised := make([]string, len(o.Inputs))
		for i, in := range o.Inputs {
			if err := ctx.Err(); err != nil {
				return err
			}
			tmp := filepath.Join(filepath.Dir(out),
				fmt.Sprintf(".mc-concat-%d-%d%s", os.Getpid(), i, encExt(out)))
			w, h := 0, 0
			if t.hasVideo {
				w, h = t.w, t.h
			}
			rate, ch := 0, 0
			if t.hasAudio {
				rate, ch = t.rate, t.channels
			}
			fmt.Fprintf(o.Stdout, "  re-encoding %s…\n", filepath.Base(in))
			if err := ffmpeg.Transcode(in, tmp, w, h, t.fpsNum, t.fpsDen, rate, ch); err != nil {
				return fmt.Errorf("re-encoding %s: %w", in, err)
			}
			tmps = append(tmps, tmp)
			normalised[i] = tmp
		}
		inputs = normalised
	}

	if err := ffmpeg.ConcatCopy(inputs, out); err != nil {
		return err
	}

	var size int64
	if fi, err := os.Stat(out); err == nil {
		size = fi.Size()
	}
	fmt.Fprintf(o.Stdout, "  ✓ %s\n", out)
	log.Info("concat", "output", out, "inputs", len(o.Inputs), "reencode", reencode)
	fmt.Fprintln(o.Stderr, media.Summary(len(o.Inputs), size, time.Since(start)))
	return nil
}

func shapeOf(p *ffmpeg.Probe) shape {
	s := shape{container: firstToken(p.Format.Name)}
	for _, st := range p.Streams {
		switch st.Type {
		case "video":
			if s.hasVideo {
				continue
			}
			s.hasVideo = true
			s.vCodec, s.w, s.h = st.Codec, st.Width, st.Height
			s.fpsNum, s.fpsDen = parseFPS(st.FPS)
		case "audio":
			if s.hasAudio {
				continue
			}
			s.hasAudio = true
			s.aCodec, s.rate, s.channels = st.Codec, st.SampleRate, st.Channels
		}
	}
	return s
}

// firstMismatch returns a human description of the first way shapes[i] differs
// from shapes[0], or "" when they are all compatible for a stream copy.
func firstMismatch(shapes []shape) string {
	a := shapes[0]
	for _, b := range shapes[1:] {
		switch {
		case a.hasVideo != b.hasVideo || a.hasAudio != b.hasAudio:
			return "stream layout"
		case a.hasVideo && (a.vCodec != b.vCodec):
			return fmt.Sprintf("%s vs %s video", a.vCodec, b.vCodec)
		case a.hasVideo && (a.w != b.w || a.h != b.h):
			return fmt.Sprintf("%dx%d vs %dx%d", a.w, a.h, b.w, b.h)
		case a.hasAudio && (a.aCodec != b.aCodec):
			return fmt.Sprintf("%s vs %s audio", a.aCodec, b.aCodec)
		case a.hasAudio && (a.rate != b.rate || a.channels != b.channels):
			return fmt.Sprintf("%dHz/%dch vs %dHz/%dch", a.rate, a.channels, b.rate, b.channels)
		}
	}
	return ""
}

func derivedName(inputs []string, reencode bool) string {
	stems := make([]string, len(inputs))
	for i, in := range inputs {
		b := filepath.Base(in)
		stems[i] = strings.TrimSuffix(b, filepath.Ext(b))
	}
	ext := filepath.Ext(inputs[0])
	if reencode && !reencodeExts[strings.ToLower(ext)] {
		ext = ".mkv"
	}
	return filepath.Join(filepath.Dir(inputs[0]), strings.Join(stems, "-")+"-combined"+ext)
}

func encExt(out string) string {
	ext := filepath.Ext(out)
	if reencodeExts[strings.ToLower(ext)] {
		return ext
	}
	return ".mkv"
}

func parseFPS(s string) (num, den int) {
	if i := strings.IndexByte(s, '/'); i > 0 {
		n, _ := strconv.Atoi(s[:i])
		d, _ := strconv.Atoi(s[i+1:])
		if n > 0 && d > 0 {
			return n, d
		}
	}
	if v, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && v > 0 {
		return v, 1
	}
	return 25, 1
}

func firstToken(s string) string {
	if i := strings.IndexByte(s, ','); i > 0 {
		return s[:i]
	}
	return s
}

func vDesc(s shape) string {
	if !s.hasVideo {
		return "-"
	}
	return fmt.Sprintf("%s %dx%d", s.vCodec, s.w, s.h)
}

func aDesc(s shape) string {
	if !s.hasAudio {
		return "-"
	}
	return fmt.Sprintf("%s %dHz %dch", s.aCodec, s.rate, s.channels)
}
