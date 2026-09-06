// Package events is the in-process change notification bus. Producers
// (indexer, media pipeline, health prober) publish topics; consumers such as
// the V0.3 WebSocket hub subscribe and forward coalesced notifications to
// clients (technical design §9, task B-214).
package events

import (
	"sync"
	"sync/atomic"

	"github.com/DituLin/Atritum/internal/domain"
)

// Event is one change notification. Topics is always sorted and deduplicated
// so consumers can compare and merge sets cheaply.
type Event struct {
	Topics  []domain.Topic `json:"topics"`
	Version int64          `json:"version"`
}

// Subscription receives events until it is closed. A slow consumer never
// blocks a publisher: when the buffer is full the oldest pending event is
// merged into the newest one, which is safe because clients re-fetch on any
// notification.
type Subscription struct {
	Name string

	bus  *Bus
	ch   chan Event
	mu   sync.Mutex
	done bool
}

// C returns the delivery channel.
func (s *Subscription) C() <-chan Event { return s.ch }

// Close unsubscribes and releases the channel.
func (s *Subscription) Close() {
	s.mu.Lock()
	if s.done {
		s.mu.Unlock()
		return
	}
	s.done = true
	s.mu.Unlock()
	s.bus.remove(s)
}

// Bus fans events out to every subscriber.
type Bus struct {
	mu      sync.RWMutex
	subs    []*Subscription
	version atomic.Int64
	// dropped counts events merged because a subscriber was behind.
	dropped atomic.Int64
}

// NewBus creates an empty bus.
func NewBus() *Bus { return &Bus{} }

// Version returns the monotonically increasing publication counter. Clients
// use it to tell a genuinely new notification from a duplicate.
func (b *Bus) Version() int64 { return b.version.Load() }

// Dropped returns how many events were merged into a later one because a
// subscriber's buffer was full.
func (b *Bus) Dropped() int64 { return b.dropped.Load() }

// Subscribe registers a consumer with a bounded buffer.
func (b *Bus) Subscribe(name string, buffer int) *Subscription {
	if buffer <= 0 {
		buffer = 16
	}
	s := &Subscription{Name: name, bus: b, ch: make(chan Event, buffer)}
	b.mu.Lock()
	b.subs = append(b.subs, s)
	b.mu.Unlock()
	return s
}

// Publish notifies every subscriber that the given topics changed. Unknown or
// duplicate topics are ignored; publishing nothing is a no-op.
func (b *Bus) Publish(topics ...domain.Topic) {
	set := NormalizeTopics(topics)
	if len(set) == 0 {
		return
	}
	ev := Event{Topics: set, Version: b.version.Add(1)}
	b.mu.RLock()
	subs := make([]*Subscription, len(b.subs))
	copy(subs, b.subs)
	b.mu.RUnlock()
	for _, s := range subs {
		b.deliver(s, ev)
	}
}

// deliver never blocks. When the buffer is full the oldest event is dropped
// and its topics are merged into the new one so nothing is lost semantically.
func (b *Bus) deliver(s *Subscription, ev Event) {
	for {
		select {
		case s.ch <- ev:
			return
		default:
		}
		select {
		case old := <-s.ch:
			b.dropped.Add(1)
			ev.Topics = MergeTopics(old.Topics, ev.Topics)
		default:
		}
	}
}

func (b *Bus) remove(s *Subscription) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, cur := range b.subs {
		if cur == s {
			b.subs = append(b.subs[:i], b.subs[i+1:]...)
			break
		}
	}
	close(s.ch)
}
