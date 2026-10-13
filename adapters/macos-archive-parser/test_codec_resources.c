// Synthetic native test only; not compiled into the production helper.
#include "jpeg_integrity.h"
#include <string.h>
int main(int argc, char **argv) {
    if (argc != 3 || !jpegLoad(argv[1]))
        return 2;
    FILE *file = fopen(argv[2], "rb");
    if (!file || fseek(file, 0, SEEK_END))
        return 2;
    long length = ftell(file);
    if (length < 1 || length > 32 * 1024 * 1024 || fseek(file, 0, SEEK_SET))
        return 2;
    unsigned char *bytes = malloc((size_t)length);
    if (!bytes || fread(bytes, 1, (size_t)length, file) != (size_t)length)
        return 2;
    fclose(file);
    // Same real 6000x6000 progressive JPEG: a constrained coefficient allocation
    // is a resource outcome, not corruption. Production uses the larger budget.
    JPEGIntegrity small = jpegComplete(bytes, (size_t)length, 6000, 6000, 64L * 1024 * 1024);
    JPEGIntegrity normal = jpegComplete(bytes, (size_t)length, 6000, 6000, 512L * 1024 * 1024);
    free(bytes);
    if (small != JPEGResourceLimit || normal != JPEGComplete)
        return 1;
    // ABI mismatch must be unavailable, without formatting library diagnostics.
    JPEGValidation *state = calloc(1, sizeof(*state));
    if (!state)
        return 2;
    state->decoder.err = &state->errors;
    state->errors.msg_code = JERR_BAD_STRUCT_SIZE;
    if (!setjmp(state->failed))
        jpegFailure((j_common_ptr)&state->decoder);
    bool unavailable = state->result == JPEGUnavailable;
    free(state);
    if (!unavailable)
        return 1;
    puts("jpeg resource and ABI outcome checks passed");
    return 0;
}
