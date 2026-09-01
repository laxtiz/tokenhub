package mcp_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"tokenhub/internal/auth"
	"tokenhub/internal/db"
	mcpserver "tokenhub/internal/mcp"
)

// newEnv 构造一个最小的 MCP 测试环境：两个用户、若干模型与日志。
func newEnv(t *testing.T) (*gin.Engine, *gorm.DB, string, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}

	uA := &db.User{Username: "alice", Role: "user"}
	uB := &db.User{Username: "bob", Role: "user"}
	database.Create(uA)
	database.Create(uB)

	plainA, hashA, prefixA := auth.GenerateDownstreamKey()
	plainB, hashB, prefixB := auth.GenerateDownstreamKey()
	database.Create(&db.DownstreamKey{UserID: uA.ID, Name: "a1", KeyHash: hashA, KeyPrefix: prefixA})
	database.Create(&db.DownstreamKey{UserID: uB.ID, Name: "b1", KeyHash: hashB, KeyPrefix: prefixB})

	// 一个可见模型 + 一个禁用模型（验证 disabled 过滤）
	database.Create(&db.Model{Name: "gpt-x", DisplayName: "GPT-X", InputPrice: 3, OutputPrice: 15, Currency: "USD"})
	database.Create(&db.Model{Name: "hidden", Disabled: true})

	// alice 的若干日志 + 一条属于 bob 的日志（验证 user 隔离）
	database.Create(&db.RequestLog{TraceID: "t-a-1", UserID: uA.ID, KeyID: 1, Model: "gpt-x", Status: 200, Cost: 0.01})
	database.Create(&db.RequestLog{TraceID: "t-a-2", UserID: uA.ID, KeyID: 1, Model: "gpt-x", Status: 500, Error: "boom"})
	database.Create(&db.RequestLog{TraceID: "t-b-1", UserID: uB.ID, KeyID: 2, Model: "gpt-x", Status: 200})

	eng := gin.New()
	eng.Any("/mcp", mcpserver.Handler(database))
	return eng, database, plainA, plainB
}

// doMCP 发送一个 JSON-RPC 请求到 /mcp，返回响应 body。
func doMCP(t *testing.T, eng *gin.Engine, bearer string, req map[string]any) map[string]any {
	t.Helper()
	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	if bearer != "" {
		httpReq.Header.Set("Authorization", "Bearer "+bearer)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	w := httptest.NewRecorder()
	eng.ServeHTTP(w, httpReq)
	raw, _ := io.ReadAll(w.Body)
	if w.Code >= 400 {
		t.Fatalf("mcp http %d: %s", w.Code, string(raw))
	}
	// 协议允许 SSE 形式：data: <json>\n\n。优先尝试按 SSE 解析。
	out := parseSSEOrJSON(raw)
	return out
}

func parseSSEOrJSON(raw []byte) map[string]any {
	for _, line := range bytes.Split(raw, []byte("\n")) {
		trim := bytes.TrimSpace(line)
		if bytes.HasPrefix(trim, []byte("data:")) {
			payload := bytes.TrimSpace(bytes.TrimPrefix(trim, []byte("data:")))
			if len(payload) == 0 {
				continue
			}
			var m map[string]any
			if err := json.Unmarshal(payload, &m); err == nil {
				return m
			}
		}
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return map[string]any{"_raw": string(raw)}
	}
	return m
}

// ---- 用例 ----

// 1. 缺鉴权 → 401
func TestMCPAuthMissing(t *testing.T) {
	eng, _, _, _ := newEnv(t)
	req := httptest.NewRequest(http.MethodPost, "/mcp",
		bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	w := httptest.NewRecorder()
	eng.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("期望 401,实际 %d: %s", w.Code, w.Body.String())
	}
}

// 2. initialize 协议握手
func TestMCPInitialize(t *testing.T) {
	eng, _, plainA, _ := newEnv(t)
	resp := doMCP(t, eng, plainA, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-06-18",
			"clientInfo":      map[string]any{"name": "tester", "version": "0.0.1"},
			"capabilities":    map[string]any{},
		},
	})
	if _, ok := resp["result"]; !ok {
		t.Fatalf("initialize 缺 result: %v", resp)
	}
	if _, ok := resp["error"]; ok {
		t.Fatalf("initialize 返回 error: %v", resp["error"])
	}
}

// 3. tools/list 应返回 6 个工具
func TestMCPListTools(t *testing.T) {
	eng, _, plainA, _ := newEnv(t)
	// 先 initialize 维持 session
	doMCP(t, eng, plainA, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}},
	})
	resp := doMCP(t, eng, plainA, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/list",
	})
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("缺 result: %v", resp)
	}
	tools, _ := result["tools"].([]any)
	if len(tools) != 6 {
		t.Fatalf("期望 6 个工具,实际 %d (%v)", len(tools), tools)
	}
	names := map[string]bool{}
	for _, t := range tools {
		m := t.(map[string]any)
		names[m["name"].(string)] = true
	}
	for _, want := range []string{"list_models", "list_my_keys", "list_my_logs", "get_trace_detail", "get_my_stats", "get_my_account"} {
		if !names[want] {
			t.Fatalf("缺少工具 %s", want)
		}
	}
}

// 4. list_models 应过滤掉 disabled 模型
func TestMCPListModels(t *testing.T) {
	eng, _, plainA, _ := newEnv(t)
	resp := callTool(t, eng, plainA, "list_models", map[string]any{})
	out := resultStructured(t, resp)
	models, _ := out["models"].([]any)
	if len(models) != 1 {
		t.Fatalf("期望 1 个可见模型,实际 %d: %v", len(models), models)
	}
	m := models[0].(map[string]any)
	if m["name"] != "gpt-x" {
		t.Fatalf("应返回 gpt-x,实际 %v", m["name"])
	}
}

// 5. list_my_keys 必须不返回 hash/plain,只能是元数据
func TestMCPListMyKeysNoSecret(t *testing.T) {
	eng, _, plainA, _ := newEnv(t)
	resp := callTool(t, eng, plainA, "list_my_keys", map[string]any{})
	out := resultStructured(t, resp)
	keys, _ := out["keys"].([]any)
	if len(keys) != 1 {
		t.Fatalf("alice 应该有 1 个 key,实际 %d", len(keys))
	}
	for _, field := range []string{"key_hash", "api_key", "plain_key", "plain"} {
		if _, ok := keys[0].(map[string]any)[field]; ok {
			t.Fatalf("list_my_keys 不应返回 %s", field)
		}
	}
	if keys[0].(map[string]any)["name"] != "a1" {
		t.Fatalf("alice 的 key 名应为 a1,实际 %v", keys[0])
	}
}

// 6. list_my_logs 必须按 user_id 过滤:alice 看不到 bob 的 trace
func TestMCPListMyLogsIsolation(t *testing.T) {
	eng, _, plainA, plainB := newEnv(t)
	resp := callTool(t, eng, plainA, "list_my_logs", map[string]any{"days": 7})
	out := resultStructured(t, resp)
	items, _ := out["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("alice 应有 2 条日志,实际 %d: %v", len(items), items)
	}
	for _, it := range items {
		m := it.(map[string]any)
		if m["trace_id"] == "t-b-1" {
			t.Fatalf("越权:alice 看到了 bob 的 trace t-b-1")
		}
	}

	// 同时验证 bob 看不到 alice 的
	respB := callTool(t, eng, plainB, "list_my_logs", map[string]any{"days": 7})
	outB := resultStructured(t, respB)
	itemsB, _ := outB["items"].([]any)
	if len(itemsB) != 1 {
		t.Fatalf("bob 应有 1 条日志,实际 %d", len(itemsB))
	}
	if itemsB[0].(map[string]any)["trace_id"] != "t-b-1" {
		t.Fatalf("bob 应只能看到 t-b-1,实际 %v", itemsB[0])
	}
}

// 7. get_trace_detail 越权访问应返回 not_found
func TestMCPTraceCrossUser(t *testing.T) {
	eng, _, plainA, _ := newEnv(t)
	resp := callTool(t, eng, plainA, "get_trace_detail", map[string]any{"trace_id": "t-b-1"})
	out := resultStructured(t, resp)
	if v, ok := out["not_found"]; !ok || v != true {
		t.Fatalf("越权访问 bob 的 trace 应返回 not_found=true,实际 %v", out)
	}
}

// 8. get_my_account 必须返回当前用户身份
func TestMCPGetMyAccount(t *testing.T) {
	eng, _, plainA, _ := newEnv(t)
	resp := callTool(t, eng, plainA, "get_my_account", map[string]any{})
	out := resultStructured(t, resp)
	acc, _ := out["account"].(map[string]any)
	if acc["username"] != "alice" {
		t.Fatalf("期望 alice,实际 %v", acc)
	}
}

// 9. get_my_stats 传 days 参数不应报类型错误
func TestMCPGetMyStatsWithDays(t *testing.T) {
	eng, _, plainA, _ := newEnv(t)
	resp := callTool(t, eng, plainA, "get_my_stats", map[string]any{"days": 7})
	if errObj, ok := resp["result"].(map[string]any)["isError"]; ok && errObj.(bool) {
		t.Fatalf("get_my_stats(days=7) 返回 isError=true: %v", resp)
	}
	out := resultStructured(t, resp)
	if d, ok := out["days"].(float64); !ok || int(d) != 7 {
		t.Fatalf("期望 days=7,实际 %v", out["days"])
	}
}

// ---- 工具函数 ----

// callTool 走完整的 initialize + tools/call。
func callTool(t *testing.T, eng *gin.Engine, bearer, name string, args map[string]any) map[string]any {
	t.Helper()
	doMCP(t, eng, bearer, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}},
	})
	return doMCP(t, eng, bearer, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": args},
	})
}

// resultStructured 从 tools/call 响应里解出 structuredContent。
func resultStructured(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("缺 result: %v", resp)
	}
	if errObj, ok := res["isError"]; ok && errObj.(bool) {
		t.Fatalf("tool 返回 isError=true: %v", res)
	}
	sc, ok := res["structuredContent"].(map[string]any)
	if !ok {
		// 回退: 从 content[].text 解析 JSON
		if contents, ok := res["content"].([]any); ok && len(contents) > 0 {
			t0 := contents[0].(map[string]any)["text"].(string)
			var m map[string]any
			if err := json.Unmarshal([]byte(t0), &m); err == nil {
				return m
			}
		}
		t.Fatalf("无法解析 tool 结果: %v", res)
	}
	return sc
}