package io.atrium.tv;

import java.util.regex.Pattern;

/**
 * The only media the native player may open: one same-origin video stream,
 * addressed by an opaque id. Everything else from the page is refused.
 */
final class VideoRequest {
    private static final Pattern PATH = Pattern.compile("^/api/v1/media/videos/[A-Za-z0-9_-]{1,64}/content$");
    private VideoRequest() {}

    /** Absolute URL for a validated path on the configured origin, or null. */
    static String resolve(String origin, String path) {
        if (origin == null || origin.isEmpty() || path == null || !PATH.matcher(path).matches()) return null;
        return origin + path;
    }

    /** Page-supplied labels are shown as plain text; keep them short. */
    static String label(String text, String fallback) {
        if (text == null) return fallback;
        String trimmed = text.trim();
        if (trimmed.isEmpty()) return fallback;
        return trimmed.length() > 60 ? trimmed.substring(0, 60) : trimmed;
    }
}
