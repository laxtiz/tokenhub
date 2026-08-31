package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"tokenhub/internal/db"
)

// ---- 用户管理 ----

func (s *Server) listUsers(c *gin.Context) {
	var users []db.User
	s.DB.Order("id ASC").Find(&users)
	c.JSON(http.StatusOK, users)
}

func (s *Server) createUser(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required,min=2"`
		Password string `json:"password" binding:"required,min=6"`
		Role     string `json:"role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "用户名≥2字符，密码≥6位"})
		return
	}
	if req.Role != "admin" && req.Role != "user" {
		req.Role = "user"
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	u := &db.User{Username: req.Username, PasswordHash: string(hash), Role: req.Role}
	if err := s.DB.Create(u).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "用户名已存在"})
		return
	}
	c.JSON(http.StatusOK, u)
}

func (s *Server) updateUser(c *gin.Context) {
	var req struct {
		Password *string `json:"password"`
		Role     *string `json:"role"`
		Disabled *bool   `json:"disabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	var u db.User
	if err := s.DB.First(&u, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "不存在"})
		return
	}
	updates := map[string]any{}
	if req.Password != nil && *req.Password != "" {
		if len(*req.Password) < 6 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "密码≥6位"})
			return
		}
		hash, _ := bcrypt.GenerateFromPassword([]byte(*req.Password), bcrypt.DefaultCost)
		updates["password_hash"] = string(hash)
	}
	if req.Role != nil {
		updates["role"] = *req.Role
	}
	if req.Disabled != nil {
		updates["disabled"] = *req.Disabled
	}
	s.DB.Model(&u).Updates(updates)
	c.JSON(http.StatusOK, u)
}

// ---- 供应商 ----

func (s *Server) listProviders(c *gin.Context) {
	var providers []db.Provider
	s.DB.Order("id ASC").Find(&providers)
	var keys []db.ProviderKey
	s.DB.Order("id ASC").Find(&keys)
	byProvider := map[uint][]db.ProviderKey{}
	for _, k := range keys {
		byProvider[k.ProviderID] = append(byProvider[k.ProviderID], k)
	}
	type providerRow struct {
		db.Provider
		Keys []db.ProviderKey `json:"keys"`
	}
	rows := make([]providerRow, 0, len(providers))
	for _, p := range providers {
		rows = append(rows, providerRow{p, byProvider[p.ID]})
	}
	c.JSON(http.StatusOK, rows)
}

func (s *Server) createProvider(c *gin.Context) {
	var p db.Provider
	if err := c.ShouldBindJSON(&p); err != nil || p.Name == "" || (p.Type != "openai" && p.Type != "anthropic") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name 必填，type 须为 openai/anthropic"})
		return
	}
	if p.BaseURL == "" {
		if p.Type == "anthropic" {
			p.BaseURL = "https://api.anthropic.com"
		} else {
			p.BaseURL = "https://api.openai.com/v1"
		}
	}
	if err := validateCustomHeaders(p.CustomHeaders); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	s.DB.Create(&p)
	c.JSON(http.StatusOK, p)
}

func (s *Server) updateProvider(c *gin.Context) {
	var p db.Provider
	if err := s.DB.First(&p, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "不存在"})
		return
	}
	var req struct {
		Name          *string `json:"name"`
		BaseURL       *string `json:"base_url"`
		Disabled      *bool   `json:"disabled"`
		UserAgent     *string `json:"user_agent"`
		CustomHeaders *string `json:"custom_headers"`
	}
	_ = c.ShouldBindJSON(&req)
	updates := map[string]any{}
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.BaseURL != nil {
		updates["base_url"] = *req.BaseURL
	}
	if req.Disabled != nil {
		updates["disabled"] = *req.Disabled
	}
	if req.UserAgent != nil {
		updates["user_agent"] = *req.UserAgent
	}
	if req.CustomHeaders != nil {
		if err := validateCustomHeaders(*req.CustomHeaders); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		updates["custom_headers"] = *req.CustomHeaders
	}
	s.DB.Model(&p).Updates(updates)
	c.JSON(http.StatusOK, p)
}

// validateCustomHeaders 校验 custom_headers：必须是合法 JSON object，
// 且不能包含鉴权头（authorization / x-api-key / anthropic-version）以避免被覆盖绕鉴权。
func validateCustomHeaders(raw string) error {
	if raw == "" {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return fmt.Errorf("custom_headers 不是合法 JSON 对象: %v", err)
	}
	if len(m) > 32 {
		return fmt.Errorf("custom_headers 最多 32 个")
	}
	for k, v := range m {
		if k == "" {
			return fmt.Errorf("custom_headers key 不能为空")
		}
		if strings.ContainsAny(k, ":\r\n") {
			return fmt.Errorf("custom_headers key 含非法字符: %q", k)
		}
		if strings.ContainsAny(v, "\r\n") {
			return fmt.Errorf("custom_headers value 含换行: %q", k)
		}
		lk := strings.ToLower(k)
		switch lk {
		case "authorization", "x-api-key", "anthropic-version", "host", "content-length", "content-type", "accept":
			return fmt.Errorf("custom_headers 不允许覆盖 %q", lk)
		}
	}
	return nil
}

func (s *Server) deleteProvider(c *gin.Context) {
	id := c.Param("id")
	var cnt int64
	s.DB.Model(&db.ModelChannel{}).Where("provider_id = ?", id).Count(&cnt)
	if cnt > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该供应商仍被渠道引用，请先删除相关渠道"})
		return
	}
	s.DB.Where("id = ?", id).Delete(&db.Provider{})
	s.DB.Where("provider_id = ?", id).Delete(&db.ProviderKey{})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ---- 供应商 Key ----

func (s *Server) createProviderKey(c *gin.Context) {
	var prov db.Provider
	if err := s.DB.First(&prov, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "供应商不存在"})
		return
	}
	var req struct {
		Name   string `json:"name"`
		APIKey string `json:"api_key" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "api_key 必填"})
		return
	}
	k := &db.ProviderKey{ProviderID: prov.ID, Name: req.Name, APIKey: req.APIKey}
	s.DB.Create(k)
	c.JSON(http.StatusOK, k)
}

func (s *Server) updateProviderKey(c *gin.Context) {
	var k db.ProviderKey
	if err := s.DB.First(&k, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "不存在"})
		return
	}
	var req struct {
		Status *string `json:"status"`
		Name   *string `json:"name"`
		APIKey *string `json:"api_key"`
	}
	_ = c.ShouldBindJSON(&req)
	updates := map[string]any{}
	if req.Status != nil {
		updates["status"] = *req.Status
		updates["consecutive_fails"] = 0
		updates["cooldown_until"] = nil
	}
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.APIKey != nil && *req.APIKey != "" {
		updates["api_key"] = *req.APIKey
	}
	s.DB.Model(&k).Updates(updates)
	c.JSON(http.StatusOK, k)
}

func (s *Server) deleteProviderKey(c *gin.Context) {
	s.DB.Where("id = ?", c.Param("id")).Delete(&db.ProviderKey{})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// testProviderKey 用 GET /v1/models 验证 Key 连通性（OpenAI 与 Anthropic 均支持该端点）。
func (s *Server) testProviderKey(c *gin.Context) {
	var k db.ProviderKey
	if err := s.DB.First(&k, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "不存在"})
		return
	}
	var prov db.Provider
	if err := s.DB.First(&prov, k.ProviderID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "供应商不存在"})
		return
	}
	// 与转发路径同规范：openai 型 BaseURL 含 /v1，anthropic 型补 /v1/models
	var url string
	if prov.Type == "anthropic" {
		url = fmt.Sprintf("%s/v1/models", trimRightSlash(prov.BaseURL))
	} else {
		url = fmt.Sprintf("%s/models", trimRightSlash(prov.BaseURL))
	}
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	if prov.Type == "anthropic" {
		req.Header.Set("x-api-key", k.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else {
		req.Header.Set("Authorization", "Bearer "+k.APIKey)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode == http.StatusOK {
		s.DB.Model(&k).Updates(map[string]any{"status": "active", "consecutive_fails": 0, "last_error": ""})
		c.JSON(http.StatusOK, gin.H{"ok": true, "status": resp.StatusCode})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": false, "status": resp.StatusCode, "error": string(bytes.TrimSpace(b))})
}

func trimRightSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

// ---- 下游模型与渠道 ----

func (s *Server) adminListModels(c *gin.Context) {
	var models []db.Model
	s.DB.Order("name ASC").Find(&models)
	var channels []db.ModelChannel
	s.DB.Order("priority ASC").Find(&channels)
	byModel := map[uint][]db.ModelChannel{}
	for _, ch := range channels {
		byModel[ch.ModelID] = append(byModel[ch.ModelID], ch)
	}
	var providers []db.Provider
	s.DB.Find(&providers)
	provMap := map[uint]db.Provider{}
	for _, p := range providers {
		provMap[p.ID] = p
	}
	type channelRow struct {
		db.ModelChannel
		ProviderName string `json:"provider_name"`
		ProviderType string `json:"provider_type"`
	}
	type modelRow struct {
		db.Model
		Channels []channelRow `json:"channels"`
	}
	rows := make([]modelRow, 0, len(models))
	for _, m := range models {
		row := modelRow{Model: m}
		for _, ch := range byModel[m.ID] {
			row.Channels = append(row.Channels, channelRow{ch, provMap[ch.ProviderID].Name, provMap[ch.ProviderID].Type})
		}
		rows = append(rows, row)
	}
	c.JSON(http.StatusOK, rows)
}

func (s *Server) createModel(c *gin.Context) {
	var m db.Model
	if err := c.ShouldBindJSON(&m); err != nil || m.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name 必填"})
		return
	}
	if m.Currency == "" {
		m.Currency = "USD"
	}
	if err := s.DB.Create(&m).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "模型名已存在"})
		return
	}
	c.JSON(http.StatusOK, m)
}

func (s *Server) updateModel(c *gin.Context) {
	var m db.Model
	if err := s.DB.First(&m, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "不存在"})
		return
	}
	var req map[string]any
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	allowed := map[string]bool{
		"display_name": true, "description": true, "context_length": true,
		"support_vision": true, "support_tools": true, "support_reasoning": true,
		"input_price": true, "output_price": true, "cache_read_price": true,
		"cache_write_price": true, "currency": true, "disabled": true,
	}
	updates := map[string]any{}
	for k, v := range req {
		if allowed[k] {
			updates[k] = v
		}
	}
	s.DB.Model(&m).Updates(updates)
	c.JSON(http.StatusOK, m)
}

func (s *Server) deleteModel(c *gin.Context) {
	id := c.Param("id")
	s.DB.Where("id = ?", id).Delete(&db.Model{})
	s.DB.Where("model_id = ?", id).Delete(&db.ModelChannel{})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) createChannel(c *gin.Context) {
	modelID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	var ch db.ModelChannel
	if err := c.ShouldBindJSON(&ch); err != nil || ch.ProviderID == 0 || ch.UpstreamModel == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "provider_id 与 upstream_model 必填"})
		return
	}
	ch.ID = 0
	ch.ModelID = uint(modelID)
	s.DB.Create(&ch)
	c.JSON(http.StatusOK, ch)
}

func (s *Server) updateChannel(c *gin.Context) {
	var ch db.ModelChannel
	if err := s.DB.First(&ch, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "不存在"})
		return
	}
	var req map[string]any
	_ = c.ShouldBindJSON(&req)
	allowed := map[string]bool{"provider_id": true, "upstream_model": true, "priority": true, "weight": true, "disabled": true}
	updates := map[string]any{}
	for k, v := range req {
		if allowed[k] {
			updates[k] = v
		}
	}
	s.DB.Model(&ch).Updates(updates)
	c.JSON(http.StatusOK, ch)
}

func (s *Server) deleteChannel(c *gin.Context) {
	s.DB.Where("id = ?", c.Param("id")).Delete(&db.ModelChannel{})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
