package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFrontendOriginsAndSession(t *testing.T) {
	old := cfg
	defer func() { cfg = old }()
	cfg.AllowedOrigins = []string{"https://example.github.io"}
	for _, origin := range []string{"null", "https://evil.example", "https://example.github.io/", "https://example.github.io.evil"} {
		if allowedFrontend(origin) {
			t.Fatalf("accepted %s", origin)
		}
	}
	req := httptest.NewRequest("OPTIONS", "/api/auth/login", nil)
	req.Header.Set("Origin", "https://example.github.io")
	rec := httptest.NewRecorder()
	routes().ServeHTTP(rec, req)
	if rec.Code != 204 || rec.Header().Get("Access-Control-Allow-Origin") != "https://example.github.io" {
		t.Fatalf("preflight: %d %v", rec.Code, rec.Header())
	}
	req.Header.Set("Origin", "https://evil.example")
	rec = httptest.NewRecorder()
	routes().ServeHTTP(rec, req)
	if rec.Code != 403 || rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("untrusted preflight allowed")
	}
	req.Header.Set("Authorization", "Bearer isolated-session")
	if requestSession(req) != "isolated-session" {
		t.Fatal("bearer session missing")
	}
}

func TestCrossOriginBearerLoginAndRevocation(t *testing.T) {
	oldCfg, oldApp := cfg, app
	defer func() { cfg = oldCfg; app = oldApp }()
	app = testApp(t)
	cfg.AllowedOrigins = []string{"https://console.example"}
	h := routes()
	req := httptest.NewRequest("POST", "http://api.example/api/auth/login", strings.NewReader(`{"username":"alice","password":"secret123"}`))
	req.Header.Set("Origin", "https://console.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	token := rec.Header().Get("X-IotCloud-Session")
	if rec.Code != 200 || token == "" {
		t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
	}
	call := func(method, path, origin string) int {
		req := httptest.NewRequest(method, "http://api.example"+path, nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if call("GET", "/api/auth/me", "https://console.example") != 200 {
		t.Fatal("bearer session rejected")
	}
	if call("POST", "/api/auth/logout", "https://evil.example") != 403 {
		t.Fatal("untrusted mutation allowed")
	}
	if call("POST", "/api/auth/logout", "https://console.example") != 200 {
		t.Fatal("logout failed")
	}
	if call("GET", "/api/auth/me", "https://console.example") != 401 {
		t.Fatal("revoked bearer accepted")
	}
}
