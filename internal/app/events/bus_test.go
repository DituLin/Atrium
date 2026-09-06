package events_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DituLin/Atrium/internal/app/events"
	"github.com/DituLin/Atrium/internal/domain"
)

func TestBusFansOutAndNormalizes(t *testing.T) {
	bus := events.NewBus()
	sub := bus.Subscribe("test", 4)
	defer sub.Close()

	bus.Publish(domain.TopicNAS, domain.TopicHome, domain.TopicHome, domain.Topic("bogus"))

	select {
	case ev := <-sub.C():
		assert.Equal(t, []domain.Topic{domain.TopicHome, domain.TopicNAS}, ev.Topics)
		assert.Equal(t, int64(1), ev.Version)
	case <-time.After(time.Second):
		t.Fatal("no event delivered")
	}
}

func TestPublishWithoutValidTopicsIsNoop(t *testing.T) {
	bus := events.NewBus()
	sub := bus.Subscribe("test", 2)
	defer sub.Close()
	bus.Publish()
	bus.Publish(domain.Topic("nope"))
	assert.Equal(t, int64(0), bus.Version())
	select {
	case ev := <-sub.C():
		t.Fatalf("unexpected event %v", ev)
	default:
	}
}

func TestSlowSubscriberMergesInsteadOfBlocking(t *testing.T) {
	bus := events.NewBus()
	sub := bus.Subscribe("slow", 1)
	defer sub.Close()

	bus.Publish(domain.TopicPhotos)
	bus.Publish(domain.TopicNAS)
	bus.Publish(domain.TopicHome)

	ev := <-sub.C()
	assert.Equal(t, []domain.Topic{domain.TopicHome, domain.TopicPhotos, domain.TopicNAS}, ev.Topics)
	assert.Equal(t, int64(3), ev.Version)
	assert.Positive(t, bus.Dropped())
}

func TestCoalesceMergesBurstIntoOneEmission(t *testing.T) {
	bus := events.NewBus()
	sub := bus.Subscribe("hub", 64)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	got := make(chan events.Event, 8)
	go events.Coalesce(ctx, sub, 20*time.Millisecond, func(ev events.Event) { got <- ev })

	for i := 0; i < 100; i++ {
		bus.Publish(domain.TopicPhotos)
	}
	bus.Publish(domain.TopicNAS)

	select {
	case ev := <-got:
		assert.Equal(t, []domain.Topic{domain.TopicPhotos, domain.TopicNAS}, ev.Topics)
		assert.Equal(t, int64(101), ev.Version)
	case <-time.After(2 * time.Second):
		t.Fatal("coalescer emitted nothing")
	}
	select {
	case ev := <-got:
		t.Fatalf("expected a single coalesced emission, got another: %v", ev)
	case <-time.After(60 * time.Millisecond):
	}
	sub.Close()
}

func TestCoalesceStopsWhenSubscriptionCloses(t *testing.T) {
	bus := events.NewBus()
	sub := bus.Subscribe("hub", 4)
	done := make(chan struct{})
	go func() {
		events.Coalesce(context.Background(), sub, 10*time.Millisecond, func(events.Event) {})
		close(done)
	}()
	bus.Publish(domain.TopicHome)
	time.Sleep(30 * time.Millisecond)
	sub.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("coalescer did not stop")
	}
	require.Equal(t, int64(1), bus.Version())
}

func TestMergeTopicsIsStable(t *testing.T) {
	merged := events.MergeTopics(
		[]domain.Topic{domain.TopicScreen, domain.TopicPhotos},
		[]domain.Topic{domain.TopicHome, domain.TopicPhotos},
	)
	assert.Equal(t, []domain.Topic{domain.TopicHome, domain.TopicPhotos, domain.TopicScreen}, merged)
}
