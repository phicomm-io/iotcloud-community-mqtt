package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Restore an account from before the MQTT principal policy. Creation tests must
// use the application entry points instead of this migration fixture.
func seedLegacyAccount(t *testing.T, a *Application, user, password string) {
	t.Helper()
	if _, err := a.store.Exec("INSERT INTO users(username,state) VALUES(?,'active')", user); err != nil {
		t.Fatal(err)
	}
	if _, err := a.broker.Command("createClient", map[string]interface{}{"username": user, "password": password}); err != nil {
		t.Fatal(err)
	}
	if err := a.syncAccount(user); err != nil {
		t.Fatal(err)
	}
}

type usernameBrokerTrace struct {
	Broker
	calls int
}

func (b *usernameBrokerTrace) Command(command string, params map[string]interface{}) (json.RawMessage, error) {
	b.calls++
	return b.Broker.Command(command, params)
}

func (b *usernameBrokerTrace) Authenticate(user, password string) error {
	b.calls++
	return b.Broker.Authenticate(user, password)
}

type usernameFixture struct {
	t     *testing.T
	h     http.Handler
	trace *usernameBrokerTrace
}

func newUsernameFixture(t *testing.T) *usernameFixture {
	t.Helper()
	previousCfg, previousApp := cfg, app
	t.Cleanup(func() { cfg = previousCfg; app = previousApp })
	cfg = Config{PublicURL: "http://example.test", RegisterLevel: 1, AdminUsername: "admin", BrokerAdminUsername: serviceUser}
	app = testApp(t)
	rateMu.Lock()
	clear(rates)
	rateMu.Unlock()
	trace := &usernameBrokerTrace{Broker: app.broker}
	app.broker = trace
	return &usernameFixture{t: t, h: routes(), trace: trace}
}

func (f *usernameFixture) request(method, path, token string, body interface{}) *httptest.ResponseRecorder {
	f.t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		f.t.Fatal(err)
	}
	r := httptest.NewRequest(method, cfg.PublicURL+path, strings.NewReader(string(data)))
	r.Header.Set("Origin", cfg.PublicURL)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
}

func (f *usernameFixture) login(user, password string) *httptest.ResponseRecorder {
	f.t.Helper()
	w := f.request("POST", "/api/auth/login", "", map[string]string{"username": user, "password": password})
	if w.Code != 200 || w.Header().Get("X-IotCloud-Session") == "" {
		f.t.Fatal(w.Code, w.Body.String())
	}
	return w
}

func usernameDBChanges(t *testing.T, a *Application) int64 {
	t.Helper()
	// openStore pins one SQLite connection, so this also catches writes that were
	// later rolled back or restored to their previous values.
	var changes int64
	if err := a.store.QueryRow("SELECT total_changes()").Scan(&changes); err != nil {
		t.Fatal(err)
	}
	return changes
}

func assertUsernameNoEffects(t *testing.T, a *Application, trace *usernameBrokerTrace, before int64) {
	t.Helper()
	if after := usernameDBChanges(t, a); after != before || trace.calls != 0 {
		t.Fatalf("invalid principal caused effects: SQLite changes %d -> %d, broker calls %d", before, after, trace.calls)
	}
}

func TestUsernameHTTPBoundariesBeforeDatabaseAndBrokerWrites(t *testing.T) {
	for _, entry := range []struct{ path, method, field string }{
		{"/api/register", "POST", "username"},
		{"/api/admin/users", "POST", "username"},
		{"/api/auth/alias", "PUT", "alias"},
	} {
		t.Run(entry.path, func(t *testing.T) {
			f := newUsernameFixture(t)
			token := ""
			if entry.path != "/api/register" {
				if _, err := app.store.Exec("INSERT INTO user_roles(username,role) VALUES('alice','admin')"); err != nil {
					t.Fatal(err)
				}
				token = f.login("alice", "secret123").Header().Get("X-IotCloud-Session")
			}
			for _, user := range []string{strings.Repeat("a", 33), "valid+tag@example.com", "__reserved", "ab", "中文用户", "abc/def"} {
				before := usernameDBChanges(t, app)
				f.trace.calls = 0
				w := f.request(entry.method, entry.path, token, map[string]string{entry.field: user, "password": "secret123"})
				if w.Code != 400 || !strings.Contains(w.Body.String(), "e.1ef6a7c0d805") {
					t.Fatalf("%q: %d %s", user, w.Code, w.Body.String())
				}
				assertUsernameNoEffects(t, app, f.trace, before)
			}
			user := "User_" + strings.Repeat("a", 24) + ".-9"
			if len(user) != 32 {
				t.Fatal("invalid boundary fixture")
			}
			w := f.request(entry.method, entry.path, token, map[string]string{entry.field: user, "password": "secret123"})
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			owner, principal, err := app.store.resolveAccount(user)
			if entry.field == "alias" {
				if err != nil || owner != "alice" || principal != user {
					t.Fatal(owner, principal, err)
				}
			} else if err != nil || owner != user || principal != user {
				t.Fatal(owner, principal, err)
			}
			if err = app.broker.Authenticate(user, "secret123"); err != nil {
				t.Fatal("32-byte principal was not provisioned", err)
			}
		})
	}
}

func TestUsernameInternalCreationEntriesCannotBypassPolicy(t *testing.T) {
	f := newUsernameFixture(t)
	flow := githubFlow{Subject: "new-subject", Login: "octocat", Email: "verified@example.com"}
	entries := []struct {
		name string
		call func(string, string) error
	}{
		{"register", app.register},
		{"registerLocked", app.registerLocked},
		{"registerWithEmail", func(u, p string) error { return app.registerWithEmail(u, p, flow.Email, true) }},
		{"registerGitHubAccountLocked", func(u, p string) error { return app.registerGitHubAccountLocked(flow, u, p, 2) }},
		{"addMQTTAlias", func(u, p string) error { return app.addMQTTAlias("alice", u, p) }},
	}
	for _, entry := range entries {
		t.Run(entry.name, func(t *testing.T) {
			for _, name := range []string{strings.Repeat("u", 33), "new@example.com"} {
				before := usernameDBChanges(t, app)
				f.trace.calls = 0
				if err := entry.call(name, "secret123"); !errors.Is(err, errInvalidUsername) {
					t.Fatal(name, err)
				}
				assertUsernameNoEffects(t, app, f.trace, before)
			}
		})
	}
	for _, entry := range entries[:4] {
		t.Run(entry.name+"Password", func(t *testing.T) {
			before := usernameDBChanges(t, app)
			f.trace.calls = 0
			if err := entry.call("valid-new-name", "short"); err == nil {
				t.Fatal("invalid password accepted")
			}
			assertUsernameNoEffects(t, app, f.trace, before)
		})
	}
	tooLongEmail := strings.Repeat("a", 243) + "@example.com"
	for _, entry := range []func() error{
		func() error { return app.registerWithEmail("email-user", "secret123", tooLongEmail, true) },
		func() error {
			flow.Email = tooLongEmail
			return app.registerGitHubAccountLocked(flow, "github-user", "secret123", 2)
		},
	} {
		before := usernameDBChanges(t, app)
		f.trace.calls = 0
		if err := entry(); err == nil {
			t.Fatal("255-byte email accepted")
		}
		assertUsernameNoEffects(t, app, f.trace, before)
	}
}

func TestUsernameEmail254IsOnlyAVerifiedLoginAlias(t *testing.T) {
	f := newUsernameFixture(t)
	cfg.RegisterLevel = 2
	email := strings.Repeat("a", 64) + "@" + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 57) + ".com"
	if len(email) != 254 {
		t.Fatal("invalid email boundary fixture", len(email))
	}
	seedVerification(t, app.store, email, "123456")
	for _, body := range []map[string]string{
		{"username": email, "email": email, "password": "secret123", "code": "123456"},
		{"username": strings.Repeat("u", 33), "email": email, "password": "secret123", "code": "123456"},
		{"username": "email-user", "email": "z" + email, "password": "secret123", "code": "123456"},
	} {
		before := usernameDBChanges(t, app)
		f.trace.calls = 0
		w := f.request("POST", "/api/register", "", body)
		if w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
		assertUsernameNoEffects(t, app, f.trace, before)
	}
	user := strings.Repeat("u", 32)
	w := f.request("POST", "/api/register", "", map[string]string{"username": user, "email": email, "password": "secret123", "code": "123456"})
	if w.Code != 200 {
		t.Fatal("rejected input consumed the email verification", w.Code, w.Body.String())
	}
	owner, principal, err := app.store.resolveAccount(email)
	if err != nil || owner != user || principal != user {
		t.Fatal(owner, principal, err)
	}
	if _, ok := f.trace.Broker.(*fakeBroker).passwords[email]; ok {
		t.Fatal("email was provisioned as an MQTT principal")
	}
	info := githubJSON(t, f.login(email, "secret123"))
	if info["username"] != user || info["mqtt_username"] != user {
		t.Fatal(info)
	}
}

func TestUsernameLegacyLongPrimaryCanLoginAndCreateShortMQTTAlias(t *testing.T) {
	f := newUsernameFixture(t)
	legacy := "legacy+" + strings.Repeat("x", 65) + "@example.com"
	seedLegacyAccount(t, app, legacy, "secret123")
	login := f.login(legacy, "secret123")
	if info := githubJSON(t, login); info["username"] != legacy || info["mqtt_username"] != legacy {
		t.Fatal(info)
	}
	alias := "mqtt_" + strings.Repeat("a", 27)
	w := f.request("PUT", "/api/auth/alias", login.Header().Get("X-IotCloud-Session"), map[string]string{"alias": alias, "password": "secret123"})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, name := range []string{legacy, alias} {
		if info := githubJSON(t, f.login(name, "secret123")); info["username"] != legacy || info["mqtt_username"] != alias {
			t.Fatal(info)
		}
	}
	if err := app.changePassword(legacy, "replacement123"); err != nil {
		t.Fatal(err)
	}
	if info := githubJSON(t, f.login(legacy, "replacement123")); info["username"] != legacy {
		t.Fatal("legacy account was renamed or locked out", info)
	}
}

func TestUsernameGitHubBoundaryDoesNotClaimFlowOrReservePrincipal(t *testing.T) {
	f := newGitHubFixture(t)
	ticket, proof := f.start(false, "")
	f.callback(ticket)
	trace := &usernameBrokerTrace{Broker: app.broker}
	app.broker = trace
	for _, user := range []string{strings.Repeat("g", 33), f.email} {
		before := usernameDBChanges(t, app)
		w := f.complete(ticket, proof, "", user, "secret123")
		if w.Code != 400 || !strings.Contains(w.Body.String(), "e.1ef6a7c0d805") {
			t.Fatal(w.Code, w.Body.String())
		}
		assertUsernameNoEffects(t, app, trace, before)
		flow, err := readGitHubFlow(ticket)
		if err != nil || flow.Phase != "ready" {
			t.Fatal("invalid username claimed the flow", flow.Phase, err)
		}
	}
	user := strings.Repeat("g", 32)
	w := f.complete(ticket, proof, "", user, "secret123")
	if w.Code != 200 || githubJSON(t, w)["username"] != user {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestUsernameGitHubKeepsExistingLongPrimary(t *testing.T) {
	f := newGitHubFixture(t)
	legacy := "legacy-" + strings.Repeat("g", 40)
	seedLegacyAccount(t, app, legacy, "secret123")
	if _, err := app.store.Exec("INSERT INTO github_identities(subject,username,login) VALUES('42',?,?)", legacy, f.login); err != nil {
		t.Fatal(err)
	}
	cfg.RegisterLevel = 0
	ticket, proof := f.start(false, "")
	f.callback(ticket)
	w := f.complete(ticket, proof, "", "", "")
	if w.Code != 200 || githubJSON(t, w)["username"] != legacy {
		t.Fatal("existing long GitHub account was locked out", w.Code, w.Body.String())
	}
}

func TestUsernameBootstrapLimitAndCompletedLegacyAdmin(t *testing.T) {
	f := newUsernameFixture(t)
	cfg.AdminUsername = strings.Repeat("a", 33)
	before := usernameDBChanges(t, app)
	if err := app.bootstrapAdmin(); err == nil {
		t.Fatal("33-byte bootstrap username accepted")
	}
	assertUsernameNoEffects(t, app, f.trace, before)
	// An upgrade must not revalidate or recreate an already bootstrapped admin.
	if _, err := app.store.Exec("INSERT INTO settings(key,value) VALUES('unified_admin_bootstrapped','1')"); err != nil {
		t.Fatal(err)
	}
	before = usernameDBChanges(t, app)
	if err := app.bootstrapAdmin(); err != nil {
		t.Fatal("completed legacy bootstrap was rejected", err)
	}
	assertUsernameNoEffects(t, app, f.trace, before)
}
