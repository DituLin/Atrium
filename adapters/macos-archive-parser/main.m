#include "jpeg_integrity.h"
#include "png_integrity.h"
#include "webp_framing.h"
#include "webp_integrity.h"
#include <CommonCrypto/CommonDigest.h>
#import <CoreGraphics/CoreGraphics.h>
#import <Foundation/Foundation.h>
#import <ImageIO/ImageIO.h>
#import <PDFKit/PDFKit.h>
#import <Vision/Vision.h>
#include <limits.h>
#include <mach-o/dyld.h>
#include <math.h>
#include <sys/stat.h>
#include <sys/sysctl.h>
#include <unistd.h>
#ifndef ATRIUM_BUILD
#error Build using build.sh to fingerprint the helper source and compiler.
#endif
static const NSUInteger TextLimit = 2 * 1024 * 1024, SegmentLimit = 20000;
static NSMutableDictionary *result;
static NSMutableArray *segments, *pages;
static NSUInteger textBytes;
static BOOL stopped;
static NSArray *box(CGRect r) {
    return @[ @(r.origin.x), @(r.origin.y), @(r.size.width), @(r.size.height) ];
}
static BOOL finiteBox(CGRect r) {
    return isfinite(r.origin.x) && isfinite(r.origin.y) && isfinite(r.size.width) &&
           isfinite(r.size.height) && r.size.width > 0 && r.size.height > 0 &&
           fabs(r.origin.x) < 1e9 && fabs(r.origin.y) < 1e9 && r.size.width < 1e9 &&
           r.size.height < 1e9;
}
static CGRect topRect(CGRect r, double w, double h) {
    return CGRectMake(r.origin.x / w, 1 - CGRectGetMaxY(r) / h, r.size.width / w,
                      r.size.height / h);
}
static void outcome(NSString *status, NSString *code) {
    result[@"status"] = status;
    result[@"code"] = code;
}
static void stop(NSString *status, NSString *code) {
    stopped = YES;
    outcome(status, code);
}
static BOOL appendObservation(NSUInteger page, NSString *text, NSString *origin, CGRect rect,
                              CGRect raw, BOOL clipped, double confidence) {
    if (!text ||
        ![text stringByTrimmingCharactersInSet:NSCharacterSet.whitespaceAndNewlineCharacterSet]
             .length)
        return YES;
    NSUInteger n = [text lengthOfBytesUsingEncoding:NSUTF8StringEncoding];
    if (n > TextLimit - textBytes) {
        stop(@"limit_exceeded", @"text_limit");
        return NO;
    }
    if (segments.count >= SegmentLimit) {
        stop(@"limit_exceeded", @"segment_limit");
        return NO;
    }
    if (!finiteBox(rect)) {
        outcome(@"partial", @"page_failed");
        return NO;
    }
    // Tiny floating arithmetic excursions are clipped against the actual bitmap.
    rect = CGRectIntersection(rect, CGRectMake(0, 0, 1, 1));
    if (!finiteBox(rect)) {
        outcome(@"partial", @"page_failed");
        return NO;
    }
    [segments addObject:@{
        @"page" : @(page),
        @"text" : text,
        @"origin" : origin,
        @"rect" : box(rect),
        @"page_space" : [origin isEqual:@"native"] ? box(raw) : NSNull.null,
        @"clipped" : @(clipped),
        @"confidence" : @(confidence)
    }];
    textBytes += n;
    return YES;
}
static VNRecognizeTextRequest *request(void) {
    VNRecognizeTextRequest *r = [VNRecognizeTextRequest new];
    r.revision = VNRecognizeTextRequestRevision3;
    r.recognitionLevel = VNRequestTextRecognitionLevelAccurate;
    r.recognitionLanguages = @[ @"zh-Hans", @"en-US" ];
    r.usesLanguageCorrection = NO;
    r.automaticallyDetectsLanguage = NO;
    return r;
}
#ifdef ATRIUM_FIXTURE_RENDER
// Compiled only into the synthetic-fixture renderer, never the production
// helper.
static NSString *fixtureDirectory;
static void saveFixtureRaster(CGImageRef image, NSUInteger page) {
    NSURL *url = [NSURL
        fileURLWithPath:[fixtureDirectory
                            stringByAppendingPathComponent:[NSString
                                                               stringWithFormat:@"page-%lu.png",
                                                                                (unsigned long)
                                                                                    page]]];
    CGImageDestinationRef d =
        CGImageDestinationCreateWithURL((__bridge CFURLRef)url, CFSTR("public.png"), 1, NULL);
    if (d) {
        CGImageDestinationAddImage(d, image, NULL);
        CGImageDestinationFinalize(d);
        CFRelease(d);
    }
}
#endif
static BOOL recognize(CGImageRef image, NSUInteger page) {
#ifdef ATRIUM_FIXTURE_RENDER
    saveFixtureRaster(image, page);
#endif
    VNRecognizeTextRequest *r = request();
    VNImageRequestHandler *handler =
        [[VNImageRequestHandler alloc] initWithCGImage:image
                                           orientation:kCGImagePropertyOrientationUp
                                               options:@{}];
    if (![handler performRequests:@[ r ] error:NULL]) {
        outcome(@"partial", @"page_failed");
        return NO;
    }
    for (VNRecognizedTextObservation *o in r.results) {
        VNRecognizedText *t = [[o topCandidates:1] firstObject];
        CGRect rect = topRect(o.boundingBox, 1, 1);
        if (!appendObservation(page, t.string, @"ocr", rect, CGRectZero, NO, t.confidence) &&
            stopped)
            break;
    }
    return YES;
}
static BOOL dimensions(double w, double h, double maxPixels, double maxEdge) {
    return isfinite(w) && isfinite(h) && w > 0 && h > 0 && floor(w) == w && floor(h) == h &&
           w <= maxEdge && h <= maxEdge && w <= maxPixels / h;
}
static NSString *pinnedCodec(NSString *name, const char *expectedHash) {
    @try {
        char executable[PATH_MAX];
        uint32_t size = sizeof(executable);
        if (_NSGetExecutablePath(executable, &size) != 0)
            return nil;
        NSString *path =
            [[[NSString stringWithUTF8String:executable] stringByDeletingLastPathComponent]
                stringByAppendingPathComponent:[@"lib" stringByAppendingPathComponent:name]];
        struct stat st;
        if (lstat(path.fileSystemRepresentation, &st) || !S_ISREG(st.st_mode) || st.st_size < 1 ||
            st.st_size > 1024 * 1024)
            return nil;
        NSData *library = [NSData dataWithContentsOfFile:path options:0 error:NULL];
        if (!library || library.length != (NSUInteger)st.st_size)
            return nil;
        unsigned char digest[CC_SHA256_DIGEST_LENGTH];
        CC_SHA256(library.bytes, (CC_LONG)library.length, digest);
        char hex[CC_SHA256_DIGEST_LENGTH * 2 + 1];
        for (size_t i = 0; i < sizeof(digest); i++)
            snprintf(hex + i * 2, 3, "%02x", digest[i]);
        if (strcmp(hex, expectedHash))
            return nil;
        return path;
    } @catch (NSException *exception) {
        (void)exception;
        return nil;
    }
}
static BOOL prepareCodecs(void) {
    NSString *jpeg = pinnedCodec(@"atrium-libjpeg-3.1.4.1.dylib", ATRIUM_JPEG_SHA256);
    NSString *webp = pinnedCodec(@"atrium-libwebp-1.6.0.dylib", ATRIUM_WEBP_SHA256);
    // libwebp's sole non-system dependency uses this fixed @loader_path name.
    NSString *yuv = pinnedCodec(@"libsharpyuv.0.dylib", ATRIUM_YUV_SHA256);
    return jpeg && webp && yuv && jpegLoad(jpeg.fileSystemRepresentation) &&
           webpLoad(webp.fileSystemRepresentation);
}
static void imageData(NSData *data) {
    result[@"format"] = @"image";
    NSMutableDictionary *coverage = result[@"coverage"];
    coverage[@"total_pages"] = @1;
    NSDictionary *noCache =
        @{(id)kCGImageSourceShouldCache : @NO,
          (id)kCGImageSourceShouldCacheImmediately : @NO};
    CGImageSourceRef source =
        CGImageSourceCreateWithData((__bridge CFDataRef)data, (__bridge CFDictionaryRef)noCache);
    if (!source) {
        outcome(@"invalid", @"invalid_input");
        return;
    }
    CFStringRef type = CGImageSourceGetType(source);
    size_t count = CGImageSourceGetCount(source);
    if (!type || count == 0) {
        outcome(@"invalid", @"invalid_input");
        CFRelease(source);
        return;
    }
    if (count != 1) {
        outcome(@"unsupported", @"multiframe_image");
        CFRelease(source);
        return;
    }
    NSSet *types = [NSSet setWithArray:@[
        @"public.png", @"public.jpeg", @"public.tiff", @"org.webmproject.webp",
        @"com.compuserve.gif", @"public.heic", @"public.heif", @"com.microsoft.bmp"
    ]];
    if (![types containsObject:(__bridge NSString *)type]) {
        outcome(@"unsupported", @"unsupported_image");
        CFRelease(source);
        return;
    }
    NSDictionary *properties = CFBridgingRelease(
        CGImageSourceCopyPropertiesAtIndex(source, 0, (__bridge CFDictionaryRef)noCache));
    double w = [properties[(id)kCGImagePropertyPixelWidth] doubleValue],
           h = [properties[(id)kCGImagePropertyPixelHeight] doubleValue];
    NSNumber *orientation = properties[(id)kCGImagePropertyOrientation] ?: @1;
    double o = orientation.doubleValue;
    if (!isfinite(w) || !isfinite(h) || w <= 0 || h <= 0 || floor(w) != w || floor(h) != h) {
        outcome(@"invalid", @"invalid_input");
        CFRelease(source);
        return;
    }
    if (!dimensions(w, h, 60000000, 16384)) {
        outcome(@"limit_exceeded", @"image_dimensions");
        CFRelease(source);
        return;
    }
    if (!isfinite(o) || o < 1 || o > 8 || floor(o) != o) {
        outcome(@"invalid", @"invalid_input");
        CFRelease(source);
        return;
    }
    if ([(__bridge NSString *)type isEqual:@"public.png"] &&
        !pngComplete(data.bytes, data.length, (uint32_t)w, (uint32_t)h)) {
        outcome(@"invalid", @"invalid_input");
        CFRelease(source);
        return;
    }
    if ([(__bridge NSString *)type isEqual:@"org.webmproject.webp"]) {
        WebPIntegrity integrity =
            webpFramed(data.bytes, data.length)
                ? webpComplete(data.bytes, data.length, (uint32_t)w, (uint32_t)h)
                : WebPInvalid;
        if (integrity != WebPComplete) {
            switch (integrity) {
            case WebPResourceLimit:
                outcome(@"limit_exceeded", @"webp_resources");
                break;
            case WebPUnsupported:
                outcome(@"unsupported", @"unsupported_image");
                break;
            case WebPUnavailable:
                outcome(@"unsupported", @"parser_unavailable");
                break;
            default:
                outcome(@"invalid", @"invalid_input");
                break;
            }
            CFRelease(source);
            return;
        }
    }
    if ([(__bridge NSString *)type isEqual:@"public.jpeg"]) {
        JPEGIntegrity integrity =
            jpegComplete(data.bytes, data.length, (uint32_t)w, (uint32_t)h, 512L * 1024 * 1024);
        if (integrity != JPEGComplete) {
            switch (integrity) {
            case JPEGResourceLimit:
                outcome(@"limit_exceeded", @"jpeg_resources");
                break;
            case JPEGUnsupported:
                outcome(@"unsupported", @"unsupported_jpeg_precision");
                break;
            case JPEGUnavailable:
                outcome(@"unsupported", @"parser_unavailable");
                break;
            default:
                outcome(@"invalid", @"invalid_input");
                break;
            }
            CFRelease(source);
            return;
        }
    }
    double edge = fmin(4096, floor(sqrt(16000000 * fmax(w, h) / fmin(w, h))));
    NSDictionary *options = @{
        (id)kCGImageSourceShouldCache : @NO,
        (id)kCGImageSourceShouldCacheImmediately : @NO,
        (id)kCGImageSourceCreateThumbnailFromImageAlways : @YES,
        (id)kCGImageSourceCreateThumbnailWithTransform : @YES,
        (id)kCGImageSourceThumbnailMaxPixelSize : @(edge)
    };
    CGImageRef image =
        CGImageSourceCreateThumbnailAtIndex(source, 0, (__bridge CFDictionaryRef)options);
    CFRelease(source);
    if (!image) {
        outcome(@"invalid", @"invalid_input");
        return;
    }
    size_t W = CGImageGetWidth(image), H = CGImageGetHeight(image);
    if (!dimensions(W, H, 16000000, 4096)) {
        outcome(@"limit_exceeded", @"image_dimensions");
        CGImageRelease(image);
        return;
    }
    NSMutableDictionary *page = [@{
        @"page" : @1,
        @"width" : @(W),
        @"height" : @(H),
        @"orientation" : @(o),
        @"rotation" : @0,
        @"effective_crop" : NSNull.null,
        @"transform" : NSNull.null,
        @"ocr_complete" : @NO,
        @"native_complete" : @NO
    } mutableCopy];
    [pages addObject:page];
    BOOL ok = recognize(image, 1);
    CGImageRelease(image);
    page[@"ocr_complete"] = @(ok);
    coverage[@"processed_pages"] = @(ok ? 1 : 0);
}
static void pdfData(NSData *data) {
    result[@"format"] = @"pdf";
    NSMutableDictionary *coverage = result[@"coverage"];
    coverage[@"annotations"] = @"excluded";
    coverage[@"forms"] = @"excluded";
    PDFDocument *doc = [[PDFDocument alloc] initWithData:data];
    if (!doc) {
        outcome(@"invalid", @"invalid_input");
        return;
    }
    result[@"encrypted"] = @(doc.isEncrypted);
    result[@"allows_copying"] = @(doc.allowsCopying);
    if (doc.isLocked) {
        outcome(@"encrypted", @"locked_pdf");
        return;
    }
    NSUInteger count = doc.pageCount;
    coverage[@"total_pages"] = @(count);
    if (count > 100) {
        outcome(@"limit_exceeded", @"page_limit");
        return;
    }
    if (count == 0) {
        outcome(@"invalid", @"invalid_input");
        return;
    }
    coverage[@"total_pages"] = @(count);
    NSUInteger processed = 0;
    for (NSUInteger i = 0; i < count && !stopped; i++) {
        @autoreleasepool {
            @try {
                PDFPage *p = [doc pageAtIndex:i];
                CGPDFPageRef ref = p.pageRef;
                if (!ref) {
                    outcome(@"partial", @"page_failed");
                    continue;
                }
                CGRect crop = CGRectIntersection(CGPDFPageGetBoxRect(ref, kCGPDFCropBox),
                                                 CGPDFPageGetBoxRect(ref, kCGPDFMediaBox));
                if (!finiteBox(crop)) {
                    outcome(@"partial", @"page_failed");
                    continue;
                }
                int rotation = ((CGPDFPageGetRotationAngle(ref) % 360) + 360) % 360;
                if (rotation % 90) {
                    outcome(@"partial", @"page_failed");
                    continue;
                }
                BOOL swap = rotation == 90 || rotation == 270;
                double dw = swap ? crop.size.height : crop.size.width,
                       dh = swap ? crop.size.width : crop.size.height;
                double scale = fmin(2, fmin(4096 / fmax(dw, dh), sqrt(16000000 / (dw * dh))));
                size_t W = (size_t)ceil(dw * scale), H = (size_t)ceil(dh * scale);
                if (!dimensions(W, H, 16000000, 4096)) {
                    scale *= .999;
                    W = (size_t)ceil(dw * scale);
                    H = (size_t)ceil(dh * scale);
                }
                if (!dimensions(W, H, 16000000, 4096)) {
                    outcome(@"partial", @"page_failed");
                    continue;
                }
                CGColorSpaceRef color = CGColorSpaceCreateDeviceRGB();
                CGContextRef ctx =
                    CGBitmapContextCreate(NULL, W, H, 8, W * 4, color,
                                          kCGImageAlphaPremultipliedLast | kCGBitmapByteOrder32Big);
                CGColorSpaceRelease(color);
                if (!ctx) {
                    outcome(@"partial", @"page_failed");
                    continue;
                }
                CGContextSetRGBFillColor(ctx, 1, 1, 1, 1);
                CGContextFillRect(ctx, CGRectMake(0, 0, W, H));
                CGContextSaveGState(ctx);
                CGContextScaleCTM(ctx, (double)W / dw, (double)H / dh);
                CGContextConcatCTM(ctx, CGPDFPageGetDrawingTransform(
                                            ref, kCGPDFCropBox, CGRectMake(0, 0, dw, dh), 0, true));
                CGAffineTransform T = CGContextGetCTM(ctx);
                CGContextClipToRect(ctx, crop);
                CGContextDrawPDFPage(ctx, ref);
                CGContextRestoreGState(ctx);
                CGImageRef image = CGBitmapContextCreateImage(ctx);
                CGContextRelease(ctx);
                if (!image) {
                    outcome(@"partial", @"page_failed");
                    continue;
                }
                NSMutableDictionary *page = [@{
                    @"page" : @(i + 1),
                    @"width" : @(W),
                    @"height" : @(H),
                    @"orientation" : @1,
                    @"rotation" : @(rotation),
                    @"effective_crop" : box(crop),
                    @"transform" : @[ @(T.a), @(T.b), @(T.c), @(T.d), @(T.tx), @(T.ty) ],
                    @"ocr_complete" : @NO,
                    @"native_complete" : @NO
                } mutableCopy];
                [pages addObject:page];
                // Every renderable page reaches Vision, including native-only and
                // footer PDFs.
                BOOL ok = recognize(image, i + 1);
                CGImageRelease(image);
                page[@"ocr_complete"] = @(ok);
                if (ok)
                    processed++;
                if (stopped)
                    continue;
                NSString *pageText = p.string ?: @"";
                if (pageText.length > TextLimit) {
                    stop(@"limit_exceeded", @"text_limit");
                    continue;
                }
                page[@"native_complete"] = @YES;
                if (pageText.length) {
                    PDFSelection *selection = [p selectionForRange:NSMakeRange(0, pageText.length)];
                    for (PDFSelection *line in selection.selectionsByLine) {
                        CGRect raw = [line boundsForPage:p];
                        if (!finiteBox(raw)) {
                            outcome(@"partial", @"page_failed");
                            continue;
                        }
                        CGRect visible = CGRectIntersection(crop, raw);
                        if (CGRectIsNull(visible) || CGRectIsEmpty(visible))
                            continue;
                        BOOL clipped = !CGRectContainsRect(crop, raw);
                        // CGRectApplyAffineTransform bounds all four corners, including
                        // rotation.
                        CGRect rect = topRect(CGRectApplyAffineTransform(visible, T), W, H);
                        if (!appendObservation(i + 1, line.string, @"native", rect, raw, clipped,
                                               0) &&
                            stopped)
                            break;
                    }
                }
                if (p.annotations.count > 0 && !stopped)
                    outcome(@"partial", @"annotations_forms_excluded");
            } @catch (NSException *exception) {
                (void)exception;
                if (!stopped)
                    outcome(@"partial", @"page_failed");
            }
        }
    }
    coverage[@"processed_pages"] = @(processed);
}
int main(int argc, const char *argv[]) {
    @autoreleasepool {
// The only input channel is an inherited regular FD, plus a small fixed config.
#ifdef ATRIUM_FIXTURE_RENDER
        if (argc != 3 || strcmp(argv[1], "--protocol=1"))
            return 2;
        fixtureDirectory = [NSString stringWithUTF8String:argv[2]];
#else
        if (argc != 2 || strcmp(argv[1], "--protocol=1"))
            return 2;
#endif
        char config[65];
        ssize_t n = read(STDIN_FILENO, config, sizeof(config));
        if (n < 0 || n > 64)
            return 2;
        NSData *configData = [NSData dataWithBytes:config length:(NSUInteger)n];
        id cfg = [NSJSONSerialization JSONObjectWithData:configData options:0 error:NULL];
        if (![cfg isKindOfClass:NSDictionary.class] || ![cfg isEqual:@{@"protocol" : @1}])
            return 2;
        struct stat st;
        if (fstat(3, &st) || !S_ISREG(st.st_mode) || st.st_size < 0)
            return 2;
        char osBuild[128] = {0};
        size_t osSize = sizeof(osBuild);
        if (sysctlbyname("kern.osversion", osBuild, &osSize, NULL, 0))
            return 2;
        segments = [NSMutableArray new];
        pages = [NSMutableArray new];
        result = [@{
            @"protocol" : @1,
            @"format" : @"",
            @"status" : @"parsed",
            @"code" : @"",
            @"segments" : segments,
            @"pages" : pages,
            @"counts" :
                [@{@"source_bytes" : @0,
                   @"text_bytes" : @0,
                   @"segments" : @0} mutableCopy],
            @"coverage" : [@{
                @"complete" : @NO,
                @"total_pages" : @0,
                @"processed_pages" : @0,
                @"annotations" : @"not_applicable",
                @"forms" : @"not_applicable"
            } mutableCopy],
            @"encrypted" : @NO,
            @"allows_copying" : @YES,
            @"engine" : @{
                @"helper" : @"atrium-native/1",
                @"build" : @ATRIUM_BUILD,
                @"os_build" : [NSString stringWithUTF8String:osBuild],
                @"vision_revision" : @3,
                @"languages" : @[ @"zh-Hans", @"en-US" ],
                @"recognition" : @"accurate",
                @"language_correction" : @NO
            }
        } mutableCopy];
        @try {
            if (st.st_size > 32 * 1024 * 1024)
                outcome(@"limit_exceeded", @"source_bytes");
            else {
                VNRecognizeTextRequest *r =
                    [VNRecognizeTextRequest.supportedRevisions containsIndex:3] ? request() : nil;
                NSArray *supported = [r supportedRecognitionLanguagesAndReturnError:NULL];
                if (!prepareCodecs() ||
                    ![VNRecognizeTextRequest.supportedRevisions containsIndex:3] ||
                    ![supported containsObject:@"zh-Hans"] || ![supported containsObject:@"en-US"])
                    outcome(@"unsupported", @"parser_unavailable");
                else {
                    NSMutableData *data = [NSMutableData dataWithLength:(NSUInteger)st.st_size];
                    NSUInteger offset = 0;
                    while (offset < data.length) {
                        size_t chunk = MIN((NSUInteger)32768, data.length - offset);
                        ssize_t got =
                            pread(3, (char *)data.mutableBytes + offset, chunk, (off_t)offset);
                        if (got <= 0)
                            return 3;
                        offset += (NSUInteger)got;
                        result[@"counts"][@"source_bytes"] = @(offset);
                    }
                    if (data.length >= 5 && !memcmp(data.bytes, "%PDF-", 5))
                        pdfData(data);
                    else
                        imageData(data);
                }
            }
        } @catch (NSException *exception) {
            (void)exception;
            outcome(@"invalid", @"invalid_input");
        }
        if ([result[@"status"] isEqual:@"parsed"] && segments.count == 0)
            outcome(@"empty", @"no_text_detected");
        NSMutableDictionary *counts = result[@"counts"];
        counts[@"text_bytes"] = @(textBytes);
        counts[@"segments"] = @(segments.count);
        result[@"coverage"][@"complete"] = @(
            (BOOL)([result[@"status"] isEqual:@"parsed"] || [result[@"status"] isEqual:@"empty"]));
        NSData *json = [NSJSONSerialization dataWithJSONObject:result
                                                       options:NSJSONWritingSortedKeys
                                                         error:NULL];
        if (!json || json.length > 8 * 1024 * 1024)
            return 4;
        if (fwrite(json.bytes, 1, json.length, stdout) != json.length)
            return 5;
        return 0;
    }
}
