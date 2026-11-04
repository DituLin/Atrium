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
