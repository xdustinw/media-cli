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

// mc_write_tag remuxes infile to outfile with stream copy, preserving all
// streams, dispositions and existing metadata, and sets the freeform global
// tag key=value. For MP4/MOV outputs it enables use_metadata_tags so arbitrary
// keys survive. Mirrors:
//
//   ffmpeg -i <in> -map 0 -c copy -map_metadata 0 \
//          -movflags use_metadata_tags -metadata key=value <out>
//
// Returns 0 on success or a negative AVERROR code (message in errbuf).
int mc_write_tag(const char *infile, const char *outfile,
                 const char *key, const char *value,
                 char *errbuf, size_t errbuf_size);

// mc_read_tag reads the freeform global tag key from filename into out.
// Returns 0 on success, 1 if the tag is absent, or a negative AVERROR code.
int mc_read_tag(const char *filename, const char *key,
                char *out, size_t out_size,
                char *errbuf, size_t errbuf_size);

#endif
