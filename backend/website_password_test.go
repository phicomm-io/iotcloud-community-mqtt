package main

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIndependentWebsiteAndMQTTPasswords(t *testing.T) {
	a := testApp(t)
	if err := a.addMQTTAlias("alice", "alice_short", "secret123"); err != nil {
		t.Fatal(err)
	}
	if err := a.changeMQTTPassword("alice", "mqtt-new-123"); err != nil {
		t.Fatal(err)
	}
	if err := a.authenticateWebsite("alice", "alice_short", "secret123"); err != nil {
		t.Fatal("MQTT change broke website login", err)
	}
	if err := a.authenticateWebsite("alice", "alice", "mqtt-new-123"); err != errCredentials {
		t.Fatal("MQTT password accepted by website", err)
	}
	for _, u := range []string{"alice", "alice_short"} {
		if err := a.broker.Authenticate(u, "mqtt-new-123"); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.changePassword("alice", "website-new-123"); err != nil {
		t.Fatal(err)
	}
	if err := a.authenticateWebsite("alice", "alice", "website-new-123"); err != nil {
		t.Fatal(err)
	}
	if err := a.broker.Authenticate("alice", "mqtt-new-123"); err != nil {
		t.Fatal("website change broke MQTT", err)
	}
	// Simulate a pre-migration account without a website hash.
	if _, err := a.store.Exec("DELETE FROM account_passwords WHERE username='bob'"); err != nil {
		t.Fatal(err)
	}
	if err := a.authenticateWebsite("bob", "bob", "wrong"); err != errCredentials {
		t.Fatal(err)
	}
	if err := a.authenticateWebsite("bob", "bob", "secret123"); err != nil {
		t.Fatal(err)
	}
	a.broker.(*fakeBroker).fail = true
	if err := a.authenticateWebsite("bob", "bob", "secret123"); err != nil {
		t.Fatal("website still depends on broker", err)
	}
}

func TestMQTTPasswordEndpoint(t *testing.T) {
	previous := app
	app = testApp(t)
	defer func() { app = previous }()
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"current_password":"wrong","password":"mqtt-new-123"}`, 401},
		{`{"current_password":"secret123","password":"short"}`, 400},
		{`{"current_password":"secret123","password":"mqtt-new-123"}`, 200},
	} {
		r := httptest.NewRequest("POST", "/api/auth/mqtt-password", strings.NewReader(tc.body))
		r = r.WithContext(context.WithValue(r.Context(), userContextKey{}, "alice"))
		w := httptest.NewRecorder()
		handleMQTTPassword(w, r)
		if w.Code != tc.status {
			t.Fatalf("got %d want %d: %s", w.Code, tc.status, w.Body.String())
		}
	}
	if err := app.authenticateWebsite("alice", "alice", "secret123"); err != nil {
		t.Fatal(err)
	}
}
