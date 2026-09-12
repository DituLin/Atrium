package brain

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/DituLin/Atrium/internal/integration"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ActionReport is a conservative, deterministic receipt for a screen action.
// unconfirmed is a local lack of evidence, distinct from Core's terminal unknown.
type ActionReport struct {
	Status      string
	ScreenID    string
	CommandID   string
	OperationID string
	ObservedAt  time.Time
	TextZH      string
}

type actionEvidence struct {
	ObservedAt time.Time
	Command    *integration.Command
	ErrorCode  string
}

// decodeActionEvidence is shared by durable observation and user-facing reports.
// In particular, an error envelope is never execution confirmation.
func decodeActionEvidence(record ActionRecord, result *mcp.CallToolResult) (actionEvidence, error) {
	invalid := func() (actionEvidence, error) { return actionEvidence{}, ErrInvalidOutcome }
	if result == nil || result.StructuredContent == nil {
		return invalid()
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil || len(raw) > 32*1024 {
		return invalid()
	}
	var envelope struct {
		Schema       string              `json:"schema_version"`
		Observed     time.Time           `json:"observed_at"`
		Availability string              `json:"availability"`
		Error        json.RawMessage     `json:"error"`
		Data         integration.Command `json:"data"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.Schema != "1" || envelope.Observed.IsZero() {
		return invalid()
	}
	if len(envelope.Error) != 0 {
		var detail struct {
			Code string `json:"code"`
		}
		if !result.IsError || json.Unmarshal(envelope.Error, &detail) != nil || detail.Code == "" || envelope.Availability != "unknown" || (envelope.Data.Status != "" && envelope.Data.Status != "unknown") {
			return invalid()
		}
		return actionEvidence{ObservedAt: envelope.Observed, ErrorCode: detail.Code}, nil
	}
	if envelope.Availability != "available" {
		return invalid()
	}
	d := envelope.Data
	if !identifier.MatchString(d.ID) || d.ScreenID != record.Action.ScreenID {
		return invalid()
	}
	if record.CommandID != "" && record.CommandID != d.ID {
		return actionEvidence{}, ErrCommandConflict
	}
	expectedKind := map[string]string{"home_refresh_screen": "refresh", "home_navigate_screen": "navigate", "home_show_photo": "show"}[record.Action.Tool]
	if expectedKind == "" || d.Kind != expectedKind || d.Payload.Route != record.Action.Route || d.Payload.Collection != record.Action.Collection || d.Payload.PhotoID != record.Action.PhotoID {
		return invalid()
	}
	switch d.Status {
	case "accepted", "applied":
		if result.IsError {
			return invalid()
		}
	case "failed", "expired", "unknown":
		if !result.IsError {
			return invalid()
		}
	default:
		return invalid()
	}
	return actionEvidence{ObservedAt: envelope.Observed, Command: &d}, nil
}

// DescribeAction formats the outcome of Executor Turn.Call for its original
// validated action. Call errors take precedence over any accompanying result.
// The runtime should display this receipt unchanged; it does not replace model
// intent parsing or certify that a runtime has followed the executor policy.
func DescribeAction(action Action, result *mcp.CallToolResult, callErr error) ActionReport {
	report := ActionReport{Status: "unconfirmed", TextZH: "无法确认屏幕动作结果。"}
	if action.validate() != nil {
		return report
	}
	report.ScreenID = action.ScreenID
	report.TextZH = "屏幕 " + action.ScreenID + "：无法确认执行结果。"
	if callErr != nil {
		var outcome *ActionOutcomeError
		if errors.As(callErr, &outcome) {
			if integration.ValidTraceRef(outcome.OperationID) {
				report.OperationID = outcome.OperationID
				report.TextZH += " 可按原操作查询：" + outcome.OperationID + "。"
			}
			if identifier.MatchString(outcome.CommandID) {
				report.CommandID = outcome.CommandID
			}
		}
		return report
	}
	evidence, err := decodeActionEvidence(ActionRecord{Action: action}, result)
	if err != nil {
		return report
	}
	report.ObservedAt = evidence.ObservedAt
	prefix := "截至 " + evidence.ObservedAt.Format(time.RFC3339Nano) + "，屏幕 " + action.ScreenID + "："
	if evidence.Command == nil {
		report.TextZH = prefix + "无法确认执行结果。" + actionFailureReason(evidence.ErrorCode)
		return report
	}
	command := evidence.Command
	report.CommandID = command.ID
	report.Status = command.Status
	switch command.Status {
	case "accepted":
		report.TextZH = prefix + "命令已接受，尚未收到屏幕执行确认。"
	case "applied":
		report.TextZH = prefix + appliedActionText(action)
	case "failed":
		report.TextZH = prefix + "命令执行失败。" + actionFailureReason(command.ErrorCode)
	case "expired":
		report.TextZH = prefix + "命令已过期，未获得执行成功确认。"
	case "unknown":
		report.TextZH = prefix + "执行结果未知，不能确认屏幕已完成动作。"
	}
	return report
}

func appliedActionText(action Action) string {
	switch action.Tool {
	case "home_refresh_screen":
		return "屏幕已确认刷新。"
	case "home_show_photo":
		return "屏幕已确认展示照片 " + action.PhotoID + "。"
	case "home_navigate_screen":
		if action.Route == "dashboard" {
			return "屏幕已确认切换到首页。"
		}
		collection := map[string]string{"recent": "最近新增集合", "captured_today": "今天拍摄集合", "random": "随机照片集合", "all": "全部照片集合"}[action.Collection]
		return "屏幕已确认切换到" + collection + "。"
	}
	return "无法确认执行结果。"
}

func actionFailureReason(code string) string {
	switch code {
	case "permission_denied":
		return "服务身份或权限不足。"
	case "screen_offline", "screen_disconnected":
		return "屏幕会话离线。"
	case "photo_unavailable", "preview_unavailable":
		return "照片预览不可用。"
	case "preview_processing":
		return "照片预览仍在处理。"
	case "source_offline":
		return "照片来源当前不可用。"
	case "idempotency_conflict":
		return "原操作标识与动作参数冲突。"
	case "operation_expired":
		return "原操作已超出允许重试时限。"
	case "ack_timeout":
		return "未及时收到屏幕确认。"
	case "render_failed":
		return "屏幕报告渲染失败。"
	case "session_replaced":
		return "屏幕会话已更换。"
	case "server_restart":
		return "服务重启后无法确认原动作结果。"
	case "delivery_failed":
		return "命令投递失败。"
	case "superseded":
		return "命令已被后续命令替代。"
	default:
		return ""
	}
}
