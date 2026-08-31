package api

import (
	"testing"
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