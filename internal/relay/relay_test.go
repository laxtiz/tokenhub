package relay_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"tokenhub/internal/auth"
	"tokenhub/internal/db"
	"tokenhub/internal/relay"
)

// ---- 测试环境搭建 ----

type env struct {
	DB     *gorm.DB
	Engine *gin.Engine
	Logs   *db.LogWriter
}

// newEnv 搭建完整网关：openaiHandler / anthHandler 为 mock 上游行为。
func newEnv(t *testing.T, openaiHandler, anthHandler http.HandlerFunc) *env {
	t.Helper()
	gin.SetMode(gin.TestMode)

	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	logs := db.NewLogWriter(database)
	t.Cleanup(logs.Close)

	openaiSrv := httptest.NewServer(openaiHandler)
	anthSrv := httptest.NewServer(anthHandler)
	t.Cleanup(openaiSrv.Close)
	t.Cleanup(anthSrv.Close)

	// 用户 + 下游 Key
	u := &db.User{Username: "u1", Role: "user"}
	database.Create(u)
	database.Create(&db.DownstreamKey{UserID: u.ID, Name: "test", KeyHash: auth.HashKey("th-test"), KeyPrefix: "th-test"})

	// 供应商
	pOpen := &db.Provider{Name: "mock-openai", Type: "openai", BaseURL: openaiSrv.URL}
	pAnth := &db.Provider{Name: "mock-anthropic", Type: "anthropic", BaseURL: anthSrv.URL}
	database.Create(pOpen)
	database.Create(pAnth)
	database.Create(&db.ProviderKey{ProviderID: pOpen.ID, APIKey: "k1"})
	database.Create(&db.ProviderKey{ProviderID: pOpen.ID, APIKey: "k2"})
	database.Create(&db.ProviderKey{ProviderID: pAnth.ID, APIKey: "a1"})

	// 模型与渠道
	m1 := &db.Model{Name: "gpt-x", InputPrice: 3, OutputPrice: 15, CacheReadPrice: 0.3, CacheWritePrice: 3.75, Currency: "USD"}
	database.Create(m1)
	database.Create(&db.ModelChannel{ModelID: m1.ID, ProviderID: pOpen.ID, UpstreamModel: "gpt-4o", Priority: 1})

	m2 := &db.Model{Name: "cross", InputPrice: 3, OutputPrice: 15, Currency: "USD"}
	database.Create(m2)
	database.Create(&db.ModelChannel{ModelID: m2.ID, ProviderID: pAnth.ID, UpstreamModel: "claude-3", Priority: 1})

	m3 := &db.Model{Name: "fb", InputPrice: 3, OutputPrice: 15, Currency: "USD"}
	database.Create(m3)
	database.Create(&db.ModelChannel{ModelID: m3.ID, ProviderID: pOpen.ID, UpstreamModel: "gpt-4o", Priority: 1})
	database.Create(&db.ModelChannel{ModelID: m3.ID, ProviderID: pAnth.ID, UpstreamModel: "claude-3", Priority: 2})

	rl := relay.New(database, logs)
	dl := auth.DownstreamAuth(database)
	engine := gin.New()
	engine.POST("/v1/chat/completions", dl, func(c *gin.Context) { rl.Handle(c, "openai") })
	engine.POST("/v1/messages", dl, func(c *gin.Context) { rl.Handle(c, "anthropic") })

	return &env{DB: database, Engine: engine, Logs: logs}
}

func postJSON(t *testing.T, eng any, path string, body map[string]any) (*httptest.ResponseRecorder, map[string]any) {
	var engine *gin.Engine
	switch v := eng.(type) {
	case *env:
		engine = v.Engine
	case *gin.Engine:
		engine = v
	}
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer th-test")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	return w, resp
}

func openaiReq(model string, stream bool) map[string]any {
	return map[string]any{
		"model":    model,
		"messages": []map[string]any{{"role": "user", "content": "hi"}},
		"stream":   stream,
	}
}

func anthropicReq(model string, stream bool) map[string]any {
	return map[string]any{
		"model":      model,
		"max_tokens": 100,
		"messages":   []map[string]any{{"role": "user", "content": "hi"}},
		"stream":     stream,
	}
}

func openaiCompletionJSON(model, text string) map[string]any {
	return map[string]any{
		"id": "cmpl-1", "object": "chat.completion", "model": model,
		"choices": []map[string]any{{
			"index":         0,
			"message":       map[string]any{"role": "assistant", "content": text},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 4, "total_tokens": 14,
			"prompt_tokens_details": map[string]any{"cached_tokens": 3}},
	}
}

func anthropicMessageJSON(model, text string) map[string]any {
	return map[string]any{
		"id": "msg-1", "type": "message", "role": "assistant", "model": model,
		"content":     []map[string]any{{"type": "text", "text": text}},
		"stop_reason": "end_turn",
		"usage": map[string]any{"input_tokens": 8, "output_tokens": 6,
			"cache_read_input_tokens": 2, "cache_creation_input_tokens": 1},
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// ---- 用例 ----

// openai → openai 透传：非流式 + 计费（缓存 token 单独计价）+ 日志。
func TestOpenAIToOpenAI_NonStream(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any
	e := newEnv(t,
		func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			b, _ := io.ReadAll(r.Body)
			json.Unmarshal(b, &gotBody)
			writeJSON(w, openaiCompletionJSON("gpt-4o", "hello!"))
		},
		nil,
	)

	w, resp := postJSON(t, e, "/v1/chat/completions", openaiReq("gpt-x", false))
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if !strings.HasPrefix(gotAuth, "Bearer k") {
		t.Fatalf("bad auth header: %q", gotAuth)
	}
	if resp["object"] != "chat.completion" {
		t.Fatalf("bad response: %v", resp)
	}
	// 下游响应的 model 必须是内部设定的下游模型名，而非上游模型名
	if resp["model"] != "gpt-x" {
		t.Fatalf("downstream model should be rewritten: %v", resp["model"])
	}

	// 透传校验：model 已映射为上游名
	if gotBody["model"] != "gpt-4o" {
		t.Fatalf("upstream model not mapped: %v", gotBody["model"])
	}

	// 计费校验：入库存完整输入 tokens（10），计费时拆分缓存
	// → (未缓存输入 (10-3)*3 + 缓存读 3*0.3 + 输出 4*15) / 1e6
	e.Logs.Flush()
	var rl db.RequestLog
	e.DB.Last(&rl)
	if rl.Status != 200 || rl.PromptTokens != 10 || rl.CompletionTokens != 4 || rl.CacheReadTokens != 3 {
		t.Fatalf("bad request log: %+v", rl)
	}
	wantCost := ((10-3)*3 + 3*0.3 + 4*15) / 1_000_000
	if diff := rl.Cost - wantCost; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("cost mismatch: got %v want %v", rl.Cost, wantCost)
	}
	var ups []db.UpstreamLog
	e.DB.Where("trace_id = ?", rl.TraceID).Find(&ups)
	if len(ups) != 1 || ups[0].StatusCode != 200 || ups[0].ProviderType != "openai" {
		t.Fatalf("bad upstream logs: %+v", ups)
	}
}

// openai 下游 → anthropic 上游：请求/响应双向非流式转换。
func TestOpenAIDown_AnthropicUp_NonStream(t *testing.T) {
	var gotBody map[string]any
	e := newEnv(t,
		nil,
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/messages" {
				t.Errorf("bad path: %s", r.URL.Path)
			}
			if r.Header.Get("x-api-key") == "" {
				t.Errorf("missing x-api-key")
			}
			b, _ := io.ReadAll(r.Body)
			json.Unmarshal(b, &gotBody)
			writeJSON(w, anthropicMessageJSON("claude-3", "bonjour"))
		},
	)

	w, resp := postJSON(t, e, "/v1/chat/completions", openaiReq("cross", false))
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	// 上游收到 anthropic 格式
	if gotBody["model"] != "claude-3" || gotBody["max_tokens"] == nil {
		t.Fatalf("bad upstream anthropic body: %v", gotBody)
	}
	msgs, _ := gotBody["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("bad messages: %v", msgs)
	}
	// 下游收到 openai 格式
	if resp["object"] != "chat.completion" {
		t.Fatalf("bad downstream format: %v", resp)
	}
	choices, _ := resp["choices"].([]any)
	c0 := choices[0].(map[string]any)
	if c0["finish_reason"] != "stop" {
		t.Fatalf("finish_reason: %v", c0["finish_reason"])
	}
	// OpenAI 语义：prompt_tokens = input(8) + cache_read(2) + cache_write(1) = 11
	usage := resp["usage"].(map[string]any)
	if usage["prompt_tokens"] != float64(11) || usage["completion_tokens"] != float64(6) {
		t.Fatalf("usage: %v", usage)
	}
	// 缓存 token 应体现在 details
	details := usage["prompt_tokens_details"].(map[string]any)
	if details["cached_tokens"] != float64(2) {
		t.Fatalf("cached tokens: %v", details)
	}
}

// 专门验证 anthropic 下游 + openai 上游。
func TestAnthropicDown_OpenAIUp(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, _ := db.Open(filepath.Join(t.TempDir(), "t.db"))
	logs := db.NewLogWriter(database)
	defer logs.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, openaiCompletionJSON("gpt-4o", "hi there"))
	}))
	defer srv.Close()

	u := &db.User{Username: "u1"}
	database.Create(u)
	database.Create(&db.DownstreamKey{UserID: u.ID, KeyHash: auth.HashKey("th-test")})
	p := &db.Provider{Name: "p", Type: "openai", BaseURL: srv.URL}
	database.Create(p)
	database.Create(&db.ProviderKey{ProviderID: p.ID, APIKey: "k1"})
	m := &db.Model{Name: "mx", InputPrice: 1, OutputPrice: 1}
	database.Create(m)
	database.Create(&db.ModelChannel{ModelID: m.ID, ProviderID: p.ID, UpstreamModel: "gpt-4o", Priority: 1})

	rl := relay.New(database, logs)
	dl := auth.DownstreamAuth(database)
	engine := gin.New()
	engine.POST("/v1/messages", dl, func(c *gin.Context) { rl.Handle(c, "anthropic") })

	b, _ := json.Marshal(anthropicReq("mx", false))
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(b))
	req.Header.Set("x-api-key", "th-test")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["type"] != "message" || resp["stop_reason"] != "end_turn" {
		t.Fatalf("bad anthropic response: %v", resp)
	}
	content, _ := resp["content"].([]any)
	if len(content) != 1 || content[0].(map[string]any)["text"] != "hi there" {
		t.Fatalf("bad content: %v", content)
	}
	// Anthropic 语义：input_tokens = prompt(10) - cached(3) = 7
	usage := resp["usage"].(map[string]any)
	if usage["input_tokens"] != float64(7) || usage["cache_read_input_tokens"] != float64(3) {
		t.Fatalf("bad usage: %v", usage)
	}
}

// 流式：anthropic 上游 → openai 下游。
func TestStream_AnthropicUp_OpenAIDown(t *testing.T) {
	e := newEnv(t,
		nil,
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			f := w.(http.Flusher)
			events := []string{
				`{"type":"message_start","message":{"id":"msg_1","role":"assistant","model":"claude-3","content":[],"usage":{"input_tokens":10}}}`,
				`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hel"}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"lo"}}`,
				`{"type":"content_block_stop","index":0}`,
				`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`,
				`{"type":"message_stop"}`,
			}
			for _, ev := range events {
				w.Write([]byte("data: " + ev + "\n\n"))
				f.Flush()
			}
		},
	)

	reqBody := openaiReq("cross", true)
	w, _ := postJSON(t, e, "/v1/chat/completions", reqBody)
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"content":"Hel"`) || !strings.Contains(body, `"content":"lo"`) {
		t.Fatalf("missing text deltas:\n%s", body)
	}
	if strings.Contains(body, `"model":"claude-3"`) || !strings.Contains(body, `"model":"cross"`) {
		t.Fatalf("model should be rewritten to downstream name:\n%s", body)
	}
	if !strings.Contains(body, `"finish_reason":"stop"`) {
		t.Fatalf("missing finish:\n%s", body)
	}
	if !strings.Contains(body, `"prompt_tokens":10`) || !strings.Contains(body, `"completion_tokens":5`) {
		t.Fatalf("missing usage:\n%s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("missing DONE")
	}

	// 用量应正确落库
	e.Logs.Flush()
	var rl db.RequestLog
	e.DB.Last(&rl)
	if rl.PromptTokens != 10 || rl.CompletionTokens != 5 {
		t.Fatalf("bad log: %+v", rl)
	}
}

// Key 轮询：第一个 key 401 → 自动换第二个 key 成功，且失败 key 被标记 invalid。
func TestKeyRotation_OnAuthFailure(t *testing.T) {
	// usableKeys 轮询起点为 counter%len，首个请求从第 2 个 key 开始，
	// 因此让第 2 个 key 失败、第 1 个成功来覆盖轮换路径。
	gin.SetMode(gin.TestMode)
	database, _ := db.Open(filepath.Join(t.TempDir(), "t.db"))
	logs := db.NewLogWriter(database)
	defer logs.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer k2" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":{"message":"bad key"}}`))
			return
		}
		writeJSON(w, openaiCompletionJSON("gpt-4o", "ok"))
	}))
	defer srv.Close()

	u := &db.User{Username: "u1"}
	database.Create(u)
	database.Create(&db.DownstreamKey{UserID: u.ID, KeyHash: auth.HashKey("th-test")})
	p := &db.Provider{Name: "p", Type: "openai", BaseURL: srv.URL}
	database.Create(p)
	k1 := &db.ProviderKey{ProviderID: p.ID, APIKey: "k1"}
	k2 := &db.ProviderKey{ProviderID: p.ID, APIKey: "k2"}
	database.Create(k1)
	database.Create(k2)
	m := &db.Model{Name: "mx", InputPrice: 1, OutputPrice: 1}
	database.Create(m)
	database.Create(&db.ModelChannel{ModelID: m.ID, ProviderID: p.ID, UpstreamModel: "gpt-4o", Priority: 1})

	rl := relay.New(database, logs)
	dl := auth.DownstreamAuth(database)
	engine := gin.New()
	engine.POST("/v1/chat/completions", dl, func(c *gin.Context) { rl.Handle(c, "openai") })

	w, _ := postJSON(t, engine, "/v1/chat/completions", openaiReq("mx", false))
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	logs.Flush()
	var logRow db.RequestLog
	database.Last(&logRow)
	if logRow.AttemptCount != 2 {
		t.Fatalf("want 2 attempts, got %d", logRow.AttemptCount)
	}
	var ups []db.UpstreamLog
	database.Where("trace_id = ?", logRow.TraceID).Order("attempt").Find(&ups)
	if len(ups) != 2 || ups[0].StatusCode != 401 || ups[0].ErrType != "auth" || ups[1].StatusCode != 200 {
		t.Fatalf("bad upstream chain: %+v", ups)
	}
	var k2After db.ProviderKey
	database.First(&k2After, k2.ID)
	if k2After.Status != "invalid" {
		t.Fatalf("k2 should be invalid, got %s", k2After.Status)
	}
}

// 渠道降级：priority 1 的渠道 500 → 自动降级到 priority 2 渠道成功。
func TestChannelFallback(t *testing.T) {
	e := newEnv(t,
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":{"message":"boom"}}`))
		},
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, anthropicMessageJSON("claude-3", "fallback!"))
		},
	)

	w, resp := postJSON(t, e, "/v1/chat/completions", openaiReq("fb", false))
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	choices, _ := resp["choices"].([]any)
	msg := choices[0].(map[string]any)["message"].(map[string]any)
	if msg["content"] != "fallback!" {
		t.Fatalf("expected fallback response: %v", msg)
	}
	e.Logs.Flush()
	var logRow db.RequestLog
	e.DB.Last(&logRow)
	if logRow.AttemptCount != 3 {
		t.Fatalf("want 3 attempts, got %d", logRow.AttemptCount)
	}
	var ups []db.UpstreamLog
	e.DB.Where("trace_id = ?", logRow.TraceID).Order("attempt").Find(&ups)
	// openai 渠道 2 个 key 各失败一次（500），随后降级到 anthropic 渠道成功
	if len(ups) != 3 || ups[0].ProviderType != "openai" || ups[1].ProviderType != "openai" || ups[2].ProviderType != "anthropic" {
		t.Fatalf("bad chain: %+v", ups)
	}
	if ups[2].StatusCode != 200 {
		t.Fatalf("last attempt should succeed: %+v", ups[2])
	}
}

// 限流轮询：429 触发 key 冷却，同渠道下一个 key 接管。
func TestRateLimitRotation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, _ := db.Open(filepath.Join(t.TempDir(), "t.db"))
	logs := db.NewLogWriter(database)
	defer logs.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer k2" {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":{"message":"rate limited"}}`))
			return
		}
		writeJSON(w, openaiCompletionJSON("gpt-4o", "ok"))
	}))
	defer srv.Close()

	u := &db.User{Username: "u1"}
	database.Create(u)
	database.Create(&db.DownstreamKey{UserID: u.ID, KeyHash: auth.HashKey("th-test")})
	p := &db.Provider{Name: "p", Type: "openai", BaseURL: srv.URL}
	database.Create(p)
	database.Create(&db.ProviderKey{ProviderID: p.ID, APIKey: "k1"})
	database.Create(&db.ProviderKey{ProviderID: p.ID, APIKey: "k2"})
	m := &db.Model{Name: "mx", InputPrice: 1, OutputPrice: 1}
	database.Create(m)
	database.Create(&db.ModelChannel{ModelID: m.ID, ProviderID: p.ID, UpstreamModel: "gpt-4o", Priority: 1})

	rl := relay.New(database, logs)
	dl := auth.DownstreamAuth(database)
	engine := gin.New()
	engine.POST("/v1/chat/completions", dl, func(c *gin.Context) { rl.Handle(c, "openai") })

	w, _ := postJSON(t, engine, "/v1/chat/completions", openaiReq("mx", false))
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	logs.Flush()
	var k2 db.ProviderKey
	database.Where("api_key = ?", "k2").First(&k2)
	if k2.Status != "rate_limited" || k2.CooldownUntil == nil {
		t.Fatalf("k2 should be cooling down: %+v", k2)
	}
	// 冷却期内的第二次请求直接使用 k1
	w2, _ := postJSON(t, engine, "/v1/chat/completions", openaiReq("mx", false))
	if w2.Code != 200 {
		t.Fatalf("second request status %d", w2.Code)
	}
	logs.Flush()
	var attempts int64
	var lr db.RequestLog
	database.Last(&lr)
	database.Model(&db.UpstreamLog{}).Where("trace_id = ?", lr.TraceID).Count(&attempts)
	if attempts != 1 {
		t.Fatalf("cooldown key should be skipped, got %d attempts", attempts)
	}
}

// 未授权的下游 key 应返回 401。
func TestBadDownstreamKey(t *testing.T) {
	e := newEnv(t, nil, nil)
	b, _ := json.Marshal(openaiReq("gpt-x", false))
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer wrong")
	w := httptest.NewRecorder()
	e.Engine.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

// 未知模型应返回 404。
func TestUnknownModel(t *testing.T) {
	e := newEnv(t, nil, nil)
	w, _ := postJSON(t, e, "/v1/chat/completions", openaiReq("nope", false))
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
}
