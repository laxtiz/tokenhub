package convert

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// StreamXformer 跨格式流式转换器：逐事件消费上游 SSE，产出可直接写往下游的字节。
type StreamXformer interface {
	// Transform 消费一个上游 SSE 事件（event 名可为空，data 为 data: 后的 JSON）。
	// 返回要写给下游的字节（含 SSE 分隔符），done 表示流已完整结束。
	Transform(event, data string) (out []byte, done bool, err error)
	// Finish 上游提前结束时输出下游所需的收尾帧。
	Finish() []byte
}

// ---------- 上游 Anthropic → 下游 OpenAI ----------

type AnthUpToOpenDown struct {
	id        string
	model     string
	created   int64
	toolIdx   map[int]int // anthropic block index → openai tool_calls index
	toolCount int
	finish    string
	usage     UpstreamUsage
}

func NewAnthUpToOpenDown(downstreamModel string) *AnthUpToOpenDown {
	return &AnthUpToOpenDown{toolIdx: map[int]int{}, created: time.Now().Unix(), model: downstreamModel}
}

func (x *AnthUpToOpenDown) chunk(delta *OpenAIRespMsg, finish *string) []byte {
	c := OpenAIResponse{
		ID: x.id, Object: "chat.completion.chunk", Created: x.created, Model: x.model,
		Choices: []OpenAIChoice{{Index: 0, Delta: delta, FinishReason: finish}},
	}
	b, _ := json.Marshal(c)
	return append([]byte("data: "), append(b, '\n', '\n')...)
}

func (x *AnthUpToOpenDown) usageChunk() []byte {
	// OpenAI 语义：prompt_tokens 为全部输入（已含缓存命中），缓存命中单独标注
	totalInput := x.usage.Prompt
	c := OpenAIResponse{
		ID: x.id, Object: "chat.completion.chunk", Created: x.created, Model: x.model,
		Choices: []OpenAIChoice{},
		Usage: &OpenAIUsage{
			PromptTokens:     totalInput,
			CompletionTokens: x.usage.Completion,
			TotalTokens:      totalInput + x.usage.Completion,
		},
	}
	if x.usage.CacheRead > 0 {
		c.Usage.PromptTokensDetails = &struct {
			CachedTokens int64 `json:"cached_tokens"`
		}{CachedTokens: x.usage.CacheRead}
	}
	b, _ := json.Marshal(c)
	return append([]byte("data: "), append(b, '\n', '\n')...)
}

func (x *AnthUpToOpenDown) Transform(event, data string) ([]byte, bool, error) {
	if data == "" {
		return nil, false, nil
	}
	var ev AnthropicStreamEvent
	if err := json.Unmarshal([]byte(data), &ev); err != nil {
		return nil, false, nil // 无法解析的事件直接丢弃
	}
	switch ev.Type {
	case "message_start":
		if ev.Message != nil {
			x.id = ev.Message.ID
			// 完整输入口径：input_tokens 不含缓存部分，需补齐
			x.usage.CacheRead = ev.Message.Usage.CacheReadInputTokens
			x.usage.CacheWrite = ev.Message.Usage.CacheCreationInputTokens
			x.usage.Prompt = ev.Message.Usage.InputTokens + x.usage.CacheRead + x.usage.CacheWrite
		}
		return x.chunk(&OpenAIRespMsg{Role: "assistant", Content: strPtr("")}, nil), false, nil
	case "content_block_start":
		if ev.ContentBlock != nil && ev.ContentBlock.Type == "tool_use" {
			idx := x.toolCount
			x.toolCount++
			if ev.Index != nil {
				x.toolIdx[*ev.Index] = idx
			}
			return x.chunk(&OpenAIRespMsg{ToolCalls: []OpenAIToolCall{{
				Index:    intPtr(idx),
				ID:       ev.ContentBlock.ID,
				Type:     "function",
				Function: OpenAIToolCallFn{Name: ev.ContentBlock.Name},
			}}}, nil), false, nil
		}
	case "content_block_delta":
		if ev.Delta == nil {
			return nil, false, nil
		}
		switch ev.Delta.Type {
		case "text_delta":
			return x.chunk(&OpenAIRespMsg{Content: strPtr(ev.Delta.Text)}, nil), false, nil
		case "input_json_delta":
			idx := 0
			if ev.Index != nil {
				idx = x.toolIdx[*ev.Index]
			}
			return x.chunk(&OpenAIRespMsg{ToolCalls: []OpenAIToolCall{{
				Index:    intPtr(idx),
				Function: OpenAIToolCallFn{Arguments: ev.Delta.PartialJSON},
			}}}, nil), false, nil
		case "thinking_delta":
			return x.chunk(&OpenAIRespMsg{ReasoningContent: ev.Delta.Thinking}, nil), false, nil
		}
	case "message_delta":
		if ev.Delta != nil && ev.Delta.StopReason != nil {
			x.finish = mapStopToOpenAI(ev.Delta.StopReason)
		}
		if ev.Usage != nil {
			x.usage.Completion = ev.Usage.OutputTokens
		}
		var out []byte
		out = append(out, x.chunk(&OpenAIRespMsg{}, &x.finish)...)
		out = append(out, x.usageChunk()...)
		return out, false, nil
	case "message_stop":
		return []byte("data: [DONE]\n\n"), true, nil
	case "error":
		if ev.Error != nil {
			return nil, true, fmt.Errorf("%s: %s", ev.Error.Type, ev.Error.Message)
		}
	}
	return nil, false, nil
}

func (x *AnthUpToOpenDown) Finish() []byte {
	if x.finish == "" {
		x.finish = "stop"
	}
	out := x.chunk(&OpenAIRespMsg{}, &x.finish)
	out = append(out, x.usageChunk()...)
	return append(out, []byte("data: [DONE]\n\n")...)
}

// ---------- 上游 OpenAI → 下游 Anthropic ----------

type OpenUpToAnthDown struct {
	id, model     string
	started       bool
	blockIdx      int         // 下一个 anthropic block index
	textOpen      bool        // 当前是否打开着 text 块
	reasonOpen    bool        // 当前是否打开着 thinking 块
	toolBlocks    map[int]int // openai tool_calls index → anthropic block index
	finish        *string
	usage         UpstreamUsage // Prompt 为完整输入（含缓存命中）
}

func NewOpenUpToAnthDown(downstreamModel string) *OpenUpToAnthDown {
	return &OpenUpToAnthDown{toolBlocks: map[int]int{}, model: downstreamModel}
}

func (x *OpenUpToAnthDown) emit(ev AnthropicStreamEvent) []byte {
	b, _ := json.Marshal(ev)
	return append([]byte("event: "), append([]byte(ev.Type), append([]byte("\ndata: "), append(b, '\n', '\n')...)...)...)
}

func (x *OpenUpToAnthDown) messageStart() []byte {
	x.started = true
	// Anthropic 语义：input_tokens 不含缓存命中部分
	input := x.usage.Prompt - x.usage.CacheRead
	if input < 0 {
		input = 0
	}
	return x.emit(AnthropicStreamEvent{
		Type: "message_start",
		Message: &AnthropicResponse{
			ID: x.id, Type: "message", Role: "assistant", Model: x.model,
			Content: []AnthropicBlock{},
			Usage:   AnthropicUsage{InputTokens: input},
		},
	})
}

func (x *OpenUpToAnthDown) openTextBlock() []byte {
	if x.textOpen {
		return nil
	}
	x.textOpen = true
	idx := x.blockIdx
	x.blockIdx++
	return x.emit(AnthropicStreamEvent{
		Type: "content_block_start", Index: idxPtr(idx),
		ContentBlock: &AnthropicBlock{Type: "text", Text: ""},
	})
}

// openReasonBlock 打开 thinking 块（仅允许出现在文本块之前）。
func (x *OpenUpToAnthDown) openReasonBlock() []byte {
	if x.reasonOpen || x.textOpen {
		return nil
	}
	x.reasonOpen = true
	idx := x.blockIdx
	x.blockIdx++
	return x.emit(AnthropicStreamEvent{
		Type: "content_block_start", Index: idxPtr(idx),
		ContentBlock: &AnthropicBlock{Type: "thinking", Thinking: ""},
	})
}

func (x *OpenUpToAnthDown) closeCurrentBlock() []byte {
	if !x.textOpen && !x.reasonOpen {
		return nil
	}
	idx := x.blockIdx - 1
	x.textOpen = false
	x.reasonOpen = false
	return x.emit(AnthropicStreamEvent{Type: "content_block_stop", Index: idxPtr(idx)})
}

func (x *OpenUpToAnthDown) Transform(event, data string) ([]byte, bool, error) {
	if data == "" {
		return nil, false, nil
	}
	if data == "[DONE]" {
		return x.finishOK(), true, nil
	}
	var c OpenAIResponse
	if err := json.Unmarshal([]byte(data), &c); err != nil {
		return nil, false, nil
	}
	if x.id == "" {
		x.id = c.ID
		if x.model == "" {
			x.model = c.Model
		}
	}
	if c.Usage != nil {
		if c.Usage.PromptTokens > 0 {
			x.usage.Prompt = c.Usage.PromptTokens
		}
		if c.Usage.CompletionTokens > 0 {
			x.usage.Completion = c.Usage.CompletionTokens
		}
		if c.Usage.PromptTokensDetails != nil {
			x.usage.CacheRead = c.Usage.PromptTokensDetails.CachedTokens
		}
	}
	if len(c.Choices) == 0 {
		return nil, false, nil
	}
	ch := c.Choices[0]
	var out []byte
	if !x.started {
		out = append(out, x.messageStart()...)
	}
	if ch.Delta != nil {
		if ch.Delta.ReasoningContent != "" {
			start := x.openReasonBlock()
			if start != nil {
				out = append(out, start...)
			}
			if x.reasonOpen {
				out = append(out, x.emit(AnthropicStreamEvent{
					Type: "content_block_delta", Index: idxPtr(x.blockIdx - 1),
					Delta: &AnthropicDelta{Type: "thinking_delta", Thinking: ch.Delta.ReasoningContent},
				})...)
			}
		}
		if ch.Delta.Content != nil && *ch.Delta.Content != "" {
			// 仅关闭 thinking 块；text 块跨多个上游 chunk 持续复用，不拆成多个块
			if x.reasonOpen && !x.textOpen {
				out = append(out, x.closeCurrentBlock()...)
			}
			start := x.openTextBlock()
			if start != nil {
				out = append(out, start...)
			}
			out = append(out, x.emit(AnthropicStreamEvent{
				Type: "content_block_delta", Index: idxPtr(x.blockIdx - 1),
				Delta: &AnthropicDelta{Type: "text_delta", Text: *ch.Delta.Content},
			})...)
		}
		for _, tc := range ch.Delta.ToolCalls {
			idx := 0
			if tc.Index != nil {
				idx = *tc.Index
			}
			blockIdx, ok := x.toolBlocks[idx]
			if !ok {
				out = append(out, x.closeCurrentBlock()...)
				blockIdx = x.blockIdx
				x.blockIdx++
				x.toolBlocks[idx] = blockIdx
				out = append(out, x.emit(AnthropicStreamEvent{
					Type: "content_block_start", Index: idxPtr(blockIdx),
					ContentBlock: &AnthropicBlock{Type: "tool_use", ID: tc.ID, Name: tc.Function.Name},
				})...)
			}
			if tc.Function.Arguments != "" {
				out = append(out, x.emit(AnthropicStreamEvent{
					Type: "content_block_delta", Index: idxPtr(blockIdx),
					Delta: &AnthropicDelta{Type: "input_json_delta", PartialJSON: tc.Function.Arguments},
				})...)
			}
		}
	}
	if ch.FinishReason != nil && *ch.FinishReason != "" {
		x.finish = ch.FinishReason
	}
	return out, false, nil
}

func (x *OpenUpToAnthDown) finishOK() []byte {
	var out []byte
	if !x.started {
		out = append(out, x.messageStart()...)
	}
	out = append(out, x.closeCurrentBlock()...)
	stop := mapFinishToAnthropic(x.finish)
	// Anthropic 语义：input_tokens 不含缓存命中部分
	input := x.usage.Prompt - x.usage.CacheRead
	if input < 0 {
		input = 0
	}
	out = append(out, x.emit(AnthropicStreamEvent{
		Type: "message_delta",
		Delta: &AnthropicDelta{StopReason: &stop},
		Usage: &AnthropicUsage{OutputTokens: x.usage.Completion, InputTokens: input,
			CacheReadInputTokens: x.usage.CacheRead},
	})...)
	out = append(out, x.emit(AnthropicStreamEvent{Type: "message_stop"})...)
	return out
}

func (x *OpenUpToAnthDown) Finish() []byte { return x.finishOK() }

// ---------- 透传流（同格式）的用量提取器 ----------

// OpenAIStreamUsage 观测 OpenAI 上游流式用量（prompt_tokens 为完整输入）。
type OpenAIStreamUsage struct {
	rawPrompt  int64
	U          UpstreamUsage
}

func (x *OpenAIStreamUsage) Observe(data string) {
	if !strings.HasPrefix(data, "{") {
		return
	}
	var c struct {
		Usage *OpenAIUsage `json:"usage"`
	}
	if err := json.Unmarshal([]byte(data), &c); err != nil || c.Usage == nil {
		return
	}
	if c.Usage.PromptTokens > 0 {
		x.rawPrompt = c.Usage.PromptTokens
	}
	if c.Usage.CompletionTokens > 0 {
		x.U.Completion = c.Usage.CompletionTokens
	}
	if c.Usage.PromptTokensDetails != nil {
		x.U.CacheRead = c.Usage.PromptTokensDetails.CachedTokens
	}
	x.U.Prompt = x.rawPrompt
}

type AnthropicStreamUsage struct{ U UpstreamUsage }

func (x *AnthropicStreamUsage) Observe(event, data string) {
	if data == "" {
		return
	}
	var ev AnthropicStreamEvent
	if err := json.Unmarshal([]byte(data), &ev); err != nil {
		return
	}
	switch ev.Type {
	case "message_start":
		if ev.Message != nil {
			x.U.Prompt = ev.Message.Usage.InputTokens
			x.U.CacheRead = ev.Message.Usage.CacheReadInputTokens
			x.U.CacheWrite = ev.Message.Usage.CacheCreationInputTokens
		}
	case "message_delta":
		if ev.Usage != nil && ev.Usage.OutputTokens > 0 {
			x.U.Completion = ev.Usage.OutputTokens
		}
	}
}

// ---------- 工厂 ----------

// NewXformer 创建跨格式转换器；up 是上游格式，down 是下游格式（"openai"/"anthropic"），
// downstreamModel 会作为下游响应中的 model 字段输出。
func NewXformer(up, down, downstreamModel string) StreamXformer {
	switch {
	case up == "anthropic" && down == "openai":
		return NewAnthUpToOpenDown(downstreamModel)
	case up == "openai" && down == "anthropic":
		return NewOpenUpToAnthDown(downstreamModel)
	}
	return nil
}

// Usage 返回上游用量（Prompt 为完整输入，含缓存命中）。
func (x *AnthUpToOpenDown) Usage() UpstreamUsage { return x.usage }

// Usage 返回已累计的上游用量（relay 层在流结束后读取）。
// Usage 返回上游用量（Prompt 为完整输入，含缓存命中）。
func (x *OpenUpToAnthDown) Usage() UpstreamUsage { return x.usage }

func strPtr(s string) *string    { return &s }
func idxPtr(i int) *int          { return &i }
func intPtr(i int) *int          { return &i }
