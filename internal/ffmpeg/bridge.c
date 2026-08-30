#include "bridge.h"

#include <string.h>
#include <stdio.h>

#include <libavcodec/avcodec.h>
#include <libavformat/avformat.h>
#include <libavutil/avutil.h>
#include <libavutil/dict.h>
#include <libavutil/error.h>
#include <libavutil/hash.h>
#include <libavutil/imgutils.h>
#include <libavutil/intreadwrite.h>
#include <libavutil/mem.h>
#include <libavutil/opt.h>

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

int mc_write_tag(const char *infile, const char *outfile,
                 const char *key, const char *value,
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
    ret = av_dict_set(&oc->metadata, key, value, 0);
    if (ret < 0) {
        goto done;
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
