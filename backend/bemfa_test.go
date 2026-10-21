package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testBemfa(t *testing.T) (*Application, *BemfaRelay, Device) {
	t.Helper()
	a := testApp(t)
	seal, err := openBemfaKey(filepath.Join(t.TempDir(), "key"))
	if err != nil {
		t.Fatal(err)
	}
	r := newBemfaRelay(a, seal)
	a.bemfa = r
	d := Device{Namespace: "account", Username: "alice", MQTTUsername: "alice", MAC: "aabbccddeeff", Model: "tc1", Version: "v2", Status: "active"}
	_, err = a.store.Exec("INSERT INTO scoped_devices(username,mqtt_username,mac,model,version,status) VALUES(?,?,?,?,?,?)", d.Username, d.MQTTUsername, d.MAC, d.Model, d.Version, d.Status)
	if err != nil {
		t.Fatal(err)
	}
	return a, r, d
}
func bemfaRequest(t *testing.T, a *Application, user, method, body string) *httptest.ResponseRecorder {
	t.Helper()
	previous := app
	app = a
	defer func() { app = previous }()
	req := httptest.NewRequest(method, "http://localhost/api/integrations/bemfa", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), userContextKey{}, user))
	rec := httptest.NewRecorder()
	handleBemfa(rec, req)
	return rec
}
func TestBemfaWriteOnlyEncryptedPersistentCredentials(t *testing.T) {
	a, r, d := testBemfa(t)
	key := strings.Repeat("a", 32)
	body, _ := json.Marshal(BemfaConfig{APIKey: key, Mappings: []BemfaMapping{{Device: d, Channel: 2, Topic: "washer001"}}})
	// Disabled configuration does not start any external connection.
	res := bemfaRequest(t, a, "alice", http.MethodPut, string(body))
	if res.Code != 200 || strings.Contains(res.Body.String(), key) {
		t.Fatal(res.Code, res.Body.String())
	}
	var raw []byte
	if err := a.store.QueryRow("SELECT config FROM bemfa_integrations WHERE username='alice'").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(key)) || bytes.Contains(raw, []byte("washer001")) {
		t.Fatal("config stored in plaintext")
	}
	c, err := r.readConfig("alice")
	if err != nil || c.APIKey != key {
		t.Fatal(c, err)
	}
	res = bemfaRequest(t, a, "alice", http.MethodPut, `{"enabled":false,"mappings":[]}`)
	if res.Code != 200 {
		t.Fatal(res.Body.String())
	}
	c, _ = r.readConfig("alice")
	if c.APIKey != key {
		t.Fatal("blank key not preserved")
	}
	res = bemfaRequest(t, a, "bob", http.MethodGet, "")
	if strings.Contains(res.Body.String(), "washer") || strings.Contains(res.Body.String(), `"key_set":true`) {
		t.Fatal("cross-account read", res.Body.String())
	}
	res = bemfaRequest(t, a, "bob", http.MethodPut, `{"api_key":"`+key+`","enabled":false,"mappings":[]}`)
	if res.Code != 400 {
		t.Fatal("shared client ID accepted", res.Code)
	}
	// A different encryption key or account AAD cannot decrypt the stored data.
	if _, err := r.seal.Open(nil, raw[:r.seal.NonceSize()], raw[r.seal.NonceSize():], []byte("bob")); err == nil {
		t.Fatal("wrong owner decrypted")
	}
	res = bemfaRequest(t, a, "alice", http.MethodDelete, "")
	if res.Code != 200 {
		t.Fatal(res.Body.String())
	}
	c, err = r.readConfig("alice")
	if err != nil || c.APIKey != "" {
		t.Fatal("not removed", err)
	}
}
func TestBemfaKeyDurabilityAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key")
	a, err := openBemfaKey(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := openBemfaKey(path)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, a.NonceSize())
	sealed := a.Seal(nil, nonce, []byte("secret"), []byte("alice"))
	if plain, err := b.Open(nil, nonce, sealed, []byte("alice")); err != nil || string(plain) != "secret" {
		t.Fatal("restart decryption", err)
	}
	stat, _ := os.Stat(path)
	if stat.Mode().Perm() != 0600 {
		t.Fatal(stat.Mode())
	}
	if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := openBemfaKey(path); err == nil {
		t.Fatal("corrupt key accepted")
	}
}
func TestBemfaOwnershipAndMappings(t *testing.T) {
	a, r, d := testBemfa(t)
	c := BemfaConfig{Enabled: true, APIKey: strings.Repeat("a", 32), Mappings: []BemfaMapping{{Device: d, Channel: 0, Topic: "plug001"}}}
	if err := r.validate("alice", &c); err != nil {
		t.Fatal(err)
	}
	if err := r.validate("bob", &c); err == nil {
		t.Fatal("foreign device allowed")
	}
	tests := []struct {
		name   string
		change func(*BemfaConfig)
	}{
		{"wrong suffix", func(c *BemfaConfig) { c.Mappings[0].Topic = "plug003" }},
		{"wildcard", func(c *BemfaConfig) { c.Mappings[0].Topic = "plug/+001" }},
		{"all outlets", func(c *BemfaConfig) { c.Mappings[0].Channel = -1 }},
		{"seventh outlet", func(c *BemfaConfig) { c.Mappings[0].Channel = 6 }},
		{"namespace", func(c *BemfaConfig) { c.Mappings[0].Device.MQTTUsername = "bob" }},
		{"legacy", func(c *BemfaConfig) { c.Mappings[0].Device.Version = "v1" }},
		{"duplicate", func(c *BemfaConfig) { c.Mappings = append(c.Mappings, c.Mappings[0]) }},
		{"no mapping", func(c *BemfaConfig) { c.Mappings = nil }},
		{"no key", func(c *BemfaConfig) { c.APIKey = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next := c
			next.Mappings = append([]BemfaMapping(nil), c.Mappings...)
			tt.change(&next)
			if r.validate("alice", &next) == nil {
				t.Fatal("invalid mapping accepted")
			}
		})
	}
	_, _ = a.store.Exec("UPDATE scoped_devices SET status='hidden' WHERE username='alice'")
	if r.validate("alice", &c) == nil {
		t.Fatal("removed device allowed")
	}
}
func TestBemfaCommandsAndCanonicalReports(t *testing.T) {
	tc := BemfaMapping{Device: Device{Model: "tc1"}, Channel: 5}
	fan := BemfaMapping{Device: Device{Model: "a1"}}
	light := fan
	light.Channel = 1
	for i, speed := range []int{20, 50, 75, 100} {
		action, p, err := bemfaCommand(fan, "on#"+string(rune('1'+i)))
		if err != nil || action != "fan.set" || p["speed"] != speed || p["mode"] != "manual" {
			t.Fatal(action, p, err)
		}
	}
	action, p, err := bemfaCommand(tc, "off")
	if err != nil || action != "outlet.set" || p["index"] != 5 || p["on"] != false {
		t.Fatal(action, p, err)
	}
	_, p, err = bemfaCommand(light, "on#50")
	if err != nil || p["brightness"] != 128 || p["enabled"] != true {
		t.Fatal(p, err)
	}
	for _, bad := range []string{"ON", "on#0", "on#5", "on#01", "on#-1", "on#1x", "off#1", "on\x00", "on#1000", " on"} {
		if _, _, err := bemfaCommand(fan, bad); err == nil {
			t.Fatal("accepted", bad)
		}
	}
	if _, _, err := bemfaCommand(tc, "on#1"); err == nil {
		t.Fatal("socket level accepted")
	}
	cases := []struct {
		mapping     BemfaMapping
		state, want string
	}{
		{tc, `{"socket_states":[0,0,0,0,0,1]}`, "on"}, {tc, `{"socket_states":[0,0,0,0,0,-1]}`, ""},
		{fan, `{"fan_on":true,"percentage":50,"speed":100}`, "on#2"}, {fan, `{"fan_on":false,"percentage":100}`, "off"},
		{fan, `{"fan_on":true,"percentage":0}`, "off"}, {fan, `{"requested_fan_on":true,"requested_speed":100}`, ""},
		{light, `{"led_visible":false,"led_enabled":true,"brightness":255}`, "off"}, {light, `{"led_visible":true,"brightness":128}`, "on#50"},
	}
	for _, tt := range cases {
		var state map[string]interface{}
		_ = json.Unmarshal([]byte(tt.state), &state)
		if got := bemfaReport(tt.mapping, state); got != tt.want {
			t.Fatal(tt.state, got, tt.want)
		}
	}
}
func TestBemfaRouteAuthenticationAndCSRF(t *testing.T) {
	a, _, _ := testBemfa(t)
	previous := app
	app = a
	defer func() { app = previous }()
	res := httptest.NewRecorder()
	routes().ServeHTTP(res, httptest.NewRequest("GET", "/api/integrations/bemfa", nil))
	if res.Code != 401 {
		t.Fatal(res.Code)
	}
	_, err := a.store.Exec("INSERT INTO sessions(token,username,expires) VALUES(?,?,?)", digest("testtoken"), "alice", time.Now().Add(time.Hour).Unix())
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("PUT", "http://localhost/api/integrations/bemfa", strings.NewReader(`{"enabled":false,"mappings":[]}`))
	req.AddCookie(&http.Cookie{Name: "phicomm_session", Value: "testtoken"})
	req.Header.Set("Origin", "https://evil.test")
	res = httptest.NewRecorder()
	routes().ServeHTTP(res, req)
	if res.Code != 403 {
		t.Fatal(res.Code)
	}
}
