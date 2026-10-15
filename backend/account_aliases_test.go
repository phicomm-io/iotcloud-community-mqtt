package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestAccountAliasesShareCredentialsRolesAndDeviceOwner(t *testing.T) {
	a := testApp(t)
	const email = "legacy@example.com"
	if err := a.register(email, "secret123"); err != nil {
		t.Fatal(err)
	}
	if err := a.addMQTTAlias(email, "short_name", "incorrect"); !errors.Is(err, errCredentials) {
		t.Fatal(err)
	}
	if err := a.addMQTTAlias(email, "alice", "secret123"); !errors.Is(err, errAccountName) {
		t.Fatal(err)
	}
	if err := a.addMQTTAlias(email, "short_name", "secret123"); err != nil {
		t.Fatal(err)
	}
	if err := a.addMQTTAlias("bob", "short_name", "secret123"); !errors.Is(err, errAccountName) {
		t.Fatal(err)
	}
	if err := a.register("short_name", "secret123"); !errors.Is(err, errAccountName) {
		t.Fatal(err)
	}
	owner, principal, err := a.store.resolveAccount("short_name")
	if err != nil || owner != email || principal != "short_name" {
		t.Fatal(owner, principal, err)
	}
	b := a.broker.(*fakeBroker)
	if err = b.Authenticate("short_name", "secret123"); err != nil {
		t.Fatal(err)
	}
	const mac = "aabbccddeeff"
	for _, name := range []string{email, "short_name"} {
		if err = a.discover("phicomm/v2/"+userKey(name)+"/discovery/"+mac, []byte(`{"schema":1,"protocol":"tmcp","protocol_version":2,"version":"v2","model":"m1"}`)); err != nil {
			t.Fatal(err)
		}
	}
	ds, err := a.store.devices(email)
	if err != nil || len(ds) != 2 {
		t.Fatal(ds, err)
	}
	if deviceRole(ds[0]) == deviceRole(ds[1]) {
		t.Fatal("MQTT namespaces collided")
	}
	for _, d := range ds {
		if d.Username != email || !b.attached["short_name"][deviceRole(d)] || !b.attached[email][deviceRole(d)] {
			t.Fatal(d)
		}
	}
	if err = a.saveDevice(email, Device{MAC: "001122334455", Model: "a1"}, false); err != nil {
		t.Fatal(err)
	}
	if !b.attached["short_name"]["device_001122334455"] {
		t.Fatal("legacy role not shared")
	}
	if err = a.changePassword(email, "replacement123"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{email, "short_name"} {
		if b.Authenticate(name, "secret123") != errCredentials {
			t.Fatal("old password accepted")
		}
		if err = b.Authenticate(name, "replacement123"); err != nil {
			t.Fatal(err)
		}
	}
	if err = a.deleteUser(email); err != nil {
		t.Fatal(err)
	}
	if _, _, err = a.store.resolveAccount("short_name"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	if _, ok := b.passwords["short_name"]; ok {
		t.Fatal("MQTT alias client survived account deletion")
	}
}

func TestEmailRegistrationRequiresShortUsernameAndSeparatesEmail(t *testing.T) {
	app = testApp(t)
	t.Cleanup(func() { app = nil })
	old := cfg
	t.Cleanup(func() { cfg = old })
	cfg.RegisterLevel = 2
	cfg.AdminUsername = "admin"
	cfg.PublicURL = "http://example.test"
	rateMu.Lock()
	clear(rates)
	rateMu.Unlock()
	call := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://example.test/api/register", strings.NewReader(body))
		r.Header.Set("Origin", cfg.PublicURL)
		w := httptest.NewRecorder()
		routes().ServeHTTP(w, r)
		return w
	}
	storeCode("new@example.com", "123456")
	if w := call(`{"username":"new@example.com","email":"new@example.com","password":"secret123","code":"123456"}`); w.Code != 400 {
		t.Fatal(w.Code, w.Body)
	}
	if w := call(`{"username":"new_user","password":"secret123","code":"123456"}`); w.Code != 400 {
		t.Fatal(w.Code, w.Body)
	}
	if w := call(`{"username":"new_user","email":"new@example.com","password":"secret123","code":"123456"}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	owner, principal, err := app.store.resolveAccount("new@example.com")
	if err != nil || owner != "new_user" || principal != "new_user" {
		t.Fatal(owner, principal, err)
	}
	r := httptest.NewRequest("POST", "http://example.test/api/auth/login", strings.NewReader(`{"username":"new@example.com","password":"secret123"}`))
	r.Header.Set("Origin", cfg.PublicURL)
	w := httptest.NewRecorder()
	routes().ServeHTTP(w, r)
	var data map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &data)
	if w.Code != 200 || data["username"] != "new_user" || data["mqtt_username"] != "new_user" {
		t.Fatal(w.Code, data)
	}
	if err = app.register("new@example.com", "secret123"); err != errAccountName {
		t.Fatal("email login alias collision", err)
	}
}

func TestScopedIdentityDatabaseUpgrade(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE users(username TEXT PRIMARY KEY,state TEXT NOT NULL);INSERT INTO users VALUES('alice','active');CREATE TABLE scoped_devices(username TEXT NOT NULL REFERENCES users(username) ON DELETE CASCADE,mac TEXT NOT NULL,model TEXT NOT NULL,version TEXT NOT NULL,status TEXT NOT NULL,PRIMARY KEY(username,mac));INSERT INTO scoped_devices VALUES('alice','aabbccddeeff','m1','community','hidden');`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	s, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var principal, status string
	if err = s.QueryRow("SELECT mqtt_username,status FROM scoped_devices WHERE username='alice'").Scan(&principal, &status); err != nil || principal != "alice" || status != "hidden" {
		t.Fatal(principal, status, err)
	}
}

type passwordFailBroker struct {
	Broker
	alias string
}

func (b passwordFailBroker) Command(command string, p map[string]interface{}) (json.RawMessage, error) {
	if command == "setClientPassword" && p["username"] == b.alias {
		return nil, errUnavailable
	}
	return b.Broker.Command(command, p)
}
func TestAliasPasswordFailureCannotLeaveOldAliasCredentialsEnabled(t *testing.T) {
	a := testApp(t)
	if err := a.addMQTTAlias("alice", "alice_short", "secret123"); err != nil {
		t.Fatal(err)
	}
	b := a.broker.(*fakeBroker)
	a.broker = passwordFailBroker{b, "alice_short"}
	if err := a.changePassword("alice", "replacement123"); err != errUnavailable {
		t.Fatal(err)
	}
	if b.Authenticate("alice_short", "secret123") != errCredentials {
		t.Fatal("stale alias password still enabled")
	}
	if err := a.reconcile(); err != nil {
		t.Fatal(err)
	}
	if b.Authenticate("alice_short", "secret123") != errCredentials {
		t.Fatal("reconcile enabled stale credentials")
	}
	a.broker = b
	if err := a.addMQTTAlias("alice", "alice_short", "replacement123"); err != nil {
		t.Fatal(err)
	}
	if err := b.Authenticate("alice_short", "replacement123"); err != nil {
		t.Fatal(err)
	}
}
