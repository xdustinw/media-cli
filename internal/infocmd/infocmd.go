// Package infocmd implements `mc info`: everything known about one file.
package infocmd

import (
	"context"
	"encoding/json"
	"io"
	"sort"
	"time"

	"github.com/xdustinw/media-cli/internal/ffmpeg"
	"github.com/xdustinw/media-cli/internal/media"
	"github.com/xdustinw/media-cli/internal/mediainfo"
	"github.com/xdustinw/media-cli/internal/render"
)

// Options configures a Run.
type Options struct {
	Path   string
	Format render.Format // toon or json
	Stdout io.Writer
}

// Run inspects Options.Path and writes its full description.
func Run(_ context.Context, o Options) error {
	f, err := mediainfo.Load(o.Path, true)
	if err != nil {
		return err
	}

	doc := build(f)

	switch o.Format {
	case render.JSON:
		enc := json.NewEncoder(o.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(doc)
	default:
		s, err := doc.TOON()
		if err != nil {
			return err
		}
		_, err = io.WriteString(o.Stdout, s+"\n")
		return err
	}
}

func build(f *mediainfo.File) *render.OM {
	doc := render.NewOM()

	file := render.NewOM(
		"path", f.Abs,
		"name", f.Name,
		"size", mediainfo.HumanSize(f.Size),
		"size_bytes", f.Size,
		"modified", f.ModTime.Format(time.RFC3339),
		"kind", f.Kind.String(),
	)
	doc.Set("file", file)

	if h, ok := f.Meta("mc.hash"); ok {
		doc.Set("mc.hash", h)
	}
	if r, ok := f.Rating(); ok {
		doc.Set("rating", r)
	}
	if a := f.Authors(); len(a) > 0 {
		doc.Set("authors", a)
	}
	if t := f.Tags(); len(t) > 0 {
		doc.Set("tags", t)
	}

	if f.Probe == nil {
		return doc
	}
	p := f.Probe
	isImage := f.Kind == media.KindImage

	format := render.NewOM("name", p.Format.Name)
	format.SetIf(p.Format.LongName != "", "long_name", p.Format.LongName)
	format.SetIf(!isImage && p.Format.Duration > 0, "duration", p.Format.Duration.Round(time.Millisecond).String())
	format.SetIf(!isImage && p.Format.BitRate > 0, "bit_rate", p.Format.BitRate)
	format.Set("streams", p.Format.Streams)
	doc.Set("format", format)

	if len(p.Streams) > 0 {
		streams := make([]*render.OM, 0, len(p.Streams))
		for _, s := range p.Streams {
			streams = append(streams, streamOM(s, isImage))
		}
		doc.Set("streams", streams)
	}

	if md := dictOM(p.Metadata); md.Len() > 0 {
		doc.Set("metadata", md)
	}

	// mc-written tags on images (JPEG COM / PNG tEXt / GIF comment / WebP chunk)
	// are invisible to FFmpeg's decoders, so surface them separately.
	if isImage {
		if tags := f.ImageTags(); len(tags) > 0 {
			keys := make([]string, 0, len(tags))
			for k := range tags {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			om := render.NewOM()
			for _, k := range keys {
				om.Set(k, mediainfo.CleanValue(tags[k]))
			}
			doc.Set("mc_metadata", om)
		}
	}
	return doc
}

func streamOM(s ffmpeg.StreamInfo, isImage bool) *render.OM {
	o := render.NewOM("index", s.Index, "type", s.Type, "codec", s.Codec)
	o.SetIf(s.CodecLong != "", "codec_long", s.CodecLong)
	o.SetIf(s.Profile != "", "profile", s.Profile)
	o.SetIf(s.Width > 0, "width", s.Width)
	o.SetIf(s.Height > 0, "height", s.Height)
	o.SetIf(s.PixFmt != "", "pix_fmt", s.PixFmt)
	o.SetIf(!isImage && s.FPS != "", "fps", s.FPS)
	o.SetIf(!isImage && s.SAR != "", "sar", s.SAR)
	o.SetIf(s.SampleRate > 0, "sample_rate", s.SampleRate)
	o.SetIf(s.Channels > 0, "channels", s.Channels)
	o.SetIf(s.ChannelLayout != "", "channel_layout", s.ChannelLayout)
	o.SetIf(s.SampleFmt != "", "sample_fmt", s.SampleFmt)
	o.SetIf(!isImage && s.BitRate > 0, "bit_rate", s.BitRate)
	o.SetIf(!isImage && s.Duration > 0, "duration", s.Duration.Round(time.Millisecond).String())
	if md := dictOM(s.Metadata); md.Len() > 0 {
		o.Set("metadata", md)
	}
	return o
}

func dictOM(kvs []ffmpeg.KV) *render.OM {
	o := render.NewOM()
	for _, kv := range kvs {
		if mediainfo.IsNoiseTag(kv.Key, kv.Value) {
			continue
		}
		o.Set(mediainfo.FriendlyKey(kv.Key), mediainfo.DisplayValue(kv.Key, kv.Value))
	}
	return o
}
