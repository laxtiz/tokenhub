package convert

import (
	"encoding/json"
	"strings"
	"time"
)

// ---------- Anthropic 响应 → OpenAI 响应 ----------

func AnthropicToOpenAIResponse(ar *AnthropicResponse) *OpenAIResponse {
	var texts []string
	var reasoning strings.Builder
	var toolCalls []OpenAIToolCall
	for _, b := range ar.Content {
		switch b.Type {
		case "text":
			texts = append(texts, b.Text)
		case "tool_use":
			toolCalls = append(toolCalls, OpenAIToolCall{
				ID:   b.ID,
				Type: "function",
				Function: OpenAIToolCallFn{
					Name:      b.Name,
					Arguments: normalizeJSONInput(b.Input),
				},
			})
		case "thinking":
			reasoning.WriteString(b.Thinking)
		}
	}
	content := strings.Join(texts, "")
	msg := &OpenAIRespMsg{Role: "assistant"}
	if content != "" || len(toolCalls) == 0 {
		msg.Content = &content
	}
	msg.ToolCalls = toolCalls
	if reasoning.Len() > 0 {
		msg.ReasoningContent = reasoning.String()
	}

	finish := mapStopToOpenAI(ar.StopReason)
	// Anthropic 的 input_tokens 不含缓存部分；OpenAI 客户端预期 prompt_tokens
	// 为全部输入（含缓存命中），details.cached_tokens 单独标注。
	totalInput := ar.Usage.InputTokens + ar.Usage.CacheReadInputTokens + ar.Usage.CacheCreationInputTokens
	usage := &OpenAIUsage{
		PromptTokens:     totalInput,
		CompletionTokens: ar.Usage.OutputTokens,
		TotalTokens:      totalInput + ar.Usage.OutputTokens,
	}
	if ar.Usage.CacheReadInputTokens > 0 {
		usage.PromptTokensDetails = &struct {
			CachedTokens int64 `json:"cached_tokens"`
		}{CachedTokens: ar.Usage.CacheReadInputTokens}
	}
	return &OpenAIResponse{
		ID:      ar.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   ar.Model,
		Choices: []OpenAIChoice{{Index: 0, Message: msg, FinishReason: &finish}},
		Usage:   usage,
	}
}

func mapStopToOpenAI(stop *string) string {
	if stop == nil {
		return "stop"
	}
	switch *stop {
	case "end_turn", "stop_sequence":
		return "stop"
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	default:
		return "stop"
	}
}

// ---------- OpenAI 响应 → Anthropic 响应 ----------

func OpenAIToAnthropicResponse(or *OpenAIResponse) *AnthropicResponse {
	var blocks []AnthropicBlock
	if len(or.Choices) > 0 {
		msg := or.Choices[0].Message
		if msg != nil {
			if msg.Content != nil && *msg.Content != "" {
				blocks = append(blocks, AnthropicBlock{Type: "text", Text: *msg.Content})
			}
			if msg.ReasoningContent != "" {
				blocks = append([]AnthropicBlock{{Type: "thinking", Thinking: msg.ReasoningContent}}, blocks...)
			}
			for _, tc := range msg.ToolCalls {
				blocks = append(blocks, AnthropicBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Function.Name,
					Input: json.RawMessage(orEmptyJSON(tc.Function.Arguments)),
				})
			}
		}
		finish := or.Choices[0].FinishReason
		stop := mapFinishToAnthropic(finish)
		// OpenAI 的 prompt_tokens 包含缓存命中部分；Anthropic 的 input_tokens 不含，
		// 转换时扣除以免下游 Anthropic 客户端重复计数。
		cacheRead := int64(0)
		if or.Usage.PromptTokensDetails != nil {
			cacheRead = or.Usage.PromptTokensDetails.CachedTokens
		}
		input := or.Usage.PromptTokens - cacheRead
		if input < 0 {
			input = 0
		}
		usage := AnthropicUsage{
			InputTokens:              input,
			OutputTokens:             or.Usage.CompletionTokens,
			CacheReadInputTokens:     cacheRead,
		}
		return &AnthropicResponse{
			ID:         or.ID,
			Type:       "message",
			Role:       "assistant",
			Model:      or.Model,
			Content:    blocks,
			StopReason: &stop,
			Usage:      usage,
		}
	}
	return &AnthropicResponse{
		ID: or.ID, Type: "message", Role: "assistant", Model: or.Model,
		Content: blocks, Usage: AnthropicUsage{
			InputTokens: or.Usage.PromptTokens, OutputTokens: or.Usage.CompletionTokens,
		},
	}
}

func mapFinishToAnthropic(finish *string) string {
	if finish == nil {
		return "end_turn"
	}
	switch *finish {
	case "length":
		return "max_tokens"
	case "tool_calls", "function_call":
		return "tool_use"
	default:
		return "end_turn"
	}
}

// ---------- 错误体转换 ----------

func OpenAIErrorBodyJSON(typ, msg string) []byte {
	b, _ := json.Marshal(map[string]any{
		"error": OpenAIErrorBody{Message: msg, Type: typ},
	})
	return b
}

func AnthropicErrorBodyJSON(typ, msg string) []byte {
	b, _ := json.Marshal(AnthropicErrorResp{Type: "error", Error: &AnthropicErrorBody{Type: typ, Message: msg}})
	return b
}

// UpstreamUsage 上游用量，供计费与日志使用。
// 统一为业界惯例口径：Prompt 为完整输入 tokens（含缓存命中部分），
// CacheRead/CacheWrite 是其中的子集，单独记录；计费时在 billing 层拆分。
type UpstreamUsage struct {
	Prompt     int64
	Completion int64
	CacheRead  int64
	CacheWrite int64
}

// ParseOpenAIUsage 从 OpenAI 响应体提取用量（prompt_tokens 本身即完整输入）。
func ParseOpenAIUsage(body []byte) (UpstreamUsage, bool) {
	var r struct {
		Usage *OpenAIUsage `json:"usage"`
	}
	if err := json.Unmarshal(body, &r); err != nil || r.Usage == nil {
		return UpstreamUsage{}, false
	}
	u := UpstreamUsage{Prompt: r.Usage.PromptTokens, Completion: r.Usage.CompletionTokens}
	if r.Usage.PromptTokensDetails != nil {
		u.CacheRead = r.Usage.PromptTokensDetails.CachedTokens
	}
	return u, true
}

// ParseAnthropicUsage 从 Anthropic 响应体提取用量。
func ParseAnthropicUsage(body []byte) (UpstreamUsage, bool) {
	var r struct {
		Usage AnthropicUsage `json:"usage"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return UpstreamUsage{}, false
	}
	// Anthropic 的 input_tokens 不含缓存部分，补齐为完整输入口径
	return UpstreamUsage{
		Prompt:     r.Usage.InputTokens + r.Usage.CacheReadInputTokens + r.Usage.CacheCreationInputTokens,
		Completion: r.Usage.OutputTokens,
		CacheRead:  r.Usage.CacheReadInputTokens,
		CacheWrite: r.Usage.CacheCreationInputTokens,
	}, true
}
