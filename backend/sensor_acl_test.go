package main

import (
	"fmt"
	"net/http/httptest"
	"testing"
)

func hasACL(rules []Rule, kind, topic string) bool {
	for _, rule := range rules {
		if rule.ACLType == kind && rule.Topic == topic && rule.Allow {
			return true
		}
	}
	return false
}

func TestM1DisplayACLTracksPrincipalScopesAndRevocation(t *testing.T) {
	a := testApp(t)
	b := a.broker.(*fakeBroker)
	if err := a.addMQTTAlias("alice", "alice_short", "secret123"); err != nil {
		t.Fatal(err)
	}
	app = a
	t.Cleanup(func() { app = nil })
	announce := func(principal, model, mac string) {
		t.Helper()
		if err := a.discover("phicomm/v2/"+userKey(principal)+"/discovery/"+mac,
			[]byte(fmt.Sprintf(`{"schema":1,"protocol":"tmcp","protocol_version":2,"version":"v2","model":%q}`, model))); err != nil {
			t.Fatal(err)
		}
	}
	remove := func(principal, mac string) {
		t.Helper()
		w := httptest.NewRecorder()
		handleScopedDeviceFor(w, httptest.NewRequest("DELETE", "/?mqtt_username="+principal, nil), mac, "alice")
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	checkReaders := func(wantM1, wantA1 bool) {
		t.Helper()
		account := b.acl["account_"+userKey("alice")]
		for _, principal := range []string{"alice", "alice_short", "bob"} {
			root := "phicomm/v2/" + userKey(principal)
			wantRead := principal != "bob" && (wantM1 || (wantA1 && principal == "alice_short"))
			for _, kind := range []string{"publishClientReceive", "subscribePattern"} {
				if got := hasACL(account, kind, root+"/sensor/+/state"); got != wantRead {
					t.Fatalf("%s sensor read %s = %t, want %t", principal, kind, got, wantRead)
				}
			}
			wantExpanded := principal != "bob" && wantM1
			if got := hasACL(account, "subscribeLiteral", root+"/sensor/+/+"); got != wantExpanded {
				t.Fatalf("%s expanded sensor filter = %t want %t", principal, got, wantExpanded)
			}
			for _, kind := range []string{"publishClientSend", "publishClientReceive", "subscribePattern"} {
				if hasACL(account, kind, root+"/sensor/+/+") {
					t.Fatal("expanded filter gained broad message permission", principal, kind)
				}
			}
			wantAvailability := principal != "bob" && wantM1
			if got := hasACL(account, "subscribeLiteral", root+"/+/+/availability"); got != wantAvailability {
				t.Fatalf("%s availability subscription = %t, want %t", principal, got, wantAvailability)
			}
			for _, kind := range []string{"publishClientReceive", "publishClientSend", "subscribePattern"} {
				if hasACL(account, kind, root+"/+/+/availability") {
					t.Fatal("availability filter gained message access", principal, kind)
				}
			}
			wantAutomation := wantA1 && principal == "alice_short"
			for _, permission := range []struct{ kind, topic string }{
				{"publishClientSend", root + "/sensor/+/command"},
				{"publishClientReceive", root + "/address/+"},
				{"subscribePattern", root + "/address/+"},
				{"publishClientSend", "phicomm/linkage/" + userKey(principal) + "/#"},
				{"publishClientReceive", "phicomm/linkage/" + userKey(principal) + "/#"},
				{"subscribePattern", "phicomm/linkage/" + userKey(principal) + "/#"},
			} {
				if got := hasACL(account, permission.kind, permission.topic); got != wantAutomation {
					t.Fatalf("automation changed: %+v = %t, want %t", permission, got, wantAutomation)
				}
			}
		}
		for _, principal := range []string{"alice", "alice_short"} {
			if !b.attached[principal]["account_"+userKey("alice")] {
				t.Fatal("owned principal lost account role", principal)
			}
		}
		if b.attached["bob"]["account_"+userKey("alice")] {
			t.Fatal("foreign principal gained account role")
		}
	}
	checkReaders(false, false)
	announce("alice", "m1", "111111111111")
	checkReaders(true, false)
	announce("alice_short", "a1", "222222222222")
	checkReaders(true, true)
	remove("alice", "111111111111")
	checkReaders(false, true)
	// Retained announcements cannot restore a removed M1 or its read capability.
	announce("alice", "m1", "111111111111")
	checkReaders(false, true)
	remove("alice_short", "222222222222")
	checkReaders(false, false)
	// Pending devices are included while retrying broker synchronization; a
	// failed unbind excludes a deleting device even before role cleanup succeeds.
	if _, err := a.store.Exec("UPDATE scoped_devices SET status='pending' WHERE model='m1'"); err != nil {
		t.Fatal(err)
	}
	if err := a.syncAccount("alice"); err != nil {
		t.Fatal(err)
	}
	checkReaders(true, false)
	if _, err := a.store.Exec("UPDATE scoped_devices SET status='deleting' WHERE model='m1'"); err != nil {
		t.Fatal(err)
	}
	if err := a.syncAccount("alice"); err != nil {
		t.Fatal(err)
	}
	checkReaders(false, false)
}

func TestTypedSensorPublishACLIsExactAndModelSpecific(t *testing.T) {
	for _, model := range []string{"tc1", "a1", "m1", "daikin"} {
		t.Run(model, func(t *testing.T) {
			a := testApp(t)
			if err := a.addMQTTAlias("alice", "alice_short", "secret123"); err != nil {
				t.Fatal(err)
			}
			const mac = "abcdef123456"
			if err := a.discover("phicomm/v2/"+userKey("alice_short")+"/discovery/"+mac,
				[]byte(fmt.Sprintf(`{"schema":1,"protocol":"tmcp","protocol_version":2,"version":"v2","model":%q}`, model))); err != nil {
				t.Fatal(err)
			}
			b := a.broker.(*fakeBroker)
			role := "scoped_" + userKey("alice_short") + "_" + mac
			rules := b.acl[role]
			root := "phicomm/v2/" + userKey("alice_short") + "/sensor/"
			if !hasACL(rules, "publishClientSend", root+mac+"/state") {
				t.Fatal("registered typed sensor cannot publish", rules)
			}
			for _, topic := range []string{root + "+/state", root + "ffffffffffff/state", root + mac + "/command", "phicomm/v2/" + userKey("bob") + "/sensor/" + mac + "/state"} {
				if hasACL(rules, "publishClientSend", topic) {
					t.Fatal("sensor publisher gained extra permission", topic)
				}
			}
			wantCommands := model == "m1" || model == "daikin"
			for _, kind := range []string{"publishClientReceive", "subscribeLiteral"} {
				if got := hasACL(rules, kind, root+mac+"/command"); got != wantCommands {
					t.Fatal("sensor command permission changed", model, kind, got)
				}
			}
			if !b.attached["alice"][role] || !b.attached["alice_short"][role] || b.attached["bob"][role] {
				t.Fatal("typed publisher role ownership changed", b.attached)
			}
		})
	}
}
