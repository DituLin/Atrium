package archive

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"math"
)

//go:embed understanding/prompt-v2.txt
var understandingPrompt string

//go:embed understanding/extraction-v2.schema.json
var understandingSchemaText string

// The fingerprint records the trusted extraction policy without paths or auth.
func UnderstandingConfigurationFingerprint() string {
	return digestJSON(struct {
		Model, Prompt, Schema, Route string
		MaxTokens                    int
	}{UnderstandingModel, understandingPrompt, understandingSchemaText, "deepseek/direct-provider/aliases:deepseek-v4-flash,deepseek-flash/zero-tools/reasoning:low/temperature:0/response-format:json_object/evidence-only:1", 12000})
}

type CompletionUsage struct {
	InputTokens      *int64   `json:"input_tokens,omitempty"`
	OutputTokens     *int64   `json:"output_tokens,omitempty"`
	TotalTokens      *int64   `json:"total_tokens,omitempty"`
	CacheReadTokens  *int64   `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens *int64   `json:"cache_write_tokens,omitempty"`
	CostUSD          *float64 `json:"cost_usd,omitempty"`
}
type CompletionReceipt struct {
	InvocationID             string           `json:"invocation_id"`
	InputDigest              string           `json:"input_digest"`
	Provider                 string           `json:"provider"`
	RequestedModel           string           `json:"requested_model"`
	Model                    string           `json:"model"`
	ResponseModel            string           `json:"response_model"`
	StopReason               string           `json:"stop_reason"`
	Execution                WorkerExecution  `json:"execution"`
	Usage                    *CompletionUsage `json:"usage,omitempty"`
	ConfigurationFingerprint string           `json:"configuration_fingerprint"`
}

func validUsage(u *CompletionUsage) bool {
	if u == nil {
		return true
	}
	for _, n := range []*int64{u.InputTokens, u.OutputTokens, u.TotalTokens, u.CacheReadTokens, u.CacheWriteTokens} {
		if n != nil && (*n < 0 || *n > 1_000_000_000_000) {
			return false
		}
	}
	return u.CostUSD == nil || !math.IsNaN(*u.CostUSD) && !math.IsInf(*u.CostUSD, 0) && *u.CostUSD >= 0 && *u.CostUSD <= 1_000_000
}
func validReceipt(r *CompletionReceipt) bool {
	if r == nil {
		return true
	}
	b, err := hex.DecodeString(r.InputDigest)
	return r.InvocationID != "" && len(r.InvocationID) <= 160 && err == nil && len(b) == sha256.Size && r.ConfigurationFingerprint == UnderstandingConfigurationFingerprint() && r.Provider == "deepseek" && r.RequestedModel == UnderstandingModel && r.Model == "deepseek-v4-flash" && (r.ResponseModel == "deepseek-flash" || r.ResponseModel == "deepseek-v4-flash") && r.StopReason == "stop" && r.Execution.Mode == "direct-provider" && r.Execution.Owner.Kind == "provider" && r.Execution.Owner.ID == "deepseek" && validUsage(r.Usage)
}
func (r WorkerResponse) receipt() *CompletionReceipt {
	return &CompletionReceipt{InvocationID: r.InvocationID, InputDigest: r.InputDigest, Provider: r.Provider, RequestedModel: r.RequestedModel, Model: r.Model, ResponseModel: r.ResponseModel, StopReason: r.StopReason, Execution: r.Execution, Usage: r.Usage, ConfigurationFingerprint: r.ConfigurationFingerprint}
}
func validatedResponse(p UnderstandingPlan, c UnderstandingChunk, response WorkerResponse) (ChunkUnderstanding, error) {
	data, _ := json.Marshal(response.Result)
	r, err := ValidateChunk(p, c, data)
	if err != nil {
		return ChunkUnderstanding{}, err
	}
	r.Receipt = response.receipt()
	if !validReceiptForChunk(r.Receipt, c) {
		return ChunkUnderstanding{}, understandingError("understand_route")
	}
	r.Digest = r.digest()
	return r, nil
}

func completionInputBytes(c UnderstandingChunk) int {
	var schema bytes.Buffer
	_ = json.Compact(&schema, []byte(understandingSchemaText))
	data, _ := json.Marshal(struct {
		Owned   []EvidenceView `json:"owned"`
		Context []EvidenceView `json:"context"`
	}{c.Owned, c.Context})
	request := struct {
		Model          string              `json:"model"`
		SystemPrompt   string              `json:"systemPrompt"`
		Messages       []map[string]string `json:"messages"`
		Purpose        string              `json:"purpose"`
		MaxTokens      int                 `json:"maxTokens"`
		Reasoning      string              `json:"reasoning"`
		Temperature    int                 `json:"temperature"`
		ResponseFormat map[string]string   `json:"responseFormat"`
	}{UnderstandingModel, understandingPrompt + "\nJSON schema:\n" + schema.String(), []map[string]string{{"role": "user", "content": string(data)}}, "atrium.archive-understand.v1", 12000, "low", 0, map[string]string{"type": "json_object"}}
	b, _ := json.Marshal(request)
	return len(b)
}

func validReceiptForChunk(r *CompletionReceipt, c UnderstandingChunk) bool {
	if !validReceipt(r) {
		return false
	}
	if r == nil {
		return true
	}
	c.InvocationID = r.InvocationID
	return c.inputDigest() == r.InputDigest
}
