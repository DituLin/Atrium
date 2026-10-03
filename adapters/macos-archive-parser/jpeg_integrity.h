// Strict validation delegates entropy decoding to the pinned public libjpeg API.
// Only scaled scanlines are materialized; ImageIO separately makes the OCR raster.
#include <dlfcn.h>
#include <setjmp.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>

// Keep public header prerequisites and ABI-version-dependent error codes in order.
// clang-format off
#include "vendor/libjpeg-turbo/include/jpeglib.h"
#include "vendor/libjpeg-turbo/include/jerror.h"
// clang-format on

typedef enum {
    JPEGComplete,
    JPEGInvalid,
    JPEGResourceLimit,
    JPEGUnsupported,
    JPEGUnavailable
} JPEGIntegrity;

typedef struct {
    __typeof__(jpeg_std_error) *standardError;
    __typeof__(jpeg_CreateDecompress) *create;
    __typeof__(jpeg_mem_src) *source;
    __typeof__(jpeg_read_header) *header;
    __typeof__(jpeg_start_decompress) *start;
    __typeof__(jpeg_read_scanlines) *scanlines;
    __typeof__(jpeg_finish_decompress) *finish;
    __typeof__(jpeg_destroy_decompress) *destroy;
} JPEGFunctions;
static JPEGFunctions jpegFunctions;
static bool jpegLoad(const char *fixedPath) {
    void *library = dlopen(fixedPath, RTLD_NOW | RTLD_LOCAL);
    if (!library)
        return false;
#define JPEG_LOAD(field, symbol)                                                                   \
    do {                                                                                           \
        jpegFunctions.field = (__typeof__(symbol) *)dlsym(library, #symbol);                       \
        if (!jpegFunctions.field) {                                                                \
            dlclose(library);                                                                      \
            return false;                                                                          \
        }                                                                                          \
    } while (0)
    JPEG_LOAD(standardError, jpeg_std_error);
    JPEG_LOAD(create, jpeg_CreateDecompress);
    JPEG_LOAD(source, jpeg_mem_src);
    JPEG_LOAD(header, jpeg_read_header);
    JPEG_LOAD(start, jpeg_start_decompress);
    JPEG_LOAD(scanlines, jpeg_read_scanlines);
    JPEG_LOAD(finish, jpeg_finish_decompress);
    JPEG_LOAD(destroy, jpeg_destroy_decompress);
#undef JPEG_LOAD
    return true; // Pinned library remains loaded until this one-file process exits.
}

typedef struct {
    struct jpeg_decompress_struct decoder; // first: callbacks safely recover this state
    struct jpeg_error_mgr errors;
    struct jpeg_progress_mgr progress;
    jmp_buf failed;
    bool created;
    unsigned char *row;
    JPEGIntegrity result;
} JPEGValidation;
static void jpegFailure(j_common_ptr decoder) {
    JPEGValidation *state = (JPEGValidation *)decoder;
    switch (decoder->err->msg_code) {
    case JERR_NO_BACKING_STORE:
    case JERR_OUT_OF_MEMORY:
        state->result = JPEGResourceLimit;
        break;
    case JERR_BAD_LIB_VERSION:
    case JERR_BAD_STRUCT_SIZE:
        state->result = JPEGUnavailable;
        break;
    default:
        state->result = JPEGInvalid;
        break;
    }
    longjmp(state->failed, 1);
}
static void jpegMessage(j_common_ptr decoder, int level) {
    // EOF insertion and recovered entropy damage are warnings, not fatal errors,
    // by default. They invalidate a complete-evidence claim. Never print text.
    if (level < 0)
        jpegFailure(decoder);
}
static void jpegProgress(j_common_ptr decoder) {
    JPEGValidation *state = (JPEGValidation *)decoder;
    if (state->decoder.input_scan_number > 128) {
        state->result = JPEGResourceLimit;
        longjmp(state->failed, 1);
    }
}
static JPEGIntegrity jpegComplete(const uint8_t *data, size_t length, uint32_t width,
                                  uint32_t height, long memoryBudget) {
    // Heap state remains defined after longjmp; no Objective-C objects cross it.
    JPEGValidation *state = calloc(1, sizeof(*state));
    if (!state)
        return JPEGResourceLimit;
    state->result = JPEGInvalid;
    struct jpeg_decompress_struct *decoder = &state->decoder;
    decoder->err = jpegFunctions.standardError(&state->errors);
    state->errors.error_exit = jpegFailure;
    state->errors.emit_message = jpegMessage;
    if (!setjmp(state->failed)) {
        jpegFunctions.create(decoder, JPEG_LIB_VERSION, sizeof(*decoder));
        state->created = true;
        decoder->mem->max_memory_to_use = memoryBudget;
        state->progress.progress_monitor = jpegProgress;
        decoder->progress = &state->progress;
        jpegFunctions.source(decoder, data, length);
        if (jpegFunctions.header(decoder, TRUE) != JPEG_HEADER_OK)
            goto cleanup;
        if (decoder->image_width != width || decoder->image_height != height)
            goto cleanup;
        if (decoder->data_precision != 8 || decoder->num_components > 4) {
            state->result = JPEGUnsupported;
            goto cleanup;
        }
        decoder->scale_num = 1;
        decoder->scale_denom = 8;
        decoder->out_color_space = decoder->jpeg_color_space;
        if (!jpegFunctions.start(decoder))
            goto cleanup;
        if (!decoder->output_width || !decoder->output_height || decoder->output_width > 4096 ||
            decoder->output_height > 4096 ||
            (uint64_t)decoder->output_width * decoder->output_height > 16000000) {
            state->result = JPEGResourceLimit;
            goto cleanup;
        }
        size_t rowBytes = (size_t)decoder->output_width * decoder->output_components;
        if (!rowBytes || rowBytes > 65536 || !(state->row = malloc(rowBytes))) {
            state->result = JPEGResourceLimit;
            goto cleanup;
        }
        while (decoder->output_scanline < decoder->output_height) {
            JSAMPROW row = state->row;
            if (jpegFunctions.scanlines(decoder, &row, 1) != 1)
                goto cleanup;
        }
        if (jpegFunctions.finish(decoder))
            state->result = JPEGComplete;
    }
cleanup:
    if (state->created)
        jpegFunctions.destroy(decoder);
    free(state->row);
    JPEGIntegrity result = state->result;
    free(state);
    return result;
}
