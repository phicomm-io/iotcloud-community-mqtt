package main

import (
	"bytes"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRealDynamicSecurityHANativeWireSeparation(t *testing.T) {
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
	store, err := openStore(filepath.Join(t.TempDir(), "ha-native.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a := &Application{store: store, broker: b}
	owner, foreign := "native_"+randomID()[:12], "foreign_"+randomID()[:12]
	const password = "isolated-ha-native-fixture"
	for _, u := range []string{owner, foreign} {
		if err = a.register(u, password); err != nil {
			t.Fatal(err)
		}
		defer a.deleteUser(u)
	}
	const mac = "aa9876543210"
	announcement := binaryAnnouncement(t, mac)
	announcement["model"] = "m1"
	announcement["variant"] = "m1"
	body, _ := encodeDeviceWire("msgpack", announcement)
	if err = a.discover("phicomm/v2/"+userKey(owner)+"/discovery/"+mac, body); err != nil {
		t.Fatal(err)
	}
	d := Device{Namespace: "account", Username: owner, MQTTUsername: owner, MAC: mac, Model: "m1", Version: "v2"}
	base := bemfaBase(d)
	connect := func(u string) mqtt.Client {
		c := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(b.url).SetTLSConfig(b.tls).SetUsername(u).SetPassword(password).SetClientID("ha_native_" + randomID()[:12]).SetAutoReconnect(false))
		if err := waitToken(c.Connect()); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Disconnect(0) })
		return c
	}
	device, other := connect(owner), connect(foreign)
	type delivery struct {
		topic string
		body  []byte
	}
	received, relayed := make(chan delivery, 8), make(chan delivery, 8)
	handler := func(_ mqtt.Client, m mqtt.Message) {
		received <- delivery{m.Topic(), append([]byte(nil), m.Payload()...)}
	}
	sub := func(c mqtt.Client, topic string, want byte, callback mqtt.MessageHandler) {
		t.Helper()
		tok := c.Subscribe(topic, 1, callback)
		if err := waitToken(tok); err != nil {
			t.Fatal(err)
		}
		if got := tok.(*mqtt.SubscribeToken).Result()[topic]; got != want {
			t.Fatalf("SUBACK suffix=%s got=%d want=%d", topic[len(base):], got, want)
		}
	}
	for _, leaf := range []string{"/ha/command", "/ha/state", "/command", "/state"} {
		sub(device, base+leaf, 1, handler)
		sub(other, base+leaf, 128, nil)
	}
	b.watchRelay(func(topic string, body []byte, _ bool) {
		if len(topic) > len(base) && topic[:len(base)] == base {
			relayed <- delivery{topic, append([]byte(nil), body...)}
		}
	})
	if err = b.subscribeRelay(b.client); err != nil {
		t.Fatal(err)
	}
	ha := []byte(`{"temperature":23.5}`)
	native, _ := encodeDeviceWire("msgpack", map[string]any{"temperature": 235})
	expected := map[string][]byte{base + "/ha/command": ha, base + "/ha/state": ha, base + "/command": native, base + "/state": native}
	for topic, payload := range expected {
		if err = waitToken(device.Publish(topic, 1, false, payload)); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 4; i++ {
		select {
		case got := <-received:
			if !bytes.Equal(expected[got.topic], got.body) {
				t.Fatal("HA/native payload changed")
			}
			delete(expected, got.topic)
		case <-time.After(3 * time.Second):
			t.Fatal("missing HA/native delivery")
		}
	}
	select {
	case got := <-relayed:
		if got.topic != base+"/state" || !bytes.Equal(got.body, native) {
			t.Fatal("HA JSON entered native relay")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("native state relay missing")
	}
	select {
	case got := <-relayed:
		t.Fatal("unexpected HA relay", got.topic)
	case <-time.After(150 * time.Millisecond):
	}
	if err = a.store.decodeDevice(d, ha, &map[string]any{}); err == nil {
		t.Fatal("HA JSON accepted as native MP")
	}
	if err = a.store.decodeDevice(d, native, &map[string]any{}); err != nil {
		t.Fatal(err)
	}
	t.Log("PASS owned HA JSON command/state and native MP command/state stay byte-transparent on distinct topics; foreign subscriptions denied; HA JSON never enters native relay/decoder")
}
