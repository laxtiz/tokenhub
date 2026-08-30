package convert

import (
	"encoding/json"
	"fmt"
	"strings"
)

// PeekRequest 从请求体中快速提取模型名与流式标记，用于路由，不做完整解析。
func PeekRequest(body []byte) (model string, stream bool, err error) {
	var p struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return "", false, fmt.Errorf("invalid JSON body: %w", err)
	}
	return p.Model, p.Stream, nil
}

// ---------- Anthropic 请求 → OpenAI 请求 ----------

func AnthropicToOpenAIRequest(ar *AnthropicRequest) (*OpenAIRequest, error) {
	or := &OpenAIRequest{
		Model:        ar.Model,
		Stream:       ar.Stream,
		Temperature:  ar.Temperature,
		TopP:         ar.TopP,
		MaxTokens:    &ar.MaxTokens,
		StreamOptions: &OpenAIStreamOptions{IncludeUsage: ar.Stream},
	}

	// system → system 消息
	sysBlocks, err := ParseBlocks(ar.System)
	if err != nil {
		return nil, err
	}
	if len(sysBlocks) > 0 {
		or.Messages = append(or.Messages, OpenAIMessage{Role: "system", Content: blocksToOpenAIContent(sysBlocks)})
	}

	for _, m := range ar.Messages {
		blocks, err := ParseBlocks(m.Content)
		if err != nil {
			return nil, fmt.Errorf("message role=%s: %w", m.Role, err)
		}
		switch m.Role {
		case "assistant":
			om := OpenAIMessage{Role: "assistant"}
			var texts []AnthropicBlock
			var reasoning strings.Builder
			for _, b := range blocks {
				switch b.Type {
				case "text":
					texts = append(texts, b)
				case "tool_use":
					om.ToolCalls = append(om.ToolCalls, OpenAIToolCall{
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
			if len(texts) > 0 {
				om.Content = blocksToOpenAIContent(texts)
			}
			if reasoning.Len() > 0 {
				om.ReasoningContent = reasoning.String()
			}
			or.Messages = append(or.Messages, om)
		case "user":
			// tool_result 块必须拆成独立的 role=tool 消息，其余合并为普通消息
			var rest []AnthropicBlock
			for _, b := range blocks {
				if b.Type == "tool_result" {
					or.Messages = append(or.Messages, OpenAIMessage{
						Role:       "tool",
						ToolCallID: b.ID,
						Content:    jsonContent(ToolResultText(b)),
					})
				} else {
					rest = append(rest, b)
				}
			}
			if len(rest) > 0 {
				or.Messages = append(or.Messages, OpenAIMessage{Role: "user", Content: blocksToOpenAIContent(rest)})
			}
		default:
			return nil, fmt.Errorf("unsupported role %q", m.Role)
		}
	}

	// stop_sequences → stop
	if len(ar.StopSequences) == 1 {
		or.Stop, _ = json.Marshal(ar.StopSequences[0])
	} else if len(ar.StopSequences) > 1 {
		or.Stop, _ = json.Marshal(ar.StopSequences)
	}

	// tools
	for _, t := range ar.Tools {
		or.Tools = append(or.Tools, OpenAITool{
			Type: "function",
			Function: OpenAIFunctionDef{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}
	if ar.ToolChoice != nil {
		switch ar.ToolChoice.Type {
		case "any":
			or.ToolChoice, _ = json.Marshal("required")
		case "tool":
			or.ToolChoice, _ = json.Marshal(map[string]any{
				"type": "function", "function": map[string]string{"name": ar.ToolChoice.Name},
			})
		default: // auto
			or.ToolChoice, _ = json.Marshal("auto")
		}
	}
	return or, nil
}

// blocksToOpenAIContent 把 Anthropic blocks 转为 OpenAI content（纯文本退化为 string，含图时用 parts）。
func blocksToOpenAIContent(blocks []AnthropicBlock) json.RawMessage {
	onlyText := true
	var texts []string
	for _, b := range blocks {
		if b.Type == "text" {
			texts = append(texts, b.Text)
		} else {
			onlyText = false
		}
	}
	if onlyText {
		b, _ := json.Marshal(strings.Join(texts, "\n"))
		return b
	}
	var parts []OpenAIContentPart
	for _, b := range blocks {
		switch b.Type {
		case "text":
			parts = append(parts, OpenAIContentPart{Type: "text", Text: b.Text})
		case "image":
			if b.Source != nil {
				if b.Source.Type == "base64" {
					parts = append(parts, OpenAIContentPart{
						Type: "image_url",
						ImageURL: &OpenAIImageURL{
							URL: fmt.Sprintf("data:%s;base64,%s", b.Source.MediaType, b.Source.Data),
						},
					})
				} else if b.Source.Type == "url" {
					parts = append(parts, OpenAIContentPart{
						Type:     "image_url",
						ImageURL: &OpenAIImageURL{URL: b.Source.URL},
					})
				}
			}
		}
	}
	b, _ := json.Marshal(parts)
	return b
}

func jsonContent(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}

func normalizeJSONInput(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "{}"
	}
	out, _ := json.Marshal(v)
	return string(out)
}

// ---------- OpenAI 请求 → Anthropic 请求 ----------

func OpenAIToAnthropicRequest(or *OpenAIRequest) (*AnthropicRequest, error) {
	ar := &AnthropicRequest{
		Model:       or.Model,
		Stream:      or.Stream,
		Temperature: or.Temperature,
		TopP:        or.TopP,
		MaxTokens:   defaultMaxTokens(or),
	}

	var systemTexts []string
	for _, m := range or.Messages {
		switch m.Role {
		case "system":
			s, _ := messageContentText(m.Content)
			systemTexts = append(systemTexts, s)
		case "assistant":
			am := AnthropicMessage{Role: "assistant"}
			var blocks []AnthropicBlock
			if s, _ := messageContentText(m.Content); s != "" {
				blocks = append(blocks, AnthropicBlock{Type: "text", Text: s})
			}
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, AnthropicBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Function.Name,
					Input: json.RawMessage(orEmptyJSON(tc.Function.Arguments)),
				})
			}
			am.Content = BlocksToRaw(blocks)
			ar.Messages = append(ar.Messages, am)
		case "tool":
			// role=tool → user 消息中的 tool_result 块
			s, _ := messageContentText(m.Content)
			tr := AnthropicBlock{Type: "tool_result", ID: m.ToolCallID, Content: jsonContent(s)}
			ar.Messages = append(ar.Messages, AnthropicMessage{
				Role:    "user",
				Content: mustMarshal([]AnthropicBlock{tr}),
			})
		case "user":
			var blocks []AnthropicBlock
			parts, err := parseOpenAIParts(m.Content)
			if err != nil {
				return nil, err
			}
			for _, p := range parts {
				switch p.Type {
				case "text":
					blocks = append(blocks, AnthropicBlock{Type: "text", Text: p.Text})
				case "image_url":
					src, err := imageSourceFromURL(p.ImageURL.URL)
					if err != nil {
						return nil, err
					}
					blocks = append(blocks, AnthropicBlock{Type: "image", Source: src})
				}
			}
			if len(blocks) > 0 {
				ar.Messages = append(ar.Messages, AnthropicMessage{
					Role:    "user",
					Content: BlocksToRaw(blocks),
				})
			}
		}
	}
	if len(systemTexts) > 0 {
		ar.System = jsonContent(strings.Join(systemTexts, "\n"))
	}

	if len(or.Stop) > 0 {
		var one string
		if err := json.Unmarshal(or.Stop, &one); err == nil {
			ar.StopSequences = []string{one}
		} else {
			json.Unmarshal(or.Stop, &ar.StopSequences)
		}
	}

	for _, t := range or.Tools {
		ar.Tools = append(ar.Tools, AnthropicTool{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			InputSchema: t.Function.Parameters,
		})
	}
	if len(or.ToolChoice) > 0 {
		var s string
		if err := json.Unmarshal(or.ToolChoice, &s); err == nil {
			switch s {
			case "required":
				ar.ToolChoice = &AnthropicToolChoice{Type: "any"}
			case "none":
				// Anthropic 无 none；移除全部 tools 表达同等语义
				ar.Tools = nil
			default: // auto
				ar.ToolChoice = &AnthropicToolChoice{Type: "auto"}
			}
		} else {
			var tc struct {
				Type     string `json:"type"`
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			}
			if err := json.Unmarshal(or.ToolChoice, &tc); err == nil && tc.Type == "function" {
				ar.ToolChoice = &AnthropicToolChoice{Type: "tool", Name: tc.Function.Name}
			}
		}
	}
	return ar, nil
}

func defaultMaxTokens(or *OpenAIRequest) int64 {
	if or.MaxCompletionTokens != nil && *or.MaxCompletionTokens > 0 {
		return *or.MaxCompletionTokens
	}
	if or.MaxTokens != nil && *or.MaxTokens > 0 {
		return *or.MaxTokens
	}
	return 8192
}

func orEmptyJSON(s string) string {
	if s == "" {
		return "{}"
	}
	return s
}

func mustMarshal(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// messageContentText 提取 OpenAI content 的纯文本（string 或 parts 中的 text 拼接）。
func messageContentText(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}
	parts, err := parseOpenAIParts(raw)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for i, p := range parts {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(p.Text)
	}
	return sb.String(), nil
}

func parseOpenAIParts(raw json.RawMessage) ([]OpenAIContentPart, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var parts []OpenAIContentPart
	if err := json.Unmarshal(raw, &parts); err != nil {
		var s string
		if err2 := json.Unmarshal(raw, &s); err2 != nil {
			return nil, fmt.Errorf("invalid openai content: %w", err)
		}
		return []OpenAIContentPart{{Type: "text", Text: s}}, nil
	}
	return parts, nil
}

// imageSourceFromURL 把 OpenAI image_url 转为 Anthropic image source（data URL → base64，其余走 url）。
func imageSourceFromURL(u string) (*AnthropicImageSource, error) {
	if strings.HasPrefix(u, "data:") {
		rest := strings.TrimPrefix(u, "data:")
		semi := strings.Index(rest, ";base64,")
		if semi < 0 {
			return nil, fmt.Errorf("unsupported data URL (only base64 supported)")
		}
		return &AnthropicImageSource{
			Type:      "base64",
			MediaType: rest[:semi],
			Data:      rest[semi+len(";base64,"):],
		}, nil
	}
	return &AnthropicImageSource{Type: "url", URL: u}, nil
}
