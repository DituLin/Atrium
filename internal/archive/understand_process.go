package archive

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

const UnderstandingModel = "deepseek/deepseek-v4-flash"

var understandingSlots = make(chan struct{}, 2)

// OpenClawUnderstandingAdapter is trusted host configuration. None of these
// fields can originate from an archive, model output, or stdin. APIKey is passed
// only as the provider's single credential environment variable, never persisted.
type OpenClawUnderstandingAdapter struct {
	NodePath, OpenClawPath, PluginPath, ProviderPath string
	APIKey                                           string
	Timeout                                          time.Duration
}
type WorkerOwner struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}
type WorkerExecution struct {
	Mode  string      `json:"mode"`
	Owner WorkerOwner `json:"owner"`
}
type WorkerResponse struct {
	ConfigurationFingerprint string           `json:"configuration_fingerprint"`
	Usage                    *CompletionUsage `json:"usage,omitempty"`
	ProtocolVersion          int              `json:"protocol_version"`
	InvocationID             string           `json:"invocation_id"`
	InputDigest              string           `json:"input_digest"`
	SchemaVersion            string           `json:"schema_version"`
	ExtractionVersion        string           `json:"extraction_version"`
	ChunkVersion             string           `json:"chunk_version"`
	Provider                 string           `json:"provider"`
	RequestedModel           string           `json:"requested_model"`
	Model                    string           `json:"model"`
	ResponseModel            string           `json:"response_model"`
	StopReason               string           `json:"stop_reason"`
	Execution                WorkerExecution  `json:"execution"`
	Result                   Extraction       `json:"result"`
}

func validateWorkerResponse(c UnderstandingChunk, r WorkerResponse) error {
	if r.ConfigurationFingerprint != UnderstandingConfigurationFingerprint() {
		return understandingError("understand_configuration")
	}
	if r.ProtocolVersion != WorkerProtocol || r.InvocationID != c.InvocationID || r.InputDigest != c.InputDigest || r.SchemaVersion != UnderstandingSchema || r.ExtractionVersion != UnderstandingVersion || r.ChunkVersion != ChunkVersion {
		return understandingError("understand_protocol")
	}
	if r.Provider != "deepseek" || r.RequestedModel != UnderstandingModel || r.Model != "deepseek-v4-flash" || (r.ResponseModel != "deepseek-flash" && r.ResponseModel != "deepseek-v4-flash") || r.Execution.Mode != "direct-provider" || r.Execution.Owner.Kind != "provider" || r.Execution.Owner.ID != "deepseek" {
		return understandingError("understand_route")
	}
	if !validUsage(r.Usage) {
		return understandingError("understand_protocol")
	}
	if r.StopReason != "stop" {
		return understandingError("understand_stop")
	}
	return nil
}

type understandingSink struct {
	mu       sync.Mutex
	b        bytes.Buffer
	limit    int
	overflow bool
	discard  bool
	cancel   context.CancelFunc
}

func (w *understandingSink) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(b) > w.limit-w.b.Len() {
		w.overflow = true
		w.cancel()
		return len(b), nil
	}
	if !w.discard {
		w.b.Write(b)
	} else if len(b) > 0 {
		w.overflow = true
		w.cancel()
	}
	return len(b), nil
}

// UnderstandChunk admits one bounded process per chunk. Retries receive a fresh
// invocation nonce and wire digest while the immutable plan identity is stable.
// Cancellation kills the process group and Run joins Wait exactly once.
func UnderstandChunk(ctx context.Context, p UnderstandingPlan, c UnderstandingChunk, a OpenClawUnderstandingAdapter) (ChunkUnderstanding, error) {
	bad := func(code string) (ChunkUnderstanding, error) { return ChunkUnderstanding{}, understandingError(code) }
	if !validPlan(p) || c.Index < 0 || c.Index >= len(p.Chunks) || digestJSON(c) != digestJSON(p.Chunks[c.Index]) {
		return bad("understand_identity")
	}
	response, err := a.complete(ctx, c)
	if err != nil {
		return ChunkUnderstanding{}, err
	}
	return validatedResponse(p, c, response)
}
func (a OpenClawUnderstandingAdapter) complete(ctx context.Context, c UnderstandingChunk) (WorkerResponse, error) {
	bad := func(code string) (WorkerResponse, error) { return WorkerResponse{}, understandingError(code) }
	timeout := a.Timeout
	if timeout <= 0 || timeout > 120*time.Second {
		timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	select {
	case understandingSlots <- struct{}{}:
		defer func() { <-understandingSlots }()
	case <-ctx.Done():
		return bad("understand_canceled")
	}
	for _, path := range []string{a.NodePath, a.OpenClawPath, a.PluginPath, a.ProviderPath} {
		if !filepath.IsAbs(path) {
			return bad("understand_configuration")
		}
		if _, err := os.Stat(path); err != nil {
			return bad("understand_configuration")
		}
	}
	root, err := os.MkdirTemp("", "atrium-understand-")
	if err != nil {
		return bad("understand_unavailable")
	}
	defer os.RemoveAll(root)
	for _, name := range []string{"home", "state", "workspace", "cwd"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			return bad("understand_unavailable")
		}
	}
	config := map[string]any{
		"agents":  map[string]any{"defaults": map[string]any{"workspace": filepath.Join(root, "workspace"), "model": map[string]any{"primary": UnderstandingModel}}},
		"env":     map[string]any{"shellEnv": map[string]any{"enabled": false}},
		"plugins": map[string]any{"enabled": true, "allow": []string{"atrium-archive-worker", "deepseek"}, "load": map[string]any{"paths": []string{a.PluginPath, a.ProviderPath}}, "entries": map[string]any{"atrium-archive-worker": map[string]any{"enabled": true, "hooks": map[string]any{"allowPromptInjection": false}, "llm": map[string]any{"allowModelOverride": true, "allowedModels": []string{UnderstandingModel}, "allowedCompletionModels": []string{UnderstandingModel}}}, "deepseek": map[string]any{"enabled": true}}},
		"logging": map[string]any{"level": "silent", "consoleLevel": "silent", "file": filepath.Join(root, "worker.log")}, "tools": map[string]any{"deny": []string{"*"}},
	}
	b, _ := json.Marshal(config)
	configPath := filepath.Join(root, "config.json")
	if os.WriteFile(configPath, b, 0600) != nil {
		return bad("understand_unavailable")
	}
	request := c
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return bad("understand_unavailable")
	}
	request.InvocationID = hex.EncodeToString(nonce)
	request.InputDigest = request.inputDigest()
	input, _ := json.Marshal(request)
	if len(input) > MaxChunkInput {
		return bad("understand_input_limit")
	}
	cmd := exec.CommandContext(ctx, a.NodePath, a.OpenClawPath, "--no-color", "--log-level", "silent", "atrium-archive-worker", "--json")
	cmd.Dir = filepath.Join(root, "cwd")
	cmd.Env = []string{"HOME=" + filepath.Join(root, "home"), "PATH=/opt/homebrew/bin:/usr/bin:/bin", "LANG=en_US.UTF-8", "LC_ALL=en_US.UTF-8", "OPENCLAW_HOME=" + filepath.Join(root, "home"), "OPENCLAW_STATE_DIR=" + filepath.Join(root, "state"), "OPENCLAW_CONFIG_PATH=" + configPath, "OPENCLAW_DISABLE_BUNDLED_PLUGINS=1", "OPENCLAW_NO_RESPAWN=1"}
	if a.APIKey != "" {
		cmd.Env = append(cmd.Env, "DEEPSEEK_API_KEY="+a.APIKey)
	}
	stdout := &understandingSink{limit: MaxChunkOutput, cancel: cancel}
	stderr := &understandingSink{limit: 4096, discard: true, cancel: cancel}
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.WaitDelay = 250 * time.Millisecond
	configureNativeProcess(cmd)
	err = cmd.Run()
	finishNativeProcess(cmd)
	if stdout.overflow || stderr.overflow {
		return bad("understand_output_limit")
	}
	if ctx.Err() != nil {
		return bad("understand_canceled")
	}
	if err != nil {
		return bad("understand_process")
	}
	var failure struct {
		Code string `json:"error_code"`
	}
	if strictJSON(stdout.b.Bytes(), &failure) == nil {
		switch failure.Code {
		case "worker_schema_duplicate", "worker_schema_type", "worker_schema_required", "worker_schema_unknown", "worker_schema_review_flag", "worker_schema_confidence", "worker_schema_unit_field", "worker_schema_currency_field", "worker_schema_value_field", "worker_schema_evidence_field", "worker_schema_limit", "worker_schema_enum", "worker_input", "worker_configuration", "worker_completion", "worker_route", "worker_schema", "worker_output_limit", "worker_failed":
			return bad("understand_" + failure.Code)
		}
	}
	var response WorkerResponse
	if err := strictJSON(stdout.b.Bytes(), &response); err != nil {
		return bad("understand_protocol")
	}
	if err := validateWorkerResponse(request, response); err != nil {
		return WorkerResponse{}, err
	}
	return response, nil
}
