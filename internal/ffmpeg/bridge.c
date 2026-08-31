#include "bridge.h"

#include <string.h>
#include <stdio.h>

#include <libavcodec/avcodec.h>
#include <libavformat/avformat.h>
#include <libavutil/avstring.h>
#include <libavutil/avutil.h>
#include <libavutil/bprint.h>
#include <libavutil/channel_layout.h>
#include <libavutil/dict.h>
#include <libavutil/error.h>
#include <libavutil/hash.h>
#include <libavutil/imgutils.h>
#include <libavutil/intreadwrite.h>
#include <libavutil/mathematics.h>
#include <libavutil/mem.h>
#include <libavutil/opt.h>
#include <libavutil/pixdesc.h>
#include <libavutil/samplefmt.h>
#include <libavutil/audio_fifo.h>
#include <libswscale/swscale.h>
#include <libswresample/swresample.h>

static void set_err(char *errbuf, size_t errbuf_size, int code) {
    if (!errbuf || errbuf_size == 0) {
        return;
    }
    av_strerror(code, errbuf, errbuf_size);
}

static int is_av_stream(enum AVMediaType t) {
    return t == AVMEDIA_TYPE_VIDEO || t == AVMEDIA_TYPE_AUDIO;
}

int mc_stream_hash(const char *filename,
                   char *out, size_t out_size, int64_t max_bytes,
                   char *errbuf, size_t errbuf_size) {
    AVFormatContext *ic = NULL;
    AVFormatContext *oc = NULL;
    AVPacket *pkt = NULL;
    int *smap = NULL;
    uint8_t *dyn = NULL;
    int ret = 0;

    ret = avformat_open_input(&ic, filename, NULL, NULL);
    if (ret < 0) {
        goto done;
    }
    // Stream copy never inspects frame contents, so skip the bitstream
    // parsers: they re-frame packets (splitting/merging without changing the
    // concatenated bytes the hash sees) and cost CPU on every packet.
    ic->flags |= AVFMT_FLAG_NOPARSE;

    // Only probe when the container header did not already classify every
    // stream (mp4/mkv/mov/webm/... carry codec type + id up front). Probing
    // reads and buffers data we would otherwise stream straight through.
    int need_probe = 0;
    for (unsigned i = 0; i < ic->nb_streams; i++) {
        enum AVMediaType t = ic->streams[i]->codecpar->codec_type;
        if (t != AVMEDIA_TYPE_VIDEO && t != AVMEDIA_TYPE_AUDIO &&
            t != AVMEDIA_TYPE_SUBTITLE && t != AVMEDIA_TYPE_DATA &&
            t != AVMEDIA_TYPE_ATTACHMENT) {
            need_probe = 1;
            break;
        }
    }
    if (ic->nb_streams == 0) {
        need_probe = 1;
    }
    if (need_probe) {
        ret = avformat_find_stream_info(ic, NULL);
        if (ret < 0) {
            goto done;
        }
    }

    ret = avformat_alloc_output_context2(&oc, NULL, "hash", NULL);
    if (ret < 0 || !oc) {
        if (ret >= 0) ret = AVERROR(ENOMEM);
        goto done;
    }
    // Default is already md5, but be explicit and future-proof.
    av_opt_set(oc->priv_data, "hash", "md5", 0);

    smap = av_malloc_array(ic->nb_streams, sizeof(*smap));
    if (!smap) {
        ret = AVERROR(ENOMEM);
        goto done;
    }
    for (unsigned i = 0; i < ic->nb_streams; i++) {
        smap[i] = -1;
    }

    // Two passes so output stream order matches `-map 0:v? -map 0:a?`:
    // every video stream first, then every audio stream. This also fixes the
    // interleaver's tie-break order for packets sharing a DTS.
    const enum AVMediaType order[] = {AVMEDIA_TYPE_VIDEO, AVMEDIA_TYPE_AUDIO};
    for (int p = 0; p < 2; p++) {
        for (unsigned i = 0; i < ic->nb_streams; i++) {
            AVStream *in = ic->streams[i];
            if (in->codecpar->codec_type != order[p]) {
                continue;
            }
            AVStream *os = avformat_new_stream(oc, NULL);
            if (!os) {
                ret = AVERROR(ENOMEM);
                goto done;
            }
            ret = avcodec_parameters_copy(os->codecpar, in->codecpar);
            if (ret < 0) {
                goto done;
            }
            os->codecpar->codec_tag = 0;
            os->time_base = in->time_base;
            smap[i] = os->index;
        }
    }

    if (oc->nb_streams == 0) {
        ret = AVERROR_INVALIDDATA;
        set_err(errbuf, errbuf_size, ret);
        if (errbuf && errbuf_size) {
            snprintf(errbuf, errbuf_size, "file has no video or audio streams");
        }
        goto done;
    }

    ret = avio_open_dyn_buf(&oc->pb);
    if (ret < 0) {
        goto done;
    }

    ret = avformat_write_header(oc, NULL);
    if (ret < 0) {
        goto done;
    }

    pkt = av_packet_alloc();
    if (!pkt) {
        ret = AVERROR(ENOMEM);
        goto done;
    }

    // Stream copy: feed demuxed packets straight to the hash muxer in demux
    // order. No decoding. av_write_frame (not av_interleaved_write_frame) —
    // the interleaver would buffer and re-copy every packet to sort by DTS,
    // which for a correctly interleaved file (the norm) leaves the byte stream
    // the hash sees unchanged but doubles memory traffic on large files.
    int64_t copied = 0;
    while ((ret = av_read_frame(ic, pkt)) >= 0) {
        int oi = smap[pkt->stream_index];
        if (oi < 0) {
            av_packet_unref(pkt);
            continue;
        }
        int pkt_size = pkt->size;
        pkt->stream_index = oi;
        pkt->pos = -1;
        pkt->pts = AV_NOPTS_VALUE;
        pkt->dts = AV_NOPTS_VALUE;
        ret = av_write_frame(oc, pkt);
        av_packet_unref(pkt);
        if (ret < 0) {
            goto done;
        }
        copied += pkt_size;
        if (max_bytes > 0 && copied >= max_bytes) {
            break;
        }
    }
    if (ret == AVERROR_EOF) {
        ret = 0;
    }
    if (ret < 0) {
        goto done;
    }
    av_write_frame(oc, NULL); // flush any muxer-buffered data

    ret = av_write_trailer(oc);
    if (ret < 0) {
        goto done;
    }

    {
        int n = avio_close_dyn_buf(oc->pb, &dyn);
        oc->pb = NULL;
        // dyn holds e.g. "MD5=0123...cdef\n"
        const char *eq = memchr(dyn, '=', n);
        if (!eq) {
            ret = AVERROR_INVALIDDATA;
            goto done;
        }
        const char *hex = eq + 1;
        int hexlen = n - (int)(hex - (const char *)dyn);
        while (hexlen > 0 && (hex[hexlen - 1] == '\n' || hex[hexlen - 1] == '\r')) {
            hexlen--;
        }
        if (hexlen <= 0 || (size_t)hexlen >= out_size) {
            ret = AVERROR(ENOSPC);
            goto done;
        }
        memcpy(out, hex, hexlen);
        out[hexlen] = '\0';
        ret = 0;
    }

done:
    if (ret < 0) {
        set_err(errbuf, errbuf_size, ret);
    }
    av_free(dyn);
    av_free(smap);
    if (pkt) {
        av_packet_free(&pkt);
    }
    if (oc) {
        if (oc->pb) {
            uint8_t *tmp = NULL;
            avio_close_dyn_buf(oc->pb, &tmp);
            av_free(tmp);
        }
        avformat_free_context(oc);
    }
    if (ic) {
        avformat_close_input(&ic);
    }
    return ret;
}

static int hex_encode(const uint8_t *in, int in_len, char *out, size_t out_size) {
    static const char hx[] = "0123456789abcdef";
    if (out_size < (size_t)(in_len * 2 + 1)) {
        return AVERROR(ENOSPC);
    }
    for (int i = 0; i < in_len; i++) {
        out[i * 2]     = hx[in[i] >> 4];
        out[i * 2 + 1] = hx[in[i] & 0x0f];
    }
    out[in_len * 2] = '\0';
    return 0;
}

// hash_frame folds a decoded frame's geometry and tightly packed pixel bytes
// into h. Padding between rows/planes is excluded so alignment never leaks in.
static int hash_frame(struct AVHashContext *h, const AVFrame *f) {
    uint8_t hdr[12];
    AV_WL32(hdr + 0, (uint32_t)f->format);
    AV_WL32(hdr + 4, (uint32_t)f->width);
    AV_WL32(hdr + 8, (uint32_t)f->height);
    av_hash_update(h, hdr, sizeof(hdr));

    int size = av_image_get_buffer_size(f->format, f->width, f->height, 1);
    if (size < 0) {
        return size;
    }
    uint8_t *buf = av_malloc(size);
    if (!buf) {
        return AVERROR(ENOMEM);
    }
    int ret = av_image_copy_to_buffer(buf, size,
                                      (const uint8_t *const *)f->data, f->linesize,
                                      f->format, f->width, f->height, 1);
    if (ret >= 0) {
        av_hash_update(h, buf, size);
    }
    av_free(buf);
    return ret < 0 ? ret : 0;
}

int mc_image_hash(const char *filename,
                  char *out, size_t out_size,
                  char *errbuf, size_t errbuf_size) {
    AVFormatContext *ic = NULL;
    AVCodecContext *dec = NULL;
    AVFrame *frame = NULL;
    AVPacket *pkt = NULL;
    struct AVHashContext *hash = NULL;
    int ret = 0;
    long frames = 0;

    ret = avformat_open_input(&ic, filename, NULL, NULL);
    if (ret < 0) {
        goto done;
    }
    ret = avformat_find_stream_info(ic, NULL);
    if (ret < 0) {
        goto done;
    }

    int vs = av_find_best_stream(ic, AVMEDIA_TYPE_VIDEO, -1, -1, NULL, 0);
    if (vs < 0) {
        ret = vs;
        goto done;
    }
    AVStream *st = ic->streams[vs];

    const AVCodec *codec = avcodec_find_decoder(st->codecpar->codec_id);
    if (!codec) {
        ret = AVERROR_DECODER_NOT_FOUND;
        goto done;
    }
    dec = avcodec_alloc_context3(codec);
    if (!dec) {
        ret = AVERROR(ENOMEM);
        goto done;
    }
    ret = avcodec_parameters_to_context(dec, st->codecpar);
    if (ret < 0) {
        goto done;
    }
    ret = avcodec_open2(dec, codec, NULL);
    if (ret < 0) {
        goto done;
    }

    ret = av_hash_alloc(&hash, "MD5");
    if (ret < 0) {
        goto done;
    }
    av_hash_init(hash);

    frame = av_frame_alloc();
    pkt = av_packet_alloc();
    if (!frame || !pkt) {
        ret = AVERROR(ENOMEM);
        goto done;
    }

    for (;;) {
        ret = av_read_frame(ic, pkt);
        int flushing = (ret == AVERROR_EOF);
        if (ret < 0 && !flushing) {
            goto done;
        }
        if (!flushing && pkt->stream_index != vs) {
            av_packet_unref(pkt);
            continue;
        }

        ret = avcodec_send_packet(dec, flushing ? NULL : pkt);
        av_packet_unref(pkt);
        if (ret < 0 && ret != AVERROR_EOF) {
            goto done;
        }

        for (;;) {
            ret = avcodec_receive_frame(dec, frame);
            if (ret == AVERROR(EAGAIN) || ret == AVERROR_EOF) {
                break;
            }
            if (ret < 0) {
                goto done;
            }
            ret = hash_frame(hash, frame);
            av_frame_unref(frame);
            if (ret < 0) {
                goto done;
            }
            frames++;
        }

        if (flushing) {
            break;
        }
    }

    if (frames == 0) {
        ret = AVERROR_INVALIDDATA;
        goto done;
    }

    {
        uint8_t digest[16];
        int n = av_hash_get_size(hash);
        if (n != (int)sizeof(digest)) {
            ret = AVERROR_BUG;
            goto done;
        }
        av_hash_final(hash, digest);
        ret = hex_encode(digest, n, out, out_size);
        if (ret < 0) {
            goto done;
        }
    }
    ret = 0;

done:
    if (ret < 0) {
        set_err(errbuf, errbuf_size, ret);
    }
    if (pkt) {
        av_packet_free(&pkt);
    }
    if (frame) {
        av_frame_free(&frame);
    }
    if (hash) {
        av_hash_freep(&hash);
    }
    if (dec) {
        avcodec_free_context(&dec);
    }
    if (ic) {
        avformat_close_input(&ic);
    }
    return ret;
}

static int muxer_is_mov(const AVOutputFormat *ofmt) {
    if (!ofmt || !ofmt->name) {
        return 0;
    }
    // "mp4", "mov", "ipod", "3gp", "psp", "mov,mp4,m4a,3gp,3g2,mj2"
    return strstr(ofmt->name, "mp4") || strstr(ofmt->name, "mov") ||
           strstr(ofmt->name, "ipod") || strstr(ofmt->name, "3gp");
}

int mc_write_tags(const char *infile, const char *outfile,
                  const char *const *keys, const char *const *values, int n,
                  int mov_freeform,
                  char *errbuf, size_t errbuf_size) {
    AVFormatContext *ic = NULL;
    AVFormatContext *oc = NULL;
    AVPacket *pkt = NULL;
    int *smap = NULL;
    int ret = 0;
    int header_written = 0;

    ret = avformat_open_input(&ic, infile, NULL, NULL);
    if (ret < 0) {
        goto done;
    }
    // Stream copy remux: no need to parse frame contents. Skip the parsers,
    // and skip the stream-info probe when the container header already types
    // every stream (it just reads data we then copy through anyway).
    ic->flags |= AVFMT_FLAG_NOPARSE;
    int need_probe = ic->nb_streams == 0;
    for (unsigned i = 0; i < ic->nb_streams && !need_probe; i++) {
        enum AVMediaType t = ic->streams[i]->codecpar->codec_type;
        if (t != AVMEDIA_TYPE_VIDEO && t != AVMEDIA_TYPE_AUDIO &&
            t != AVMEDIA_TYPE_SUBTITLE && t != AVMEDIA_TYPE_DATA &&
            t != AVMEDIA_TYPE_ATTACHMENT) {
            need_probe = 1;
        }
    }
    if (need_probe) {
        ret = avformat_find_stream_info(ic, NULL);
        if (ret < 0) {
            goto done;
        }
    }

    ret = avformat_alloc_output_context2(&oc, NULL, NULL, outfile);
    if (ret < 0 || !oc) {
        if (ret >= 0) ret = AVERROR(ENOMEM);
        goto done;
    }

    smap = av_malloc_array(ic->nb_streams, sizeof(*smap));
    if (!smap) {
        ret = AVERROR(ENOMEM);
        goto done;
    }
    for (unsigned i = 0; i < ic->nb_streams; i++) {
        smap[i] = -1;
        AVStream *in = ic->streams[i];
        enum AVMediaType t = in->codecpar->codec_type;
        if (!is_av_stream(t) && t != AVMEDIA_TYPE_SUBTITLE &&
            t != AVMEDIA_TYPE_DATA && t != AVMEDIA_TYPE_ATTACHMENT) {
            continue;
        }
        AVStream *os = avformat_new_stream(oc, NULL);
        if (!os) {
            ret = AVERROR(ENOMEM);
            goto done;
        }
        ret = avcodec_parameters_copy(os->codecpar, in->codecpar);
        if (ret < 0) {
            goto done;
        }
        os->codecpar->codec_tag = 0;
        os->time_base = in->time_base;
        os->disposition = in->disposition;
        av_dict_copy(&os->metadata, in->metadata, 0);
        smap[i] = os->index;
    }

    av_dict_copy(&oc->metadata, ic->metadata, 0);
    for (int k = 0; k < n; k++) {
        ret = av_dict_set(&oc->metadata, keys[k], values[k], 0);
        if (ret < 0) {
            goto done;
        }
    }
    ret = 0;

    // For MP4/MOV, only enable the "mdta" freeform key box when the caller
    // needs arbitrary keys to survive (e.g. `mc hash` storing mc.hash). `mc set`
    // leaves it off so standard fields land in the iTunes ilst atoms that
    // Windows Explorer and QuickTime read.
    if (mov_freeform && muxer_is_mov(oc->oformat)) {
        av_opt_set(oc->priv_data, "movflags", "+use_metadata_tags", 0);
    }

    if (!(oc->oformat->flags & AVFMT_NOFILE)) {
        ret = avio_open(&oc->pb, outfile, AVIO_FLAG_WRITE);
        if (ret < 0) {
            goto done;
        }
    }

    ret = avformat_write_header(oc, NULL);
    if (ret < 0) {
        goto done;
    }
    header_written = 1;

    pkt = av_packet_alloc();
    if (!pkt) {
        ret = AVERROR(ENOMEM);
        goto done;
    }
    while ((ret = av_read_frame(ic, pkt)) >= 0) {
        int oi = smap[pkt->stream_index];
        if (oi < 0) {
            av_packet_unref(pkt);
            continue;
        }
        AVStream *in = ic->streams[pkt->stream_index];
        AVStream *os = oc->streams[oi];
        av_packet_rescale_ts(pkt, in->time_base, os->time_base);
        pkt->stream_index = oi;
        pkt->pos = -1;
        ret = av_interleaved_write_frame(oc, pkt);
        av_packet_unref(pkt);
        if (ret < 0) {
            goto done;
        }
    }
    if (ret == AVERROR_EOF) {
        ret = 0;
    }
    if (ret < 0) {
        goto done;
    }

    ret = av_write_trailer(oc);
    header_written = 0;
    if (ret < 0) {
        goto done;
    }

done:
    if (ret < 0) {
        set_err(errbuf, errbuf_size, ret);
    }
    if (pkt) {
        av_packet_free(&pkt);
    }
    av_free(smap);
    if (oc) {
        if (header_written) {
            av_write_trailer(oc);
        }
        if (oc->pb && !(oc->oformat->flags & AVFMT_NOFILE)) {
            avio_closep(&oc->pb);
        }
        avformat_free_context(oc);
    }
    if (ic) {
        avformat_close_input(&ic);
    }
    return ret;
}

int mc_read_tag(const char *filename, const char *key,
                char *out, size_t out_size,
                char *errbuf, size_t errbuf_size) {
    AVFormatContext *ic = NULL;
    int ret = avformat_open_input(&ic, filename, NULL, NULL);
    if (ret < 0) {
        set_err(errbuf, errbuf_size, ret);
        return ret;
    }

    int rc;
    const AVDictionaryEntry *e = av_dict_get(ic->metadata, key, NULL, 0);
    if (!e) {
        rc = 1;
    } else if (strlen(e->value) >= out_size) {
        rc = AVERROR(ENOSPC);
        set_err(errbuf, errbuf_size, rc);
    } else {
        strcpy(out, e->value);
        rc = 0;
    }

    avformat_close_input(&ic);
    return rc;
}

// ---- mc_probe ---------------------------------------------------------------

static void probe_emit(AVBPrint *bp, const char *key, const char *val) {
    if (!val) {
        return;
    }
    av_bprintf(bp, "%s=", key);
    for (const unsigned char *p = (const unsigned char *)val; *p; p++) {
        switch (*p) {
        case '\\': av_bprintf(bp, "\\\\"); break;
        case '\n': av_bprintf(bp, "\\n"); break;
        case '\t': av_bprintf(bp, "\\t"); break;
        case '\r': break;
        default:   av_bprint_chars(bp, (char)*p, 1);
        }
    }
    av_bprint_chars(bp, '\n', 1);
}

static void probe_emit_int(AVBPrint *bp, const char *key, long long v) {
    char b[32];
    snprintf(b, sizeof b, "%lld", v);
    probe_emit(bp, key, b);
}

static void probe_emit_dict(AVBPrint *bp, const char *prefix, const AVDictionary *d) {
    const AVDictionaryEntry *e = NULL;
    char key[512];
    while ((e = av_dict_get(d, "", e, AV_DICT_IGNORE_SUFFIX))) {
        snprintf(key, sizeof key, "%s%s", prefix, e->key);
        probe_emit(bp, key, e->value);
    }
}

static void probe_deep_frame_meta(AVBPrint *bp, AVFormatContext *ic) {
    int vs = av_find_best_stream(ic, AVMEDIA_TYPE_VIDEO, -1, -1, NULL, 0);
    if (vs < 0) {
        return;
    }
    AVStream *st = ic->streams[vs];
    const AVCodec *dec = avcodec_find_decoder(st->codecpar->codec_id);
    if (!dec) {
        return;
    }
    AVCodecContext *dc = avcodec_alloc_context3(dec);
    if (!dc) {
        return;
    }
    AVPacket *pkt = av_packet_alloc();
    AVFrame *fr = av_frame_alloc();
    if (dc && pkt && fr &&
        avcodec_parameters_to_context(dc, st->codecpar) >= 0 &&
        avcodec_open2(dc, dec, NULL) >= 0) {
        int done = 0;
        while (!done && av_read_frame(ic, pkt) >= 0) {
            if (pkt->stream_index == vs && avcodec_send_packet(dc, pkt) >= 0) {
                if (avcodec_receive_frame(dc, fr) >= 0) {
                    probe_emit_dict(bp, "metadata.", fr->metadata);
                    av_frame_unref(fr);
                    done = 1;
                }
            }
            av_packet_unref(pkt);
        }
    }
    av_frame_free(&fr);
    av_packet_free(&pkt);
    avcodec_free_context(&dc);
}

int mc_probe(const char *filename, int deep,
             char **out, char *errbuf, size_t errbuf_size) {
    if (out) {
        *out = NULL;
    }

    AVFormatContext *ic = NULL;
    int ret = avformat_open_input(&ic, filename, NULL, NULL);
    if (ret < 0) {
        set_err(errbuf, errbuf_size, ret);
        return ret;
    }
    ret = avformat_find_stream_info(ic, NULL);
    if (ret < 0) {
        set_err(errbuf, errbuf_size, ret);
        avformat_close_input(&ic);
        return ret;
    }

    AVBPrint bp;
    av_bprint_init(&bp, 4096, AV_BPRINT_SIZE_UNLIMITED);

    probe_emit(&bp, "format.name", ic->iformat && ic->iformat->name ? ic->iformat->name : "");
    if (ic->iformat && ic->iformat->long_name) {
        probe_emit(&bp, "format.long_name", ic->iformat->long_name);
    }
    if (ic->duration != AV_NOPTS_VALUE) {
        probe_emit_int(&bp, "format.duration_us", (long long)ic->duration);
    }
    if (ic->bit_rate > 0) {
        probe_emit_int(&bp, "format.bit_rate", (long long)ic->bit_rate);
    }
    probe_emit_int(&bp, "format.nb_streams", (long long)ic->nb_streams);
    probe_emit_dict(&bp, "metadata.", ic->metadata);

    for (unsigned i = 0; i < ic->nb_streams; i++) {
        AVStream *st = ic->streams[i];
        AVCodecParameters *cp = st->codecpar;
        char pfx[32];
        snprintf(pfx, sizeof pfx, "stream.%u.", i);
        char key[64];
#define PK(name) (snprintf(key, sizeof key, "%s%s", pfx, (name)), key)

        const char *mt = av_get_media_type_string(cp->codec_type);
        probe_emit(&bp, PK("type"), mt ? mt : "unknown");
        probe_emit(&bp, PK("codec"), avcodec_get_name(cp->codec_id));
        const AVCodecDescriptor *cd = avcodec_descriptor_get(cp->codec_id);
        if (cd && cd->long_name) {
            probe_emit(&bp, PK("codec_long"), cd->long_name);
        }
        const char *prof = avcodec_profile_name(cp->codec_id, cp->profile);
        if (prof) {
            probe_emit(&bp, PK("profile"), prof);
        }

        if (cp->codec_type == AVMEDIA_TYPE_VIDEO) {
            probe_emit_int(&bp, PK("width"), cp->width);
            probe_emit_int(&bp, PK("height"), cp->height);
            const char *pf = av_get_pix_fmt_name((enum AVPixelFormat)cp->format);
            if (pf) {
                probe_emit(&bp, PK("pix_fmt"), pf);
            }
            if (st->avg_frame_rate.num > 0 && st->avg_frame_rate.den > 0) {
                char b[32];
                snprintf(b, sizeof b, "%.6g", av_q2d(st->avg_frame_rate));
                probe_emit(&bp, PK("fps"), b);
            }
            if (cp->sample_aspect_ratio.num > 0) {
                char b[32];
                snprintf(b, sizeof b, "%d:%d",
                         cp->sample_aspect_ratio.num, cp->sample_aspect_ratio.den);
                probe_emit(&bp, PK("sar"), b);
            }
        } else if (cp->codec_type == AVMEDIA_TYPE_AUDIO) {
            probe_emit_int(&bp, PK("sample_rate"), cp->sample_rate);
            probe_emit_int(&bp, PK("channels"), cp->ch_layout.nb_channels);
            char b[64];
            if (av_channel_layout_describe(&cp->ch_layout, b, sizeof b) > 0) {
                probe_emit(&bp, PK("channel_layout"), b);
            }
            const char *sf = av_get_sample_fmt_name((enum AVSampleFormat)cp->format);
            if (sf) {
                probe_emit(&bp, PK("sample_fmt"), sf);
            }
        }

        if (cp->bit_rate > 0) {
            probe_emit_int(&bp, PK("bit_rate"), (long long)cp->bit_rate);
        }
        if (st->duration != AV_NOPTS_VALUE) {
            long long us = av_rescale_q(st->duration, st->time_base,
                                        (AVRational){1, 1000000});
            if (us > 0) {
                probe_emit_int(&bp, PK("duration_us"), us);
            }
        }
        probe_emit_dict(&bp, PK("metadata."), st->metadata);
#undef PK
    }

    if (deep) {
        probe_deep_frame_meta(&bp, ic);
    }

    if (!av_bprint_is_complete(&bp)) {
        av_bprint_finalize(&bp, NULL);
        avformat_close_input(&ic);
        set_err(errbuf, errbuf_size, AVERROR(ENOMEM));
        return AVERROR(ENOMEM);
    }

    char *s = NULL;
    ret = av_bprint_finalize(&bp, &s);
    avformat_close_input(&ic);
    if (ret < 0) {
        set_err(errbuf, errbuf_size, ret);
        return ret;
    }
    *out = s; // caller frees with av_free
    return 0;
}

// ---------------------------------------------------------------------------
// mc_split / mc_concat
// ---------------------------------------------------------------------------

static int carried_stream(enum AVMediaType t) {
    return t == AVMEDIA_TYPE_VIDEO || t == AVMEDIA_TYPE_AUDIO ||
           t == AVMEDIA_TYPE_SUBTITLE;
}

int mc_split(const char *infile, const char *out_pattern,
             const char *cut_times_sec, char *errbuf, size_t errbuf_size) {
    AVFormatContext *ic = NULL, *oc = NULL;
    AVPacket *pkt = NULL;
    int *smap = NULL;
    AVDictionary *opts = NULL;
    int ret = 0, header_written = 0;

    ret = avformat_open_input(&ic, infile, NULL, NULL);
    if (ret < 0) goto done;
    ret = avformat_find_stream_info(ic, NULL);
    if (ret < 0) goto done;

    ret = avformat_alloc_output_context2(&oc, NULL, "segment", out_pattern);
    if (ret < 0 || !oc) { if (ret >= 0) ret = AVERROR(ENOMEM); goto done; }

    smap = av_malloc_array(ic->nb_streams, sizeof(*smap));
    if (!smap) { ret = AVERROR(ENOMEM); goto done; }
    for (unsigned i = 0; i < ic->nb_streams; i++) {
        smap[i] = -1;
        AVStream *in = ic->streams[i];
        if (!carried_stream(in->codecpar->codec_type)) continue;
        AVStream *os = avformat_new_stream(oc, NULL);
        if (!os) { ret = AVERROR(ENOMEM); goto done; }
        ret = avcodec_parameters_copy(os->codecpar, in->codecpar);
        if (ret < 0) goto done;
        os->codecpar->codec_tag = 0;
        os->time_base = in->time_base;
        smap[i] = os->index;
    }
    if (oc->nb_streams == 0) { ret = AVERROR_INVALIDDATA; goto done; }

    av_dict_set(&opts, "segment_times", cut_times_sec, 0);
    av_dict_set(&opts, "segment_start_number", "1", 0);
    av_dict_set(&opts, "reset_timestamps", "1", 0);
    av_dict_set(&opts, "individual_header_trailer", "1", 0);

    ret = avformat_write_header(oc, &opts);
    if (ret < 0) goto done;
    header_written = 1;

    pkt = av_packet_alloc();
    if (!pkt) { ret = AVERROR(ENOMEM); goto done; }
    while ((ret = av_read_frame(ic, pkt)) >= 0) {
        int oi = smap[pkt->stream_index];
        if (oi < 0) { av_packet_unref(pkt); continue; }
        AVStream *in = ic->streams[pkt->stream_index];
        AVStream *os = oc->streams[oi];
        av_packet_rescale_ts(pkt, in->time_base, os->time_base);
        pkt->stream_index = oi;
        pkt->pos = -1;
        ret = av_interleaved_write_frame(oc, pkt);
        av_packet_unref(pkt);
        if (ret < 0) goto done;
    }
    if (ret == AVERROR_EOF) ret = 0;
    if (ret < 0) goto done;

    ret = av_write_trailer(oc);
    header_written = 0;

done:
    if (ret < 0) set_err(errbuf, errbuf_size, ret);
    av_dict_free(&opts);
    if (pkt) av_packet_free(&pkt);
    av_free(smap);
    if (oc) {
        if (header_written) av_write_trailer(oc);
        avformat_free_context(oc);
    }
    if (ic) avformat_close_input(&ic);
    return ret;
}

// out_map maps input stream index -> output stream index, matching video then
// audio in order. Streams beyond the first of each kind are dropped.
static void concat_map(AVFormatContext *ic, AVFormatContext *oc, int *map) {
    int have_v = 0, have_a = 0;
    for (unsigned i = 0; i < ic->nb_streams; i++) {
        map[i] = -1;
        enum AVMediaType t = ic->streams[i]->codecpar->codec_type;
        for (unsigned j = 0; j < oc->nb_streams; j++) {
            if (oc->streams[j]->codecpar->codec_type != t) continue;
            if (t == AVMEDIA_TYPE_VIDEO && have_v) continue;
            if (t == AVMEDIA_TYPE_AUDIO && have_a) continue;
            map[i] = j;
            if (t == AVMEDIA_TYPE_VIDEO) have_v = 1;
            if (t == AVMEDIA_TYPE_AUDIO) have_a = 1;
            break;
        }
    }
}

int mc_concat_copy(const char *const *infiles, int n_in, const char *outfile,
                   char *errbuf, size_t errbuf_size) {
    AVFormatContext *oc = NULL;
    AVPacket *pkt = NULL;
    int ret = 0, header_written = 0;
    int64_t base_us = 0;

    ret = avformat_alloc_output_context2(&oc, NULL, NULL, outfile);
    if (ret < 0 || !oc) { if (ret >= 0) ret = AVERROR(ENOMEM); goto done; }

    // First input defines the output streams.
    AVFormatContext *ic0 = NULL;
    ret = avformat_open_input(&ic0, infiles[0], NULL, NULL);
    if (ret < 0) goto done;
    ret = avformat_find_stream_info(ic0, NULL);
    if (ret < 0) { avformat_close_input(&ic0); goto done; }
    for (unsigned i = 0; i < ic0->nb_streams; i++) {
        AVStream *in = ic0->streams[i];
        enum AVMediaType t = in->codecpar->codec_type;
        if (t != AVMEDIA_TYPE_VIDEO && t != AVMEDIA_TYPE_AUDIO) continue;
        // one video + one audio only
        int dup = 0;
        for (unsigned j = 0; j < oc->nb_streams; j++)
            if (oc->streams[j]->codecpar->codec_type == t) dup = 1;
        if (dup) continue;
        AVStream *os = avformat_new_stream(oc, NULL);
        if (!os) { avformat_close_input(&ic0); ret = AVERROR(ENOMEM); goto done; }
        avcodec_parameters_copy(os->codecpar, in->codecpar);
        os->codecpar->codec_tag = 0;
    }
    avformat_close_input(&ic0);
    if (oc->nb_streams == 0) { ret = AVERROR_INVALIDDATA; goto done; }

    if (!(oc->oformat->flags & AVFMT_NOFILE)) {
        ret = avio_open(&oc->pb, outfile, AVIO_FLAG_WRITE);
        if (ret < 0) goto done;
    }
    ret = avformat_write_header(oc, NULL);
    if (ret < 0) goto done;
    header_written = 1;

    pkt = av_packet_alloc();
    if (!pkt) { ret = AVERROR(ENOMEM); goto done; }

    int64_t *last_dts = av_calloc(oc->nb_streams, sizeof(*last_dts));
    if (!last_dts) { ret = AVERROR(ENOMEM); goto done; }
    for (unsigned j = 0; j < oc->nb_streams; j++) last_dts[j] = AV_NOPTS_VALUE;

    for (int k = 0; k < n_in; k++) {
        AVFormatContext *ic = NULL;
        ret = avformat_open_input(&ic, infiles[k], NULL, NULL);
        if (ret < 0) { av_free(last_dts); goto done; }
        ret = avformat_find_stream_info(ic, NULL);
        if (ret < 0) { avformat_close_input(&ic); av_free(last_dts); goto done; }

        int *map = av_malloc_array(ic->nb_streams, sizeof(*map));
        if (!map) { avformat_close_input(&ic); av_free(last_dts); ret = AVERROR(ENOMEM); goto done; }
        concat_map(ic, oc, map);

        int64_t *off = av_calloc(oc->nb_streams, sizeof(*off));
        int64_t *first = av_malloc_array(oc->nb_streams, sizeof(*first));
        if (!off || !first) { av_free(map); av_free(off); av_free(first);
                              avformat_close_input(&ic); av_free(last_dts);
                              ret = AVERROR(ENOMEM); goto done; }
        for (unsigned j = 0; j < oc->nb_streams; j++) {
            off[j] = av_rescale_q(base_us, AV_TIME_BASE_Q, oc->streams[j]->time_base);
            first[j] = AV_NOPTS_VALUE;
        }

        while ((ret = av_read_frame(ic, pkt)) >= 0) {
            int oi = map[pkt->stream_index];
            if (oi < 0) { av_packet_unref(pkt); continue; }
            AVStream *is = ic->streams[pkt->stream_index];
            AVStream *os = oc->streams[oi];
            av_packet_rescale_ts(pkt, is->time_base, os->time_base);
            int64_t ref = pkt->dts != AV_NOPTS_VALUE ? pkt->dts : pkt->pts;
            if (first[oi] == AV_NOPTS_VALUE) first[oi] = ref;
            int64_t shift = off[oi] - first[oi];
            if (pkt->pts != AV_NOPTS_VALUE) pkt->pts += shift;
            if (pkt->dts != AV_NOPTS_VALUE) pkt->dts += shift;
            if (pkt->dts != AV_NOPTS_VALUE && last_dts[oi] != AV_NOPTS_VALUE &&
                pkt->dts <= last_dts[oi]) {
                int64_t bump = last_dts[oi] + 1 - pkt->dts;
                pkt->dts += bump;
                if (pkt->pts != AV_NOPTS_VALUE) pkt->pts += bump;
            }
            if (pkt->dts != AV_NOPTS_VALUE) last_dts[oi] = pkt->dts;
            pkt->stream_index = oi;
            pkt->pos = -1;
            ret = av_interleaved_write_frame(oc, pkt);
            av_packet_unref(pkt);
            if (ret < 0) break;
        }
        int64_t dur = ic->duration;
        av_free(map); av_free(off); av_free(first);
        avformat_close_input(&ic);
        if (ret < 0 && ret != AVERROR_EOF) { av_free(last_dts); goto done; }
        ret = 0;
        base_us += dur > 0 ? dur : 0;
    }
    av_free(last_dts);

    ret = av_write_trailer(oc);
    header_written = 0;

done:
    if (ret < 0) set_err(errbuf, errbuf_size, ret);
    if (pkt) av_packet_free(&pkt);
    if (oc) {
        if (header_written) av_write_trailer(oc);
        if (oc->pb && !(oc->oformat->flags & AVFMT_NOFILE)) avio_closep(&oc->pb);
        avformat_free_context(oc);
    }
    return ret;
}

// enc_write sends frame (NULL to flush) to enc and muxes every packet it
// produces into oc on stream os.
static int enc_write(AVFormatContext *oc, AVCodecContext *enc, AVStream *os,
                     AVFrame *frame) {
    int ret = avcodec_send_frame(enc, frame);
    if (ret < 0) return ret;
    AVPacket *pkt = av_packet_alloc();
    if (!pkt) return AVERROR(ENOMEM);
    while ((ret = avcodec_receive_packet(enc, pkt)) >= 0) {
        av_packet_rescale_ts(pkt, enc->time_base, os->time_base);
        pkt->stream_index = os->index;
        ret = av_interleaved_write_frame(oc, pkt);
        av_packet_unref(pkt);
        if (ret < 0) break;
    }
    av_packet_free(&pkt);
    if (ret == AVERROR(EAGAIN) || ret == AVERROR_EOF) ret = 0;
    return ret;
}

// mc_transcode re-encodes infile to outfile as MPEG-4 video + AAC audio at the
// given geometry / rate (there is no native H.264 encoder in this build).
// vw <= 0 drops video; sample_rate <= 0 drops audio. Used by `mc concat` to
// normalise mismatched inputs before a stream-copy join.
int mc_transcode(const char *infile, const char *outfile,
                 int vw, int vh, int fps_num, int fps_den,
                 int sample_rate, int channels,
                 char *errbuf, size_t errbuf_size) {
    AVFormatContext *ic = NULL, *oc = NULL;
    AVCodecContext *vdec = NULL, *adec = NULL, *venc = NULL, *aenc = NULL;
    struct SwsContext *sws = NULL;
    SwrContext *swr = NULL;
    AVAudioFifo *fifo = NULL;
    AVFrame *fr = NULL, *sf = NULL;
    AVPacket *pkt = NULL;
    AVStream *vos = NULL, *aos = NULL;
    int vsi = -1, asi = -1, ret = 0, header_written = 0;
    int64_t v_pts = 0, a_pts = 0, v_first = AV_NOPTS_VALUE, v_last = AV_NOPTS_VALUE;
    AVRational v_tb = (AVRational){0, 1};

    if (fps_num <= 0 || fps_den <= 0) { fps_num = 25; fps_den = 1; }

    ret = avformat_open_input(&ic, infile, NULL, NULL);
    if (ret < 0) goto done;
    ret = avformat_find_stream_info(ic, NULL);
    if (ret < 0) goto done;

    ret = avformat_alloc_output_context2(&oc, NULL, NULL, outfile);
    if (ret < 0 || !oc) { if (ret >= 0) ret = AVERROR(ENOMEM); goto done; }

    if (vw > 0) {
        vsi = av_find_best_stream(ic, AVMEDIA_TYPE_VIDEO, -1, -1, NULL, 0);
        if (vsi < 0) { ret = AVERROR_STREAM_NOT_FOUND; goto done; }
        const AVCodec *d = avcodec_find_decoder(ic->streams[vsi]->codecpar->codec_id);
        if (!d) { ret = AVERROR_DECODER_NOT_FOUND; goto done; }
        vdec = avcodec_alloc_context3(d);
        avcodec_parameters_to_context(vdec, ic->streams[vsi]->codecpar);
        v_tb = ic->streams[vsi]->time_base;
        vdec->pkt_timebase = v_tb; // so best_effort_timestamp is populated
        ret = avcodec_open2(vdec, d, NULL);
        if (ret < 0) goto done;

        const AVCodec *e = avcodec_find_encoder(AV_CODEC_ID_MPEG4);
        if (!e) { ret = AVERROR_ENCODER_NOT_FOUND; goto done; }
        venc = avcodec_alloc_context3(e);
        venc->width = vw; venc->height = vh;
        venc->pix_fmt = AV_PIX_FMT_YUV420P;
        venc->time_base = (AVRational){fps_den, fps_num};
        venc->framerate = (AVRational){fps_num, fps_den};
        venc->gop_size = 12;
        venc->max_b_frames = 0;
        venc->bit_rate = (int64_t)vw * vh * 2;
        if (oc->oformat->flags & AVFMT_GLOBALHEADER)
            venc->flags |= AV_CODEC_FLAG_GLOBAL_HEADER;
        ret = avcodec_open2(venc, e, NULL);
        if (ret < 0) goto done;

        vos = avformat_new_stream(oc, NULL);
        if (!vos) { ret = AVERROR(ENOMEM); goto done; }
        avcodec_parameters_from_context(vos->codecpar, venc);
        vos->time_base = venc->time_base;

        sws = sws_getContext(vdec->width, vdec->height,
                             vdec->pix_fmt == AV_PIX_FMT_NONE ? AV_PIX_FMT_YUV420P : vdec->pix_fmt,
                             vw, vh, AV_PIX_FMT_YUV420P, SWS_BILINEAR, NULL, NULL, NULL);
        if (!sws) { ret = AVERROR(ENOMEM); goto done; }
    }

    if (sample_rate > 0) {
        if (channels <= 0) channels = 2;
        asi = av_find_best_stream(ic, AVMEDIA_TYPE_AUDIO, -1, -1, NULL, 0);
        if (asi < 0) { ret = AVERROR_STREAM_NOT_FOUND; goto done; }
        const AVCodec *d = avcodec_find_decoder(ic->streams[asi]->codecpar->codec_id);
        if (!d) { ret = AVERROR_DECODER_NOT_FOUND; goto done; }
        adec = avcodec_alloc_context3(d);
        avcodec_parameters_to_context(adec, ic->streams[asi]->codecpar);
        adec->pkt_timebase = ic->streams[asi]->time_base;
        ret = avcodec_open2(adec, d, NULL);
        if (ret < 0) goto done;

        const AVCodec *e = avcodec_find_encoder(AV_CODEC_ID_AAC);
        if (!e) { ret = AVERROR_ENCODER_NOT_FOUND; goto done; }
        aenc = avcodec_alloc_context3(e);
        aenc->sample_rate = sample_rate;
        av_channel_layout_default(&aenc->ch_layout, channels);
        aenc->sample_fmt = AV_SAMPLE_FMT_FLTP;
        aenc->bit_rate = 128000;
        aenc->time_base = (AVRational){1, sample_rate};
        if (oc->oformat->flags & AVFMT_GLOBALHEADER)
            aenc->flags |= AV_CODEC_FLAG_GLOBAL_HEADER;
        ret = avcodec_open2(aenc, e, NULL);
        if (ret < 0) goto done;

        aos = avformat_new_stream(oc, NULL);
        if (!aos) { ret = AVERROR(ENOMEM); goto done; }
        avcodec_parameters_from_context(aos->codecpar, aenc);
        aos->time_base = aenc->time_base;

        AVChannelLayout inlay;
        if (adec->ch_layout.nb_channels > 0)
            av_channel_layout_copy(&inlay, &adec->ch_layout);
        else
            av_channel_layout_default(&inlay, 2);
        ret = swr_alloc_set_opts2(&swr, &aenc->ch_layout, aenc->sample_fmt, sample_rate,
                                  &inlay, adec->sample_fmt, adec->sample_rate, 0, NULL);
        av_channel_layout_uninit(&inlay);
        if (ret < 0) goto done;
        ret = swr_init(swr);
        if (ret < 0) goto done;
        fifo = av_audio_fifo_alloc(aenc->sample_fmt, aenc->ch_layout.nb_channels, 1);
        if (!fifo) { ret = AVERROR(ENOMEM); goto done; }
    }

    if (oc->nb_streams == 0) { ret = AVERROR_INVALIDDATA; goto done; }

    if (!(oc->oformat->flags & AVFMT_NOFILE)) {
        ret = avio_open(&oc->pb, outfile, AVIO_FLAG_WRITE);
        if (ret < 0) goto done;
    }
    ret = avformat_write_header(oc, NULL);
    if (ret < 0) goto done;
    header_written = 1;

    fr = av_frame_alloc();
    sf = av_frame_alloc();
    pkt = av_packet_alloc();
    if (!fr || !sf || !pkt) { ret = AVERROR(ENOMEM); goto done; }

    int flushing = 0;
    while (1) {
        int have_pkt = 0;
        if (!flushing) {
            ret = av_read_frame(ic, pkt);
            if (ret == AVERROR_EOF) { flushing = 1; }
            else if (ret < 0) goto done;
            else have_pkt = 1;
        }

        for (int pass = 0; pass < 2; pass++) {
            AVCodecContext *dec = pass == 0 ? vdec : adec;
            int si = pass == 0 ? vsi : asi;
            if (!dec) continue;
            if (have_pkt && pkt->stream_index != si) continue;
            if (!flushing && !have_pkt) continue;

            ret = avcodec_send_packet(dec, flushing ? NULL : pkt);
            if (ret < 0 && ret != AVERROR_EOF) goto done;

            while ((ret = avcodec_receive_frame(dec, fr)) >= 0) {
                if (pass == 0) {
                    // Convert to the target constant frame rate: place each
                    // frame at round(real_time * fps) and drop frames that land
                    // on an already-used slot (input faster than target).
                    int64_t ts = fr->best_effort_timestamp;
                    if (ts == AV_NOPTS_VALUE) ts = fr->pts;
                    int64_t want;
                    if (ts == AV_NOPTS_VALUE) {
                        want = v_pts++;
                    } else {
                        if (v_first == AV_NOPTS_VALUE) v_first = ts;
                        double rt = (double)(ts - v_first) * av_q2d(v_tb);
                        want = (int64_t)(rt * (double)fps_num / (double)fps_den + 0.5);
                        v_pts = want + 1;
                    }
                    if (v_last != AV_NOPTS_VALUE && want <= v_last) {
                        av_frame_unref(fr);
                        continue;
                    }
                    v_last = want;
                    av_frame_unref(sf);
                    sf->format = AV_PIX_FMT_YUV420P; sf->width = vw; sf->height = vh;
                    ret = av_frame_get_buffer(sf, 0);
                    if (ret < 0) { av_frame_unref(fr); goto done; }
                    sws_scale(sws, (const uint8_t * const *)fr->data, fr->linesize,
                              0, fr->height, sf->data, sf->linesize);
                    sf->pts = want;
                    ret = enc_write(oc, venc, vos, sf);
                    av_frame_unref(fr);
                    if (ret < 0) goto done;
                } else {
                    int cap = swr_get_out_samples(swr, fr->nb_samples);
                    uint8_t **conv = NULL; int lines = 0;
                    ret = av_samples_alloc_array_and_samples(&conv, &lines,
                            aenc->ch_layout.nb_channels, cap, aenc->sample_fmt, 0);
                    if (ret < 0) { av_frame_unref(fr); goto done; }
                    int got = swr_convert(swr, conv, cap,
                                          (const uint8_t **)fr->extended_data, fr->nb_samples);
                    if (got > 0) av_audio_fifo_write(fifo, (void **)conv, got);
                    if (conv) { av_freep(&conv[0]); av_freep(&conv); }
                    av_frame_unref(fr);
                    if (got < 0) { ret = got; goto done; }

                    while (av_audio_fifo_size(fifo) >= aenc->frame_size) {
                        AVFrame *af = av_frame_alloc();
                        af->nb_samples = aenc->frame_size;
                        af->format = aenc->sample_fmt;
                        av_channel_layout_copy(&af->ch_layout, &aenc->ch_layout);
                        af->sample_rate = aenc->sample_rate;
                        if ((ret = av_frame_get_buffer(af, 0)) < 0) { av_frame_free(&af); goto done; }
                        av_audio_fifo_read(fifo, (void **)af->data, aenc->frame_size);
                        af->pts = a_pts; a_pts += aenc->frame_size;
                        ret = enc_write(oc, aenc, aos, af);
                        av_frame_free(&af);
                        if (ret < 0) goto done;
                    }
                }
            }
            if (ret != AVERROR(EAGAIN) && ret != AVERROR_EOF) goto done;
        }

        if (have_pkt) av_packet_unref(pkt);
        if (flushing) break;
    }

    // Flush the resampler's internal buffer into the FIFO.
    if (aenc && swr) {
        int fcap = swr_get_out_samples(swr, 0);
        if (fcap > 0) {
            uint8_t **fc = NULL; int fl = 0;
            if (av_samples_alloc_array_and_samples(&fc, &fl, aenc->ch_layout.nb_channels,
                                                   fcap, aenc->sample_fmt, 0) >= 0) {
                int fg = swr_convert(swr, fc, fcap, NULL, 0);
                if (fg > 0) av_audio_fifo_write(fifo, (void **)fc, fg);
                if (fc) { av_freep(&fc[0]); av_freep(&fc); }
            }
        }
        while (av_audio_fifo_size(fifo) >= aenc->frame_size) {
            AVFrame *af = av_frame_alloc();
            af->nb_samples = aenc->frame_size;
            af->format = aenc->sample_fmt;
            av_channel_layout_copy(&af->ch_layout, &aenc->ch_layout);
            af->sample_rate = aenc->sample_rate;
            if ((ret = av_frame_get_buffer(af, 0)) < 0) { av_frame_free(&af); goto done; }
            av_audio_fifo_read(fifo, (void **)af->data, aenc->frame_size);
            af->pts = a_pts; a_pts += aenc->frame_size;
            ret = enc_write(oc, aenc, aos, af);
            av_frame_free(&af);
            if (ret < 0) goto done;
        }
    }

    // Drain the audio FIFO's tail as a final short frame, then flush encoders.
    if (aenc && av_audio_fifo_size(fifo) > 0) {
        int n = av_audio_fifo_size(fifo);
        AVFrame *af = av_frame_alloc();
        af->nb_samples = n;
        af->format = aenc->sample_fmt;
        av_channel_layout_copy(&af->ch_layout, &aenc->ch_layout);
        af->sample_rate = aenc->sample_rate;
        if ((ret = av_frame_get_buffer(af, 0)) >= 0) {
            av_audio_fifo_read(fifo, (void **)af->data, n);
            af->pts = a_pts; a_pts += n;
            ret = enc_write(oc, aenc, aos, af);
        }
        av_frame_free(&af);
        if (ret < 0) goto done;
    }
    if (venc && (ret = enc_write(oc, venc, vos, NULL)) < 0) goto done;
    if (aenc && (ret = enc_write(oc, aenc, aos, NULL)) < 0) goto done;

    ret = av_write_trailer(oc);
    header_written = 0;

done:
    if (ret < 0) set_err(errbuf, errbuf_size, ret);
    if (pkt) av_packet_free(&pkt);
    if (fr) av_frame_free(&fr);
    if (sf) av_frame_free(&sf);
    if (fifo) av_audio_fifo_free(fifo);
    if (swr) swr_free(&swr);
    if (sws) sws_freeContext(sws);
    if (vdec) avcodec_free_context(&vdec);
    if (adec) avcodec_free_context(&adec);
    if (venc) avcodec_free_context(&venc);
    if (aenc) avcodec_free_context(&aenc);
    if (oc) {
        if (header_written) av_write_trailer(oc);
        if (oc->pb && !(oc->oformat->flags & AVFMT_NOFILE)) avio_closep(&oc->pb);
        avformat_free_context(oc);
    }
    if (ic) avformat_close_input(&ic);
    return ret < 0 ? ret : 0;
}
