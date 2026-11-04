package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/vmihailenco/msgpack/v5"
)

func sensorACLAnnouncement(t *testing.T, model, codec, mac string) []byte {
	t.Helper()
	if codec == "json" {
		return []byte(fmt.Sprintf(`{"schema":1,"protocol":"tmcp","protocol_version":2,"version":"v2","model":%q}`, model))
	}
	// Reuse the announcement captured from the production C writer; only the
	// synthetic model/device identity varies in this compatibility matrix.
	payload, err := os.ReadFile("testdata/s30-announcement.msgpack")
	if err != nil {
		t.Fatal(err)
	}
	var announcement map[string]any
	if err = msgpack.Unmarshal(payload, &announcement); err != nil {
		t.Fatal(err)
	}
	announcement["model"], announcement["device_id"] = model, mac
	payload, err = msgpack.Marshal(announcement)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestSensorCommandACLDiscoveryCompatibility(t *testing.T) {
	for _, model := range []string{"tc1", "a1", "m1", "daikin", "s30-v1b"} {
		for _, codec := range []string{"json", "msgpack"} {
			if model == "s30-v1b" && codec == "json" {
				continue // The existing binary-only S30 test covers this rejection.
			}
			t.Run(model+"/"+codec, func(t *testing.T) {
				a := testApp(t)
				if err := a.addMQTTAlias("alice", "alice_short", "secret123"); err != nil {
					t.Fatal(err)
				}
				const mac = "abcdef123456"
				if err := a.discover("phicomm/v2/"+userKey("alice_short")+"/discovery/"+mac, sensorACLAnnouncement(t, model, codec, mac)); err != nil {
					t.Fatal(err)
				}
				devices, err := a.store.scopedDevices("alice")
				if err != nil || len(devices) != 1 || devices[0].WireFormat != codec || devices[0].Status != "active" {
					t.Fatal("discovery identity/codec changed", devices, err)
				}
				d := devices[0]
				b := a.broker.(*fakeBroker)
				rules := b.acl[deviceRole(d)]
				base := "phicomm/v2/" + userKey("alice_short") + "/sensor/" + mac
				wantCommand := model == "m1" || model == "daikin" || model == "s30-v1b"
				for _, kind := range []string{"publishClientReceive", "subscribeLiteral"} {
					if hasACL(rules, kind, base+"/command") != wantCommand {
						t.Fatalf("%s exact sensor command grant missing/unexpected", kind)
					}
				}
				for _, leaf := range []string{"state", "metadata"} {
					if !hasACL(rules, "publishClientSend", base+"/"+leaf) {
						t.Fatal("existing sensor publication lost", leaf)
					}
				}
				for _, rule := range rules {
					if strings.Contains(rule.Topic, "/sensor/") && rule.ACLType != "publishClientSend" {
						if !wantCommand || rule.Topic != base+"/command" || (rule.ACLType != "publishClientReceive" && rule.ACLType != "subscribeLiteral") {
							t.Fatal("sensor permission broadened", rule)
						}
					}
					if rule.ACLType == "publishClientSend" && strings.HasSuffix(rule.Topic, "/command") {
						t.Fatal("sensor command publishing was granted", rule)
					}
				}
				role := deviceRole(d)
				if !b.attached["alice"][role] || !b.attached["alice_short"][role] || b.attached["bob"][role] {
					t.Fatal("account/alias role isolation changed")
				}
			})
		}
	}
}

func TestS30DiscoveryRefreshesMissingSensorCommandACL(t *testing.T) {
	a := testApp(t)
	const mac = "aabbccddeeff"
	topic := "phicomm/v2/" + userKey("alice") + "/discovery/" + mac
	payload := sensorACLAnnouncement(t, "s30-v1b", "msgpack", mac)
	if err := a.discover(topic, payload); err != nil {
		t.Fatal(err)
	}
	devices, err := a.store.scopedDevices("alice")
	if err != nil || len(devices) != 1 {
		t.Fatal(devices, err)
	}
	d := devices[0]
	b := a.broker.(*fakeBroker)
	role := deviceRole(d)
	command := "phicomm/v2/" + userKey("alice") + "/sensor/" + mac + "/command"
	old := []Rule{}
	for _, rule := range b.acl[role] {
		if rule.Topic != command {
			old = append(old, rule)
		}
	}
	b.acl[role] = old
	for i := 0; i < 2; i++ {
		if err = a.discover(topic, payload); err != nil {
			t.Fatal(err)
		}
		for _, kind := range []string{"publishClientReceive", "subscribeLiteral"} {
			if !hasACL(b.acl[role], kind, command) {
				t.Fatal("existing active S30 command ACL was not refreshed", kind)
			}
		}
		got, err := a.store.scopedDevices("alice")
		if err != nil || !reflect.DeepEqual(got, devices) {
			t.Fatal("ACL refresh changed stored device identity/codec", got, err)
		}
	}
}

func TestRealDynamicSecurityS30SensorCommandExact(t *testing.T) {
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
	store, err := openStore(filepath.Join(t.TempDir(), "s30-acl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a := &Application{store: store, broker: b}
	suffix := randomID()[:10]
	owner, alias, other := "s30_owner_"+suffix, "s30_alias_"+suffix, "s30_other_"+suffix
	const password = "isolated-s30-acl-fixture"
	for _, user := range []string{owner, other} {
		if err = a.register(user, password); err != nil {
			t.Fatal(err)
		}
		defer a.deleteUser(user)
	}
	if err = a.addMQTTAlias(owner, alias, password); err != nil {
		t.Fatal(err)
	}
	const mac = "aabbccddeeff"
	root := "phicomm/v2/" + userKey(alias)
	topic := root + "/sensor/" + mac + "/command"
	discovery := root + "/discovery/" + mac
	payload := sensorACLAnnouncement(t, "s30-v1b", "msgpack", mac)
	if err = a.discover(discovery, payload); err != nil {
		t.Fatal(err)
	}
	d := Device{Namespace: "account", Username: owner, MQTTUsername: alias, Model: "s30-v1b", MAC: mac, Version: "v2"}
	old := []Rule{}
	for _, rule := range scopeRules(d) {
		if rule.Topic != topic {
			old = append(old, rule)
		}
	}
	if _, err = b.Command("modifyRole", map[string]any{"rolename": deviceRole(d), "acls": old}); err != nil {
		t.Fatal(err)
	}
	type packet struct {
		topic string
		body  []byte
	}
	inboxes := map[string]chan packet{}
	connect := func(user string) mqtt.Client {
		inboxes[user] = make(chan packet, 8)
		c := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(b.url).SetTLSConfig(b.tls).
			SetUsername(user).SetPassword(password).SetClientID("s30_acl_" + randomID()[:12]).SetAutoReconnect(false).
			SetDefaultPublishHandler(func(_ mqtt.Client, m mqtt.Message) {
				inboxes[user] <- packet{m.Topic(), append([]byte(nil), m.Payload()...)}
			}))
		if err := waitToken(c.Connect()); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Disconnect(0) })
		return c
	}
	clients := map[string]mqtt.Client{owner: connect(owner), alias: connect(alias), other: connect(other)}
	subscribe := func(user, filter string, want byte) {
		t.Helper()
		token := clients[user].Subscribe(filter, 1, nil)
		if err := waitToken(token); err != nil {
			t.Fatal(err)
		}
		if got, ok := token.(*mqtt.SubscribeToken).Result()[filter]; !ok || got != want {
			t.Fatalf("SUBACK=%d present=%t want=%d", got, ok, want)
		}
	}
	subscribe(owner, topic, 128)
	t.Log("production-equivalent S30 policy: exact sensor command SUBACK128")
	if err = a.discover(discovery, payload); err != nil {
		t.Fatal(err)
	}
	for _, user := range []string{owner, alias} {
		subscribe(user, topic, 1)
		for _, denied := range []string{root + "/sensor/ffffffffffff/command", root + "/sensor/+/command", root + "/sensor/#", "phicomm/v2/" + userKey(other) + "/sensor/" + mac + "/command"} {
			subscribe(user, denied, 128)
		}
	}
	subscribe(other, topic, 128)
	legacyJSON := []byte(`{"action":"sensor.grant","params":{"boot":1},"request_id":"json-fixture"}`)
	binary, err := msgpack.Marshal(map[string]any{"action": "sensor.grant", "params": map[string]any{"boot": 1}, "request_id": "mp-fixture"})
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range [][]byte{legacyJSON, binary} {
		if err = b.relayPublish(topic, body); err != nil {
			t.Fatal(err)
		}
		for _, user := range []string{owner, alias} {
			select {
			case got := <-inboxes[user]:
				if got.topic != topic || !bytes.Equal(got.body, body) {
					t.Fatal("broker changed the authorized JSON/MP bytes")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("authorized sensor command was not delivered")
			}
		}
		select {
		case <-inboxes[other]:
			t.Fatal("sensor command crossed account boundary")
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Log("PASS S30 exact command QoS1, active-role refresh, owner/alias access, foreign/device/wildcard denial, byte-identical JSON/MP")
}
