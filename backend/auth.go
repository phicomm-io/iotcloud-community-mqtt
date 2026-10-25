package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}
type userContextKey struct{}

func currentUser(r *http.Request) string {
	u, _ := r.Context().Value(userContextKey{}).(string)
	return u
}
func decode(r *http.Request, v interface{}) error {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return errors.New(messageText("e.2529b19bc41c"))
	}
	if err := d.Decode(new(interface{})); err != io.EOF {
		return errors.New(messageText("e.2529b19bc41c"))
	}
	return nil
}
func method(w http.ResponseWriter, r *http.Request, expected string) bool {
	if r.Method != expected {
		w.Header().Set("Allow", expected)
		jsonError(w, messageText("e.4f34574afee4"), 405)
		return false
	}
	return true
}
func sameOrigin(w http.ResponseWriter, r *http.Request) bool {
	origin := r.Header.Get("Origin")
	u, err := url.Parse(origin)
	cfgMu.RLock()
	expected := cfg.PublicURL
	cfgMu.RUnlock()
	valid := err == nil && u.Host == r.Host && (u.Scheme == "http" || u.Scheme == "https") && u.Path == ""
	if expected != "" {
		valid = origin == strings.TrimRight(expected, "/")
	}
	valid = valid || allowedFrontend(origin)
	if !valid {
		jsonError(w, messageText("e.2f31aac81862"), 403)
	}
	return valid
}
func userAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := requestSession(r)
		if token == "" {
			jsonError(w, messageText("e.48144f96c150"), 401)
			return
		}
		var user string
		err := app.store.QueryRow("SELECT s.username FROM sessions s JOIN users u ON s.username=u.username WHERE s.token=? AND s.expires>? AND u.state='active'", digest(token), time.Now().Unix()).Scan(&user)
		if err != nil {
			jsonError(w, messageText("e.48c14e645b3b"), 401)
			return
		}
		if r.Method != http.MethodGet && !sameOrigin(w, r) {
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userContextKey{}, user)))
	}
}
func sessionCookie(value string, age int) *http.Cookie {
	cfgMu.RLock()
	secure := cfg.CookieSecure
	cfgMu.RUnlock()
	return &http.Cookie{Name: "phicomm_session", Value: value, Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: age}
}

const sessionLifetime = 30 * 24 * time.Hour

func createSession(w http.ResponseWriter, user string) error {
	token := randomID()
	app.store.Exec("DELETE FROM sessions WHERE expires<=?", time.Now().Unix())
	_, err := app.store.Exec("INSERT INTO sessions(token,username,expires) VALUES(?,?,?)", digest(token), user, time.Now().Add(sessionLifetime).Unix())
	if err != nil {
		return err
	}
	http.SetCookie(w, sessionCookie(token, int(sessionLifetime/time.Second)))
	w.Header().Set("X-IotCloud-Session", token)
	return nil
}

var rateMu sync.Mutex
var rates = map[string]struct {
	start time.Time
	n     int
}{}

func allowAttempt(r *http.Request, key string, limit int, period time.Duration) bool {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	cfgMu.RLock()
	trust := cfg.TrustProxy
	cfgMu.RUnlock()
	if trust {
		if ip := net.ParseIP(r.Header.Get("X-Real-IP")); ip != nil {
			host = ip.String()
		}
	}
	key = host + ":" + key
	rateMu.Lock()
	defer rateMu.Unlock()
	now := time.Now()
	for k, v := range rates {
		if now.Sub(v.start) > 10*time.Minute {
			delete(rates, k)
		}
	}
	v := rates[key]
	if now.Sub(v.start) >= period {
		v.start = now
		v.n = 0
	}
	if v.n >= limit || len(rates) >= 10000 {
		return false
	}
	v.n++
	rates[key] = v
	return true
}
func handleLogin(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "POST") || !sameOrigin(w, r) {
		return
	}
	if !allowAttempt(r, "login", 20, time.Minute) {
		jsonError(w, messageText("e.9be2505cb51c"), 429)
		return
	}
	var req loginReq
	if err := decode(r, &req); err != nil {
		jsonError(w, err.Error(), 400)
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	app.mu.Lock()
	defer app.mu.Unlock()
	owner, principal, err := app.store.resolveAccount(req.Username)
	if errors.Is(err, sql.ErrNoRows) && emailRegex.MatchString(req.Username) {
		owner, principal, err = app.store.resolveAccount(strings.ToLower(req.Username))
	}
	if err != nil {
		jsonError(w, errCredentials.Error(), 401)
		return
	}
	state, err := app.store.state(owner)
	if err != nil || (state != "active" && state != "pending") || reservedUser(req.Username) {
		jsonError(w, errCredentials.Error(), 401)
		return
	}
	if err = app.authenticateWebsite(owner, principal, req.Password); err != nil {
		operationError(w, err)
		return
	}
	if state == "pending" {
		var githubPending int
		if err = app.store.QueryRow("SELECT COUNT(*) FROM github_identities WHERE username=?", owner).Scan(&githubPending); err != nil {
			operationError(w, err)
			return
		}
		// An OAuth reservation has a website hash before Broker provisioning.
		// Do not activate it until the device credentials actually work.
		if githubPending > 0 {
			if err = app.broker.Authenticate(principal, req.Password); err != nil {
				operationError(w, err)
				return
			}
		}
		if _, err = app.store.Exec("UPDATE users SET state='active' WHERE username=? AND state='pending'", owner); err != nil {
			operationError(w, err)
			return
		}
	}
	if err = createSession(w, owner); err != nil {
		operationError(w, err)
		return
	}
	info, err := app.store.accountInfo(owner)
	if err != nil {
		operationError(w, err)
		return
	}
	jsonResp(w, info)
}
func handleLogout(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "POST") {
		return
	}
	if _, err := app.store.Exec("DELETE FROM sessions WHERE token=?", digest(requestSession(r))); err != nil {
		operationError(w, err)
		return
	}
	http.SetCookie(w, sessionCookie("", -1))
	jsonResp(w, map[string]bool{"ok": true})
}
func handleMe(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "GET") {
		return
	}
	info, err := app.store.accountInfo(currentUser(r))
	if err != nil {
		operationError(w, err)
		return
	}
	jsonResp(w, info)
}
func handlePassword(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "POST") {
		return
	}
	var req struct {
		CurrentPassword string `json:"current_password"`
		Password        string `json:"password"`
	}
	if err := decode(r, &req); err != nil {
		jsonError(w, err.Error(), 400)
		return
	}
	u := currentUser(r)
	if err := app.authenticateWebsite(u, u, req.CurrentPassword); err != nil {
		operationError(w, err)
		return
	}
	if err := app.changePassword(u, req.Password); err != nil {
		operationError(w, err)
		return
	}
	http.SetCookie(w, sessionCookie("", -1))
	jsonResp(w, map[string]string{"message": messageText("e.9c0b05f1e23d")})
}
func (a *Application) changeMQTTPassword(user, password string) error {
	if len(password) < 6 || len(password) > 256 {
		return errors.New(messageText("e.447af662b999"))
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	state, err := a.store.state(user)
	if err != nil || state != "active" {
		return sql.ErrNoRows
	}
	if _, err = a.store.Exec("UPDATE account_passwords SET mqtt_separate=1 WHERE username=?", user); err != nil {
		return err
	}
	aliases, err := a.store.aliases(user)
	if err != nil {
		return err
	}
	for _, alias := range aliases {
		if alias.Kind == "mqtt" {
			if _, err = a.store.Exec("UPDATE account_aliases SET state='password_pending' WHERE alias=?", alias.Name); err != nil {
				return err
			}
			if _, err = a.broker.Command("disableClient", map[string]interface{}{"username": alias.Name}); err != nil {
				return err
			}
		}
	}
	if _, err = a.broker.Command("setClientPassword", map[string]interface{}{"username": user, "password": password}); err != nil {
		return err
	}
	for _, alias := range aliases {
		if alias.Kind == "mqtt" {
			if _, err = a.broker.Command("setClientPassword", map[string]interface{}{"username": alias.Name, "password": password}); err != nil {
				return err
			}
			if _, err = a.broker.Command("enableClient", map[string]interface{}{"username": alias.Name}); err != nil {
				return err
			}
			if _, err = a.store.Exec("UPDATE account_aliases SET state='active' WHERE alias=?", alias.Name); err != nil {
				return err
			}
		}
	}
	return a.syncAccount(user)
}
func adminAuth(next http.HandlerFunc) http.HandlerFunc {
	return userAuth(func(w http.ResponseWriter, r *http.Request) {
		if !app.store.isAdmin(currentUser(r)) {
			jsonError(w, messageText("e.90343cedc288"), http.StatusForbidden)
			return
		}
		next(w, r)
	})
}
func operationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errAccountName):
		jsonError(w, err.Error(), 409)
	case errors.Is(err, errLastAdmin):
		jsonError(w, err.Error(), 409)
	case errors.Is(err, errConflict):
		jsonError(w, err.Error(), 409)
	case errors.Is(err, sql.ErrNoRows):
		jsonError(w, messageText("e.e4ba03868d92"), 404)
	case errors.Is(err, errCredentials):
		jsonError(w, err.Error(), 401)
	case errors.Is(err, errUnavailable):
		jsonError(w, errUnavailable.Error(), 503)
	default:
		var brokerErr *brokerFailure
		if errors.As(err, &brokerErr) {
			jsonError(w, messageText("e.f0a422fd901c"), 503)
		} else {
			jsonError(w, messageText("e.5b81b37e66d1"), 500)
		}
	}
}
