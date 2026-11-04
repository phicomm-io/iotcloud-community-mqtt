package main

import (
	"bytes"
	"encoding/hex"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// The cloud transport owns topic routing, not the device payload schema.
// Exercise the production relay methods over actual MQTT connections: NUL and
// invalid UTF-8 bytes must survive without JSON, base64 or text conversion.
func TestRelayPreservesOpaqueBinary(t *testing.T) {
	fixture := newWireBroker(t)
	connect := func(id string) mqtt.Client {
		c := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(fixture.url()).
			SetClientID(id).SetAutoReconnect(false))
		if err := waitToken(c.Connect()); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Disconnect(0) })
		return c
	}
	manager, device := connect("binary-manager"), connect("binary-device")
	b := &DynamicBroker{client: manager}
	const base = "phicomm/v2/u_616c696365/m1/aabbccddeeff"
	type delivery struct {
		topic    string
		body     []byte
		retained bool
	}
	commands, replies := make(chan delivery, 8), make(chan delivery, 8)
	if err := waitToken(device.Subscribe(base+"/command", 0, func(_ mqtt.Client, m mqtt.Message) {
		commands <- delivery{m.Topic(), append([]byte(nil), m.Payload()...), m.Retained()}
	})); err != nil {
		t.Fatal(err)
	}
	b.watchRelay(func(topic string, body []byte, retained bool) {
		replies <- delivery{topic, append([]byte(nil), body...), retained}
	})
	if err := b.subscribeRelay(manager); err != nil {
		t.Fatal(err)
	}
	receive := func(ch <-chan delivery, topic string, want []byte) {
		t.Helper()
		select {
		case got := <-ch:
			if got.topic != topic || !bytes.Equal(got.body, want) || got.retained {
				t.Fatalf("opaque payload changed: topic=%s bytes=%x retained=%v", got.topic, got.body, got.retained)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("missing application payload")
		}
	}
	for _, wire := range []string{
		"81a178c40400ff807f", // MessagePack map with a binary value.
		"007b7dfffe80c0",     // Opaque bytes deliberately not valid JSON/UTF-8.
	} {
		body, err := hex.DecodeString(wire)
		if err != nil {
			t.Fatal(err)
		}
		if err = b.relayPublish(base+"/command", body); err != nil {
			t.Fatal(err)
		}
		receive(commands, base+"/command", body)
		if err = waitToken(device.Publish(base+"/result", 0, false, body)); err != nil {
			t.Fatal(err)
		}
		receive(replies, base+"/result", body)
	}
}
