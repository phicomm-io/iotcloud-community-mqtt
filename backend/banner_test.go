package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBannerPermissionsValidationAndPersistence(t *testing.T) {
	old := cfg
	defer func() { cfg = old }()
	cfg.PublicURL = "https://console.example"
	app = testApp(t)
	h := routes()
	request := func(method, path, origin, token, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, cfg.PublicURL+path, strings.NewReader(body))
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	login := request("POST", "/api/auth/login", cfg.PublicURL, "", `{"username":"alice","password":"secret123"}`)
	if login.Code != 200 {
		t.Fatal(login.Code)
	}
	token := login.Header().Get("X-IotCloud-Session")
	valid := Banner{Enabled: true, Text: "交流 QQ 群：1021750178", URL: "https://qm.qq.com/q/9L150V85kk?from=tim", Label: "加入 QQ 群"}
	raw, _ := json.Marshal(valid)
	for _, method := range []string{"GET", "PUT"} {
		if w := request(method, "/api/admin/banner", cfg.PublicURL, "", string(raw)); w.Code != 401 {
			t.Fatal("anonymous", w.Code)
		}
		if w := request(method, "/api/admin/banner", cfg.PublicURL, token, string(raw)); w.Code != 403 {
			t.Fatal("ordinary user", w.Code)
		}
	}
	if _, err := app.store.Exec("INSERT INTO user_roles(username,role) VALUES('alice','admin')"); err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"", "https://evil.example"} {
		if w := request("PUT", "/api/admin/banner", origin, token, string(raw)); w.Code != 403 {
			t.Fatal("CSRF", w.Code)
		}
	}
	if w := request("GET", "/api/banner", "", "", ""); w.Code != 200 || strings.Contains(w.Body.String(), "1021750178") {
		t.Fatal(w.Code, w.Body.String())
	}
	// YAML supplies the initial announcement until an administrator saves an override.
	cfg.Banner = valid
	if w := request("GET", "/api/banner", "", "", ""); !strings.Contains(w.Body.String(), "1021750178") {
		t.Fatal("configured announcement missing")
	}
	for _, bad := range []Banner{
		{Enabled: true}, {Text: strings.Repeat("群", 301)}, {URL: "https://example.com"},
		{URL: "javascript:alert(1)", Label: "bad"}, {URL: "//evil.example", Label: "bad"},
		{URL: "https://user:secret@example.com", Label: "bad"}, {URL: "https://good.example\\@evil.example", Label: "bad"},
		{URL: "https://example.com\n", Label: "bad" + strings.Repeat("群", 41)},
	} {
		b, _ := json.Marshal(bad)
		if w := request("PUT", "/api/admin/banner", cfg.PublicURL, token, string(b)); w.Code != 400 {
			t.Fatalf("accepted invalid banner: %s (%d)", b, w.Code)
		}
	}
	if w := request("PUT", "/api/admin/banner", cfg.PublicURL, token, string(raw)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var saved string
	if err := app.store.QueryRow("SELECT value FROM settings WHERE key='platform_banner'").Scan(&saved); err != nil || saved != string(raw) {
		t.Fatal("not persisted", err)
	}
	if w := request("GET", "/api/banner", "", "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "1021750178") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	valid.Enabled = false
	raw, _ = json.Marshal(valid)
	if w := request("PUT", "/api/admin/banner", cfg.PublicURL, token, string(raw)); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := request("GET", "/api/banner", "", "", ""); strings.Contains(w.Body.String(), "1021750178") {
		t.Fatal("disabled override did not suppress YAML announcement")
	}
	if w := request("GET", "/api/admin/banner", "", token, ""); !strings.Contains(w.Body.String(), "1021750178") {
		t.Fatal("disabled draft lost")
	}
}
