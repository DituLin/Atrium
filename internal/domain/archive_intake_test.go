package domain

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestArchiveSourceIdentityCanonicalAndScoped(t *testing.T) {
	s := ArchiveSourceIdentity{Kind: ArchiveInputText, Channel: "wechat", Account: "account", Conversation: "chat", Sender: "sender", ProviderMessageID: "message", SlotIndex: 0}
	k, err := s.Key("home", "principal")
	require.NoError(t, err)
	require.Len(t, k, 64)
	same, err := s.Key("home", "principal")
	require.NoError(t, err)
	require.Equal(t, k, same)
	for _, changed := range []struct {
		h, p string
		s    ArchiveSourceIdentity
	}{{"other", "principal", s}, {"home", "other", s}, {"home", "principal", func() ArchiveSourceIdentity { x := s; x.Kind = ArchiveInputFile; return x }()}} {
		other, err := changed.s.Key(changed.h, changed.p)
		require.NoError(t, err)
		require.NotEqual(t, k, other)
	}
}
func TestArchiveTaskProjectionIsAllowlisted(t *testing.T) {
	p := ProjectArchiveTask(&ArchiveTask{ID: "task", Status: ArchiveTaskRunning, Stage: "/private/server", InputJSON: json.RawMessage(`{"path":"/input"}`), StateJSON: json.RawMessage(`{"secret":"/state"}`), ResultJSON: json.RawMessage(`{"path":"/result","parse_status":"/secret","fact_count":3}`), ClarificationJSON: json.RawMessage(`{"question":"/clarify","code":"/bad"}`), LastErrorCode: "/error", LeaseToken: "lease"})
	b, err := json.Marshal(p)
	require.NoError(t, err)
	for _, secret := range []string{"/private", "/input", "/state", "/result", "/secret", "/clarify", "/bad", "/error", "lease"} {
		require.NotContains(t, string(b), secret)
	}
	require.Equal(t, "processing", p.Stage)
}
func TestArchiveProjectionRetainsEveryParserStatus(t *testing.T) {
	for _, status := range []string{"partial", "empty", "encrypted", "invalid", "limit_exceeded"} {
		p := ProjectArchiveTask(&ArchiveTask{ResultJSON: json.RawMessage(`{"parse_status":"` + status + `"}`)})
		if p.Result == nil || p.Result.ParseStatus != status {
			t.Fatalf("lost %s", status)
		}
	}
}
