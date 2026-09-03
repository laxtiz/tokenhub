package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"tokenhub/internal/db"
)

// adminAnalytics 聚合全站用量，支持多维过滤。
// 仅供 admin 端使用：返回按用户、供应商、模型、状态、错误类型等维度的统计。
// 所有过滤都作用在 request_log 上，错误类型等依赖上游的字段通过 join upstream_log 二次筛选。
func (s *Server) adminAnalytics(c *gin.Context) {
	q := analyticsQuery{
		Days:             7,
		UserID:           parseUintQ(c.Query("user_id")),
		ProviderID:       parseUintQ(c.Query("provider_id")),
		KeyID:            parseUintQ(c.Query("key_id")),
		Model:            c.Query("model"),
		UpstreamModel:    c.Query("upstream_model"),
		DownstreamFormat: c.Query("downstream_format"),
		Status:           parseIntQ(c.Query("status")),
		ErrType:          c.Query("err_type"),
	}
	if n := parseIntQ(c.Query("days")); n > 0 && n <= 90 {
		q.Days = n
	}
	since := time.Now().AddDate(0, 0, -q.Days)

	// 预计算受上游字段过滤的 trace id 集合（与请求字段联动）。
	traceIDs, hasUpstreamFilter := s.upstreamFilteredTraceIDs(q, since)

	// --- totals（基于 request_log） ---
	totals := s.analyticsTotals(q, since, traceIDs, hasUpstreamFilter)
	successRate := 0.0
	if totals.Requests > 0 {
		successRate = float64(totals.SuccessCount) / float64(totals.Requests)
	}
	avgLatency := 0.0
	if totals.LatencyCount > 0 {
		avgLatency = float64(totals.LatencySum) / float64(totals.LatencyCount)
	}
	p95Latency := s.analyticsP95(q, since, traceIDs, hasUpstreamFilter)

	// --- 每日趋势 ---
	daily := s.analyticsDaily(q, since, traceIDs, hasUpstreamFilter)

	// --- 各维度 TOP（同样应用全部过滤） ---
	byUser := s.analyticsByUser(q, since, traceIDs, hasUpstreamFilter)
	byProvider := s.analyticsByProvider(q, since, traceIDs, hasUpstreamFilter)
	byModel := s.analyticsByModel(q, since, traceIDs, hasUpstreamFilter)
	byStatus := s.analyticsByStatus(q, since, traceIDs, hasUpstreamFilter)
	byErrType := s.analyticsByErrType(q, since, traceIDs, hasUpstreamFilter)

	c.JSON(http.StatusOK, gin.H{
		"since":   since,
		"days":    q.Days,
		"filters": q,
		"totals": gin.H{
			"requests":           totals.Requests,
			"prompt_tokens":      totals.PromptTokens,
			"completion_tokens":  totals.CompletionTokens,
			"cache_read_tokens":  totals.CacheReadTokens,
			"cache_write_tokens": totals.CacheWriteTokens,
			"cost":               totals.Cost,
			"success_rate":       successRate,
			"avg_latency_ms":     avgLatency,
			"p95_latency_ms":     p95Latency,
		},
		"daily":       daily,
		"by_user":     byUser,
		"by_provider": byProvider,
		"by_model":    byModel,
		"by_status":   byStatus,
		"by_err_type": byErrType,
	})
}

type analyticsQuery struct {
	Days             int    `json:"days"`
	UserID           uint   `json:"user_id"`
	ProviderID       uint   `json:"provider_id"`
	KeyID            uint   `json:"key_id"`
	Model            string `json:"model"`
	UpstreamModel    string `json:"upstream_model"`
	DownstreamFormat string `json:"downstream_format"`
	Status           int    `json:"status"`
	ErrType          string `json:"err_type"`
}

// upstreamFilteredTraceIDs 返回仅上游字段（provider/upstream_model/err_type/status_code）命中的 trace id 集合。
// 如果没有上游相关过滤，返回 (nil, false)——调用方直接用 request_log 全量聚合。
func (s *Server) upstreamFilteredTraceIDs(q analyticsQuery, since time.Time) ([]string, bool) {
	if q.ProviderID == 0 && q.UpstreamModel == "" && q.ErrType == "" {
		return nil, false
	}
	tx := s.DB.Model(&db.UpstreamLog{}).Where("created_at >= ?", since).
		Select("DISTINCT trace_id")
	if q.ProviderID > 0 {
		tx = tx.Where("provider_id = ?", q.ProviderID)
	}
	if q.UpstreamModel != "" {
		tx = tx.Where("upstream_model = ?", q.UpstreamModel)
	}
	if q.ErrType != "" {
		tx = tx.Where("err_type = ?", q.ErrType)
	}
	if q.Status > 0 {
		tx = tx.Where("status_code = ?", q.Status)
	}
	var ids []string
	tx.Find(&ids)
	return ids, true
}

// applyRequestFilters 把所有 request_log 维度的过滤应用到 *gorm.DB。
// 用表别名 `r.` 限定列名，让带 LEFT JOIN 的查询（by_user）也能工作。
func applyRequestFilters(q analyticsQuery, since time.Time, traceIDs []string, hasUpstreamFilter bool, db *gorm.DB) *gorm.DB {
	db = db.Where("r.created_at >= ?", since)
	if q.UserID > 0 {
		db = db.Where("r.user_id = ?", q.UserID)
	}
	if q.KeyID > 0 {
		db = db.Where("r.key_id = ?", q.KeyID)
	}
	if q.Model != "" {
		db = db.Where("r.model = ?", q.Model)
	}
	if q.DownstreamFormat != "" {
		db = db.Where("r.downstream_format = ?", q.DownstreamFormat)
	}
	if q.Status > 0 {
		db = db.Where("r.status = ?", q.Status)
	}
	if hasUpstreamFilter {
		if len(traceIDs) == 0 {
			// 与上游过滤无交集：用一个永远为假的条件短路
			db = db.Where("1 = 0")
		} else {
			db = db.Where("r.trace_id IN ?", traceIDs)
		}
	}
	return db
}

// baseRequest 以 `request_logs AS r` 为根，所有列都用 r. 限定，避免 join users 后 created_at 模糊。
func (s *Server) baseRequest() *gorm.DB {
	return s.DB.Table("request_logs AS r")
}

type analyticsTotalsRow struct {
	Requests         int64
	PromptTokens     int64
	CompletionTokens int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	Cost             float64
	SuccessCount     int64
	LatencySum       int64
	LatencyCount     int64
}

func (s *Server) analyticsTotals(q analyticsQuery, since time.Time, traceIDs []string, hasUpstreamFilter bool) analyticsTotalsRow {
	var r analyticsTotalsRow
	row := applyRequestFilters(q, since, traceIDs, hasUpstreamFilter, s.baseRequest()).
		Select(`count(*) as requests,
			COALESCE(sum(r.prompt_tokens),0) as prompt_tokens,
			COALESCE(sum(r.completion_tokens),0) as completion_tokens,
			COALESCE(sum(r.cache_read_tokens),0) as cache_read_tokens,
			COALESCE(sum(r.cache_write_tokens),0) as cache_write_tokens,
			COALESCE(sum(r.cost),0) as cost,
			COALESCE(sum(CASE WHEN r.status BETWEEN 200 AND 299 THEN 1 ELSE 0 END),0) as success_count,
			COALESCE(sum(r.latency_ms),0) as latency_sum,
			COALESCE(sum(CASE WHEN r.latency_ms > 0 THEN 1 ELSE 0 END),0) as latency_count`).
		Row()
	row.Scan(&r.Requests, &r.PromptTokens, &r.CompletionTokens, &r.CacheReadTokens, &r.CacheWriteTokens,
		&r.Cost, &r.SuccessCount, &r.LatencySum, &r.LatencyCount)
	return r
}

func (s *Server) analyticsP95(q analyticsQuery, since time.Time, traceIDs []string, hasUpstreamFilter bool) float64 {
	type row struct {
		L int64 `gorm:"column:latency_ms"`
	}
	var rows []row
	applyRequestFilters(q, since, traceIDs, hasUpstreamFilter, s.baseRequest()).
		Where("r.latency_ms > 0").
		Order("r.latency_ms ASC").
		Distinct("r.latency_ms").
		Find(&rows)
	if len(rows) == 0 {
		return 0
	}
	idx := int(float64(len(rows)) * 0.95)
	if idx >= len(rows) {
		idx = len(rows) - 1
	}
	return float64(rows[idx].L)
}

func (s *Server) analyticsDaily(q analyticsQuery, since time.Time, traceIDs []string, hasUpstreamFilter bool) []map[string]any {
	var out []map[string]any
	applyRequestFilters(q, since, traceIDs, hasUpstreamFilter, s.baseRequest()).
		Select(`date(r.created_at) as day,
			count(*) as requests,
			COALESCE(sum(r.prompt_tokens),0) as prompt_tokens,
			COALESCE(sum(r.completion_tokens),0) as completion_tokens,
			COALESCE(sum(r.cache_read_tokens),0) as cache_read_tokens,
			COALESCE(sum(r.cache_write_tokens),0) as cache_write_tokens,
			COALESCE(sum(r.cost),0) as cost,
			COALESCE(sum(CASE WHEN r.status BETWEEN 200 AND 299 THEN 1 ELSE 0 END),0) as success_count`).
		Group("day").Order("day ASC").
		Scan(&out)
	return out
}

func (s *Server) analyticsByUser(q analyticsQuery, since time.Time, traceIDs []string, hasUpstreamFilter bool) []map[string]any {
	var out []map[string]any
	applyRequestFilters(q, since, traceIDs, hasUpstreamFilter, s.baseRequest()).
		Select(`r.user_id as user_id,
			COALESCE(u.username, '') as username,
			count(*) as requests,
			COALESCE(sum(r.prompt_tokens),0) as prompt_tokens,
			COALESCE(sum(r.completion_tokens),0) as completion_tokens,
			COALESCE(sum(r.cost),0) as cost`).
		Joins("LEFT JOIN users u ON u.id = r.user_id").
		Group("r.user_id, u.username").Order("cost DESC").Limit(20).
		Scan(&out)
	return out
}

func (s *Server) analyticsByModel(q analyticsQuery, since time.Time, traceIDs []string, hasUpstreamFilter bool) []map[string]any {
	var out []map[string]any
	applyRequestFilters(q, since, traceIDs, hasUpstreamFilter, s.baseRequest()).
		Select(`r.model as model,
			count(*) as requests,
			COALESCE(sum(r.prompt_tokens),0) as prompt_tokens,
			COALESCE(sum(r.completion_tokens),0) as completion_tokens,
			COALESCE(sum(r.cost),0) as cost`).
		Group("r.model").Order("cost DESC").Limit(20).
		Scan(&out)
	return out
}

func (s *Server) analyticsByStatus(q analyticsQuery, since time.Time, traceIDs []string, hasUpstreamFilter bool) []map[string]any {
	var out []map[string]any
	applyRequestFilters(q, since, traceIDs, hasUpstreamFilter, s.baseRequest()).
		Select(`r.status as status, count(*) as requests`).
		Group("r.status").Order("requests DESC").Limit(20).
		Scan(&out)
	return out
}

// analyticsByProvider 走 upstream_log 维度，包含上游的 attempt 数与成功率。
// 这里不再叠加 request_log 的 trace_id 限制——上游日志本身就是每次尝试，
// 用户给「供应商=X」就是想看 X 的所有尝试，而非仅成功的。
func (s *Server) analyticsByProvider(q analyticsQuery, since time.Time, traceIDs []string, hasUpstreamFilter bool) []map[string]any {
	tx := s.DB.Model(&db.UpstreamLog{}).Where("created_at >= ?", since)
	if q.UserID > 0 {
		// 把 user 过滤折回上游日志：仅保留这些 trace 对应的 upstream_log 行
		tx = tx.Where("trace_id IN (SELECT trace_id FROM request_logs WHERE user_id = ? AND created_at >= ?)", q.UserID, since)
	}
	if q.ProviderID > 0 {
		tx = tx.Where("provider_id = ?", q.ProviderID)
	}
	if q.UpstreamModel != "" {
		tx = tx.Where("upstream_model = ?", q.UpstreamModel)
	}
	if q.Status > 0 {
		tx = tx.Where("status_code = ?", q.Status)
	}
	_ = traceIDs
	_ = hasUpstreamFilter
	var out []map[string]any
	tx.
		Select(`provider_id as provider_id,
			COALESCE(provider_name, '') as provider_name,
			COALESCE(provider_type, '') as provider_type,
			count(*) as attempts,
			COALESCE(sum(prompt_tokens),0) as prompt_tokens,
			COALESCE(sum(completion_tokens),0) as completion_tokens,
			COALESCE(sum(CASE WHEN status_code BETWEEN 200 AND 299 THEN 1 ELSE 0 END),0) as success_count`).
		Group("provider_id, provider_name, provider_type").
		Order("attempts DESC").Limit(20).
		Scan(&out)
	return out
}

func (s *Server) analyticsByErrType(q analyticsQuery, since time.Time, traceIDs []string, hasUpstreamFilter bool) []map[string]any {
	tx := s.DB.Model(&db.UpstreamLog{}).Where("created_at >= ?", since)
	if q.UserID > 0 {
		tx = tx.Where("trace_id IN (SELECT trace_id FROM request_logs WHERE user_id = ? AND created_at >= ?)", q.UserID, since)
	}
	if q.ProviderID > 0 {
		tx = tx.Where("provider_id = ?", q.ProviderID)
	}
	if q.UpstreamModel != "" {
		tx = tx.Where("upstream_model = ?", q.UpstreamModel)
	}
	if q.ErrType != "" {
		tx = tx.Where("err_type = ?", q.ErrType)
	}
	_ = traceIDs
	_ = hasUpstreamFilter
	var out []map[string]any
	tx.
		Select(`err_type, count(*) as attempts`).
		Where("err_type != ''").
		Group("err_type").Order("attempts DESC").Limit(20).
		Scan(&out)
	return out
}

func parseIntQ(s string) int {
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

func parseUintQ(s string) uint {
	if s == "" {
		return 0
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0
	}
	return uint(n)
}
