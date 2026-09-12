package io.atrium.tv;
import org.junit.Test;
import static org.junit.Assert.*;
public class KeyDispatchGuardTest {
    @Test public void handledBackNeverOpensMenuAndUnhandledBackOpensOnce() {
        KeyDispatchGuard guard = new KeyDispatchGuard();
        assertFalse(guard.complete(guard.begin(), false, true));
        KeyDispatchGuard.Ticket ticket = guard.begin();
        assertTrue(guard.complete(ticket, true, true));
        assertFalse(guard.complete(ticket, true, true));
    }
    @Test public void oldPageBackgroundAndOverlayCallbacksCannotOpenMenu() {
        KeyDispatchGuard guard = new KeyDispatchGuard();
        KeyDispatchGuard.Ticket old = guard.begin();
        guard.invalidate();
        assertFalse(guard.complete(old, true, true));
        KeyDispatchGuard.Ticket hidden = guard.begin();
        assertFalse(guard.complete(hidden, true, false));
        assertFalse(guard.complete(hidden, true, true));
    }
    @Test public void directionsRepeatButActivationAndBackDoNot() {
        assertTrue(KeyDispatchGuard.shouldSend("ArrowLeft", 4));
        assertTrue(KeyDispatchGuard.shouldSend("Enter", 0));
        assertFalse(KeyDispatchGuard.shouldSend("Enter", 1));
        assertFalse(KeyDispatchGuard.shouldSend("Escape", 2));
    }
}
