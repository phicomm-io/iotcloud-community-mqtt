package main

import (
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRealDynamicSecurityM1ExpandedSensorFilter(t *testing.T) {
	runtime := os.Getenv("PHICOMM_TEST_RUNTIME")
	if runtime == "" {
		t.Skip("isolated broker required")
	}
	pass, err := readSecret(runtime + "/secrets/broker-password")
	if err != nil {
		t.Fatal(err)
	}
	b, err := newDynamicBroker(os.Getenv("PHICOMM_TEST_BROKER"), runtime+"/broker/certs/ca.crt", serviceUser, pass)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	eventuallyBemfa(t, b.client.IsConnectionOpen)
	store, err := openStore(filepath.Join(t.TempDir(), "acl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a := &Application{store: store, broker: b}
	user := "filter_" + randomID()[:12]
	const password = "isolated-filter-fixture"
	if err = a.register(user, password); err != nil {
		t.Fatal(err)
	}
	defer a.deleteUser(user)
	mac := "aa9876543210"
	root := "phicomm/v2/" + userKey(user)
	base := root + "/sensor/"
	filter := base + "+/+"
	if err = a.discover(root+"/discovery/"+mac, []byte(`{"schema":1,"protocol":"tmcp","protocol_version":2,"version":"v2","model":"m1"}`)); err != nil {
		t.Fatal(err)
	}
	connect := func() mqtt.Client {
		c := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(b.url).SetTLSConfig(b.tls).SetUsername(user).SetPassword(password).SetClientID("filter_" + randomID()[:12]).SetAutoReconnect(false))
		if err := waitToken(c.Connect()); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Disconnect(0) })
		return c
	}
	sub := func(c mqtt.Client, topic string, want byte) {
		t.Helper()
		tok := c.Subscribe(topic, 1, nil)
		if err := waitToken(tok); err != nil {
			t.Fatal(err)
		}
		got, ok := tok.(*mqtt.SubscribeToken).Result()[topic]
		if !ok || got != want {
			t.Fatalf("SUBACK=%d present=%t want=%d", got, ok, want)
		}
		t.Logf("SUBACK %s = %d", strings.ReplaceAll(topic, userKey(user), "u_<masked>"), got)
	}
	rules, err := a.accountRules(user)
	if err != nil {
		t.Fatal(err)
	}
	oldRules := []Rule{}
	for _, rule := range rules {
		if rule.Topic == filter || strings.HasSuffix(rule.Topic, "/sensor/+/metadata") {
			continue
		}
		oldRules = append(oldRules, rule)
	}
	if _, err = b.Command("modifyRole", map[string]any{"rolename": "account_" + userKey(user), "acls": oldRules}); err != nil {
		t.Fatal(err)
	}
	old := connect()
	sub(old, base+"+/state", 1)
	sub(old, filter, 128)
	old.Disconnect(0)
	previousRules := []Rule{}
	for _, rule := range rules {
		if rule.Topic != filter {
			previousRules = append(previousRules, rule)
		}
	}
	if _, err = b.Command("modifyRole", map[string]any{"rolename": "account_" + userKey(user), "acls": previousRules}); err != nil {
		t.Fatal(err)
	}
	previous := connect()
	sub(previous, base+"+/metadata", 1)
	sub(previous, filter, 128)
	previous.Disconnect(0)
	if err = a.syncAccount(user); err != nil {
		t.Fatal(err)
	}
	current := connect()
	sub(current, filter, 1)
	sub(current, "phicomm/v2/u_foreign/sensor/+/+", 128)
	inbox := make(chan string, 8)
	tok := current.Subscribe(filter, 1, func(_ mqtt.Client, m mqtt.Message) { inbox <- m.Topic() })
	if err = waitToken(tok); err != nil {
		t.Fatal(err)
	}
	// Manager can publish an unregistered sensor command; no account receive
	// rule grants this leaf. The expanded subscription must not widen access.
	if err = b.relayPublish(base+"ffffffffffff/command", []byte("hidden-command")); err != nil {
		t.Fatal(err)
	}
	if err = waitToken(current.Publish(base+mac+"/metadata", 1, false, []byte{0x80})); err != nil {
		t.Fatal(err)
	}
	if err = waitToken(current.Publish(base+mac+"/state", 1, false, []byte{0x80})); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case topic := <-inbox:
			if strings.HasSuffix(topic, "/command") {
				t.Fatal("command leaked")
			}
			seen[topic] = true
		case <-time.After(3 * time.Second):
			t.Fatal("state/metadata missing", seen)
		}
	}
	select {
	case topic := <-inbox:
		t.Fatal("unexpected leaf", topic)
	case <-time.After(150 * time.Millisecond):
	}
	t.Log("PASS old policy rejects exact M1 filter; patched policy grants QoS1 without command/foreign receive access")
}
