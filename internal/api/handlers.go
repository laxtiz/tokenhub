package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"tokenhub/internal/auth"
	"tokenhub/internal/db"
)

type Server struct {
	DB  *gorm.DB
	AM  *auth.Manager
	Log *db.LogWriter
}

func (s *Server) Setup(r *gin.Engine) {
	r.POST("/api/login", s.login)

	jwt := r.Group("/api", s.AM.JWTMiddleware())
	jwt.GET("/me", s.me)
	jwt.POST("/me/password", s.changePassword)

	// 用户门户
	user := jwt.Group("/user")
	{
		user.GET("/models", s.listModels)
		user.GET("/keys", s.listKeys)
		user.POST("/keys", s.createKey)
		user.PATCH("/keys/:id", s.updateKey)
		user.POST("/keys/:id/revoke", s.revokeKey)
		user.DELETE("/keys/:id", s.deleteKey)
		user.GET("/logs", s.listLogs(false))
		user.GET("/logs/:traceId", s.traceDetail(false))
		user.GET("/stats", s.stats(false))
	}

	// 管理端
	admin := jwt.Group("/admin", auth.AdminOnly())
	{
		admin.GET("/users", s.listUsers)
		admin.POST("/users", s.createUser)
		admin.PATCH("/users/:id", s.updateUser)

		admin.GET("/providers", s.listProviders)
		admin.POST("/providers", s.createProvider)
		admin.PATCH("/providers/:id", s.updateProvider)
		admin.DELETE("/providers/:id", s.deleteProvider)
		admin.GET("/providers/:id/models", s.listProviderModels)
		admin.POST("/providers/:id/keys", s.createProviderKey)
		admin.PATCH("/provider-keys/:id", s.updateProviderKey)
		admin.DELETE("/provider-keys/:id", s.deleteProviderKey)
		admin.POST("/provider-keys/:id/test", s.testProviderKey)

		admin.GET("/models", s.adminListModels)
		admin.POST("/models", s.createModel)
		admin.PATCH("/models/:id", s.updateModel)
		admin.DELETE("/models/:id", s.deleteModel)
		admin.POST("/models/:id/channels", s.createChannel)
		admin.PATCH("/channels/:id", s.updateChannel)
		admin.DELETE("/channels/:id", s.deleteChannel)

		admin.GET("/logs", s.listLogs(true))
		admin.GET("/logs/:traceId", s.traceDetail(true))
		admin.GET("/stats", s.stats(true))
	}
}

func (s *Server) login(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	var u db.User
	if err := s.DB.Where("username = ?", req.Username).First(&u).Error; err != nil || u.Disabled {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)) != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}
	token, err := s.AM.IssueToken(&u)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "签发 token 失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token, "user": u})
}

func currentUser(c *gin.Context) *db.User { return c.MustGet("user").(*db.User) }

func (s *Server) me(c *gin.Context) { c.JSON(http.StatusOK, currentUser(c)) }

func (s *Server) changePassword(c *gin.Context) {
	var req struct {
		OldPassword string `json:"old_password" binding:"required"`
		NewPassword string `json:"new_password" binding:"required,min=6"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "新密码至少 6 位"})
		return
	}
	u := currentUser(c)
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.OldPassword)) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "旧密码错误"})
		return
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	s.DB.Model(u).Update("password_hash", string(hash))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ---- 用户：下游 Key ----

func (s *Server) listKeys(c *gin.Context) {
	u := currentUser(c)
	var keys []db.DownstreamKey
	s.DB.Where("user_id = ?", u.ID).Order("id DESC").Find(&keys)
	c.JSON(http.StatusOK, keys)
}

func (s *Server) createKey(c *gin.Context) {
	u := currentUser(c)
	var req struct {
		Name string `json:"name"`
	}
	_ = c.ShouldBindJSON(&req)
	plain, hash, prefix := auth.GenerateDownstreamKey()
	dk := &db.DownstreamKey{UserID: u.ID, Name: req.Name, KeyHash: hash, KeyPrefix: prefix}
	if err := s.DB.Create(dk).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建失败"})
		return
	}
	// 明文仅此一次返回
	c.JSON(http.StatusOK, gin.H{"key": dk, "plain_key": plain})
}

func (s *Server) deleteKey(c *gin.Context) {
	u := currentUser(c)
	res := s.DB.Where("id = ? AND user_id = ?", c.Param("id"), u.ID).Delete(&db.DownstreamKey{})
	if res.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// updateKey 局部更新下游 Key：仅允许 name / disabled。
func (s *Server) updateKey(c *gin.Context) {
	u := currentUser(c)
	var dk db.DownstreamKey
	if err := s.DB.Where("id = ? AND user_id = ?", c.Param("id"), u.ID).First(&dk).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "不存在"})
		return
	}
	var req map[string]any
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	updates := map[string]any{}
	if v, ok := req["name"].(string); ok {
		v = strings.TrimSpace(v)
		if v == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "name 不能为空"})
			return
		}
		if len(v) > 128 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "name 超过 128 字符"})
			return
		}
		updates["name"] = v
	}
	if v, ok := req["disabled"].(bool); ok {
		updates["disabled"] = v
	}
	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无可更新字段"})
		return
	}
	s.DB.Model(&dk).Updates(updates)
	s.DB.First(&dk, dk.ID)
	c.JSON(http.StatusOK, dk)
}

// revokeKey 重新生成下游 Key 的 hash/prefix（保留 id 与 name）；
// 历史记录（last_used_at、日志关联）继续可用，明文仅本次返回。
func (s *Server) revokeKey(c *gin.Context) {
	u := currentUser(c)
	var dk db.DownstreamKey
	if err := s.DB.Where("id = ? AND user_id = ?", c.Param("id"), u.ID).First(&dk).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "不存在"})
		return
	}
	plain, hash, prefix := auth.GenerateDownstreamKey()
	updates := map[string]any{"key_hash": hash, "key_prefix": prefix}
	if dk.Disabled {
		updates["disabled"] = false
	}
	s.DB.Model(&dk).Updates(updates)
	s.DB.First(&dk, dk.ID)
	c.JSON(http.StatusOK, gin.H{"key": dk, "plain_key": plain})
}

// ---- 用户：模型列表（含元属性与价格） ----

func (s *Server) listModels(c *gin.Context) {
	var models []db.Model
	s.DB.Where("disabled = ?", false).Order("name ASC").Find(&models)
	c.JSON(http.StatusOK, models)
}

// ---- 日志 ----

// listLogs 查询请求日志。admin=true 时可看全站并按用户过滤，否则仅本人。
func (s *Server) listLogs(admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		type LogQuery struct {
			Page    int    `form:"page,default=1"`
			Size    int    `form:"size,default=20"`
			Model   string `form:"model"`
			UserID  uint   `form:"user_id"`
			Status  int    `form:"status"`
			TraceID string `form:"trace_id"`
			Days    int    `form:"days,default=7"`
		}
		var q LogQuery
		_ = c.ShouldBindQuery(&q)
		if q.Size > 100 {
			q.Size = 100
		}
		where := s.DB.Model(&db.RequestLog{})
		if !admin {
			where = where.Where("user_id = ?", currentUser(c).ID)
		} else if q.UserID > 0 {
			where = where.Where("user_id = ?", q.UserID)
		}
		if q.Model != "" {
			where = where.Where("model = ?", q.Model)
		}
		if q.Status > 0 {
			where = where.Where("status = ?", q.Status)
		}
		if q.TraceID != "" {
			where = where.Where("trace_id = ?", q.TraceID)
		}
		if q.Days > 0 {
			where = where.Where("created_at >= ?", time.Now().AddDate(0, 0, -q.Days))
		}
		var total int64
		where.Count(&total)
		var logs []db.RequestLog
		where.Order("id DESC").Offset((q.Page - 1) * q.Size).Limit(q.Size).Find(&logs)

		// 附加用户名与 key 前缀，便于展示
		var users []db.User
		s.DB.Find(&users)
		userMap := map[uint]string{}
		for _, u := range users {
			userMap[u.ID] = u.Username
		}
		var keys []db.DownstreamKey
		s.DB.Find(&keys)
		keyMap := map[uint]string{}
		for _, k := range keys {
			keyMap[k.ID] = k.KeyPrefix
		}
		type logRow struct {
			db.RequestLog
			Username  string `json:"username"`
			KeyPrefix string `json:"key_prefix"`
		}
		rows := make([]logRow, 0, len(logs))
		for _, l := range logs {
			rows = append(rows, logRow{l, userMap[l.UserID], keyMap[l.KeyID]})
		}
		c.JSON(http.StatusOK, gin.H{"total": total, "items": rows})
	}
}

// traceDetail 返回某 trace 的完整调用链：下游请求 + 全部上游尝试。
func (s *Server) traceDetail(admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		tid := c.Param("traceId")
		var rl db.RequestLog
		if err := s.DB.Where("trace_id = ?", tid).First(&rl).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "不存在"})
			return
		}
		if !admin && rl.UserID != currentUser(c).ID {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权访问"})
			return
		}
		var ups []db.UpstreamLog
		s.DB.Where("trace_id = ?", tid).Order("attempt ASC").Find(&ups)
		c.JSON(http.StatusOK, gin.H{"request": rl, "upstream": ups})
	}
}

// stats 按天聚合用量。admin=true 全站，否则本人。
func (s *Server) stats(admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		days := 7
		if d := c.Query("days"); d != "" {
			for _, ch := range d {
				_ = ch
			}
			if n := parseInt(d); n > 0 && n <= 90 {
				days = n
			}
		}
		since := time.Now().AddDate(0, 0, -days)
		q := s.DB.Model(&db.RequestLog{}).Where("created_at >= ?", since).
			Select(`date(created_at) as day, count(*) as requests,
				COALESCE(sum(prompt_tokens),0) as prompt_tokens,
				COALESCE(sum(completion_tokens),0) as completion_tokens,
				COALESCE(sum(cache_read_tokens),0) as cache_read_tokens,
				COALESCE(sum(cache_write_tokens),0) as cache_write_tokens,
				COALESCE(sum(cost),0) as cost`).
			Group("day").Order("day DESC")
		if !admin {
			q = q.Where("user_id = ?", currentUser(c).ID)
		}
		var rows []map[string]any
		q.Scan(&rows)

		// 模型维度 TOP
		mq := s.DB.Model(&db.RequestLog{}).Where("created_at >= ?", since).
			Select(`model, count(*) as requests,
				COALESCE(sum(prompt_tokens),0) as prompt_tokens,
				COALESCE(sum(completion_tokens),0) as completion_tokens,
				COALESCE(sum(cost),0) as cost`).
			Group("model").Order("cost DESC").Limit(20)
		if !admin {
			mq = mq.Where("user_id = ?", currentUser(c).ID)
		}
		var byModel []map[string]any
		mq.Scan(&byModel)

		c.JSON(http.StatusOK, gin.H{"daily": rows, "by_model": byModel})
	}
}

func parseInt(s string) int {
	n := 0
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0
		}
		n = n*10 + int(ch-'0')
	}
	return n
}
