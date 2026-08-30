// Package convert 实现 OpenAI Chat Completions 与 Anthropic Messages 两种协议的双向转换。
package convert

import (
	"encoding/json"
	"fmt"
)

// ---------- OpenAI 类型 ----------

type OpenAIRequest struct {
	Model              string           `json:"model"`
	Messages           []OpenAIMessage  `json:"messages"`
	Stream             bool             `json:"stream,omitempty"`
	MaxTokens          *int64           `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int64          `json:"max_completion_tokens,omitempty"`
	Temperature        *float64         `json:"temperature,omitempty"`
	TopP               *float64         `json:"top_p,omitempty"`
	Stop               json.RawMessage  `json:"stop,omitempty"`
	Tools              []OpenAITool     `json:"tools,omitempty"`
	ToolChoice         json.RawMessage  `json:"tool_choice,omitempty"`
	StreamOptions      *OpenAIStreamOptions `json:"stream_options,omitempty"`
	ParallelToolCalls  *bool            `json:"parallel_tool_calls,omitempty"`
	User               string           `json:"user,omitempty"`
}

type OpenAIStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type OpenAIMessage struct {
	Role       string           `json:"role"`
	Content    json.RawMessage  `json:"content,omitempty"` // string | []part
	Name       string           `json:"name,omitempty"`
	ToolCalls  []OpenAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`

	ReasoningContent string `json:"reasoning_content,omitempty"` // 第三方推理模型常见扩展字段
}

type OpenAIContentPart struct {
	Type     string          `json:"type"` // text | image_url
	Text     string          `json:"text,omitempty"`
	ImageURL *OpenAIImageURL `json:"image_url,omitempty"`
}

type OpenAIImageURL struct {
	URL string `json:"url"`
}

type OpenAITool struct {
	Type     string            `json:"type"` // function
	Function OpenAIFunctionDef `json:"function"`
}

type OpenAIFunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type OpenAIToolCall struct {
	Index    *int            `json:"index,omitempty"` // 流式 delta 中的序号
	ID       string          `json:"id,omitempty"`
	Type     string          `json:"type,omitempty"`
	Function OpenAIToolCallFn `json:"function"`
}

type OpenAIToolCallFn struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type OpenAIResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []OpenAIChoice `json:"choices"`
	Usage   *OpenAIUsage   `json:"usage,omitempty"`
}

type OpenAIChoice struct {
	Index        int            `json:"index"`
	Message      *OpenAIRespMsg `json:"message,omitempty"`
	Delta        *OpenAIRespMsg `json:"delta,omitempty"`
	FinishReason *string        `json:"finish_reason"`
}

type OpenAIRespMsg struct {
	Role             string           `json:"role,omitempty"`
	Content          *string          `json:"content,omitempty"`
	ToolCalls        []OpenAIToolCall `json:"tool_calls,omitempty"`
	ReasoningContent string           `json:"reasoning_content,omitempty"`
}

type OpenAIUsage struct {
	PromptTokens        int64 `json:"prompt_tokens"`
	CompletionTokens    int64 `json:"completion_tokens"`
	TotalTokens         int64 `json:"total_tokens"`
	PromptTokensDetails *struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *struct {
		ReasoningTokens int64 `json:"reasoning_tokens"`
	} `json:"completion_tokens_details,omitempty"`
}

type OpenAIErrorBody struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    any    `json:"code,omitempty"`
}

// ---------- Anthropic 类型 ----------

type AnthropicRequest struct {
	Model         string             `json:"model"`
	Messages      []AnthropicMessage `json:"messages"`
	System        json.RawMessage    `json:"system,omitempty"` // string | []block
	MaxTokens     int64              `json:"max_tokens"`
	Stream        bool               `json:"stream,omitempty"`
	Temperature   *float64           `json:"temperature,omitempty"`
	TopP          *float64           `json:"top_p,omitempty"`
	StopSequences []string           `json:"stop_sequences,omitempty"`
	Tools         []AnthropicTool    `json:"tools,omitempty"`
	ToolChoice    *AnthropicToolChoice `json:"tool_choice,omitempty"`
}

type AnthropicMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"` // string | []block
}

type AnthropicBlock struct {
	Type      string                `json:"type"` // text | image | tool_use | tool_result | thinking
	Text      string                `json:"text,omitempty"`
	ID        string                `json:"id,omitempty"`
	Name      string                `json:"name,omitempty"`
	Input     json.RawMessage       `json:"input,omitempty"`
	Source    *AnthropicImageSource `json:"source,omitempty"`
	Content   json.RawMessage       `json:"content,omitempty"` // tool_result: string | []block
	IsError   bool                  `json:"is_error,omitempty"`
	Thinking  string                `json:"thinking,omitempty"`
	Signature string                `json:"signature,omitempty"`
}

type AnthropicImageSource struct {
	Type      string `json:"type"` // base64 | url
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

type AnthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type AnthropicToolChoice struct {
	Type string `json:"type"` // auto | any | tool
	Name string `json:"name,omitempty"`
}

type AnthropicResponse struct {
	ID           string           `json:"id"`
	Type         string           `json:"type"` // message
	Role         string           `json:"role"`
	Model        string           `json:"model"`
	Content      []AnthropicBlock `json:"content"`
	StopReason   *string          `json:"stop_reason"`
	StopSequence *string          `json:"stop_sequence"`
	Usage        AnthropicUsage   `json:"usage"`
}

type AnthropicUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens,omitempty"`
}

type AnthropicErrorBody struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type AnthropicErrorResp struct {
	Type  string             `json:"type"` // error
	Error *AnthropicErrorBody `json:"error,omitempty"`
}

// 流式事件
type AnthropicStreamEvent struct {
	Type         string            `json:"type"` // message_start | content_block_start | content_block_delta | content_block_stop | message_delta | message_stop | ping | error
	Message      *AnthropicResponse `json:"message,omitempty"`
	Index        *int              `json:"index,omitempty"`
	ContentBlock *AnthropicBlock   `json:"content_block,omitempty"`
	Delta        *AnthropicDelta   `json:"delta,omitempty"`
	Usage        *AnthropicUsage   `json:"usage,omitempty"`
	Error        *AnthropicErrorBody `json:"error,omitempty"`
}

type AnthropicDelta struct {
	Type         string  `json:"type,omitempty"` // text_delta | input_json_delta | thinking_delta | signature_delta
	Text         string  `json:"text,omitempty"`
	PartialJSON  string  `json:"partial_json,omitempty"`
	Thinking     string  `json:"thinking,omitempty"`
	Signature    string  `json:"signature,omitempty"`
	StopReason   *string `json:"stop_reason,omitempty"` // message_delta 专用
	StopSequence *string `json:"stop_sequence,omitempty"`
}

// ---------- 通用辅助 ----------

// ParseBlocks 解析 Anthropic content（string | []block）为统一的 block 列表。
func ParseBlocks(raw json.RawMessage) ([]AnthropicBlock, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if s == "" {
			return nil, nil
		}
		return []AnthropicBlock{{Type: "text", Text: s}}, nil
	}
	var blocks []AnthropicBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, fmt.Errorf("invalid content: %w", err)
	}
	return blocks, nil
}

// BlocksToRaw 把 block 列表编码回 Anthropic content 字段（单 text 块时退化为纯字符串）。
func BlocksToRaw(blocks []AnthropicBlock) json.RawMessage {
	if len(blocks) == 1 && blocks[0].Type == "text" {
		b, _ := json.Marshal(blocks[0].Text)
		return b
	}
	if len(blocks) == 0 {
		return nil
	}
	b, _ := json.Marshal(blocks)
	return b
}

// ToolResultText 提取 tool_result 块的文本内容。
func ToolResultText(b AnthropicBlock) string {
	if len(b.Content) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(b.Content, &s); err == nil {
		return s
	}
	var blocks []AnthropicBlock
	if err := json.Unmarshal(b.Content, &blocks); err == nil {
		out := ""
		for _, bb := range blocks {
			if bb.Type == "text" {
				if out != "" {
					out += "\n"
				}
				out += bb.Text
			}
		}
		return out
	}
	return string(b.Content)
}
