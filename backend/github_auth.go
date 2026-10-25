package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Credentials are server-only; GitHub access tokens are used once and never persisted.
type GitHubConfig struct {
	ClientID         string `yaml:"client_id"`
	ClientSecretFile string `yaml:"client_secret_file"`
	secret           string
}

var githubHTTP = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
var githubTokenURL = "https://github.com/login/oauth/access_token"
var githubAPIURL = "https://api.github.com"
var browserProofPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func validateGitHubConfig() error {
	g := &cfg.GitHub
	if g.ClientID == "" && g.ClientSecretFile == "" {
		return nil
	}
	if g.ClientID == "" || g.ClientSecretFile == "" {
		return errors.New("GitHub requires client_id and client_secret_file")
	}
	u, err := url.Parse(cfg.PublicURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("GitHub requires an HTTPS public_url origin")
	}
	data, err := os.ReadFile(g.ClientSecretFile)
	if err != nil {
		return fmt.Errorf("read GitHub client secret: %w", err)
	}
	g.secret = strings.TrimSpace(string(data))
	if len(g.secret) < 16 {
		return errors.New("GitHub client secret is too short")
	}
	return nil
}
func githubEnabled() bool {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	return cfg.GitHub.ClientID != "" && cfg.GitHub.secret != ""
}
func handleGitHubStatus(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "GET") {
		return
	}
	jsonResp(w, map[string]bool{"enabled": githubEnabled()})
}

// Return to an explicit trusted frontend, including Pages subpaths. The fragment is
// assigned by the backend, not caller-controlled. No session token travels in a URL.
func githubReturnURL(raw, origin string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || len(raw) > 2048 || strings.ContainsAny(raw, "\\\r\n") {
		return "", errors.New(messageText("e.8f1fa2479432"))
	}
	targetOrigin := u.Scheme + "://" + u.Host
	cfgMu.RLock()
	public := strings.TrimRight(cfg.PublicURL, "/")
	cfgMu.RUnlock()
	if targetOrigin != origin || (targetOrigin != public && !allowedFrontend(targetOrigin)) || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"))) {
		return "", errors.New(messageText("e.a860e18970a9"))
	}
	return u.String(), nil
}
func handleGitHubStart(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "POST") || !sameOrigin(w, r) {
		return
	}
	if !githubEnabled() {
		jsonError(w, messageText("e.caf05a2ad17d"), 503)
		return
	}
	if !allowAttempt(r, "github-start", 10, time.Minute) {
		jsonError(w, messageText("e.9be2505cb51c"), 429)
		return
	}
	var req struct {
		ReturnURL        string `json:"return_url"`
		BrowserChallenge string `json:"browser_challenge"`
		Link             bool   `json:"link"`
	}
	if err := decode(r, &req); err != nil {
		jsonError(w, err.Error(), 400)
		return
	}
	target, err := githubReturnURL(req.ReturnURL, r.Header.Get("Origin"))
	if err != nil || !browserProofPattern.MatchString(req.BrowserChallenge) {
		jsonError(w, messageText("e.0c5a83d0e3bc"), 400)
		return
	}
	linkSession := ""
	if req.Link {
		linkSession = digest(requestSession(r))
		var user string
		if err = app.store.QueryRow("SELECT s.username FROM sessions s JOIN users u ON u.username=s.username WHERE s.token=? AND s.expires>? AND u.state='active'", linkSession, time.Now().Unix()).Scan(&user); err != nil {
			jsonError(w, messageText("e.b44953a2b3c9"), 401)
			return
		}
	}
	state, verifier := randomID(), randomID()+randomID()
	_, err = app.store.Exec("DELETE FROM github_oauth_flows WHERE expires<=?", time.Now().Unix())
	if err == nil {
		_, err = app.store.Exec("INSERT INTO github_oauth_flows(state,browser_challenge,verifier,return_url,link_session,expires) VALUES(?,?,?,?,?,?)", digest(state), req.BrowserChallenge, verifier, target, linkSession, time.Now().Add(10*time.Minute).Unix())
	}
	if err != nil {
		operationError(w, err)
		return
	}
	hash := sha256.Sum256([]byte(verifier))
	cfgMu.RLock()
	clientID, callback := cfg.GitHub.ClientID, strings.TrimRight(cfg.PublicURL, "/")+"/api/auth/github/callback"
	cfgMu.RUnlock()
	query := url.Values{"client_id": {clientID}, "redirect_uri": {callback}, "scope": {"user:email"}, "state": {state}, "code_challenge": {base64.RawURLEncoding.EncodeToString(hash[:])}, "code_challenge_method": {"S256"}}
	jsonResp(w, map[string]string{"authorization_url": "https://github.com/login/oauth/authorize?" + query.Encode(), "ticket": state})
}

type githubFlow struct {
	State, Challenge, Verifier, ReturnURL, LinkSession, Phase, Subject, Login, Email, Error string
	Expires                                                                                 int64
}

func readGitHubFlow(state string) (githubFlow, error) {
	f := githubFlow{State: digest(state)}
	err := app.store.QueryRow("SELECT browser_challenge,verifier,return_url,link_session,phase,subject,login,email,error,expires FROM github_oauth_flows WHERE state=? AND expires>?", f.State, time.Now().Unix()).Scan(&f.Challenge, &f.Verifier, &f.ReturnURL, &f.LinkSession, &f.Phase, &f.Subject, &f.Login, &f.Email, &f.Error, &f.Expires)
	return f, err
}
func githubRequest(r *http.Request, dst interface{}) error {
	response, err := githubHTTP.Do(r)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return errors.New("GitHub request failed")
	}
	return json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(dst)
}
func githubIdentity(r *http.Request, code, verifier string) (string, string, string, error) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	cfgMu.RLock()
	g, callback := cfg.GitHub, strings.TrimRight(cfg.PublicURL, "/")+"/api/auth/github/callback"
	cfgMu.RUnlock()
	values := url.Values{"client_id": {g.ClientID}, "client_secret": {g.secret}, "code": {code}, "redirect_uri": {callback}, "code_verifier": {verifier}}
	request, err := http.NewRequestWithContext(ctx, "POST", githubTokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return "", "", "", err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err = githubRequest(request, &token); err != nil || token.AccessToken == "" {
		return "", "", "", errors.New("GitHub token exchange failed")
	}
	get := func(path string, dst interface{}) error {
		q, e := http.NewRequestWithContext(ctx, "GET", githubAPIURL+path, nil)
		if e != nil {
			return e
		}
		q.Header.Set("Accept", "application/vnd.github+json")
		q.Header.Set("Authorization", "Bearer "+token.AccessToken)
		q.Header.Set("User-Agent", "IotCloud")
		return githubRequest(q, dst)
	}
	var user struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	}
	if err = get("/user", &user); err != nil || user.ID <= 0 || user.Login == "" {
		return "", "", "", errors.New("GitHub identity lookup failed")
	}
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err = get("/user/emails", &emails); err != nil {
		return "", "", "", err
	}
	email := ""
	for _, e := range emails {
		if e.Primary && e.Verified && len(e.Email) <= 254 && emailRegex.MatchString(e.Email) {
			email = strings.ToLower(e.Email)
			break
		}
	}
	return strconv.FormatInt(user.ID, 10), user.Login, email, nil
}
func handleGitHubCallback(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "GET") {
		return
	}
	w.Header().Set("Referrer-Policy", "no-referrer")
	if !githubEnabled() {
		jsonError(w, messageText("e.caf05a2ad17d"), 503)
		return
	}
	ticket := r.URL.Query().Get("state")
	f, err := readGitHubFlow(ticket)
	if err != nil || f.Phase != "pending" {
		jsonError(w, messageText("e.2650498d5197"), 400)
		return
	}
	result, err := app.store.Exec("UPDATE github_oauth_flows SET phase='processing' WHERE state=? AND phase='pending' AND expires>?", f.State, time.Now().Unix())
	if err != nil {
		operationError(w, err)
		return
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		jsonError(w, messageText("e.af4854ffbbbf"), 400)
		return
	}
	subject, login, email, message := "", "", "", ""
	if r.URL.Query().Get("error") != "" || r.URL.Query().Get("code") == "" {
		message = messageText("e.9334077996f7")
	} else {
		subject, login, email, err = githubIdentity(r, r.URL.Query().Get("code"), f.Verifier)
		if err != nil {
			message = messageText("e.6a978bf88a03")
		}
	}
	_, err = app.store.Exec("UPDATE github_oauth_flows SET phase='ready',verifier='',subject=?,login=?,email=?,error=? WHERE state=?", subject, login, email, message, f.State)
	if err != nil {
		operationError(w, err)
		return
	}
	target, _ := url.Parse(f.ReturnURL)
	target.Fragment = "/github?ticket=" + url.QueryEscape(ticket)
	http.Redirect(w, r, target.String(), http.StatusSeeOther)
}

func handleGitHubComplete(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "POST") || !sameOrigin(w, r) {
		return
	}
	if !githubEnabled() {
		jsonError(w, messageText("e.caf05a2ad17d"), 503)
		return
	}
	if !allowAttempt(r, "github-complete", 20, time.Minute) {
		jsonError(w, messageText("e.9be2505cb51c"), 429)
		return
	}
	var req struct {
		Ticket          string `json:"ticket"`
		BrowserVerifier string `json:"browser_verifier"`
		Username        string `json:"username"`
		Password        string `json:"password"`
	}
	if err := decode(r, &req); err != nil {
		jsonError(w, err.Error(), 400)
		return
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	f, err := readGitHubFlow(req.Ticket)
	if err != nil || f.Phase != "ready" || !browserProofPattern.MatchString(req.BrowserVerifier) || subtle.ConstantTimeCompare([]byte(digest(req.BrowserVerifier)), []byte(f.Challenge)) != 1 {
		jsonError(w, messageText("e.c8b7463c89de"), 400)
		return
	}
	// A flow can only be redeemed from the same trusted frontend that started it.
	if _, err = githubReturnURL(f.ReturnURL, r.Header.Get("Origin")); err != nil {
		jsonError(w, messageText("e.026effbde3cd"), 403)
		return
	}
	if f.Error != "" {
		app.store.Exec("DELETE FROM github_oauth_flows WHERE state=?", f.State)
		jsonError(w, f.Error, 400)
		return
	}
	var owner string
	err = app.store.QueryRow("SELECT username FROM github_identities WHERE subject=?", f.Subject).Scan(&owner)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		operationError(w, err)
		return
	}
	// Validate a new principal before claiming the flow or reserving an account.
	// Existing identities keep their original names and can still sign in.
	if owner == "" && f.LinkSession == "" && (req.Username != "" || req.Password != "") {
		req.Username = strings.TrimSpace(req.Username)
		if err = validateAccount(req.Username, req.Password); err != nil {
			jsonError(w, err.Error(), 400)
			return
		}
	}
	result, err := app.store.Exec("UPDATE github_oauth_flows SET phase='completing' WHERE state=? AND phase='ready' AND expires>?", f.State, time.Now().Unix())
	if err != nil {
		operationError(w, err)
		return
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		jsonError(w, messageText("e.af4854ffbbbf"), 400)
		return
	}
	consumed := false
	defer func() {
		if !consumed {
			app.store.Exec("UPDATE github_oauth_flows SET phase='ready' WHERE state=? AND phase='completing'", f.State)
		}
	}()
	// Resume interrupted Broker provisioning only for the identity that reserved it.
	resuming := false
	if owner != "" && f.LinkSession == "" {
		state, e := app.store.state(owner)
		if e != nil {
			operationError(w, e)
			return
		}
		if state == "pending" {
			if req.Username == "" && req.Password == "" {
				jsonResp(w, map[string]interface{}{"needs_registration": true, "github_login": f.Login, "username": owner})
				return
			}
			if req.Username != owner {
				jsonError(w, messageText("e.495c0e57a489"), 409)
				return
			}
			if e = validateAccount(owner, req.Password); e != nil {
				jsonError(w, e.Error(), 400)
				return
			}
			if e = app.registerLocked(owner, req.Password); e != nil {
				operationError(w, e)
				return
			}
			resuming = true
		}
	}
	if f.LinkSession != "" {
		var linkedUser string
		if digest(requestSession(r)) != f.LinkSession {
			jsonError(w, messageText("e.a8eac6146daf"), 403)
			return
		}
		if err = app.store.QueryRow("SELECT s.username FROM sessions s JOIN users u ON u.username=s.username WHERE s.token=? AND s.expires>? AND u.state='active'", f.LinkSession, time.Now().Unix()).Scan(&linkedUser); err != nil {
			jsonError(w, messageText("e.ed7faac67984"), 401)
			return
		}
		if owner != "" && owner != linkedUser {
			jsonError(w, messageText("e.5119665361dd"), 409)
			return
		}
		owner = linkedUser
		if err = saveGitHubIdentity(f, owner); err != nil {
			jsonError(w, messageText("e.c47314603a76"), 409)
			return
		}
	} else if owner == "" {
		cfgMu.RLock()
		level := cfg.RegisterLevel
		cfgMu.RUnlock()
		if level == 0 {
			jsonError(w, messageText("e.12a6d18e21bf"), 403)
			return
		}
		if level == 2 && f.Email == "" {
			jsonError(w, messageText("e.9fe5e5854355"), 403)
			return
		}
		if req.Username == "" && req.Password == "" {
			jsonResp(w, map[string]interface{}{"needs_registration": true, "github_login": f.Login})
			return
		}
		// Never adopt an existing platform account, even if its username/email matches.
		available, e := app.store.nameAvailable(req.Username)
		if e != nil {
			operationError(w, e)
			return
		}
		if !available {
			jsonError(w, messageText("e.ff56818dd391"), 409)
			return
		}
		// Reserve the identity and account together before touching the Broker. A failed
		// Broker operation retains ownership for password-authenticated recovery.
		if err = app.registerGitHubAccountLocked(f, req.Username, req.Password, level); err != nil {
			operationError(w, err)
			return
		}
		owner = req.Username
	} else {
		if !resuming && (req.Username != "" || req.Password != "") {
			jsonError(w, messageText("e.850ca422df35"), 409)
			return
		}
		if err = saveGitHubIdentity(f, owner); err != nil {
			operationError(w, err)
			return
		}
	}
	state, err := app.store.state(owner)
	if err != nil || state != "active" {
		jsonError(w, messageText("e.c54193e580e8"), 403)
		return
	}
	info, err := app.store.accountInfo(owner)
	if err != nil {
		operationError(w, err)
		return
	}
	// Consume before issuing a session so callback replay cannot issue another one.
	if _, err = app.store.Exec("DELETE FROM github_oauth_flows WHERE state=?", f.State); err != nil {
		operationError(w, err)
		return
	}
	consumed = true
	if f.LinkSession == "" {
		if err = createSession(w, owner); err != nil {
			operationError(w, err)
			return
		}
	}
	jsonResp(w, info)
}
func saveGitHubIdentity(f githubFlow, user string) error {
	result, err := app.store.Exec("INSERT INTO github_identities(subject,username,login) VALUES(?,?,?) ON CONFLICT(subject) DO UPDATE SET login=excluded.login WHERE github_identities.username=excluded.username", f.Subject, user, f.Login)
	if err == nil {
		n, _ := result.RowsAffected()
		if n != 1 {
			return errConflict
		}
	}
	return err
}
func (a *Application) registerGitHubAccountLocked(f githubFlow, user, password string, level int) error {
	if err := validateAccount(user, password); err != nil {
		return err
	}
	if level == 2 && (!emailRegex.MatchString(f.Email) || len(f.Email) > 254) {
		return errors.New(messageText("e.132431225954"))
	}
	tx, err := a.store.Begin()
	if err != nil {
		return err
	}
	var count int
	err = tx.QueryRow("SELECT (SELECT COUNT(*) FROM users WHERE username=?)+(SELECT COUNT(*) FROM account_aliases WHERE alias=?)", user, user).Scan(&count)
	if err == nil && count > 0 {
		err = errAccountName
	}
	if err == nil {
		_, err = tx.Exec("INSERT INTO users(username,state) VALUES(?,'pending')", user)
	}
	if err == nil {
		_, err = tx.Exec("INSERT INTO github_identities(subject,username,login) VALUES(?,?,?)", f.Subject, user, f.Login)
	}
	if err == nil {
		salt := randomID()
		_, err = tx.Exec("INSERT INTO account_passwords(username,hash) VALUES(?,?)", user, salt+":"+websiteHash(password, salt))
	}
	if err == nil && level == 2 {
		var count int
		err = tx.QueryRow("SELECT (SELECT COUNT(*) FROM users WHERE username=?)+(SELECT COUNT(*) FROM account_aliases WHERE alias=?)", f.Email, f.Email).Scan(&count)
		if err == nil && count > 0 {
			err = errAccountName
		}
		if err == nil {
			_, err = tx.Exec("INSERT INTO account_aliases(alias,username,kind,state) VALUES(?,?,'email','active')", f.Email, user)
		}
	}
	if err == nil {
		err = tx.Commit()
	} else {
		tx.Rollback()
	}
	if err != nil {
		return err
	}
	// The existing account initializer owns Broker provisioning and recovery.
	return a.registerLocked(user, password)
}
func handleGitHubUnlink(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "POST") {
		return
	}
	if !allowAttempt(r, "github-unlink", 10, time.Minute) {
		jsonError(w, messageText("e.9be2505cb51c"), 429)
		return
	}
	var req struct {
		Password string `json:"current_password"`
	}
	if err := decode(r, &req); err != nil {
		jsonError(w, err.Error(), 400)
		return
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	user := currentUser(r)
	if err := app.authenticateWebsite(user, user, req.Password); err != nil {
		operationError(w, err)
		return
	}
	tx, err := app.store.Begin()
	if err != nil {
		operationError(w, err)
		return
	}
	defer tx.Rollback()
	if _, err = tx.Exec("DELETE FROM github_identities WHERE username=?", user); err != nil {
		operationError(w, err)
		return
	}
	// Invalidate in-flight link requests as well as sessions after unlinking.
	if _, err = tx.Exec("DELETE FROM sessions WHERE username=?", user); err != nil {
		operationError(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		operationError(w, err)
		return
	}
	http.SetCookie(w, sessionCookie("", -1))
	jsonResp(w, map[string]bool{"ok": true})
}
