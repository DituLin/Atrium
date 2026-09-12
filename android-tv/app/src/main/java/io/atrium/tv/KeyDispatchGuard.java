package io.atrium.tv;

/** Lifecycle-scoped, single-use result of a native-to-web key dispatch. */
final class KeyDispatchGuard {
    private long epoch;
    static final class Ticket {
        final long epoch;
        boolean consumed;
        Ticket(long epoch) { this.epoch = epoch; }
    }
    Ticket begin() { return new Ticket(epoch); }
    void invalidate() { epoch++; }
    boolean complete(Ticket ticket, boolean unhandled, boolean eligible) {
        if (ticket.consumed) return false;
        ticket.consumed = true;
        return ticket.epoch == epoch && unhandled && eligible;
    }
    static boolean shouldSend(String key, int repeatCount) {
        return repeatCount == 0 || !("Enter".equals(key) || "Escape".equals(key));
    }
}
