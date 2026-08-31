package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"tokenhub/internal/api"
	"tokenhub/internal/auth"
	"tokenhub/internal/config"
	"tokenhub/internal/db"
	mcpserver "tokenhub/internal/mcp"
	"tokenhub/internal/relay"
	"tokenhub/internal/web"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})))

	cfg := config.Load()

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		panic("打开数据库失败: " + err.Error())
	}
	logs := db.NewLogWriter(database)
	defer logs.Close()

	seedAdmin(database, cfg)
	slog.Info("database ready", "path", cfg.DBPath)

	am, err := auth.NewManager(database, cfg.JWTSecret)
	if err != nil {
		panic("初始化鉴权失败: " + err.Error())
	}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery(), accessLog(), bodyLimit(cfg.BodyLimitMB))

	apiSrv := &api.Server{DB: database, AM: am, Log: logs}
	apiSrv.Setup(r)

	// ---- 网关端点（下游 API Key 鉴权） ----
	rl := relay.New(database, logs)
	dl := auth.DownstreamAuth(database)
	r.POST("/v1/chat/completions", dl, func(c *gin.Context) { rl.Handle(c, "openai") })
	r.POST("/v1/messages", dl, func(c *gin.Context) { rl.Handle(c, "anthropic") })
	r.GET("/v1/models", dl, gatewayModels(database))

	// ---- MCP 服务器（Streamable HTTP，agent 用下游 Key 接入） ----
	r.Any("/mcp", mcpserver.Handler(database))

	// ---- Web 管理台（嵌入静态资源） ----
	r.NoRoute(web.Handler())

	slog.Info("server listening", "port", cfg.Port)
	if err := r.Run(":" + cfg.Port); err != nil {
		panic(err)
	}
}

// accessLog 输出 HTTP 访问日志：方法、路径、状态码、耗时与已认证用户。
func accessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		status := c.Writer.Status()
		attrs := []any{
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", status,
			"ms", time.Since(start).Milliseconds(),
		}
		if u, ok := c.Get("user"); ok {
			attrs = append(attrs, "user", u.(*db.User).Username)
		}
		if tid, ok := c.Get("trace_id"); ok {
			attrs = append(attrs, "trace_id", tid)
		}
		if c.Request.URL.Path == "/api/login" {
			attrs = append(attrs, "ip", c.ClientIP())
		}
		switch {
		case status >= 500:
			slog.Error("http", attrs...)
		case status >= 400:
			slog.Warn("http", attrs...)
		default:
			slog.Info("http", attrs...)
		}
	}
}

func bodyLimit(mb int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, mb<<20)
		c.Next()
	}
}

// gatewayModels 以 OpenAI 格式返回模型列表，附带元属性与价格。
func gatewayModels(g *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var models []db.Model
		g.Where("disabled = ?", false).Order("name ASC").Find(&models)
		data := make([]gin.H, 0, len(models))
		now := time.Now().Unix()
		for _, m := range models {
			data = append(data, gin.H{
				"id":                m.Name,
				"object":            "model",
				"created":           now,
				"owned_by":          "tokenhub",
				"context_length":    m.ContextLength,
				"supports_vision":   m.SupportVision,
				"supports_tools":    m.SupportTools,
				"supports_reasoning": m.SupportReasoning,
				"pricing": gin.H{
					"input_per_million":  m.InputPrice,
					"output_per_million": m.OutputPrice,
					"cache_read_per_million":  m.CacheReadPrice,
					"cache_write_per_million": m.CacheWritePrice,
					"currency":           m.Currency,
				},
			})
		}
		c.JSON(http.StatusOK, gin.H{"object": "list", "data": data})
	}
}

func seedAdmin(g *gorm.DB, cfg *config.Config) {
	var cnt int64
	g.Model(&db.User{}).Count(&cnt)
	if cnt > 0 {
		return
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(cfg.AdminPassword), bcrypt.DefaultCost)
	admin := &db.User{Username: cfg.AdminUsername, PasswordHash: string(hash), Role: "admin"}
	if err := g.Create(admin).Error; err != nil {
		slog.Error("创建管理员失败", "err", err)
	}
}
