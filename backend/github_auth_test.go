package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type githubFixture struct {
	t              *testing.T
	h              http.Handler
	login, email   string
	subject        int
	verified, fail bool
	tokenCalls     int
}

func newGitHubFixture(t *testing.T) *githubFixture {
	t.Helper()
	previousCfg, previousApp, oldToken, oldAPI := cfg, app, githubTokenURL, githubAPIURL
	t.Cleanup(func() { cfg = previousCfg; app = previousApp; githubTokenURL = oldToken; githubAPIURL = oldAPI })
	cfg = Config{PublicURL: "https://backend.example", AllowedOrigins: []string{"https://console.example"}, RegisterLevel: 2, AdminUsername: "admin", BrokerAdminUsername: "__manager", GitHub: GitHubConfig{ClientID: "test-client", secret: "test-client-secret"}}
	app = testApp(t)
	rateMu.Lock()
	rates = map[string]struct {
		start time.Time
		n     int
	}{}
	rateMu.Unlock()
	f := &githubFixture{t: t, h: routes(), login: "octocat", subject: 42, email: "verified@example.com", verified: true}
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f.fail {
			w.WriteHeader(503)
			return
		}
		switch r.URL.Path {
		case "/token":
			f.tokenCalls++
			r.ParseForm()
			if r.Method != "POST" || r.Form.Get("client_secret") != "test-client-secret" || r.Form.Get("redirect_uri") != "https://backend.example/api/auth/github/callback" {
				t.Error("invalid token exchange")
			}
			flow, err := readGitHubFlow(r.Form.Get("code"))
			if err != nil || flow.Verifier != r.Form.Get("code_verifier") {
				t.Error("PKCE verifier not tied to flow")
			}
			jsonResp(w, map[string]string{"access_token": "synthetic-github-token"})
		case "/user":
			if r.Header.Get("Authorization") != "Bearer synthetic-github-token" {
				t.Error("missing API authentication")
			}
			jsonResp(w, map[string]interface{}{"id": f.subject, "login": f.login})
		case "/user/emails":
			jsonResp(w, []map[string]interface{}{{"email": f.email, "verified": f.verified, "primary": true}})
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(mock.Close)
	githubTokenURL = mock.URL + "/token"
	githubAPIURL = mock.URL
	return f
}
func (f *githubFixture) request(method, path, origin, token string, body interface{}) *httptest.ResponseRecorder {
	f.t.Helper()
	data, _ := json.Marshal(body)
	r := httptest.NewRequest(method, "https://backend.example"+path, strings.NewReader(string(data)))
	r.Header.Set("Origin", origin)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
}
func githubJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err, w.Body.String())
	}
	return result
}
func (f *githubFixture) loginAs(user string) string {
	f.t.Helper()
	w := f.request("POST", "/api/auth/login", "https://console.example", "", map[string]string{"username": user, "password": "secret123"})
	if w.Code != 200 {
		f.t.Fatal(w.Code, w.Body.String())
	}
	return w.Header().Get("X-IotCloud-Session")
}
func (f *githubFixture) start(link bool, token string) (string, string) {
	f.t.Helper()
	proof := strings.Repeat("a", 64)
	w := f.request("POST", "/api/auth/github/start", "https://console.example", token, map[string]interface{}{"return_url": "https://console.example/iotcloud/", "browser_challenge": digest(proof), "link": link})
	if w.Code != 200 {
		f.t.Fatal(w.Code, w.Body.String())
	}
	result := githubJSON(f.t, w)
	authorization, _ := url.Parse(result["authorization_url"].(string))
	ticket := result["ticket"].(string)
	q := authorization.Query()
	if authorization.Host != "github.com" || q.Get("scope") != "user:email" || q.Get("state") != ticket || q.Get("code_challenge_method") != "S256" {
		f.t.Fatal("invalid authorization request")
	}
	flow, err := readGitHubFlow(ticket)
	if err != nil {
		f.t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(flow.Verifier))
	if q.Get("code_challenge") != base64.RawURLEncoding.EncodeToString(hash[:]) {
		f.t.Fatal("invalid PKCE challenge")
	}
	return ticket, proof
}
func (f *githubFixture) callback(ticket string) *httptest.ResponseRecorder {
	f.t.Helper()
	w := f.request("GET", "/api/auth/github/callback?state="+ticket+"&code="+ticket, "", "", nil)
	if w.Code != 303 || w.Header().Get("Location") != "https://console.example/iotcloud/#/github?ticket="+ticket {
		f.t.Fatal(w.Code, w.Header().Get("Location"), w.Body.String())
	}
	if strings.Contains(w.Header().Get("Location"), "synthetic-github-token") {
		f.t.Fatal("credential in redirect")
	}
	return w
}
func (f *githubFixture) complete(ticket, proof, token, user, password string) *httptest.ResponseRecorder {
	return f.request("POST", "/api/auth/github/complete", "https://console.example", token, map[string]string{"ticket": ticket, "browser_verifier": proof, "username": user, "password": password})
}
func TestGitHubLinkAndStableIdentityLogin(t *testing.T) {
	f := newGitHubFixture(t)
	token := f.loginAs("alice")
	if err := app.saveDevice("alice", Device{MAC: "aabbccddeeff", Model: "m1"}, false); err != nil {
		t.Fatal(err)
	}
	ticket, proof := f.start(true, token)
	f.callback(ticket)
	w := f.complete(ticket, proof, token, "", "")
	if w.Code != 200 || githubJSON(t, w)["username"] != "alice" {
		t.Fatal(w.Code, w.Body.String())
	}
	f.login = "renamed-on-github"
	ticket, proof = f.start(false, "")
	f.callback(ticket)
	w = f.complete(ticket, proof, "", "", "")
	if w.Code != 200 || w.Header().Get("X-IotCloud-Session") == "" {
		t.Fatal(w.Code, w.Body.String())
	}
	info := githubJSON(t, w)
	if info["username"] != "alice" || info["github_login"] != f.login || info["is_admin"] != false {
		t.Fatal(info)
	}
	devices, err := app.store.devices("alice")
	if err != nil || len(devices) != 1 {
		t.Fatal(devices, err)
	}
	if err = app.broker.Authenticate("alice", "secret123"); err != nil {
		t.Fatal("binding changed MQTT password")
	}
	cfg.RegisterLevel = 0
	ticket, proof = f.start(false, "")
	f.callback(ticket)
	if w = f.complete(ticket, proof, "", "", ""); w.Code != 200 {
		t.Fatal("closed registration blocked linked login", w.Code)
	}
	app.store.Exec("UPDATE users SET state='disabled' WHERE username='alice'")
	ticket, proof = f.start(false, "")
	f.callback(ticket)
	if w = f.complete(ticket, proof, "", "", ""); w.Code != 403 {
		t.Fatal("disabled account signed in", w.Code)
	}
}
func TestGitHubRegistrationConflictVerifiedEmailAndBrokerRecovery(t *testing.T) {
	f := newGitHubFixture(t)
	ticket, proof := f.start(false, "")
	f.callback(ticket)
	w := f.complete(ticket, proof, "", "", "")
	if w.Code != 200 || githubJSON(t, w)["needs_registration"] != true {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = f.complete(ticket, proof, "", "alice", "secret123"); w.Code != 409 {
		t.Fatal("adopted existing username", w.Code)
	}
	// The GitHub email is already an existing account's alias: do not merge accounts.
	app.store.Exec("INSERT INTO account_aliases(alias,username,kind,state) VALUES(?,'alice','email','active')", f.email)
	if w = f.complete(ticket, proof, "", "new-user", "secret456"); w.Code != 409 {
		t.Fatal("adopted email alias", w.Code, w.Body.String())
	}
	if _, err := app.store.state("new-user"); err == nil {
		t.Fatal("failed registration left an account")
	}
	app.store.Exec("DELETE FROM account_aliases WHERE alias=?", f.email)
	app.broker.(*fakeBroker).fail = true
	if w = f.complete(ticket, proof, "", "new-user", "secret456"); w.Code != 503 {
		t.Fatal(w.Code, w.Body.String())
	}
	app.broker.(*fakeBroker).fail = false
	login := f.request("POST", "/api/auth/login", "https://console.example", "", map[string]string{"username": "new-user", "password": "secret456"})
	if login.Code == 200 {
		t.Fatal("website login activated account before Broker provisioning")
	}
	if err := app.register("new-user", "attacker-password"); err == nil {
		t.Fatal("public registration adopted OAuth reservation")
	}
	// Provisioning can be resumed in another authorization flow after an outage.
	ticket, proof = f.start(false, "")
	f.callback(ticket)
	w = f.complete(ticket, proof, "", "", "")
	if w.Code != 200 || githubJSON(t, w)["username"] != "new-user" {
		t.Fatal(w.Code, w.Body.String())
	}
	w = f.complete(ticket, proof, "", "new-user", "secret456")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if err := app.broker.Authenticate("new-user", "secret456"); err != nil {
		t.Fatal(err)
	}
	if err := app.authenticateWebsite("new-user", "new-user", "secret456"); err != nil {
		t.Fatal(err)
	}
	owner, _, err := app.store.resolveAccount(f.email)
	if err != nil || owner != "new-user" {
		t.Fatal(owner, err)
	}
	if w = f.complete(ticket, proof, "", "", ""); w.Code != 400 {
		t.Fatal("redeemed twice", w.Code)
	}
	var persisted int
	app.store.QueryRow("SELECT COUNT(*) FROM github_oauth_flows WHERE verifier!=''").Scan(&persisted)
	// Only the abandoned, already processed first flow remains, with its verifier cleared.
	if persisted != 0 {
		t.Fatal("PKCE verifier retained after callback")
	}
}
func TestGitHubFlowOriginProofExpiryDenialAndReplay(t *testing.T) {
	f := newGitHubFixture(t)
	for _, target := range []string{"https://evil.example/", "https://console.example.evil/", "https://console.example@evil.example/", "https://console.example/iotcloud/#fragment", "https://console.example/iotcloud/?query=yes"} {
		w := f.request("POST", "/api/auth/github/start", "https://console.example", "", map[string]interface{}{"return_url": target, "browser_challenge": digest(strings.Repeat("a", 64))})
		if w.Code != 400 {
			t.Fatal(target, w.Code)
		}
	}
	if w := f.request("POST", "/api/auth/github/start", "https://evil.example", "", nil); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := f.request("POST", "/api/auth/github/start", "https://console.example", "", map[string]interface{}{"return_url": "https://console.example/", "browser_challenge": digest(strings.Repeat("a", 64)), "link": true}); w.Code != 401 {
		t.Fatal(w.Code)
	}
	ticket, proof := f.start(false, "")
	f.callback(ticket)
	if w := f.complete(ticket, strings.Repeat("b", 64), "", "", ""); w.Code != 400 {
		t.Fatal("cross-browser login accepted")
	}
	w := f.request("POST", "/api/auth/github/complete", "https://backend.example", "", map[string]string{"ticket": ticket, "browser_verifier": proof})
	if w.Code != 403 {
		t.Fatal("cross-frontend redemption accepted", w.Code)
	}
	w = f.request("GET", "/api/auth/github/callback?state="+ticket+"&code="+ticket, "", "", nil)
	if w.Code != 400 || f.tokenCalls != 1 {
		t.Fatal("callback replay exchanged code", w.Code, f.tokenCalls)
	}
	app.store.Exec("UPDATE github_oauth_flows SET expires=? WHERE state=?", time.Now().Add(-time.Second).Unix(), digest(ticket))
	if w = f.complete(ticket, proof, "", "", ""); w.Code != 400 {
		t.Fatal("expired flow accepted")
	}
	ticket, proof = f.start(false, "")
	w = f.request("GET", "/api/auth/github/callback?state="+ticket+"&error=access_denied", "", "", nil)
	if w.Code != 303 {
		t.Fatal(w.Code)
	}
	if w = f.complete(ticket, proof, "", "", ""); w.Code != 400 || !strings.Contains(w.Body.String(), "授权未完成") {
		t.Fatal(w.Code, w.Body.String())
	}
	f.fail = true
	ticket, proof = f.start(false, "")
	f.callback(ticket)
	if w = f.complete(ticket, proof, "", "", ""); w.Code != 400 || !strings.Contains(w.Body.String(), "无法完成 GitHub 验证") {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestGitHubRegistrationPolicyAndLinkSessionChanges(t *testing.T) {
	f := newGitHubFixture(t)
	cfg.RegisterLevel = 0
	ticket, proof := f.start(false, "")
	f.callback(ticket)
	if w := f.complete(ticket, proof, "", "new-user", "secret456"); w.Code != 403 {
		t.Fatal("closed registration bypassed", w.Code)
	}
	cfg.RegisterLevel = 2
	f.verified = false
	ticket, proof = f.start(false, "")
	f.callback(ticket)
	if w := f.complete(ticket, proof, "", "new-user", "secret456"); w.Code != 403 {
		t.Fatal("unverified email accepted", w.Code)
	}
	cfg.RegisterLevel = 1
	ticket, proof = f.start(false, "")
	f.callback(ticket)
	if w := f.complete(ticket, proof, "", "new-user", "secret456"); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	token := f.loginAs("alice")
	ticket, proof = f.start(true, token)
	f.callback(ticket)
	if w := f.complete(ticket, proof, token, "", ""); w.Code != 409 {
		t.Fatal("GitHub identity attached to two accounts", w.Code)
	}
	f.subject = 43
	ticket, proof = f.start(true, token)
	f.callback(ticket)
	bobToken := f.loginAs("bob")
	if w := f.complete(ticket, proof, bobToken, "", ""); w.Code != 403 {
		t.Fatal("switched platform account during bind", w.Code)
	}
	app.store.Exec("DELETE FROM sessions WHERE token=?", digest(token))
	if w := f.complete(ticket, proof, token, "", ""); w.Code != 401 {
		t.Fatal("revoked link session accepted", w.Code)
	}
}
func TestGitHubUnlinkRequiresPasswordAndRevokesSessions(t *testing.T) {
	f := newGitHubFixture(t)
	token := f.loginAs("alice")
	ticket, proof := f.start(true, token)
	f.callback(ticket)
	if w := f.complete(ticket, proof, token, "", ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	w := f.request("POST", "/api/auth/github/unlink", "https://console.example", token, map[string]string{"current_password": "wrong-password"})
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	var count int
	app.store.QueryRow("SELECT COUNT(*) FROM github_identities").Scan(&count)
	if count != 1 {
		t.Fatal("wrong password unlinked identity")
	}
	w = f.request("POST", "/api/auth/github/unlink", "https://console.example", token, map[string]string{"current_password": "secret123"})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	app.store.QueryRow("SELECT COUNT(*) FROM github_identities").Scan(&count)
	if count != 0 {
		t.Fatal("identity not removed")
	}
	if w = f.request("GET", "/api/auth/me", "https://console.example", token, nil); w.Code != 401 {
		t.Fatal("session not revoked")
	}
}
func TestGitHubConcurrentCompletionIssuesOneSession(t *testing.T) {
	f := newGitHubFixture(t)
	token := f.loginAs("alice")
	ticket, proof := f.start(true, token)
	f.callback(ticket)
	if w := f.complete(ticket, proof, token, "", ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	ticket, proof = f.start(false, "")
	f.callback(ticket)
	var wg sync.WaitGroup
	results := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- f.complete(ticket, proof, "", "", "").Code }()
	}
	wg.Wait()
	close(results)
	codes := map[int]int{}
	for code := range results {
		codes[code]++
	}
	if codes[200] != 1 || codes[400] != 1 {
		t.Fatal(codes)
	}
}
func TestGitHubConfigDisabledAndSecretFileValidation(t *testing.T) {
	previous := cfg
	t.Cleanup(func() { cfg = previous })
	cfg = Config{}
	if err := validateGitHubConfig(); err != nil || githubEnabled() {
		t.Fatal(err)
	}
	cfg.GitHub.ClientID = "client"
	if err := validateGitHubConfig(); err == nil {
		t.Fatal("partial config accepted")
	}
	file := filepath.Join(t.TempDir(), "github-secret")
	if err := os.WriteFile(file, []byte("test-client-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg.GitHub.ClientSecretFile = file
	for _, public := range []string{"http://example.com", "https://example.com/path", "https://user@example.com", "https://example.com?x=y"} {
		cfg.PublicURL = public
		if err := validateGitHubConfig(); err == nil {
			t.Fatal(public)
		}
	}
	cfg.PublicURL = "https://example.com"
	if err := validateGitHubConfig(); err != nil || !githubEnabled() {
		t.Fatal(err)
	}
}
