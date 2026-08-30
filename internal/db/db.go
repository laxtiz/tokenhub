package db

import (
	"log"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Setting struct {
	Key   string `gorm:"primaryKey;size:64"`
	Value string
}

type User struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Username     string    `gorm:"uniqueIndex;size:64" json:"username"`
	PasswordHash string    `json:"-"`
	Role         string    `gorm:"size:16;default:user" json:"role"` // admin | user
	Disabled     bool      `json:"disabled"`
	Spend        float64   `json:"spend"` // 累计消费（美元）
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type DownstreamKey struct {
	ID         uint       `gorm:"primaryKey" json:"id"`
	UserID     uint       `gorm:"index" json:"user_id"`
	Name       string     `gorm:"size:128" json:"name"`
	KeyHash    string     `gorm:"uniqueIndex;size:64" json:"-"`
	KeyPrefix  string     `gorm:"size:16" json:"key_prefix"` // 展示用前缀
	Disabled   bool       `json:"disabled"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

type Provider struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"size:128" json:"name"`
	Type      string    `gorm:"size:16" json:"type"` // openai | anthropic
	BaseURL   string    `gorm:"size:256" json:"base_url"`
	Disabled  bool      `json:"disabled"`
	CreatedAt time.Time `json:"created_at"`
}

type ProviderKey struct {
	ID               uint       `gorm:"primaryKey" json:"id"`
	ProviderID       uint       `gorm:"index" json:"provider_id"`
	APIKey           string     `gorm:"size:256" json:"api_key"`
	Status           string     `gorm:"size:16;default:active" json:"status"` // active | rate_limited | invalid | disabled
	CooldownUntil    *time.Time `json:"cooldown_until"`
	ConsecutiveFails int        `json:"consecutive_fails"`
	LastError        string     `gorm:"size:512" json:"last_error"`
	LastUsedAt       *time.Time `json:"last_used_at"`
	CreatedAt        time.Time  `json:"created_at"`
}

type Model struct {
	ID               uint      `gorm:"primaryKey" json:"id"`
	Name             string    `gorm:"uniqueIndex;size:128" json:"name"` // 下游模型名
	DisplayName      string    `gorm:"size:128" json:"display_name"`
	Description      string    `gorm:"size:512" json:"description"`
	ContextLength    int       `json:"context_length"`
	SupportVision    bool      `json:"support_vision"`
	SupportTools     bool      `json:"support_tools"`
	SupportReasoning bool      `json:"support_reasoning"`
	InputPrice       float64   `json:"input_price"` // 每百万 token
	OutputPrice      float64   `json:"output_price"`
	CacheReadPrice   float64   `json:"cache_read_price"`
	CacheWritePrice  float64   `json:"cache_write_price"`
	Currency         string    `gorm:"size:8;default:USD" json:"currency"`
	Disabled         bool      `json:"disabled"`
	CreatedAt        time.Time `json:"created_at"`
}

type ModelChannel struct {
	ID            uint   `gorm:"primaryKey" json:"id"`
	ModelID       uint   `gorm:"index" json:"model_id"`
	ProviderID    uint   `gorm:"index" json:"provider_id"`
	UpstreamModel string `gorm:"size:128" json:"upstream_model"`
	Priority      int    `json:"priority"` // 数字越小优先级越高
	Weight        int    `gorm:"default:1" json:"weight"`
	Disabled      bool   `json:"disabled"`
}

type RequestLog struct {
	ID                int64    `gorm:"primaryKey" json:"id"`
	TraceID           string   `gorm:"index;size:36" json:"trace_id"`
	UserID            uint     `gorm:"index" json:"user_id"`
	KeyID             uint     `json:"key_id"`
	DownstreamFormat  string   `gorm:"size:16" json:"downstream_format"` // openai | anthropic
	Model             string   `gorm:"size:128;index" json:"model"`
	Stream            bool     `json:"stream"`
	Status            int      `json:"status"` // 返回给下游的 HTTP 状态码
	AttemptCount      int      `json:"attempt_count"`
	PromptTokens      int64    `json:"prompt_tokens"`
	CompletionTokens  int64    `json:"completion_tokens"`
	CacheReadTokens   int64    `json:"cache_read_tokens"`
	CacheWriteTokens  int64    `json:"cache_write_tokens"`
	Cost              float64  `json:"cost"`
	LatencyMS         int64    `json:"latency_ms"`
	FirstTokenMS      int64    `json:"first_token_ms"`
	Error             string   `gorm:"size:1024" json:"error"`
	CreatedAt         time.Time `gorm:"index" json:"created_at"`
}

type UpstreamLog struct {
	ID                int64     `gorm:"primaryKey" json:"id"`
	TraceID           string    `gorm:"index;size:36" json:"trace_id"`
	Attempt           int       `json:"attempt"`
	ProviderID        uint      `json:"provider_id"`
	ProviderName      string    `gorm:"size:128" json:"provider_name"`
	ProviderType      string    `gorm:"size:16" json:"provider_type"`
	KeyID             uint      `json:"key_id"`
	UpstreamModel     string    `gorm:"size:128" json:"upstream_model"`
	StatusCode        int       `json:"status_code"`
	ErrType           string    `gorm:"size:16" json:"err_type"` // none | auth | rate_limit | server | timeout | network | client
	Stream            bool      `json:"stream"`
	PromptTokens      int64     `json:"prompt_tokens"`
	CompletionTokens  int64     `json:"completion_tokens"`
	CacheReadTokens   int64     `json:"cache_read_tokens"`
	CacheWriteTokens  int64     `json:"cache_write_tokens"`
	LatencyMS         int64     `json:"latency_ms"`
	Error             string    `gorm:"size:1024" json:"error"`
	CreatedAt         time.Time `json:"created_at"`
}

func Open(path string) (*gorm.DB, error) {
	gormLogger := logger.New(
		log.New(os.Stdout, "", log.LstdFlags),
		logger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true, // 查询未命中是正常业务，不刷屏
		},
	)
	g, err := gorm.Open(sqlite.Open(path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"), &gorm.Config{
		Logger: gormLogger,
	})
	if err != nil {
		return nil, err
	}
	if err := g.AutoMigrate(
		&Setting{}, &User{}, &DownstreamKey{}, &Provider{}, &ProviderKey{},
		&Model{}, &ModelChannel{}, &RequestLog{}, &UpstreamLog{},
	); err != nil {
		return nil, err
	}
	return g, nil
}

// LogWriter 异步批量落库请求日志，避免高频写入阻塞转发链路。
type LogWriter struct {
	db      *gorm.DB
	reqCh   chan *RequestLog
	upCh    chan *UpstreamLog
	closed  chan struct{}
	pending sync.WaitGroup
}

func NewLogWriter(db *gorm.DB) *LogWriter {
	w := &LogWriter{
		db:     db,
		reqCh:  make(chan *RequestLog, 4096),
		upCh:   make(chan *UpstreamLog, 8192),
		closed: make(chan struct{}),
	}
	go w.loop()
	return w
}

func (w *LogWriter) writeRequest(l *RequestLog) {
	if err := w.db.Create(l).Error; err != nil {
		slog.Error("write request log failed", "error", err)
	}
	w.pending.Done()
}

func (w *LogWriter) writeUpstream(l *UpstreamLog) {
	if err := w.db.Create(l).Error; err != nil {
		slog.Error("write upstream log failed", "error", err)
	}
	w.pending.Done()
}

func (w *LogWriter) WriteRequest(l *RequestLog) {
	w.pending.Add(1)
	select {
	case w.reqCh <- l:
	default: // 队列满时降级为同步写入，宁可慢也不丢日志
		slog.Warn("request log queue full, falling back to sync write")
		w.writeRequest(l)
	}
}

func (w *LogWriter) WriteUpstream(l *UpstreamLog) {
	w.pending.Add(1)
	select {
	case w.upCh <- l:
	default:
		w.writeUpstream(l)
	}
}

// Flush 等待队列中所有日志落库（测试与优雅退出用）。
func (w *LogWriter) Flush() { w.pending.Wait() }

func (w *LogWriter) Close() { close(w.closed) }

func (w *LogWriter) loop() {
	for {
		select {
		case <-w.closed:
			return
		case l := <-w.reqCh:
			w.writeRequest(l)
		case l := <-w.upCh:
			w.writeUpstream(l)
		}
	}
}
