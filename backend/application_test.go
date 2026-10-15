package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type fakeBroker struct {
	fail      bool
	roles     map[string]bool
	attached  map[string]map[string]bool
	passwords map[string]string
	disabled  map[string]bool
	acl       map[string][]Rule
}

func (b *fakeBroker) Authenticate(u, p string) error {
	if b.fail {
		return errUnavailable
	}
	if b.disabled[u] || b.passwords[u] != p {
		return errCredentials
	}
	return nil
}
func (b *fakeBroker) Command(c string, p map[string]interface{}) (json.RawMessage, error) {
	if b.fail {
		return nil, errUnavailable
	}
	u, _ := p["username"].(string)
	r, _ := p["rolename"].(string)
	switch c {
	case "createClient":
		b.passwords[u] = p["password"].(string)
		b.attached[u] = map[string]bool{}
	case "getClient":
		if _, ok := b.passwords[u]; !ok {
			return nil, &brokerFailure{"not found"}
		}
		roles := []map[string]string{}
		for r := range b.attached[u] {
			roles = append(roles, map[string]string{"rolename": r})
		}
		data, _ := json.Marshal(map[string]interface{}{"client": map[string]interface{}{"roles": roles}})
		return data, nil
	case "deleteClient":
		delete(b.passwords, u)
		delete(b.attached, u)
	case "disableClient":
		if b.disabled == nil {
			b.disabled = map[string]bool{}
		}
		b.disabled[u] = true
	case "enableClient":
		delete(b.disabled, u)
	case "setClientPassword":
		b.passwords[u] = p["password"].(string)
	case "getRole":
		if !b.roles[r] {
			return nil, &brokerFailure{"not found"}
		}
	case "createRole", "modifyRole":
		b.roles[r] = true
		if b.acl == nil {
			b.acl = map[string][]Rule{}
		}
		b.acl[r], _ = p["acls"].([]Rule)
	case "deleteRole":
		delete(b.roles, r)
		delete(b.acl, r)
	case "addClientRole":
		b.attached[u][r] = true
	case "removeClientRole":
		delete(b.attached[u], r)
	}
	return json.RawMessage(`{}`), nil
}
func testApp(t *testing.T) *Application {
	t.Helper()
	s, e := openStore(filepath.Join(t.TempDir(), "app.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	b := &fakeBroker{roles: map[string]bool{}, attached: map[string]map[string]bool{}, passwords: map[string]string{}}
	a := &Application{store: s, broker: b}
	for _, u := range []string{"alice", "bob"} {
		if e = a.register(u, "secret123"); e != nil {
			t.Fatal(e)
		}
	}
	return a
}
func TestOwnershipFailureRecoveryAndRelease(t *testing.T) {
	a := testApp(t)
	b := a.broker.(*fakeBroker)
	d := Device{MAC: "AA:BB:CC:DD:EE:FF", Model: "m1"}
	b.fail = true
	if e := a.saveDevice("alice", d, false); !errors.Is(e, errUnavailable) {
		t.Fatal(e)
	}
	got, _ := a.store.device("aabbccddeeff")
	if got.Status != "pending" {
		t.Fatal(got)
	}
	if e := a.saveDevice("bob", d, false); !errors.Is(e, errConflict) {
		t.Fatal(e)
	}
	b.fail = false
	if e := a.reconcile(); e != nil {
		t.Fatal(e)
	}
	got, _ = a.store.device(got.MAC)
	if got.Status != "active" {
		t.Fatal(got)
	}
	if e := a.removeDevice("bob", got.MAC); !errors.Is(e, sql.ErrNoRows) {
		t.Fatal(e)
	}
	b.fail = true
	if e := a.removeDevice("alice", got.MAC); !errors.Is(e, errUnavailable) {
		t.Fatal(e)
	}
	if e := a.saveDevice("bob", d, false); !errors.Is(e, errConflict) {
		t.Fatal(e)
	}
	b.fail = false
	if e := a.reconcile(); e != nil {
		t.Fatal(e)
	}
	if b.attached["alice"]["device_"+got.MAC] {
		t.Fatal("role retained")
	}
	if e := a.saveDevice("bob", d, false); e != nil {
		t.Fatal(e)
	}
}
func TestConcurrentExclusiveMAC(t *testing.T) {
	a := testApp(t)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, u := range []string{"alice", "bob"} {
		wg.Add(1)
		go func(u string) { defer wg.Done(); results <- a.saveDevice(u, Device{MAC: "aabbccddeeff"}, false) }(u)
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for e := range results {
		if e == nil {
			success++
		} else if errors.Is(e, errConflict) {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal(success, conflict)
	}
}
func TestSessionsCSRFAndAdminSeparation(t *testing.T) {
	app = testApp(t)
	cfg.AdminUsername = "admin"
	cfg.PublicURL = "http://example.test"
	h := routes()
	r := httptest.NewRequest("POST", "http://example.test/api/auth/login", strings.NewReader(`{"username":"alice","password":"secret123"}`))
	r.Header.Set("Origin", "http://example.test")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly {
		t.Fatal(cookies)
	}
	check := func(method, path, origin, body string) int {
		r := httptest.NewRequest(method, "http://example.test"+path, strings.NewReader(body))
		r.AddCookie(cookies[0])
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if n := check("GET", "/api/devices", "", ""); n != 200 {
		t.Fatal(n)
	}
	if n := check("POST", "/api/devices", "https://evil.test", `{"mac":"aabbccddeeff"}`); n != 403 {
		t.Fatal(n)
	}
	if n := check("GET", "/api/admin/users", "", ""); n != 403 {
		t.Fatal(n)
	}
	if n := check("POST", "/api/devices", "http://example.test", `{"mac":"aabbccddeeff","username":"bob"}`); n != 400 {
		t.Fatal(n)
	}
	if e := app.changePassword("alice", "newsecret123"); e != nil {
		t.Fatal(e)
	}
	if n := check("GET", "/api/devices", "", ""); n != 401 {
		t.Fatal(n)
	}
	if safeReturnTo("//evil.test") != "" || safeReturnTo("/\\evil.test") != "" {
		t.Fatal("redirect accepted")
	}
}

func TestAccountAndDeviceInputBoundaries(t *testing.T) {
	cfg.AdminUsername = "admin"
	cfg.BrokerAdminUsername = serviceUser
	for _, u := range []string{"valid-user", "valid+tag@example.com"} {
		if e := validateAccount(u, "secret123"); e != nil {
			t.Fatal(u, e)
		}
	}
	for _, u := range []string{"admin", serviceUser, "__reserved", "abc\\def", "abc/def", "abc\nxyz"} {
		if e := validateAccount(u, "secret123"); e == nil {
			t.Fatal("unsafe account", u)
		}
	}
	if !emailRegex.MatchString("name+tag@example.com") || emailRegex.MatchString("name@example/com") {
		t.Fatal("email format")
	}
	for _, mac := range []string{"aa:bb-cc:dd:ee:ff", "aabbccddeefg", "aabbccddeeff/#"} {
		if isValidMAC(mac) {
			t.Fatal("unsafe MAC", mac)
		}
	}
}
