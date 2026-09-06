package events

import (
	"context"
	"sort"
	"time"

	"github.com/DituLin/Atrium/internal/domain"
)

// CoalesceWindow is the delivery window from design §9: notifications raised
// within it are merged into a single message per session.
const CoalesceWindow = 250 * time.Millisecond

// topicOrder fixes the emitted order so payloads are byte-stable.
var topicOrder = map[domain.Topic]int{
	domain.TopicHome:   0,
	domain.TopicPhotos: 1,
	domain.TopicNAS:    2,
	domain.TopicScreen: 3,
}

// NormalizeTopics drops unknown and duplicate topics and sorts the rest.
func NormalizeTopics(topics []domain.Topic) []domain.Topic {
	seen := make(map[domain.Topic]struct{}, len(topics))
	out := make([]domain.Topic, 0, len(topics))
	for _, t := range topics {
		if !t.Valid() {
			continue
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return topicOrder[out[i]] < topicOrder[out[j]] })
	return out
}

// MergeTopics returns the union of two topic sets.
func MergeTopics(a, b []domain.Topic) []domain.Topic {
	return NormalizeTopics(append(append([]domain.Topic{}, a...), b...))
}

// Coalesce reads from sub and calls emit at most once per window with the
// union of the topics observed in it. It returns when ctx is done or the
// subscription is closed. The first event starts a window; every event inside
// it is merged, so a burst of hundreds of publications yields one call.
func Coalesce(ctx context.Context, sub *Subscription, window time.Duration, emit func(Event)) {
	if window <= 0 {
		window = CoalesceWindow
	}
	timer := time.NewTimer(window)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()

	var (
		pending Event
		open    bool
	)
	flush := func() {
		if !open {
			return
		}
		open = false
		emit(pending)
		pending = Event{}
	}
	for {
		select {
		case <-ctx.Done():
			flush()
			return
		case ev, ok := <-sub.C():
			if !ok {
				flush()
				return
			}
			if !open {
				open = true
				pending = ev
				timer.Reset(window)
				continue
			}
			pending.Topics = MergeTopics(pending.Topics, ev.Topics)
			if ev.Version > pending.Version {
				pending.Version = ev.Version
			}
		case <-timer.C:
			flush()
		}
	}
}
