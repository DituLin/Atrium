package events_test

import (
	"github.com/DituLin/Atrium/internal/app/events"
	"github.com/DituLin/Atrium/internal/domain"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestIoTTopicCoalescesInStableOrder(t *testing.T) {
	require.Equal(t, []domain.Topic{domain.TopicHome, domain.TopicArchives, domain.TopicIoT}, events.NormalizeTopics([]domain.Topic{domain.TopicIoT, domain.TopicArchives, domain.TopicHome, domain.TopicIoT}))
}

func TestIoTClearSurvivesMergingAndSlowSubscribers(t *testing.T) {
	bus := events.NewBus()
	sub := bus.Subscribe("clear", 1)
	defer sub.Close()
	bus.Publish(domain.TopicIoT, domain.TopicIoTClear)
	bus.Publish(domain.TopicIoT)
	event := <-sub.C()
	require.Equal(t, []domain.Topic{domain.TopicIoT, domain.TopicIoTClear}, event.Topics)
	require.Equal(t, event.Topics, events.MergeTopics(event.Topics, []domain.Topic{domain.TopicIoT}))
}
