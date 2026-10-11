package archive

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

func TestRetainedModelPublicationSurvivesPromptUpgrade(t *testing.T) {
	raw, e := os.ReadFile("testdata/t15-h02-retained.json")
	require.NoError(t, e)
	var b struct {
		Parsed        ParseResult
		Plan          UnderstandingPlan
		Understanding Understanding
	}
	require.NoError(t, json.Unmarshal(raw, &b))
	out, e := CompilePublication("synthetic", "doc", b.Parsed, b.Plan, b.Understanding)
	require.NoError(t, e)
	require.NotEmpty(t, out.Events)
	r := b.Understanding.Chunks[0]
	r.Receipt.ConfigurationFingerprint = "unknown-policy"
	r.Digest = r.digest()
	_, e = MergeChunks(b.Plan, []ChunkUnderstanding{r})
	require.Error(t, e)
}
