package api

import (
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

func TestValidateCustomHeaders(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{"empty", "", false},
		{"valid one", `{"X-Trace":"abc"}`, false},
		{"valid many", `{"X-A":"1","X-B":"2"}`, false},
		{"invalid json", `{not json`, true},
		{"array not object", `["x"]`, true},
		{"empty key", `{"":"v"}`, true},
		{"key with newline", "{\"X\\nA\":\"v\"}", true},
		{"value with newline", "{\"X-A\":\"line1\\nline2\"}", true},
		{"reserved authorization", `{"Authorization":"Bearer x"}`, true},
		{"reserved authorization upper/lower mix", `{"authorization":"x"}`, true},
		{"reserved x-api-key", `{"x-api-key":"x"}`, true},
		{"reserved anthropic-version", `{"anthropic-version":"2023-06-01"}`, true},
		{"reserved host", `{"Host":"x"}`, true},
		{"reserved content-type", `{"content-type":"text/plain"}`, true},
		{"reserved accept", `{"Accept":"text/plain"}`, true},
		{"reserved content-length", `{"content-length":"1"}`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateCustomHeaders(c.raw)
			if (err != nil) != c.wantErr {
				t.Fatalf("raw=%q err=%v wantErr=%v", c.raw, err, c.wantErr)
			}
		})
	}

	// 数量上限：33 个键
	many := "{"
	for i := 0; i < 33; i++ {
		if i > 0 {
			many += ","
		}
		many += `"x` + string(rune('a'+i%26)) + string(rune('0'+i/26)) + `":"v"`
	}
	many += "}"
	if err := validateCustomHeaders(many); err == nil {
		t.Fatalf(">32 个键应报错, raw=%q", many)
	}
}

// ---- listProviderModels 测试 ----

func newAdminTestEnv(t *testing.T) (*gin.Engine, *gorm.DB, string) {
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
	admin := &db.User{Username: "root", Role: "admin"}
	database.Create(admin)
	tok, err := am.IssueToken(admin)
	if err != nil {
		t.Fatal(err)
	}
	srv := &Server{DB: database, AM: am}
	engine := gin.New()
	srv.Setup(engine)
	return engine, database, "Bearer " + tok
}

func doGET(eng *gin.Engine, path, authHeader string) (*httptest.ResponseRecorder, map[string]any) {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	eng.ServeHTTP(w, req)
	var resp map[string]any
	if w.Body.Len() > 0 {
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
	}
	return w, resp
}

func TestListProviderModels_OpenAI(t *testing.T) {
	var gotPath string
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"object":"list","data":[{"id":"gpt-4o"},{"id":"gpt-4o-mini","object":"model"}]}`))
	}))
	t.Cleanup(upstream.Close)

	eng, database, authHdr := newAdminTestEnv(t)
	prov := &db.Provider{Name: "mock-openai", Type: "openai", BaseURL: upstream.URL}
	database.Create(prov)
	database.Create(&db.ProviderKey{ProviderID: prov.ID, APIKey: "k1", Status: "active"})

	w, resp := doGET(eng, "/api/admin/providers/"+itoa(prov.ID)+"/models", authHdr)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if gotPath != "/models" {
		t.Errorf("upstream path=%q want /models", gotPath)
	}
	if gotAuth != "Bearer k1" {
		t.Errorf("upstream auth=%q want Bearer k1", gotAuth)
	}
	models, _ := resp["models"].([]any)
	if len(models) != 2 {
		t.Fatalf("models len=%d want 2, body=%s", len(models), w.Body.String())
	}
	if resp["count"].(float64) != 2 {
		t.Errorf("count=%v want 2", resp["count"])
	}
}

func TestListProviderModels_Anthropic(t *testing.T) {
	var gotPath string
	var gotAPIKey string
	var gotVersion string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"data":[{"id":"claude-3-5-sonnet-20241022","type":"model"},{"id":"claude-3-haiku-20240307","type":"model"}]}`))
	}))
	t.Cleanup(upstream.Close)

	eng, database, authHdr := newAdminTestEnv(t)
	prov := &db.Provider{Name: "mock-anthropic", Type: "anthropic", BaseURL: upstream.URL}
	database.Create(prov)
	database.Create(&db.ProviderKey{ProviderID: prov.ID, APIKey: "ak", Status: "active"})

	w, resp := doGET(eng, "/api/admin/providers/"+itoa(prov.ID)+"/models", authHdr)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if gotPath != "/v1/models" {
		t.Errorf("upstream path=%q want /v1/models", gotPath)
	}
	if gotAPIKey != "ak" || gotVersion != "2023-06-01" {
		t.Errorf("anthropic headers wrong: key=%q version=%q", gotAPIKey, gotVersion)
	}
	models, _ := resp["models"].([]any)
	if len(models) != 2 {
		t.Fatalf("models len=%d want 2", len(models))
	}
}

func TestListProviderModels_NoActiveKey(t *testing.T) {
	eng, database, authHdr := newAdminTestEnv(t)
	prov := &db.Provider{Name: "p", Type: "openai", BaseURL: "http://unused"}
	database.Create(prov)
	database.Create(&db.ProviderKey{ProviderID: prov.ID, APIKey: "k1", Status: "invalid"})

	w, _ := doGET(eng, "/api/admin/providers/"+itoa(prov.ID)+"/models", authHdr)
	if w.Code != 400 {
		t.Fatalf("status=%d want 400, body=%s", w.Code, w.Body.String())
	}
}

func TestListProviderModels_AllKeysFail(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"error":"invalid api key"}`))
	}))
	t.Cleanup(upstream.Close)

	eng, database, authHdr := newAdminTestEnv(t)
	prov := &db.Provider{Name: "p", Type: "openai", BaseURL: upstream.URL}
	database.Create(prov)
	database.Create(&db.ProviderKey{ProviderID: prov.ID, APIKey: "bad1", Status: "active"})
	database.Create(&db.ProviderKey{ProviderID: prov.ID, APIKey: "bad2", Status: "active"})

	w, _ := doGET(eng, "/api/admin/providers/"+itoa(prov.ID)+"/models", authHdr)
	if w.Code != 502 {
		t.Fatalf("status=%d want 502, body=%s", w.Code, w.Body.String())
	}
}

func TestListProviderModels_Unauthenticated(t *testing.T) {
	eng, _, _ := newAdminTestEnv(t)
	w, _ := doGET(eng, "/api/admin/providers/1/models", "")
	if w.Code != 401 {
		t.Fatalf("status=%d want 401", w.Code)
	}
}

func itoa(n uint) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}