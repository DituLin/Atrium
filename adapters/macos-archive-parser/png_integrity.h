// Bounded PNG completion validation. No pixel plane is allocated or unfiltered.
// Specification: https://www.w3.org/TR/png-3/ (chunks, CRC, filtering, Adam7).
#include <stdbool.h>
#include <stdint.h>
#include <string.h>
#include <zlib.h>

typedef struct {
    uint64_t rowBytes[7], rows[7];
    unsigned pass, passes;
    uint64_t remaining;
} PNGScanlines;

static uint32_t pngU32(const uint8_t *p) {
    return ((uint32_t)p[0] << 24) | ((uint32_t)p[1] << 16) | ((uint32_t)p[2] << 8) | p[3];
}
static void pngNextPass(PNGScanlines *s) {
    while (s->pass < s->passes && s->rows[s->pass] == 0)
        s->pass++;
    s->remaining = s->pass < s->passes ? s->rowBytes[s->pass] + 1 : 0;
}
static bool pngConsume(PNGScanlines *s, const uint8_t *p, size_t n) {
    while (n) {
        if (s->pass == s->passes)
            return false; // More inflated bytes than the IHDR requires.
        if (s->remaining == s->rowBytes[s->pass] + 1 && *p > 4)
            return false;
        size_t take = n < s->remaining ? n : (size_t)s->remaining;
        s->remaining -= take;
        p += take;
        n -= take;
        if (!s->remaining) {
            s->rows[s->pass]--;
            pngNextPass(s);
        }
    }
    return true;
}
static bool pngPlan(PNGScanlines *s, uint32_t w, uint32_t h, unsigned bits, bool adam7) {
    static const unsigned x[7] = {0, 4, 0, 2, 0, 1, 0}, y[7] = {0, 0, 4, 0, 2, 0, 1};
    static const unsigned dx[7] = {8, 8, 4, 4, 2, 2, 1}, dy[7] = {8, 8, 8, 4, 4, 2, 2};
    memset(s, 0, sizeof(*s));
    s->passes = adam7 ? 7 : 1;
    uint64_t total = 0;
    for (unsigned i = 0; i < s->passes; i++) {
        uint64_t pw = adam7 ? (w > x[i] ? (w - x[i] + dx[i] - 1) / dx[i] : 0) : w;
        uint64_t ph = adam7 ? (h > y[i] ? (h - y[i] + dy[i] - 1) / dy[i] : 0) : h;
        if (!pw || !ph)
            continue;
        s->rowBytes[i] = (pw * bits + 7) / 8;
        s->rows[i] = ph;
        total += (s->rowBytes[i] + 1) * ph;
    }
    // 60 MP * 8 bytes/pixel plus filter bytes; independent of compressed size.
    if (!total || total > 500000000)
        return false;
    pngNextPass(s);
    return true;
}
static bool pngComplete(const uint8_t *data, size_t length, uint32_t width, uint32_t height) {
    if (length < 8 || memcmp(data, "\x89PNG\r\n\x1a\n", 8))
        return false;
    bool ihdr = false, palette = false, idat = false, afterIDAT = false;
    bool initialized = false, streamEnd = false, valid = false;
    unsigned color = 0, depth = 0;
    size_t offset = 8;
    z_stream stream = {0};
    PNGScanlines lines = {0};
    uint8_t output[32768];
    for (unsigned chunks = 0; offset < length && chunks < 65536; chunks++) {
        if (length - offset < 12)
            goto done;
        uint32_t n = pngU32(data + offset);
        if (n > length - offset - 12)
            goto done;
        const uint8_t *type = data + offset + 4, *payload = type + 4;
        for (unsigned i = 0; i < 4; i++) {
            if (!((type[i] >= 'A' && type[i] <= 'Z') || (type[i] >= 'a' && type[i] <= 'z')))
                goto done;
        }
        if (type[2] & 32)
            goto done; // Reserved chunk-type bit.
        if (crc32(0, type, n + 4) != pngU32(payload + n))
            goto done;
        offset += (size_t)n + 12;
        if (!ihdr && memcmp(type, "IHDR", 4))
            goto done;
        if (memcmp(type, "IDAT", 4) && idat)
            afterIDAT = true;
        if (!memcmp(type, "IHDR", 4)) {
            if (ihdr || n != 13 || pngU32(payload) != width || pngU32(payload + 4) != height)
                goto done;
            ihdr = true;
            depth = payload[8];
            color = payload[9];
            unsigned channels = 0;
            switch (color) {
            case 0:
                channels = 1;
                if (!(depth == 1 || depth == 2 || depth == 4 || depth == 8 || depth == 16))
                    goto done;
                break;
            case 2:
                channels = 3;
                if (!(depth == 8 || depth == 16))
                    goto done;
                break;
            case 3:
                channels = 1;
                if (!(depth == 1 || depth == 2 || depth == 4 || depth == 8))
                    goto done;
                break;
            case 4:
                channels = 2;
                if (!(depth == 8 || depth == 16))
                    goto done;
                break;
            case 6:
                channels = 4;
                if (!(depth == 8 || depth == 16))
                    goto done;
                break;
            default:
                goto done;
            }
            if (payload[10] || payload[11] || payload[12] > 1)
                goto done;
            if (!pngPlan(&lines, width, height, channels * depth, payload[12] == 1))
                goto done;
        } else if (!memcmp(type, "PLTE", 4)) {
            if (palette || idat || !n || n % 3 || n > 768 || color == 0 || color == 4 ||
                (color == 3 && n / 3 > (1u << depth)))
                goto done;
            palette = true;
        } else if (!memcmp(type, "IDAT", 4)) {
            if (afterIDAT || (color == 3 && !palette))
                goto done;
            idat = true;
            if (!initialized) {
                if (inflateInit(&stream) != Z_OK)
                    goto done;
                initialized = true;
            }
            if (streamEnd && n)
                goto done;
            stream.next_in = (Bytef *)payload;
            stream.avail_in = n;
            while (stream.avail_in) {
                stream.next_out = output;
                stream.avail_out = sizeof(output);
                uInt before = stream.avail_in;
                int code = inflate(&stream, Z_NO_FLUSH);
                if (!pngConsume(&lines, output, sizeof(output) - stream.avail_out))
                    goto done;
                if (code == Z_STREAM_END) {
                    streamEnd = true;
                    if (stream.avail_in)
                        goto done;
                    break;
                }
                if (code != Z_OK ||
                    (before == stream.avail_in && stream.avail_out == sizeof(output)))
                    goto done;
            }
        } else if (!memcmp(type, "IEND", 4)) {
            valid = !n && idat && streamEnd && lines.pass == lines.passes && offset == length;
            goto done;
        } else if (!(type[0] & 32)) {
            goto done; // Unknown critical chunks cannot be ignored as a complete image.
        }
    }
done:
    if (initialized)
        inflateEnd(&stream);
    return valid;
}
