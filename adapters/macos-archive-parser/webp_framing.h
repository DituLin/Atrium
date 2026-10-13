// RIFF/WebP container bounds. webp_integrity.h additionally validates compressed
// completion with libwebp; framing alone does not prove pixel data is complete.
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include <string.h>
static uint32_t webpU32(const uint8_t *p) {
    return (uint32_t)p[0] | ((uint32_t)p[1] << 8) | ((uint32_t)p[2] << 16) | ((uint32_t)p[3] << 24);
}
static bool webpFramed(const uint8_t *data, size_t length) {
    if (length < 12 || memcmp(data, "RIFF", 4) || memcmp(data + 8, "WEBP", 4) ||
        webpU32(data + 4) != length - 8)
        return false;
    size_t offset = 12;
    for (unsigned chunks = 0; offset < length && chunks < 65536; chunks++) {
        if (length - offset < 8)
            return false;
        uint64_t n = webpU32(data + offset + 4);
        uint64_t padded = n + (n & 1);
        if (padded > length - offset - 8)
            return false;
        if ((n & 1) && data[offset + 8 + n] != 0)
            return false;
        offset += 8 + (size_t)padded;
    }
    return offset == length;
}
