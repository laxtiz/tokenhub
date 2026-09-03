package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"tokenhub/internal/auth"
	"tokenhub/internal/db"
)

// newUserTestEnv 创建一个普通 (non-admin) 用户的 Server + Engine，返回 jwt。
func newUserTestEnv(t *testing.T) (*gin.Engine, *gorm.DB, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	am, err := auth.NewManager(database, "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	u := &db.User{Username: "alice", Role: "user"}
	database.Create(u)
	tok, err := am.IssueToken(u)
	if err != nil {
		t.Fatal(err)
	}
	srv := &Server{DB: database, AM: am}
	engine := gin.New()
	srv.Setup(engine)
	return engine, database, "Bearer " + tok
}

func doJSON(eng *gin.Engine, method, path, authHeader string, body any) (*httptest.ResponseRecorder, map[string]any) {
	var buf *bytes.Buffer
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewBuffer(b)
	} else {
		buf = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequest(method, path, buf)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	eng.ServeHTTP(w, req)
	var resp map[string]any
	if w.Body.Len() > 0 {
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
	}
	return w, resp
}

func createDownstreamKey(t *testing.T, database *gorm.DB, userID uint, name, plain string) *db.DownstreamKey {
	t.Helper()
	_, _, prefix := auth.GenerateDownstreamKey()
	if plain == "" {
		// 默认密文，调用方不在意明文
		plain = "unused"
	}
	// 让 hash 已知为 plain 的 sha256，便于在测试里直接猜测旧/新明文。
	dk := &db.DownstreamKey{UserID: userID, Name: name, KeyHash: auth.HashKey(plain), KeyPrefix: prefix}
	if err := database.Create(dk).Error; err != nil {
		t.Fatal(err)
	}
	return dk
}

func TestUpdateKey_Rename(t *testing.T) {
	eng, database, authHdr := newUserTestEnv(t)
	var u db.User
	database.First(&u)
	dk := createDownstreamKey(t, database, u.ID, "old-name", "")

	w, resp := doJSON(eng, http.MethodPatch, "/api/user/keys/"+itoa(dk.ID), authHdr, map[string]any{"name": "new-name"})
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	got, _ := resp["name"].(string)
	if got != "new-name" {
		t.Fatalf("resp.name=%q want new-name", got)
	}
	database.First(dk, dk.ID)
	if dk.Name != "new-name" {
		t.Fatalf("db.name=%q want new-name", dk.Name)
	}
}

func TestUpdateKey_ToggleDisabled(t *testing.T) {
	eng, database, authHdr := newUserTestEnv(t)
	var u db.User
	database.First(&u)
	dk := createDownstreamKey(t, database, u.ID, "k", "")

	w, _ := doJSON(eng, http.MethodPatch, "/api/user/keys/"+itoa(dk.ID), authHdr, map[string]any{"disabled": true})
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	database.First(dk, dk.ID)
	if !dk.Disabled {
		t.Fatalf("disabled 字段未翻转")
	}
}

func TestUpdateKey_OnlyAllowedFields(t *testing.T) {
	eng, database, authHdr := newUserTestEnv(t)
	var u db.User
	database.First(&u)
	dk := createDownstreamKey(t, database, u.ID, "k", "")
	origHash := dk.KeyHash

	// 仅含禁用字段的请求 → 400（更新路径非 silent filter，避免误改隐藏字段）
	w, _ := doJSON(eng, http.MethodPatch, "/api/user/keys/"+itoa(dk.ID), authHdr, map[string]any{
		"key_hash": "deadbeef",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400, body=%s", w.Code, w.Body.String())
	}
	database.First(dk, dk.ID)
	if dk.KeyHash != origHash {
		t.Fatalf("key_hash 不应被改写")
	}
}

func TestUpdateKey_EmptyName(t *testing.T) {
	eng, database, authHdr := newUserTestEnv(t)
	var u db.User
	database.First(&u)
	dk := createDownstreamKey(t, database, u.ID, "k", "")
	w, _ := doJSON(eng, http.MethodPatch, "/api/user/keys/"+itoa(dk.ID), authHdr, map[string]any{"name": "   "})
	if w.Code != 400 {
		t.Fatalf("status=%d want 400", w.Code)
	}
}

func TestUpdateKey_OtherUsersKey(t *testing.T) {
	eng, database, authHdr := newUserTestEnv(t)
	var u db.User
	database.First(&u)
	// 创建属于别的 user 的 key
	other := &db.User{Username: "bob", Role: "user"}
	database.Create(other)
	dk := createDownstreamKey(t, database, other.ID, "k", "")

	w, _ := doJSON(eng, http.MethodPatch, "/api/user/keys/"+itoa(dk.ID), authHdr, map[string]any{"name": "x"})
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d want 404, body=%s", w.Code, w.Body.String())
	}
}

func TestRevokeKey_RotatesHashAndUnlocks(t *testing.T) {
	eng, database, authHdr := newUserTestEnv(t)
	var u db.User
	database.First(&u)
	dk := createDownstreamKey(t, database, u.ID, "k", "th-old")
	// 先禁用，再撤销 —— 期望撤销同时解锁
	database.Model(dk).Update("disabled", true)

	w, resp := doJSON(eng, http.MethodPost, "/api/user/keys/"+itoa(dk.ID)+"/revoke", authHdr, nil)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	plain, _ := resp["plain_key"].(string)
	if plain == "" || plain == "th-old" {
		t.Fatalf("plain_key 应是新的非空字符串, got=%q", plain)
	}
	database.First(dk, dk.ID)
	if dk.KeyHash == auth.HashKey("th-old") {
		t.Fatalf("key_hash 未轮换")
	}
	if dk.KeyHash != auth.HashKey(plain) {
		t.Fatalf("新 key_hash 应等于 sha256(plain_key)")
	}
	if dk.Disabled {
		t.Fatalf("撤销后应自动解锁")
	}
}
