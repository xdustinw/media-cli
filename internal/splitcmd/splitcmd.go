// Package splitcmd implements `mc split`: cut a media file at given timestamps
// into <name>-Part1.<ext>, <name>-Part2.<ext>, … by stream copy.
package splitcmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/xdustinw/media-cli/internal/ffmpeg"
	"github.com/xdustinw/media-cli/internal/media"
	"github.com/xdustinw/media-cli/internal/toon"
)

// Options configures a Run.
type Options struct {
	File      string
	Cuts      string // comma-separated timestamps, e.g. "1:20,2:30,4:50"
	OutputDir string // "" => the file's own directory

	AssumeYes bool
	Stdout    io.Writer
	Stderr    io.Writer
	Confirm   func(prompt string) (bool, error)
	Logger    *slog.Logger
}

// Run parses the cut list, previews the parts and (on confirmation) writes them.
func Run(ctx context.Context, o Options) error {
	log := o.Logger
	if log == nil {
		log = slog.Default()
	}

	secs, err := parseCuts(o.Cuts)
	if err != nil {
		return err
	}
	if len(secs) == 0 {
		return fmt.Errorf("no cut timestamps given")
	}

	info, err := os.Stat(o.File)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory; split takes a single file", o.File)
	}

	var duration float64
	if p, perr := ffmpeg.Inspect(o.File, false); perr == nil {
		duration = p.Format.Duration.Seconds()
	}
	if duration > 0 {
		for _, s := range secs {
			if s >= duration {
				return fmt.Errorf("cut %s is at or past the file's duration (%s)",
					formatSecs(s), formatSecs(duration))
			}
		}
	}

	outDir := o.OutputDir
	if outDir == "" {
		outDir = filepath.Dir(o.File)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	ext := filepath.Ext(o.File)
	stem := strings.TrimSuffix(filepath.Base(o.File), ext)
	pattern := filepath.Join(outDir, fmt.Sprintf("%s-Part%%d%s", stem, ext))

	// Preview the parts.
	doc := &toon.Document{}
	doc.AddField("source", o.File)
	doc.AddField("output", outDir)
	tbl := toon.Table{Name: "parts", Columns: []string{"file", "from", "to"}}
	bounds := append([]float64{0}, secs...)
	if duration > 0 {
		bounds = append(bounds, duration)
	} else {
		bounds = append(bounds, -1)
	}
	for i := 0; i < len(bounds)-1; i++ {
		to := "end"
		if bounds[i+1] >= 0 {
			to = formatSecs(bounds[i+1])
		}
		tbl.Rows = append(tbl.Rows, []string{
			fmt.Sprintf("%s-Part%d%s", stem, i+1, ext), formatSecs(bounds[i]), to,
		})
	}
	doc.AddTable(tbl)
	fmt.Fprint(o.Stdout, doc.String())
	fmt.Fprintln(o.Stderr, "  (stream copy — each part starts at the nearest keyframe)")

	if !o.AssumeYes {
		ok := false
		if o.Confirm != nil {
			var cErr error
			ok, cErr = o.Confirm(fmt.Sprintf("\nWrite %d part(s)? [y/N] ", len(bounds)-1))
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
	cutCSV := make([]string, len(secs))
	for i, s := range secs {
		cutCSV[i] = strconv.FormatFloat(s, 'f', 3, 64)
	}
	if err := ffmpeg.Split(o.File, pattern, strings.Join(cutCSV, ",")); err != nil {
		return err
	}

	var wrote int
	var bytes int64
	for i := 1; ; i++ {
		p := filepath.Join(outDir, fmt.Sprintf("%s-Part%d%s", stem, i, ext))
		fi, serr := os.Stat(p)
		if serr != nil {
			break
		}
		wrote++
		bytes += fi.Size()
		fmt.Fprintf(o.Stdout, "  ✓ %s\n", media.RelTo(outDir, p))
		log.Info("split", "part", p)
	}
	fmt.Fprintln(o.Stderr, media.Summary(wrote, bytes, time.Since(start)))
	return nil
}

// parseCuts turns "1:20,2:30,4:50" into a sorted, de-duplicated list of seconds.
func parseCuts(s string) ([]float64, error) {
	var out []float64
	seen := map[float64]bool{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		v, err := parseTimecode(part)
		if err != nil {
			return nil, err
		}
		if v <= 0 {
			return nil, fmt.Errorf("cut timestamp %q must be positive", part)
		}
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Float64s(out)
	return out, nil
}

// parseTimecode accepts SS(.mmm), MM:SS(.mmm) or HH:MM:SS(.mmm).
func parseTimecode(s string) (float64, error) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) == 0 || len(parts) > 3 {
		return 0, fmt.Errorf("bad timestamp %q", s)
	}
	var total float64
	for _, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil || v < 0 {
			return 0, fmt.Errorf("bad timestamp %q", s)
		}
		total = total*60 + v
	}
	return total, nil
}

func formatSecs(s float64) string {
	if s < 0 {
		return "end"
	}
	total := int(s)
	h, m, sec := total/3600, (total%3600)/60, total%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, sec)
	}
	return fmt.Sprintf("%d:%02d", m, sec)
}
