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
                   char *out, size_t out_size,
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
    ret = avformat_find_stream_info(ic, NULL);
    if (ret < 0) {
        goto done;
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
    ret = avformat_find_stream_info(ic, NULL);
    if (ret < 0) {
        goto done;
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

    if (muxer_is_mov(oc->oformat)) {
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
