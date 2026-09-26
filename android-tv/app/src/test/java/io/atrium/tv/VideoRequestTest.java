package io.atrium.tv;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNull;

import org.junit.Test;

public class VideoRequestTest {
    private static final String ORIGIN = "https://192.168.1.10:8443";

    @Test public void resolvesOnlySameOriginVideoStreams() {
        assertEquals(ORIGIN + "/api/v1/media/videos/01ABC_x-9/content", VideoRequest.resolve(ORIGIN, "/api/v1/media/videos/01ABC_x-9/content"));
    }

    @Test public void refusesAnythingElse() {
        assertNull(VideoRequest.resolve(ORIGIN, "https://evil.example/api/v1/media/videos/a/content"));
        assertNull(VideoRequest.resolve(ORIGIN, "/api/v1/media/videos/../../etc/content"));
        assertNull(VideoRequest.resolve(ORIGIN, "/api/v1/media/videos/a/cover"));
        assertNull(VideoRequest.resolve(ORIGIN, "/api/v1/media/photos/a"));
        assertNull(VideoRequest.resolve(ORIGIN, "/api/v1/media/videos/a/content?x=1"));
        assertNull(VideoRequest.resolve(ORIGIN, "//evil.example/api/v1/media/videos/a/content"));
        assertNull(VideoRequest.resolve("", "/api/v1/media/videos/a/content"));
        assertNull(VideoRequest.resolve(ORIGIN, null));
    }

    @Test public void labelsAreTrimmedAndBounded() {
        assertEquals("家庭影像", VideoRequest.label("  ", "家庭影像"));
        assertEquals("影像 01", VideoRequest.label(" 影像 01 ", "x"));
        assertEquals(60, VideoRequest.label(new String(new char[80]).replace('\0', 'a'), "x").length());
    }
}
