package main

import (
	"os"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// Exercise the filters HA actually sends, not a filter narrowed to one user.
// The same test runs before/after a real broker restart in test-integration.sh.
func TestRealDynamicSecurityHADiscovery(t *testing.T) {
	runtime := os.Getenv("PHICOMM_TEST_RUNTIME")
	if runtime == "" {
		t.Skip("isolated broker required")
	}
	secret, err := readSecret(runtime + "/secrets/broker-password")
	if err != nil {
		t.Fatal(err)
	}
	b, err := newDynamicBroker(os.Getenv("PHICOMM_TEST_BROKER"), runtime+"/broker/certs/ca.crt", serviceUser, secret)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	deadline := time.Now().Add(10 * time.Second)
	for !b.client.IsConnectionOpen() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if !b.client.IsConnectionOpen() {
		t.Fatal("broker manager did not connect")
	}
	s, err := openStore(runtime + "/ha-discovery.db")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := &Application{store: s, broker: b}
	phase := os.Getenv("PHICOMM_TEST_PHASE")
	suffix := "restart"
	if phase == "" {
		suffix = randomID()[:10]
	}
	owner, alias, other := "ha_owner_"+suffix, "ha_alias_"+suffix, "ha_other_"+suffix
	const password = "ha-discovery-test-password"
	if phase != "verify" {
		for _, u := range []string{owner, other} {
			if err = a.register(u, password); err != nil {
				t.Fatal(err)
			}
		}
		if err = a.addMQTTAlias(owner, alias, password); err != nil {
			t.Fatal(err)
		}
	}
	if phase != "seed" {
		defer a.deleteUser(owner)
		defer a.deleteUser(other)
	}
	topics := map[string]string{}
	for _, u := range []string{owner, alias, other} {
		topics[u] = "homeassistant/sensor/" + userKey(u) + "/m1_aabbccddeeff_temperature/config"
	}
	type delivery struct {
		topic, payload string
		retained       bool
	}
	connect := func(u string, inbox chan delivery) mqtt.Client {
		opts := mqtt.NewClientOptions().AddBroker(b.url).SetTLSConfig(b.tls).
			SetUsername(u).SetPassword(password).SetClientID("ha_" + randomID()[:16]).SetAutoReconnect(false)
		if inbox != nil {
			opts.SetDefaultPublishHandler(func(_ mqtt.Client, m mqtt.Message) {
				inbox <- delivery{m.Topic(), string(m.Payload()), m.Retained()}
			})
		}
		c := mqtt.NewClient(opts)
		if err := waitToken(c.Connect()); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Disconnect(0) })
		return c
	}
	publish := func(u, topic, body string, retained bool) {
		c := connect(u, nil)
		if err := waitToken(c.Publish(topic, 1, retained, body)); err != nil {
			t.Fatal(err)
		}
		c.Disconnect(250)
	}
	if phase != "verify" {
		for u, topic := range topics {
			publish(u, topic, "retained-"+u, true)
		}
	}
	check := func(label string) {
		clients := map[string]mqtt.Client{}
		inboxes := map[string]chan delivery{}
		for _, u := range []string{owner, alias, other} {
			inbox := make(chan delivery, 32)
			c := connect(u, inbox)
			clients[u], inboxes[u] = c, inbox
			for _, filter := range []string{"homeassistant/sensor/+/config", "homeassistant/sensor/+/+/config"} {
				tok := c.Subscribe(filter, 1, nil)
				if err := waitToken(tok); err != nil {
					t.Fatal(label, u, filter, err)
				}
				if tok.(*mqtt.SubscribeToken).Result()[filter] == 128 {
					t.Fatal("HA filter denied", u, filter)
				}
			}
		}
		assertReceived := func(prefix string, retained bool) {
			for _, u := range []string{owner, alias, other} {
				want := map[string]string{topics[other]: prefix + other}
				if u != other {
					want = map[string]string{topics[owner]: prefix + owner, topics[alias]: prefix + alias}
				}
				deadline := time.After(5 * time.Second)
				for len(want) > 0 {
					select {
					case got := <-inboxes[u]:
						expected, ok := want[got.topic]
						if !ok || got.payload != expected || got.retained != retained {
							t.Fatalf("%s: %s unexpected delivery %+v", label, u, got)
						}
						delete(want, got.topic)
					case <-deadline:
						t.Fatal(label, u, "missing messages", want)
					}
				}
			}
			// Keep all three inboxes under observation, including after each
			// client received its expected messages. Cross-account deliveries fail.
			select {
			case got := <-inboxes[owner]:
				t.Fatal("owner received extra/foreign message", got)
			case got := <-inboxes[alias]:
				t.Fatal("alias received extra/foreign message", got)
			case got := <-inboxes[other]:
				t.Fatal("other received extra/foreign message", got)
			case <-time.After(400 * time.Millisecond):
			}
		}
		assertReceived("retained-", true)
		// A successful SUBACK must not grant permission to publish into another
		// account. Its subscriber would see this marker if send ACLs regressed.
		publish(owner, topics[other], "spoof", false)
		for u, topic := range topics {
			publish(u, topic, "live-"+u, false)
		}
		assertReceived("live-", false)
		// HA sends this birth notification after an integration reload. All
		// accounts can publish this exact topic and receive the notification.
		for u, c := range clients {
			tok := c.Subscribe("homeassistant/status", 1, nil)
			if err := waitToken(tok); err != nil || tok.(*mqtt.SubscribeToken).Result()["homeassistant/status"] == 128 {
				t.Fatal("HA birth subscription denied", u, err)
			}
		}
		for _, sender := range []string{owner, alias, other} {
			publish(sender, "homeassistant/status", "online", false)
			for u, inbox := range inboxes {
				select {
				case got := <-inbox:
					if got.topic != "homeassistant/status" || got.payload != "online" || got.retained {
						t.Fatalf("unexpected HA birth delivery to %s: %+v", u, got)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("HA birth missing", sender, u)
				}
			}
		}
		for _, c := range clients {
			c.Disconnect(0)
		}
	}
	check("initial/restart")
	if err = a.syncAccounts(); err != nil {
		t.Fatal(err)
	}
	check("after account reconciliation")
	if phase != "seed" {
		for u, topic := range topics {
			publish(u, topic, "", true)
		}
	}
}
