package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnifiedAdminAuthorization(t *testing.T) {
	previousCfg, previousApp := cfg, app
	t.Cleanup(func() { cfg = previousCfg; app = previousApp })
	app = testApp(t)
	cfg.AdminUsername = "admin"
	cfg.PublicURL = "http://example.test"
	h := routes()
	request := func(cookie *http.Cookie, method, path, origin, body, bearer string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://example.test"+path, strings.NewReader(body))
		r.Header.Set("Origin", origin)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		if bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	login := request(nil, "POST", "/api/auth/login", cfg.PublicURL, `{"username":"alice","password":"secret123"}`, "")
	if login.Code != 200 || !strings.Contains(login.Body.String(), `"is_admin":false`) {
		t.Fatal(login.Code, login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	for _, path := range []string{"/api/admin/users", "/api/admin/devices", "/api/admin/config", "/api/admin/verification-emails", "/api/admin/acl"} {
		if w := request(nil, "GET", path, "", "", "forged"); w.Code != 401 {
			t.Fatal(path, w.Code)
		}
		if w := request(cookie, "GET", path, "", "", ""); w.Code != 403 {
			t.Fatal(path, w.Code)
		}
	}
	if w := request(nil, "POST", "/api/admin/login", cfg.PublicURL, `{"username":"admin","password":"anything"}`, ""); w.Code != 404 {
		t.Fatal(w.Code)
	}
	if _, err := app.store.Exec("INSERT INTO user_roles(username,role) VALUES('alice','admin')"); err != nil {
		t.Fatal(err)
	}
	if w := request(cookie, "GET", "/api/admin/users", "", "", ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := request(cookie, "GET", "/api/auth/me", "", "", ""); !strings.Contains(w.Body.String(), `"is_admin":true`) {
		t.Fatal(w.Body.String())
	}
	for _, origin := range []string{"", "https://evil.test"} {
		if w := request(cookie, "PUT", "/api/admin/user/bob/role", origin, `{"role":"admin"}`, ""); w.Code != 403 {
			t.Fatal(w.Code)
		}
	}
	if w := request(cookie, "PUT", "/api/admin/user/alice/role", cfg.PublicURL, `{"role":"user"}`, ""); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(cookie, "DELETE", "/api/admin/user/alice", cfg.PublicURL, "", ""); w.Code != 409 {
		t.Fatal(w.Code)
	}
	if w := request(cookie, "PUT", "/api/admin/user/bob/role", cfg.PublicURL, `{"role":"admin"}`, ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(cookie, "PUT", "/api/admin/user/alice/role", cfg.PublicURL, `{"role":"user"}`, ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := request(cookie, "GET", "/api/admin/users", "", "", ""); w.Code != 403 {
		t.Fatal("revoked role accepted", w.Code)
	}
	if _, err := app.store.Exec("INSERT INTO user_roles(username,role) VALUES('alice','admin')"); err != nil {
		t.Fatal(err)
	}
	if w := request(cookie, "POST", "/api/auth/logout", cfg.PublicURL, `{}`, ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := request(cookie, "GET", "/api/admin/users", "", "", ""); w.Code != 401 {
		t.Fatal("logged out session accepted", w.Code)
	}
}
func TestAdminBootstrapOnceAndNoPrivilegeAdoption(t *testing.T) {
	previousCfg, previousApp := cfg, app
	t.Cleanup(func() { cfg = previousCfg; app = previousApp })
	app = testApp(t)
	cfg.AdminUsername = "admin"
	cfg.BrokerAdminUsername = serviceUser
	cfg.AdminPasswordFile = filepath.Join(t.TempDir(), "admin-password")
	if err := os.WriteFile(cfg.AdminPasswordFile, []byte("bootstrap-password-123"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := app.bootstrapAdmin(); err != nil {
		t.Fatal(err)
	}
	if !app.store.isAdmin("admin") {
		t.Fatal("bootstrap role absent")
	}
	if err := app.broker.Authenticate("admin", "bootstrap-password-123"); err != nil {
		t.Fatal(err)
	}
	if err := app.changePassword("admin", "changed-password-123"); err != nil {
		t.Fatal(err)
	}
	if err := app.bootstrapAdmin(); err != nil {
		t.Fatal(err)
	}
	if err := app.authenticateWebsite("admin", "admin", "changed-password-123"); err != nil {
		t.Fatal("restart reset password", err)
	}
	app = testApp(t)
	cfg.AdminUsername = "alice"
	if err := app.bootstrapAdmin(); err == nil || app.store.isAdmin("alice") {
		t.Fatal("existing username adopted")
	}
}
