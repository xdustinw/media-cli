#ifndef MEDIA_CLI_BRIDGE_H
#define MEDIA_CLI_BRIDGE_H

#include <stddef.h>

// mc_stream_hash computes the MD5 of a media file's video and audio elementary
// streams, ignoring all container metadata. It mirrors:
//
//   ffmpeg -i <file> -map 0:v? -map 0:a? -c copy -f hash -hash md5 -
//
// On success it writes a lowercase 32-char hex digest (NUL terminated) into
// out and returns 0. On failure it returns a negative AVERROR code and, when
// errbuf is non-NULL, a human readable message.
int mc_stream_hash(const char *filename,
                   char *out, size_t out_size,
                   char *errbuf, size_t errbuf_size);

// mc_image_hash computes the MD5 of a still image's decoded pixel data,
// ignoring every form of embedded metadata (EXIF, XMP, ICC, text chunks,
// comments). The digest covers, per decoded frame, the pixel format, width,
// height and tightly packed plane bytes; animated inputs fold in every frame.
//
// On success it writes a lowercase 32-char hex digest (NUL terminated) into
// out and returns 0. On failure it returns a negative AVERROR code (message in
// errbuf).
int mc_image_hash(const char *filename,
                  char *out, size_t out_size,
                  char *errbuf, size_t errbuf_size);

// mc_write_tags remuxes infile to outfile with stream copy, preserving all
// streams, dispositions and existing metadata, and sets each of the n freeform
// global tags keys[i]=values[i]. For MP4/MOV outputs it enables
// use_metadata_tags so arbitrary keys survive. Mirrors:
//
//   ffmpeg -i <in> -map 0 -c copy -map_metadata 0 \
//          -movflags use_metadata_tags -metadata k1=v1 -metadata k2=v2 <out>
//
// Returns 0 on success or a negative AVERROR code (message in errbuf).
int mc_write_tags(const char *infile, const char *outfile,
                  const char *const *keys, const char *const *values, int n,
                  char *errbuf, size_t errbuf_size);

// mc_read_tag reads the freeform global tag key from filename into out.
// Returns 0 on success, 1 if the tag is absent, or a negative AVERROR code.
int mc_read_tag(const char *filename, const char *key,
                char *out, size_t out_size,
                char *errbuf, size_t errbuf_size);

// mc_probe opens filename and produces a flat "key=value\n" report describing
// the container, every stream and all metadata. Values are escaped: backslash
// -> "\\", newline -> "\n", tab -> "\t".
//
// Keys:
//   format.name, format.long_name, format.duration_us, format.bit_rate,
//   format.nb_streams
//   metadata.<key>
//   stream.<i>.type|codec|codec_long|profile|bit_rate|duration_us
//   stream.<i>.width|height|pix_fmt|fps|sar            (video)
//   stream.<i>.sample_rate|channels|channel_layout|sample_fmt  (audio)
//   stream.<i>.metadata.<key>
//
// When deep is non-zero, the first video frame is decoded and its frame-level
// metadata (image EXIF, PNG text, ...) is merged under metadata.<key>.
//
// On success *out is set to an av_malloc'd NUL-terminated string that the
// caller must free with av_free, and 0 is returned. On failure a negative
// AVERROR code is returned (message in errbuf) and *out is left NULL.
int mc_probe(const char *filename, int deep,
             char **out, char *errbuf, size_t errbuf_size);

#endif
