package convert

import (
	"encoding/json"
	"testing"
)

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestAnthropicToOpenAIRequest_Basic(t *testing.T) {
	ar := &AnthropicRequest{
		Model: "claude-x",
		System: mustJSON(t, "you are helpful"),
		Messages: []AnthropicMessage{
			{Role: "user", Content: mustJSON(t, "hello")},
			{Role: "assistant", Content: mustJSON(t, []AnthropicBlock{
				{Type: "text", Text: "hi"},
			})},
		},
		MaxTokens:     1000,
		StopSequences: []string{"END"},
	}
	or, err := AnthropicToOpenAIRequest(ar)
	if err != nil {
		t.Fatal(err)
	}
	if or.Model != "claude-x" || *or.MaxTokens != 1000 {
		t.Fatalf("bad model/maxtokens: %+v", or)
	}
	if len(or.Messages) != 3 {
		t.Fatalf("want 3 messages (system+2), got %d", len(or.Messages))
	}
	if or.Messages[0].Role != "system" {
		t.Fatalf("first message should be system, got %s", or.Messages[0].Role)
	}
	var sys string
	json.Unmarshal(or.Messages[0].Content, &sys)
	if sys != "you are helpful" {
		t.Fatalf("bad system: %q", sys)
	}
	var user string
	json.Unmarshal(or.Messages[1].Content, &user)
	if user != "hello" {
		t.Fatalf("bad user content: %q", user)
	}
	var stop string
	if err := json.Unmarshal(or.Stop, &stop); err != nil || stop != "END" {
		t.Fatalf("bad stop: %s", string(or.Stop))
	}
}

func TestAnthropicToOpenAIRequest_Tools(t *testing.T) {
	ar := &AnthropicRequest{
		Model:     "m",
		MaxTokens: 100,
		Messages: []AnthropicMessage{
			{Role: "user", Content: mustJSON(t, "weather?")},
			{Role: "assistant", Content: mustJSON(t, []AnthropicBlock{
				{Type: "tool_use", ID: "toolu_1", Name: "get_weather", Input: mustJSON(t, map[string]any{"city": "SF"})},
			})},
			{Role: "user", Content: mustJSON(t, []AnthropicBlock{
				{Type: "tool_result", ID: "toolu_1", Content: mustJSON(t, "sunny")},
			})},
		},
		Tools: []AnthropicTool{{
			Name:        "get_weather",
			Description: "get weather",
			InputSchema: mustJSON(t, map[string]any{"type": "object", "properties": map[string]any{}}),
		}},
		ToolChoice: &AnthropicToolChoice{Type: "any"},
	}
	or, err := AnthropicToOpenAIRequest(ar)
	if err != nil {
		t.Fatal(err)
	}
	// assistant tool_use → tool_calls
	asst := or.Messages[1]
	if len(asst.ToolCalls) != 1 || asst.ToolCalls[0].ID != "toolu_1" || asst.ToolCalls[0].Function.Name != "get_weather" {
		t.Fatalf("bad tool_calls: %+v", asst.ToolCalls)
	}
	// tool_result → role=tool
	tool := or.Messages[2]
	if tool.Role != "tool" || tool.ToolCallID != "toolu_1" {
		t.Fatalf("bad tool message: %+v", tool)
	}
	// tools
	if len(or.Tools) != 1 || or.Tools[0].Function.Name != "get_weather" {
		t.Fatalf("bad tools: %+v", or.Tools)
	}
	// tool_choice any → required
	var tc string
	json.Unmarshal(or.ToolChoice, &tc)
	if tc != "required" {
		t.Fatalf("bad tool_choice: %s", tc)
	}
}

func TestOpenAIToAnthropicRequest_Basic(t *testing.T) {
	or := &OpenAIRequest{
		Model: "gpt-x",
		Messages: []OpenAIMessage{
			{Role: "system", Content: mustJSON(t, "be nice")},
			{Role: "user", Content: mustJSON(t, "hi")},
			{Role: "assistant", Content: mustJSON(t, "hello"), ToolCalls: []OpenAIToolCall{{
				ID: "call_1", Type: "function",
				Function: OpenAIToolCallFn{Name: "f", Arguments: `{"a":1}`},
			}}},
			{Role: "tool", ToolCallID: "call_1", Content: mustJSON(t, "result")},
		},
		MaxTokens:  func() *int64 { v := int64(500); return &v }(),
		Stop:       mustJSON(t, []string{"A", "B"}),
		Tools:      []OpenAITool{{Type: "function", Function: OpenAIFunctionDef{Name: "f", Parameters: mustJSON(t, map[string]any{"type": "object"})}}},
		ToolChoice: mustJSON(t, "required"),
	}
	ar, err := OpenAIToAnthropicRequest(or)
	if err != nil {
		t.Fatal(err)
	}
	if ar.Model != "gpt-x" || ar.MaxTokens != 500 {
		t.Fatalf("bad basic: %+v", ar)
	}
	sys, _ := ParseBlocks(ar.System)
	if len(sys) != 1 || sys[0].Text != "be nice" {
		t.Fatalf("bad system: %+v", sys)
	}
	if len(ar.Messages) != 3 { // system 提走；assistant 含 tool_use；tool → user tool_result
		t.Fatalf("want 3 messages, got %d", len(ar.Messages))
	}
	blocks, _ := ParseBlocks(ar.Messages[1].Content)
	if len(blocks) != 2 || blocks[0].Type != "text" || blocks[1].Type != "tool_use" {
		t.Fatalf("bad assistant blocks: %+v", blocks)
	}
	if blocks[1].ID != "call_1" {
		t.Fatalf("bad tool_use id: %s", blocks[1].ID)
	}
	tb, _ := ParseBlocks(ar.Messages[2].Content)
	if len(tb) != 1 || tb[0].Type != "tool_result" || tb[0].ID != "call_1" {
		t.Fatalf("bad tool_result: %+v", tb)
	}
	if len(ar.StopSequences) != 2 {
		t.Fatalf("bad stop_sequences: %v", ar.StopSequences)
	}
	if ar.ToolChoice == nil || ar.ToolChoice.Type != "any" {
		t.Fatalf("bad tool_choice: %+v", ar.ToolChoice)
	}
}

func TestOpenAIToAnthropicRequest_ImageDataURL(t *testing.T) {
	or := &OpenAIRequest{
		Model: "m",
		Messages: []OpenAIMessage{{
			Role: "user",
			Content: mustJSON(t, []OpenAIContentPart{
				{Type: "text", Text: "look"},
				{Type: "image_url", ImageURL: &OpenAIImageURL{URL: "data:image/png;base64,QUJD"}},
			}),
		}},
	}
	ar, err := OpenAIToAnthropicRequest(or)
	if err != nil {
		t.Fatal(err)
	}
	blocks, _ := ParseBlocks(ar.Messages[0].Content)
	if len(blocks) != 2 || blocks[1].Type != "image" {
		t.Fatalf("blocks: %+v", blocks)
	}
	if blocks[1].Source.MediaType != "image/png" || blocks[1].Source.Data != "QUJD" {
		t.Fatalf("source: %+v", blocks[1].Source)
	}
}

func TestNonStreamResponseConversions(t *testing.T) {
	// anthropic → openai
	ar := &AnthropicResponse{
		ID: "msg_1", Type: "message", Role: "assistant", Model: "claude-x",
		Content: []AnthropicBlock{
			{Type: "text", Text: "answer"},
			{Type: "tool_use", ID: "tu1", Name: "f", Input: mustJSON(t, map[string]any{"x": 1})},
		},
		StopReason: strPtr("tool_use"),
		Usage:      AnthropicUsage{InputTokens: 10, OutputTokens: 5, CacheReadInputTokens: 4},
	}
	or := AnthropicToOpenAIResponse(ar)
	if or.Choices[0].FinishReason == nil || *or.Choices[0].FinishReason != "tool_calls" {
		t.Fatalf("finish: %v", or.Choices[0].FinishReason)
	}
	if len(or.Choices[0].Message.ToolCalls) != 1 || or.Choices[0].Message.ToolCalls[0].ID != "tu1" {
		t.Fatalf("toolcalls: %+v", or.Choices[0].Message.ToolCalls)
	}
	// OpenAI 语义：prompt_tokens = 全部输入（含缓存命中 4）
	if or.Usage.PromptTokens != 14 || or.Usage.CompletionTokens != 5 {
		t.Fatalf("usage: %+v", or.Usage)
	}
	if or.Usage.PromptTokensDetails.CachedTokens != 4 {
		t.Fatalf("cache read lost")
	}

	// openai → anthropic
	or2 := &OpenAIResponse{
		ID: "chatcmpl_1", Object: "chat.completion", Model: "gpt-x",
		Choices: []OpenAIChoice{{
			Index: 0,
			Message: &OpenAIRespMsg{
				Role: "assistant", Content: strPtr("hello"),
				ToolCalls: []OpenAIToolCall{{ID: "c1", Type: "function", Function: OpenAIToolCallFn{Name: "f", Arguments: `{"a":1}`}}},
			},
			FinishReason: strPtr("length"),
		}},
		Usage: &OpenAIUsage{PromptTokens: 7, CompletionTokens: 3,
			PromptTokensDetails: &struct {
				CachedTokens int64 `json:"cached_tokens"`
			}{CachedTokens: 2}},
	}
	ar2 := OpenAIToAnthropicResponse(or2)
	if *ar2.StopReason != "max_tokens" {
		t.Fatalf("stop: %s", *ar2.StopReason)
	}
	// Anthropic 语义：input_tokens 不含缓存命中（7-2=5）
	if ar2.Usage.InputTokens != 5 || ar2.Usage.CacheReadInputTokens != 2 {
		t.Fatalf("usage: %+v", ar2.Usage)
	}
	kinds := map[string]bool{}
	for _, b := range ar2.Content {
		kinds[b.Type] = true
	}
	if !kinds["text"] || !kinds["tool_use"] {
		t.Fatalf("content blocks: %+v", ar2.Content)
	}
}

func TestAnthUpToOpenDown_TextAndToolStream(t *testing.T) {
	x := NewAnthUpToOpenDown("gpt-x")
	var out []byte
	ended := false
	feed := func(ev string, data string) {
		b, done, err := x.Transform(ev, data)
		if err != nil {
			t.Fatalf("transform err: %v", err)
		}
		out = append(out, b...)
		if ended && len(b) > 0 {
			t.Fatalf("data after done")
		}
		if done {
			ended = true
		}
	}
	feed("", `{"type":"message_start","message":{"id":"msg_1","role":"assistant","model":"claude-x","content":[],"usage":{"input_tokens":10}}}`)
	feed("", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
	feed("", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"he"}}`)
	feed("", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"llo"}}`)
	feed("", `{"type":"content_block_stop","index":0}`)
	feed("", `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"tu1","name":"f"}}`)
	feed("", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"a\":"}}`)
	feed("", `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"1}"}}`)
	feed("", `{"type":"content_block_stop","index":1}`)
	feed("", `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":8}}`)
	feed("", `{"type":"message_stop"}`)

	s := string(out)
	if !contains(s, `"role":"assistant"`) || !contains(s, `"content":"he"`) || !contains(s, `"content":"llo"`) {
		t.Fatalf("missing text deltas:\n%s", s)
	}
	if !contains(s, `"id":"tu1"`) || !contains(s, `"name":"f"`) || !contains(s, `{\"a\":`) || !contains(s, `1}`) {
		t.Fatalf("missing tool deltas:\n%s", s)
	}
	if !contains(s, `"finish_reason":"tool_calls"`) || !contains(s, `"prompt_tokens":10`) || !contains(s, `"completion_tokens":8`) {
		t.Fatalf("missing finish/usage:\n%s", s)
	}
	// message_stop 输出 [DONE]
	if !contains(s, "[DONE]") {
		t.Fatalf("missing DONE:\n%s", s)
	}
}

func TestOpenUpToAnthDown_TextAndToolStream(t *testing.T) {
	x := NewOpenUpToAnthDown("claude-x")
	var out []byte
	done := false
	feed := func(data string) {
		b, d, err := x.Transform("", data)
		if err != nil {
			t.Fatalf("transform err: %v", err)
		}
		out = append(out, b...)
		done = done || d
	}
	feed(`{"id":"c1","object":"chat.completion.chunk","model":"gpt-x","choices":[{"index":0,"delta":{"role":"assistant","content":"he"}}]}`)
	feed(`{"id":"c1","object":"chat.completion.chunk","model":"gpt-x","choices":[{"index":0,"delta":{"content":"llo"}}]}`)
	feed(`{"id":"c1","object":"chat.completion.chunk","model":"gpt-x","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c9","type":"function","function":{"name":"f","arguments":""}}]}}]}`)
	feed(`{"id":"c1","object":"chat.completion.chunk","model":"gpt-x","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"a\":1}"}}]}}]}`)
	feed(`{"id":"c1","object":"chat.completion.chunk","model":"gpt-x","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":11,"completion_tokens":9}}`)
	feed("[DONE]")
	if !done {
		t.Fatal("expected done after [DONE]")
	}

	s := string(out)
	for _, want := range []string{
		`"type":"message_start"`,
		`"type":"content_block_start"`, `"type":"text"`,
		`"type":"text_delta","text":"he"`, `"text_delta","text":"llo"`,
		`"type":"tool_use","id":"c9","name":"f"`,
		`"type":"input_json_delta","partial_json":"{\"a\":1}"`,
		`"stop_reason":"tool_use"`,
		`"type":"message_stop"`,
	} {
		if !contains(s, want) {
			t.Fatalf("missing %s in:\n%s", want, s)
		}
	}
}

func countOccurrences(s, sub string) int {
	n := 0
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			n++
		}
	}
	return n
}

// 回归测试：OpenAI 上游多个文本 chunk 必须合并为同一个 anthropic text 块，
// 不能每个 chunk 都重建块（否则 Claude Code 会把一句话渲染成多行）。
func TestOpenUpToAnthDownTextChunksMergeIntoSingleBlock(t *testing.T) {
	x := NewOpenUpToAnthDown("claude-x")
	var out []byte
	feed := func(data string) {
		b, d, err := x.Transform("", data)
		if err != nil {
			t.Fatalf("transform err: %v", err)
		}
		out = append(out, b...)
		if d {
			t.Fatal("unexpected done before [DONE]")
		}
	}
	feed(`{"id":"c1","object":"chat.completion.chunk","model":"gpt-x","choices":[{"index":0,"delta":{"role":"assistant","content":"Hel"}}]}`)
	feed(`{"id":"c1","object":"chat.completion.chunk","model":"gpt-x","choices":[{"index":0,"delta":{"content":"lo"}}]}`)
	feed(`{"id":"c1","object":"chat.completion.chunk","model":"gpt-x","choices":[{"index":0,"delta":{"content":"！"}}]}`)
	s := string(out)
	// 整个流必须只出现一个 text content_block_start，且 [DONE] 之前不得出现任何 content_block_stop
	if got := countOccurrences(s, `"type":"content_block_start"`); got != 1 {
		t.Fatalf("want exactly 1 content_block_start, got %d in:\n%s", got, s)
	}
	if got := countOccurrences(s, `"type":"content_block_stop"`); got != 0 {
		t.Fatalf("want 0 content_block_stop before done, got %d in:\n%s", got, s)
	}
	// 三个 chunk 的文本都在 index 0 的同一个 text 块里
	for _, want := range []string{
		`"type":"content_block_start"`, `"type":"text"`,
		`"type":"text_delta","text":"Hel"`,
		`"type":"text_delta","text":"lo"`,
		`"type":"text_delta","text":"！"`,
		`"index":0`,
	} {
		if !contains(s, want) {
			t.Fatalf("missing %s in:\n%s", want, s)
		}
	}
	feed(`{"id":"c1","object":"chat.completion.chunk","model":"gpt-x","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`)
	feed(`{"id":"c1","object":"chat.completion.chunk","model":"gpt-x","choices":[],"usage":{"prompt_tokens":3,"completion_tokens":4}}`)
	b, d, err := x.Transform("", "[DONE]")
	if err != nil {
		t.Fatalf("transform err: %v", err)
	}
	out = append(out, b...)
	if !d {
		t.Fatal("expected done after [DONE]")
	}
	// [DONE] 收尾应恰好关闭 text 块并发出 message_stop
	if got := countOccurrences(string(out), `"type":"content_block_stop"`); got != 1 {
		t.Fatalf("want 1 content_block_stop total, got %d in:\n%s", got, string(out))
	}
	if !contains(string(out), `"type":"message_stop"`) {
		t.Fatalf("missing message_stop in:\n%s", string(out))
	}
}

func TestStreamUsageObservers(t *testing.T) {
	o := &OpenAIStreamUsage{}
	o.Observe(`{"choices":[{"delta":{}}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`)
	o.Observe(`{"choices":[],"usage":{"prompt_tokens":5,"completion_tokens":9,"prompt_tokens_details":{"cached_tokens":3}}}`)
	// Prompt 为完整输入（含缓存命中 3）
	if o.U.Prompt != 5 || o.U.Completion != 9 || o.U.CacheRead != 3 {
		t.Fatalf("openai stream usage: %+v", o.U)
	}
	a := &AnthropicStreamUsage{}
	a.Observe("message_start", `{"type":"message_start","message":{"usage":{"input_tokens":7,"cache_read_input_tokens":1,"cache_creation_input_tokens":2}}}`)
	a.Observe("message_delta", `{"type":"message_delta","usage":{"output_tokens":6}}`)
	if a.U.Prompt != 7 || a.U.Completion != 6 || a.U.CacheRead != 1 || a.U.CacheWrite != 2 {
		t.Fatalf("anthropic stream usage: %+v", a.U)
	}
}

func TestRoundTripAnthropicToOpenAIToAnthropic(t *testing.T) {
	ar := &AnthropicRequest{
		Model:     "m",
		MaxTokens: 100,
		System:    mustJSON(t, "sys"),
		Messages: []AnthropicMessage{
			{Role: "user", Content: mustJSON(t, "hi")},
		},
	}
	or, err := AnthropicToOpenAIRequest(ar)
	if err != nil {
		t.Fatal(err)
	}
	ar2, err := OpenAIToAnthropicRequest(or)
	if err != nil {
		t.Fatal(err)
	}
	if ar2.Model != "m" || ar2.MaxTokens != 100 || len(ar2.Messages) != 1 {
		t.Fatalf("roundtrip mismatch: %+v", ar2)
	}
	b, _ := ParseBlocks(ar2.Messages[0].Content)
	if len(b) != 1 || b[0].Text != "hi" {
		t.Fatalf("roundtrip content: %+v", b)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
