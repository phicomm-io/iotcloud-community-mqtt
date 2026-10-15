package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestScopedDiscoveryIsolationAndLegacyOwnership(t *testing.T) {
	a := testApp(t)
	mac := "aabbccddeeff"
	payload := []byte(`{"schema":1,"protocol":"tmcp","protocol_version":2,"version":"v2","model":"m1"}`)
	for _, u := range []string{"alice", "bob"} {
		if err := a.discover("phicomm/v2/"+userKey(u)+"/discovery/"+mac, payload); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.saveDevice("alice", Device{MAC: mac, Model: "m1"}, false); err != nil {
		t.Fatal(err)
	}
	if err := a.saveDevice("bob", Device{MAC: mac, Model: "m1"}, false); err != errConflict {
		t.Fatal("legacy no longer first-come", err)
	}
	list, err := a.store.devices("alice")
	if err != nil || len(list) != 2 {
		t.Fatal(list, err)
	}
	if deviceRole(list[0]) == deviceRole(list[1]) {
		t.Fatal("roles collided")
	}
	for _, topic := range []string{"phicomm/m1/" + mac + "/sensor", "phicomm/discovery/alice/" + mac, "phicomm/v2/" + userKey("alice") + "/discovery/AA:BB:CC:DD:EE:FF"} {
		if a.discover(topic, payload) == nil {
			t.Fatal("malformed announcement accepted", topic)
		}
	}
	if err = a.discover("phicomm/v2/"+userKey("alice")+"/discovery/"+mac, []byte(`{"schema":1,"protocol":"tmcp","protocol_version":2,"version":"v2","model":"a1"}`)); err != errConflict {
		t.Fatal("model overwritten", err)
	}
	// Offline ACL changes persist pending and reconcile without claiming success.
	b := a.broker.(*fakeBroker)
	b.fail = true
	if err = a.discover("phicomm/v2/"+userKey("alice")+"/discovery/001122334455", payload); err != errUnavailable {
		t.Fatal(err)
	}
	b.fail = false
	if err = a.reconcile(); err != nil {
		t.Fatal(err)
	}
	app = a
	t.Cleanup(func() { app = nil })
	req := httptest.NewRequest("DELETE", "/api/devices/scoped/"+mac, nil)
	w := httptest.NewRecorder()
	handleScopedDeviceFor(w, req, mac, "alice")
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	if err = a.discover("phicomm/v2/"+userKey("alice")+"/discovery/"+mac, payload); err != nil {
		t.Fatal(err)
	}
	var status string
	a.store.QueryRow("SELECT status FROM scoped_devices WHERE username='alice' AND mac=?", mac).Scan(&status)
	if status != "hidden" {
		t.Fatal("retained announcement restored deletion", status)
	}
	if b.attached["alice"]["scoped_"+userKey("alice")+"_"+mac] {
		t.Fatal("control role retained")
	}
	req = httptest.NewRequest("PUT", "/api/devices/scoped/"+mac, strings.NewReader(`{}`))
	w = httptest.NewRecorder()
	handleScopedDeviceFor(w, req, mac, "alice")
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	if err = a.deleteUser("alice"); err != nil {
		t.Fatal(err)
	}
	list, err = a.store.devices("bob")
	if err != nil || len(list) != 1 || list[0].Namespace != "account" {
		t.Fatal("other owner affected", list, err)
	}
}
func TestUserKey(t *testing.T) {
	if userKey("alice") != "u_616c696365" || strings.ContainsAny(userKey("a+b@example.com"), "+#/") {
		t.Fatal("unsafe username encoding")
	}
}

func TestTMCPDeviceACLTracksRegistrationAndUnbind(t *testing.T) {
	a := testApp(t)
	b := a.broker.(*fakeBroker)
	const mac = "aabbccddeeff"
	role := "scoped_" + userKey("alice") + "_" + mac
	topic := "phicomm/v2/" + userKey("alice") + "/a1/" + mac + "/#"
	if err := a.discover("phicomm/v2/"+userKey("alice")+"/discovery/"+mac, []byte(`{"schema":1,"protocol":"tmcp","protocol_version":2,"version":"v2","model":"a1"}`)); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range b.acl[role] {
		if r.Topic == topic && r.Allow {
			found = true
		}
	}
	if !found {
		t.Fatal("TMCP root missing")
	}
	app = a
	t.Cleanup(func() { app = nil })
	w := httptest.NewRecorder()
	handleScopedDeviceFor(w, httptest.NewRequest("DELETE", "/", nil), mac, "alice")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if b.roles[role] {
		t.Fatal("unbound role survived")
	}
	if a.discover("phicomm/discovery/"+userKey("alice")+"/"+mac, []byte(`{"schema":1,"model":"a1","version":"community"}`)) == nil {
		t.Fatal("obsolete protocol accepted")
	}
}
