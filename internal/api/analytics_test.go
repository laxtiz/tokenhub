package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gorm.io/gorm"

	"tokenhub/internal/db"
)

// logShell 包装 *gorm.DB，对外只暴露 Create，analytics_test 不再 import gorm。
type logShell struct{ db *gorm.DB }

func (s *logShell) Create(v any) { s.db.Create(v) }

func mkLog(t *testing.T, ls *logShell, traceID, model string, status int,
	cost float64, prompt, completion, latency int64, dayOffset int,
	userID, providerID uint, errType string, statusCode int) {
	t.Helper()
	ls.Create(&db.UpstreamLog{
		TraceID: traceID, Attempt: 1, ProviderID: providerID,
		ProviderName: "p", ProviderType: "openai",
		UpstreamModel: model, StatusCode: statusCode, ErrType: errType,
		PromptTokens: prompt, CompletionTokens: completion, LatencyMS: latency,
		CreatedAt: time.Now().AddDate(0, 0, -dayOffset),
	})
	ls.Create(&db.RequestLog{
		TraceID: traceID, UserID: userID, DownstreamFormat: "openai",
		Model: model, Status: status, Cost: cost,
		PromptTokens: prompt, CompletionTokens: completion,
		LatencyMS: latency, CreatedAt: time.Now().AddDate(0, 0, -dayOffset),
	})
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	return m
}

func TestAdminAnalytics_BasicTotals(t *testing.T) {
	eng, database, authHdr := newAdminTestEnv(t)
	ua := &db.User{Username: "alice", Role: "user"}
	database.Create(ua)
	p1 := &db.Provider{Name: "p1", Type: "openai", BaseURL: "http://unused"}
	database.Create(p1)

	ls := &logShell{database}
	mkLog(t, ls, "t1", "gpt-x", 200, 0.1, 100, 50, 1000, 0, ua.ID, p1.ID, "none", 200)
	mkLog(t, ls, "t2", "gpt-x", 500, 0.0, 200, 0, 500, 1, ua.ID, p1.ID, "server", 500)
	mkLog(t, ls, "t3", "gpt-y", 200, 0.05, 50, 30, 800, 0, ua.ID, p1.ID, "none", 200)

	w, _ := doGET(eng, "/api/admin/analytics?days=7", authHdr)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	resp := decode(t, w)
	totals := resp["totals"].(map[string]any)
	if int(totals["requests"].(float64)) != 3 {
		t.Errorf("requests=%v want 3", totals["requests"])
	}
	if c := totals["cost"].(float64); c < 0.14 || c > 0.16 {
		t.Errorf("cost=%v want ~0.15", c)
	}
	if sr := totals["success_rate"].(float64); sr < 0.66 || sr > 0.67 {
		t.Errorf("success_rate=%v want ~0.667", sr)
	}
}

func TestAdminAnalytics_FilterByUser(t *testing.T) {
	eng, database, authHdr := newAdminTestEnv(t)
	ua := &db.User{Username: "alice", Role: "user"}
	database.Create(ua)
	ub := &db.User{Username: "bob", Role: "user"}
	database.Create(ub)
	p1 := &db.Provider{Name: "p1", Type: "openai", BaseURL: "http://unused"}
	database.Create(p1)

	ls := &logShell{database}
	mkLog(t, ls, "t1", "gpt-x", 200, 0.1, 100, 50, 1000, 0, ua.ID, p1.ID, "none", 200)
	mkLog(t, ls, "t2", "gpt-y", 200, 0.2, 100, 50, 1000, 0, ub.ID, p1.ID, "none", 200)

	w, _ := doGET(eng, fmt.Sprintf("/api/admin/analytics?days=7&user_id=%d", ua.ID), authHdr)
	resp := decode(t, w)
	totals := resp["totals"].(map[string]any)
	if int(totals["requests"].(float64)) != 1 {
		t.Errorf("requests=%v want 1", totals["requests"])
	}
}

func TestAdminAnalytics_FilterByProvider(t *testing.T) {
	eng, database, authHdr := newAdminTestEnv(t)
	ua := &db.User{Username: "alice", Role: "user"}
	database.Create(ua)
	p1 := &db.Provider{Name: "p1", Type: "openai", BaseURL: "http://1"}
	database.Create(p1)
	p2 := &db.Provider{Name: "p2", Type: "openai", BaseURL: "http://2"}
	database.Create(p2)

	ls := &logShell{database}
	mkLog(t, ls, "t1", "gpt-x", 200, 0.1, 100, 50, 1000, 0, ua.ID, p1.ID, "none", 200)
	mkLog(t, ls, "t2", "gpt-y", 200, 0.2, 100, 50, 1000, 0, ua.ID, p2.ID, "none", 200)

	w, _ := doGET(eng, fmt.Sprintf("/api/admin/analytics?days=7&provider_id=%d", p1.ID), authHdr)
	resp := decode(t, w)
	totals := resp["totals"].(map[string]any)
	if int(totals["requests"].(float64)) != 1 {
		t.Errorf("requests=%v want 1", totals["requests"])
	}
}

func TestAdminAnalytics_FilterByErrType(t *testing.T) {
	eng, database, authHdr := newAdminTestEnv(t)
	ua := &db.User{Username: "alice", Role: "user"}
	database.Create(ua)
	p1 := &db.Provider{Name: "p1", Type: "openai", BaseURL: "http://1"}
	database.Create(p1)

	ls := &logShell{database}
	mkLog(t, ls, "t1", "gpt-x", 200, 0.1, 100, 50, 1000, 0, ua.ID, p1.ID, "none", 200)
	mkLog(t, ls, "t2", "gpt-y", 500, 0.0, 200, 0, 500, 0, ua.ID, p1.ID, "rate_limit", 429)

	w, _ := doGET(eng, "/api/admin/analytics?days=7&err_type=rate_limit", authHdr)
	resp := decode(t, w)
	totals := resp["totals"].(map[string]any)
	if int(totals["requests"].(float64)) != 1 {
		t.Errorf("requests=%v want 1", totals["requests"])
	}
}

func TestAdminAnalytics_NoMatchReturnsEmpty(t *testing.T) {
	eng, _, authHdr := newAdminTestEnv(t)
	w, _ := doGET(eng, "/api/admin/analytics?days=7&err_type=auth", authHdr)
	if w.Code != 200 {
		t.Fatalf("status=%d", w.Code)
	}
	resp := decode(t, w)
	totals := resp["totals"].(map[string]any)
	if int(totals["requests"].(float64)) != 0 {
		t.Errorf("requests=%v want 0", totals["requests"])
	}
}

func TestAdminAnalytics_Unauthenticated(t *testing.T) {
	eng, _, _ := newAdminTestEnv(t)
	w, _ := doGET(eng, "/api/admin/analytics", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want 401", w.Code)
	}
}
