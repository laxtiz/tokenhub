package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"tokenhub/internal/db"
)

// registerTools 把所有只读工具注册到 server 上。
// 每个 handler 第一步都调用 identityFromCtx 拿到当前用户与共享 *gorm.DB，
// 并在所有查询中强制按 user_id 过滤。
// 使用包级 mcp.AddTool：输入/输出 JSON Schema 由 SDK 从泛型 In/Out 自动推导。
func registerTools(s *mcp.Server) {
	mcp.AddTool(s, toolListModels(), listModelsHandler)
	mcp.AddTool(s, toolListMyKeys(), listMyKeysHandler)
	mcp.AddTool(s, toolListMyLogs(), listMyLogsHandler)
	mcp.AddTool(s, toolGetTraceDetail(), getTraceDetailHandler)
	mcp.AddTool(s, toolGetMyStats(), getMyStatsHandler)
	mcp.AddTool(s, toolGetMyAccount(), getMyAccountHandler)
}

func parseDays(d int) int {
	if d <= 0 {
		return 7
	}
	if d > 90 {
		return 90
	}
	return d
}

func parseSize(n int) int {
	if n <= 0 {
		return 20
	}
	if n > 100 {
		return 100
	}
	return n
}

// ---- list_models ----

type ListModelsInput struct{}

type ModelInfo struct {
	Name             string  `json:"name" jsonschema:"模型名（下游调用时使用）"`
	DisplayName      string  `json:"display_name"`
	Description      string  `json:"description"`
	ContextLength    int     `json:"context_length"`
	SupportVision    bool    `json:"support_vision"`
	SupportTools     bool    `json:"support_tools"`
	SupportReasoning bool    `json:"support_reasoning"`
	InputPrice       float64 `json:"input_price_per_million"`
	OutputPrice      float64 `json:"output_price_per_million"`
	CacheReadPrice   float64 `json:"cache_read_price_per_million"`
	CacheWritePrice  float64 `json:"cache_write_price_per_million"`
	Currency         string  `json:"currency"`
}

type ListModelsResult struct {
	Models []ModelInfo `json:"models"`
	Total  int         `json:"total"`
}

func toolListModels() *mcp.Tool {
	return &mcp.Tool{
		Name: "list_models",
		Description: "列出当前 TokenHub 实例上所有可用模型（含价格、能力标识）。" +
			"返回的模型名是下游 /v1/chat/completions 与 /v1/messages 的 model 字段。",
	}
}

func listModelsHandler(ctx context.Context, _ *mcp.CallToolRequest, _ ListModelsInput) (*mcp.CallToolResult, ListModelsResult, error) {
	u, _, g, err := identityFromCtx(ctx)
	if err != nil {
		return nil, ListModelsResult{}, err
	}
	debugLog("list_models", u.Username, nil)

	var rows []db.Model
	g.WithContext(ctx).Where("disabled = ?", false).Order("name ASC").Find(&rows)
	out := ListModelsResult{Models: make([]ModelInfo, 0, len(rows)), Total: len(rows)}
	for _, m := range rows {
		out.Models = append(out.Models, ModelInfo{
			Name:             m.Name,
			DisplayName:      m.DisplayName,
			Description:      m.Description,
			ContextLength:    m.ContextLength,
			SupportVision:    m.SupportVision,
			SupportTools:     m.SupportTools,
			SupportReasoning: m.SupportReasoning,
			InputPrice:       m.InputPrice,
			OutputPrice:      m.OutputPrice,
			CacheReadPrice:   m.CacheReadPrice,
			CacheWritePrice:  m.CacheWritePrice,
			Currency:         m.Currency,
		})
	}
	return nil, out, nil
}

// ---- list_my_keys ----

type ListMyKeysInput struct{}

type KeyInfo struct {
	ID         uint       `json:"id"`
	Name       string     `json:"name"`
	KeyPrefix  string     `json:"key_prefix"`
	Disabled   bool       `json:"disabled"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

type ListMyKeysResult struct {
	Keys []KeyInfo `json:"keys"`
}

func toolListMyKeys() *mcp.Tool {
	return &mcp.Tool{
		Name:        "list_my_keys",
		Description: "列出当前用户的所有下游 API Key（仅元数据：id、name、prefix、状态；绝不返回明文或 hash）。",
	}
}

func listMyKeysHandler(ctx context.Context, _ *mcp.CallToolRequest, _ ListMyKeysInput) (*mcp.CallToolResult, ListMyKeysResult, error) {
	u, _, g, err := identityFromCtx(ctx)
	if err != nil {
		return nil, ListMyKeysResult{}, err
	}
	debugLog("list_my_keys", u.Username, nil)

	var rows []db.DownstreamKey
	g.WithContext(ctx).Where("user_id = ?", u.ID).Order("id DESC").Find(&rows)
	out := ListMyKeysResult{Keys: make([]KeyInfo, 0, len(rows))}
	for _, k := range rows {
		out.Keys = append(out.Keys, KeyInfo{
			ID:         k.ID,
			Name:       k.Name,
			KeyPrefix:  k.KeyPrefix,
			Disabled:   k.Disabled,
			LastUsedAt: k.LastUsedAt,
			CreatedAt:  k.CreatedAt,
		})
	}
	return nil, out, nil
}

// ---- list_my_logs ----

type ListMyLogsInput struct {
	Page    int    `json:"page,omitempty" jsonschema:"页码,从 1 开始,默认 1"`
	Size    int    `json:"size,omitempty" jsonschema:"每页条数,默认 20,最大 100"`
	Model   string `json:"model,omitempty" jsonschema:"按模型名精确过滤"`
	Status  int    `json:"status,omitempty" jsonschema:"按 HTTP 状态码过滤,如 200/400/500"`
	Days    int    `json:"days,omitempty" jsonschema:"只看最近 N 天,默认 7,最大 90"`
	TraceID string `json:"trace_id,omitempty" jsonschema:"按 trace_id 精确查询"`
}

type LogRow struct {
	ID               int64     `json:"id"`
	TraceID          string    `json:"trace_id"`
	Model            string    `json:"model"`
	DownstreamFormat string    `json:"downstream_format"`
	Stream           bool      `json:"stream"`
	Status           int       `json:"status"`
	AttemptCount     int       `json:"attempt_count"`
	PromptTokens     int64     `json:"prompt_tokens"`
	CompletionTokens int64     `json:"completion_tokens"`
	CacheReadTokens  int64     `json:"cache_read_tokens"`
	CacheWriteTokens int64     `json:"cache_write_tokens"`
	Cost             float64   `json:"cost"`
	LatencyMS        int64     `json:"latency_ms"`
	FirstTokenMS     int64     `json:"first_token_ms"`
	Error            string    `json:"error,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

type ListMyLogsResult struct {
	Total int64    `json:"total"`
	Items []LogRow `json:"items"`
}

func toolListMyLogs() *mcp.Tool {
	return &mcp.Tool{
		Name:        "list_my_logs",
		Description: "分页查询当前用户的请求日志（仅元数据，不含消息内容）。支持按 model/status/trace_id/天数 过滤。",
	}
}

func listMyLogsHandler(ctx context.Context, _ *mcp.CallToolRequest, in ListMyLogsInput) (*mcp.CallToolResult, ListMyLogsResult, error) {
	u, _, g, err := identityFromCtx(ctx)
	if err != nil {
		return nil, ListMyLogsResult{}, err
	}
	debugLog("list_my_logs", u.Username, in)

	page := in.Page
	if page <= 0 {
		page = 1
	}
	size := parseSize(in.Size)
	days := parseDays(in.Days)
	since := time.Now().AddDate(0, 0, -days)

	q := g.WithContext(ctx).Model(&db.RequestLog{}).Where("user_id = ?", u.ID)
	if in.Model != "" {
		q = q.Where("model = ?", in.Model)
	}
	if in.Status > 0 {
		q = q.Where("status = ?", in.Status)
	}
	if in.TraceID != "" {
		q = q.Where("trace_id = ?", in.TraceID)
	}
	q = q.Where("created_at >= ?", since)

	var total int64
	q.Count(&total)

	var rows []db.RequestLog
	q.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&rows)

	out := ListMyLogsResult{Total: total, Items: make([]LogRow, 0, len(rows))}
	for _, r := range rows {
		out.Items = append(out.Items, LogRow{
			ID:               r.ID,
			TraceID:          r.TraceID,
			Model:            r.Model,
			DownstreamFormat: r.DownstreamFormat,
			Stream:           r.Stream,
			Status:           r.Status,
			AttemptCount:     r.AttemptCount,
			PromptTokens:     r.PromptTokens,
			CompletionTokens: r.CompletionTokens,
			CacheReadTokens:  r.CacheReadTokens,
			CacheWriteTokens: r.CacheWriteTokens,
			Cost:             r.Cost,
			LatencyMS:        r.LatencyMS,
			FirstTokenMS:     r.FirstTokenMS,
			Error:            r.Error,
			CreatedAt:        r.CreatedAt,
		})
	}
	return nil, out, nil
}

// ---- get_trace_detail ----

type GetTraceDetailInput struct {
	TraceID string `json:"trace_id" jsonschema:"trace_id（必填）"`
}

type UpstreamAttempt struct {
	Attempt          int    `json:"attempt"`
	ProviderName     string `json:"provider_name"`
	ProviderType     string `json:"provider_type"`
	UpstreamModel    string `json:"upstream_model"`
	StatusCode       int    `json:"status_code"`
	ErrType          string `json:"err_type"`
	Stream           bool   `json:"stream"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
	CacheReadTokens  int64  `json:"cache_read_tokens"`
	CacheWriteTokens int64  `json:"cache_write_tokens"`
	LatencyMS        int64  `json:"latency_ms"`
	Error            string `json:"error,omitempty"`
}

type TraceDetailResult struct {
	Request  LogRow           `json:"request"`
	Upstream []UpstreamAttempt `json:"upstream"`
	// NotFound 当 trace 不属于当前用户（或不存在）时为 true
	NotFound bool `json:"not_found,omitempty"`
}

func toolGetTraceDetail() *mcp.Tool {
	return &mcp.Tool{
		Name:        "get_trace_detail",
		Description: "查询单次调用的完整 trace：下游请求元数据 + 所有上游尝试。仅返回属于当前用户的 trace，越权访问返回 not_found=true。",
	}
}

func getTraceDetailHandler(ctx context.Context, _ *mcp.CallToolRequest, in GetTraceDetailInput) (*mcp.CallToolResult, TraceDetailResult, error) {
	u, _, g, err := identityFromCtx(ctx)
	if err != nil {
		return nil, TraceDetailResult{}, err
	}
	debugLog("get_trace_detail", u.Username, in)

	if in.TraceID == "" {
		return nil, TraceDetailResult{}, fmt.Errorf("trace_id 不能为空")
	}
	var r db.RequestLog
	if err := g.WithContext(ctx).Where("trace_id = ? AND user_id = ?", in.TraceID, u.ID).First(&r).Error; err != nil {
		// 越权或不存在一律当作"找不到"
		return nil, TraceDetailResult{NotFound: true}, nil
	}
	var ups []db.UpstreamLog
	g.WithContext(ctx).Where("trace_id = ?", in.TraceID).Order("attempt ASC").Find(&ups)

	out := TraceDetailResult{
		Request: LogRow{
			ID:               r.ID,
			TraceID:          r.TraceID,
			Model:            r.Model,
			DownstreamFormat: r.DownstreamFormat,
			Stream:           r.Stream,
			Status:           r.Status,
			AttemptCount:     r.AttemptCount,
			PromptTokens:     r.PromptTokens,
			CompletionTokens: r.CompletionTokens,
			CacheReadTokens:  r.CacheReadTokens,
			CacheWriteTokens: r.CacheWriteTokens,
			Cost:             r.Cost,
			LatencyMS:        r.LatencyMS,
			FirstTokenMS:     r.FirstTokenMS,
			Error:            r.Error,
			CreatedAt:        r.CreatedAt,
		},
		Upstream: make([]UpstreamAttempt, 0, len(ups)),
	}
	for _, u := range ups {
		out.Upstream = append(out.Upstream, UpstreamAttempt{
			Attempt:          u.Attempt,
			ProviderName:     u.ProviderName,
			ProviderType:     u.ProviderType,
			UpstreamModel:    u.UpstreamModel,
			StatusCode:       u.StatusCode,
			ErrType:          u.ErrType,
			Stream:           u.Stream,
			PromptTokens:     u.PromptTokens,
			CompletionTokens: u.CompletionTokens,
			CacheReadTokens:  u.CacheReadTokens,
			CacheWriteTokens: u.CacheWriteTokens,
			LatencyMS:        u.LatencyMS,
			Error:            u.Error,
		})
	}
	return nil, out, nil
}

// ---- get_my_stats ----

type GetMyStatsInput struct {
	Days int `json:"days,omitempty" jsonschema:"聚合天数,默认 7,最大 90"`
}

type DailyStat struct {
	Day              string `json:"day"`
	Requests         int64  `json:"requests"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
	CacheReadTokens  int64  `json:"cache_read_tokens"`
	CacheWriteTokens int64  `json:"cache_write_tokens"`
	Cost             float64 `json:"cost"`
}

type ModelStat struct {
	Model    string  `json:"model"`
	Requests int64   `json:"requests"`
	Cost     float64 `json:"cost"`
}

type GetMyStatsResult struct {
	Days    int         `json:"days"`
	Daily   []DailyStat `json:"daily"`
	ByModel []ModelStat `json:"by_model"`
}

func toolGetMyStats() *mcp.Tool {
	return &mcp.Tool{
		Name:        "get_my_stats",
		Description: "按天聚合当前用户的请求量、用量、cost，并按模型给出 TOP。",
	}
}

func getMyStatsHandler(ctx context.Context, _ *mcp.CallToolRequest, in GetMyStatsInput) (*mcp.CallToolResult, GetMyStatsResult, error) {
	u, _, g, err := identityFromCtx(ctx)
	if err != nil {
		return nil, GetMyStatsResult{}, err
	}
	debugLog("get_my_stats", u.Username, in)
	days := parseDays(in.Days)
	since := time.Now().AddDate(0, 0, -days)

	// 按天聚合
	type dailyRow struct {
		Day              string
		Requests         int64
		PromptTokens     int64
		CompletionTokens int64
		CacheReadTokens  int64
		CacheWriteTokens int64
		Cost             float64
	}
	var drows []dailyRow
	g.WithContext(ctx).Model(&db.RequestLog{}).
		Where("user_id = ? AND created_at >= ?", u.ID, since).
		Select(`date(created_at) as day,
			count(*) as requests,
			COALESCE(sum(prompt_tokens),0) as prompt_tokens,
			COALESCE(sum(completion_tokens),0) as completion_tokens,
			COALESCE(sum(cache_read_tokens),0) as cache_read_tokens,
			COALESCE(sum(cache_write_tokens),0) as cache_write_tokens,
			COALESCE(sum(cost),0) as cost`).
		Group("day").Order("day DESC").
		Scan(&drows)

	// 模型维度 TOP
	var mrows []struct {
		Model    string
		Requests int64
		Cost     float64
	}
	g.WithContext(ctx).Model(&db.RequestLog{}).
		Where("user_id = ? AND created_at >= ?", u.ID, since).
		Select(`model, count(*) as requests, COALESCE(sum(cost),0) as cost`).
		Group("model").Order("cost DESC").Limit(20).
		Scan(&mrows)

	out := GetMyStatsResult{Days: days, Daily: make([]DailyStat, 0, len(drows)), ByModel: make([]ModelStat, 0, len(mrows))}
	for _, r := range drows {
		out.Daily = append(out.Daily, DailyStat{
			Day: r.Day, Requests: r.Requests,
			PromptTokens: r.PromptTokens, CompletionTokens: r.CompletionTokens,
			CacheReadTokens: r.CacheReadTokens, CacheWriteTokens: r.CacheWriteTokens,
			Cost: r.Cost,
		})
	}
	for _, r := range mrows {
		out.ByModel = append(out.ByModel, ModelStat{Model: r.Model, Requests: r.Requests, Cost: r.Cost})
	}
	return nil, out, nil
}

// ---- get_my_account ----

type GetMyAccountInput struct{}

type AccountInfo struct {
	Username  string  `json:"username"`
	Role      string  `json:"role"`
	Spend     float64 `json:"spend"` // 累计消费（美元）
	CreatedAt time.Time `json:"created_at"`
}

type GetMyAccountResult struct {
	Account AccountInfo `json:"account"`
}

func toolGetMyAccount() *mcp.Tool {
	return &mcp.Tool{
		Name:        "get_my_account",
		Description: "返回当前 API Key 所属用户的账号信息（用户名、角色、累计消费、注册时间）。",
	}
}

func getMyAccountHandler(ctx context.Context, _ *mcp.CallToolRequest, _ GetMyAccountInput) (*mcp.CallToolResult, GetMyAccountResult, error) {
	u, _, _, err := identityFromCtx(ctx)
	if err != nil {
		return nil, GetMyAccountResult{}, err
	}
	debugLog("get_my_account", u.Username, nil)
	return nil, GetMyAccountResult{Account: AccountInfo{
		Username: u.Username, Role: u.Role, Spend: u.Spend, CreatedAt: u.CreatedAt,
	}}, nil
}