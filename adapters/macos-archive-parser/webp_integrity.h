// The pinned public decoder must finish the compressed image, not just produce
// a recovery thumbnail. A 1x1 external output avoids a full output pixel plane;
// source-sized internal lossless work remains subject to input/process limits.
#include "vendor/libwebp/include/decode.h"
#include <dlfcn.h>
#include <stdbool.h>

typedef enum {
    WebPComplete,
    WebPInvalid,
    WebPResourceLimit,
    WebPUnsupported,
    WebPUnavailable
} WebPIntegrity;
typedef struct {
    __typeof__(WebPGetDecoderVersion) *version;
    __typeof__(WebPInitDecoderConfigInternal) *initialize;
    __typeof__(WebPGetFeaturesInternal) *features;
    __typeof__(WebPDecode) *decode;
    __typeof__(WebPFreeDecBuffer) *freeBuffer;
} WebPFunctions;
static WebPFunctions webpFunctions;
static bool webpLoad(const char *fixedPath) {
    void *library = dlopen(fixedPath, RTLD_NOW | RTLD_LOCAL);
    if (!library)
        return false;
#define WEBP_LOAD(field, symbol)                                                                   \
    do {                                                                                           \
        webpFunctions.field = (__typeof__(symbol) *)dlsym(library, #symbol);                       \
        if (!webpFunctions.field) {                                                                \
            dlclose(library);                                                                      \
            return false;                                                                          \
        }                                                                                          \
    } while (0)
    WEBP_LOAD(version, WebPGetDecoderVersion);
    WEBP_LOAD(initialize, WebPInitDecoderConfigInternal);
    WEBP_LOAD(features, WebPGetFeaturesInternal);
    WEBP_LOAD(decode, WebPDecode);
    WEBP_LOAD(freeBuffer, WebPFreeDecBuffer);
#undef WEBP_LOAD
    return webpFunctions.version() == 0x010600;
}
static WebPIntegrity webpStatus(VP8StatusCode status) {
    switch (status) {
    case VP8_STATUS_OK:
        return WebPComplete;
    case VP8_STATUS_OUT_OF_MEMORY:
        return WebPResourceLimit;
    case VP8_STATUS_UNSUPPORTED_FEATURE:
        return WebPUnsupported;
    default:
        return WebPInvalid;
    }
}
static WebPIntegrity webpComplete(const uint8_t *data, size_t length, uint32_t width,
                                  uint32_t height) {
    WebPDecoderConfig config;
    if (!webpFunctions.initialize(&config, WEBP_DECODER_ABI_VERSION))
        return WebPUnavailable;
    VP8StatusCode status =
        webpFunctions.features(data, length, &config.input, WEBP_DECODER_ABI_VERSION);
    if (status != VP8_STATUS_OK)
        return webpStatus(status);
    if (config.input.has_animation)
        return WebPUnsupported;
    if (config.input.width <= 0 || config.input.height <= 0 ||
        (uint32_t)config.input.width != width || (uint32_t)config.input.height != height)
        return WebPInvalid;
    uint8_t pixel[4];
    config.options.use_scaling = 1;
    config.options.scaled_width = 1;
    config.options.scaled_height = 1;
    config.output.colorspace = MODE_RGBA;
    config.output.is_external_memory = 1;
    config.output.u.RGBA.rgba = pixel;
    config.output.u.RGBA.stride = sizeof(pixel);
    config.output.u.RGBA.size = sizeof(pixel);
    status = webpFunctions.decode(data, length, &config);
    webpFunctions.freeBuffer(&config.output);
    return webpStatus(status);
}
