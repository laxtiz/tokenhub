package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"tokenhub/internal/db"
)

type upstreamRecording struct {
	gotPath        string
	gotAuth        string
	gotAPIKey      string
	gotAPIVersion  string
	gotContentType string
	gotBody        string
	hits           int
}

func startMockUpstream(t *testing.T, status int, body string, rec *upstreamRecording) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rec != nil {
			rec.hits++
			rec.gotPath = r.URL.Path
			rec.gotAuth = r.Header.Get("Authorization")
			rec.gotAPIKey = r.Header.Get("x-api-key")
			rec.gotAPIVersion = r.Header.Get("anthropic-version")
			rec.gotContentType = r.Header.Get("Content-Type")
			rec.gotBody = readAll(r.Body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func readAll(r interface{ Read(p []byte) (int, error) }) string {
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	return string(buf[:n])
}

func doAdminJSON(eng interface{ ServeHTTP(http.ResponseWriter, *http.Request) },
	method, path, authHeader string, body any) *httptest.ResponseRecorder {
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
	return w
}

func TestTestProviderModel_OpenAISuccess(t *testing.T) {
	rec := &upstreamRecording{}
	upstream := startMockUpstream(t, 200, `{"id":"gpt-4o","choices":[{"message":{"content":"hi"}}]}`, rec)

	eng, database, authHdr := newAdminTestEnv(t)
	prov := &db.Provider{Name: "p1", Type: "openai", BaseURL: upstream.URL}
	database.Create(prov)
	database.Create(&db.ProviderKey{ProviderID: prov.ID, APIKey: "k1", Status: "active"})

	w := doAdminJSON(eng, http.MethodPost,
		fmt.Sprintf("/api/admin/providers/%d/models/test", prov.ID),
		authHdr, map[string]string{"model": "gpt-4o"})

	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if ok, _ := resp["ok"].(bool); !ok {
		t.Errorf("ok=false: %v", resp)
	}
	if rec.gotPath != "/chat/completions" {
		t.Errorf("path=%q want /chat/completions", rec.gotPath)
	}
	if rec.gotAuth != "Bearer k1" {
		t.Errorf("auth=%q want Bearer k1", rec.gotAuth)
	}
	if rec.gotBody == "" {
		t.Errorf("body should be sent")
	}
	var got map[string]any
	json.Unmarshal([]byte(rec.gotBody), &got)
	if int(got["max_tokens"].(float64)) != 1 {
		t.Errorf("max_tokens=%v want 1", got["max_tokens"])
	}
}

func TestTestProviderModel_AnthropicSuccess(t *testing.T) {
	rec := &upstreamRecording{}
	upstream := startMockUpstream(t, 200, `{"id":"claude-3","content":[{"text":"hi"}]}`, rec)

	eng, database, authHdr := newAdminTestEnv(t)
	prov := &db.Provider{Name: "p-anth", Type: "anthropic", BaseURL: upstream.URL}
	database.Create(prov)
	database.Create(&db.ProviderKey{ProviderID: prov.ID, APIKey: "ak", Status: "active"})

	w := doAdminJSON(eng, http.MethodPost,
		fmt.Sprintf("/api/admin/providers/%d/models/test", prov.ID),
		authHdr, map[string]string{"model": "claude-3-haiku"})

	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if rec.gotPath != "/v1/messages" {
		t.Errorf("path=%q want /v1/messages", rec.gotPath)
	}
	if rec.gotAPIKey != "ak" {
		t.Errorf("x-api-key=%q want ak", rec.gotAPIKey)
	}
	if rec.gotAPIVersion != "2023-06-01" {
		t.Errorf("anthropic-version=%q want 2023-06-01", rec.gotAPIVersion)
	}
}

func TestTestProviderModel_AllKeysFail(t *testing.T) {
	upstream := startMockUpstream(t, 404, `{"error":"model not found"}`, nil)

	eng, database, authHdr := newAdminTestEnv(t)
	prov := &db.Provider{Name: "p1", Type: "openai", BaseURL: upstream.URL}
	database.Create(prov)
	database.Create(&db.ProviderKey{ProviderID: prov.ID, APIKey: "k1", Status: "active"})

	w := doAdminJSON(eng, http.MethodPost,
		fmt.Sprintf("/api/admin/providers/%d/models/test", prov.ID),
		authHdr, map[string]string{"model": "gpt-bogus"})

	if w.Code != 502 {
		t.Fatalf("status=%d want 502, body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if ok, _ := resp["ok"].(bool); ok {
		t.Errorf("ok should be false: %v", resp)
	}
}

func TestTestProviderModel_NoActiveKey(t *testing.T) {
	eng, database, authHdr := newAdminTestEnv(t)
	prov := &db.Provider{Name: "p1", Type: "openai", BaseURL: "http://unused"}
	database.Create(prov)
	database.Create(&db.ProviderKey{ProviderID: prov.ID, APIKey: "k1", Status: "invalid"})

	w := doAdminJSON(eng, http.MethodPost,
		fmt.Sprintf("/api/admin/providers/%d/models/test", prov.ID),
		authHdr, map[string]string{"model": "gpt-x"})

	if w.Code != 400 {
		t.Fatalf("status=%d want 400", w.Code)
	}
}

func TestTestProviderModel_MissingModel(t *testing.T) {
	eng, database, authHdr := newAdminTestEnv(t)
	prov := &db.Provider{Name: "p1", Type: "openai", BaseURL: "http://unused"}
	database.Create(prov)
	database.Create(&db.ProviderKey{ProviderID: prov.ID, APIKey: "k1", Status: "active"})

	w := doAdminJSON(eng, http.MethodPost,
		fmt.Sprintf("/api/admin/providers/%d/models/test", prov.ID),
		authHdr, map[string]string{})

	if w.Code != 400 {
		t.Fatalf("status=%d want 400", w.Code)
	}
}

func TestTestProviderModel_FallsBackToSecondKey(t *testing.T) {
	rec := &upstreamRecording{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.hits++
		if rec.hits == 1 {
			w.WriteHeader(401)
			w.Write([]byte(`{"error":"bad key"}`))
			return
		}
		w.WriteHeader(200)
		w.Write([]byte(`{"id":"gpt-4o"}`))
	}))
	t.Cleanup(upstream.Close)

	eng, database, authHdr := newAdminTestEnv(t)
	prov := &db.Provider{Name: "p1", Type: "openai", BaseURL: upstream.URL}
	database.Create(prov)
	database.Create(&db.ProviderKey{ProviderID: prov.ID, APIKey: "bad", Status: "active"})
	database.Create(&db.ProviderKey{ProviderID: prov.ID, APIKey: "good", Status: "active"})

	w := doAdminJSON(eng, http.MethodPost,
		fmt.Sprintf("/api/admin/providers/%d/models/test", prov.ID),
		authHdr, map[string]string{"model": "gpt-4o"})

	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if rec.hits < 2 {
		t.Errorf("expected fallback hit, hits=%d", rec.hits)
	}
}
