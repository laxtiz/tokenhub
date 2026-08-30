package auth

import (
	"crypto/rand"
	"log/slog"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"

	"tokenhub/internal/db"
)

type Manager struct {
	DB     *gorm.DB
	Secret []byte
}

func NewManager(database *gorm.DB, secret string) (*Manager, error) {
	if secret == "" {
		secret = getOrCreateSecret(database)
	}
	return &Manager{DB: database, Secret: []byte(secret)}, nil
}

func getOrCreateSecret(g *gorm.DB) string {
	var s db.Setting
	if err := g.Where("key = ?", "jwt_secret").First(&s).Error; err == nil && s.Value != "" {
		return s.Value
	}
	buf := make([]byte, 32)
	rand.Read(buf)
	secret := hex.EncodeToString(buf)
	g.Assign(map[string]any{"value": secret}).Where("key = ?", "jwt_secret").FirstOrCreate(&db.Setting{Key: "jwt_secret"})
	return secret
}

// ---- JWT（管理台/用户门户） ----

type Claims struct {
	UserID   uint   `json:"uid"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

func (m *Manager) IssueToken(u *db.User) (string, error) {
	claims := &Claims{
		UserID:   u.ID,
		Username: u.Username,
		Role:     u.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(7 * 24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.Secret)
}

func (m *Manager) ParseToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return m.Secret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

func (m *Manager) JWTMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing token"})
			return
		}
		claims, err := m.ParseToken(strings.TrimPrefix(h, "Bearer "))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}
		var u db.User
		if err := m.DB.First(&u, claims.UserID).Error; err != nil || u.Disabled {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "user disabled or not found"})
			return
		}
		c.Set("user", &u)
		c.Next()
	}
}

func AdminOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		u := c.MustGet("user").(*db.User)
		if u.Role != "admin" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin only"})
			return
		}
		c.Next()
	}
}

// ---- 下游 API Key（网关侧） ----

func HashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func GenerateDownstreamKey() (plain string, hash string, prefix string) {
	buf := make([]byte, 24)
	rand.Read(buf)
	plain = "th-" + hex.EncodeToString(buf)
	return plain, HashKey(plain), plain[:11]
}

// DownstreamAuth 中间件：从 Authorization: Bearer 或 x-api-key 提取下游 Key。
func DownstreamAuth(g *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := c.GetHeader("x-api-key")
		if raw == "" {
			if h := c.GetHeader("Authorization"); strings.HasPrefix(h, "Bearer ") {
				raw = strings.TrimPrefix(h, "Bearer ")
			}
		}
		if raw == "" {
			abortDownstream(c, http.StatusUnauthorized, "missing_api_key", "Missing API key")
			return
		}
		var dk db.DownstreamKey
		if err := g.Where("key_hash = ?", HashKey(raw)).First(&dk).Error; err != nil || dk.Disabled {
			slog.Warn("downstream auth failed", "ip", c.ClientIP(), "reason", "invalid_or_disabled_key")
			abortDownstream(c, http.StatusUnauthorized, "invalid_api_key", "Invalid or disabled API key")
			return
		}
		var u db.User
		if err := g.First(&u, dk.UserID).Error; err != nil || u.Disabled {
			abortDownstream(c, http.StatusUnauthorized, "user_disabled", "User disabled")
			return
		}
		now := time.Now()
		go g.Model(&dk).Updates(map[string]any{"last_used_at": &now})
		c.Set("downstream_key", &dk)
		c.Set("user", &u)
		c.Next()
	}
}

func abortDownstream(c *gin.Context, status int, typ, msg string) {
	// 同时兼容两种下游格式，客户端无论哪种都能解析到错误
	c.AbortWithStatusJSON(status, gin.H{
		"error": gin.H{"type": typ, "message": msg},
	})
}
