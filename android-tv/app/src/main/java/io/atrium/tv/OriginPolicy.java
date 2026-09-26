package io.atrium.tv;

import java.net.URI;
import java.util.Locale;

/** Limits the authenticated WebView to an explicitly configured HTTPS origin. */
public final class OriginPolicy {
    private OriginPolicy() {}
    public static String normalize(String input) {
        try {
            URI u = new URI(input.trim());
            if (!"https".equalsIgnoreCase(u.getScheme()) || u.getHost() == null ||
                u.getUserInfo() != null || u.getRawQuery() != null || u.getRawFragment() != null ||
                !(u.getRawPath().isEmpty() || "/".equals(u.getRawPath())) ||
                u.getPort() == 0 || u.getPort() > 65535) throw new IllegalArgumentException();
            return new URI("https", null, u.getHost().toLowerCase(Locale.ROOT),
                u.getPort() == 443 ? -1 : u.getPort(), null, null, null).toString();
        } catch (Exception e) { throw new IllegalArgumentException("Enter an HTTPS server origin, e.g. https://macmini.local:8443"); }
    }
    public static boolean allows(String origin, String url) {
        try {
            URI u = new URI(url);
            if (u.getUserInfo() != null) return false;
            return normalize(origin).equals(normalize(new URI(u.getScheme(), null, u.getHost(), u.getPort(), null, null, null).toString()));
        } catch (Exception e) { return false; }
    }
}
