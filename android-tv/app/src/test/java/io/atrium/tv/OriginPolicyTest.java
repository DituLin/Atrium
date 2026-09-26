package io.atrium.tv;
import org.junit.Test;
import static org.junit.Assert.*;
public class OriginPolicyTest {
 @Test public void normalizesHttpsOrigin() { assertEquals("https://home.local:8443", OriginPolicy.normalize(" https://home.local:8443/ ")); }
 @Test public void blocksUnsafeOrAmbiguousInput() {
  for(String s:new String[]{"http://home.local", "file:///etc", "https://a@home.local", "https://home.local/path", "https://home.local?q=1", "https://home.local#x", "https://home.local:0", "https://home.local:99999"}) {
   try { OriginPolicy.normalize(s); fail(s); } catch(IllegalArgumentException expected) {}
  }
 }
 @Test public void restrictsNavigation() {
  assertTrue(OriginPolicy.allows("https://home.local:8443", "https://home.local:8443/api/v1/home"));
  assertFalse(OriginPolicy.allows("https://home.local:8443", "https://home.local.evil:8443/"));
  assertFalse(OriginPolicy.allows("https://home.local:8443", "http://home.local:8443/"));
  assertFalse(OriginPolicy.allows("https://home.local:8443", "https://user@home.local:8443/"));
  assertTrue(OriginPolicy.allows("https://home.local", "https://home.local:443/"));
 }
}
