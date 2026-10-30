package main

import (
	"github.com/vmihailenco/msgpack/v5"
	"os"
	"testing"
)

func binaryAnnouncement(t *testing.T, mac string) map[string]any {
	t.Helper()
	return map[string]any{"schema": 2, "protocol": "tmcp", "protocol_version": 2, "version": "v2", "model": "a1", "device_id": mac, "firmware_version": "1.0-rc.4", "variant": "a1", "wire_version": 1, "wire_formats": []string{"msgpack"}, "default_wire_format": "msgpack"}
}
func TestDiscoveryCodecUpgradeDowngrade(t *testing.T) {
	a := testApp(t)
	mac := "aabbccddeeff"
	topic := "phicomm/v2/" + userKey("alice") + "/discovery/" + mac
	d := Device{Namespace: "account", Username: "alice", MAC: mac, Model: "a1", Version: "v2"}
	if _, err := a.store.deviceCodec(d); err == nil {
		t.Fatal("unknown device may write")
	}
	announcement := binaryAnnouncement(t, mac)
	data, _ := msgpack.Marshal(announcement)
	if err := a.discover(topic, data); err != nil {
		t.Fatal(err)
	}
	if codec, err := a.store.deviceCodec(d); err != nil || codec != "msgpack" {
		t.Fatal(codec, err)
	}
	list, err := a.store.scopedDevices("alice")
	if err != nil || len(list) != 1 || list[0].FirmwareVersion != "1.0-rc.4" || list[0].WireVersion != 1 {
		t.Fatal(list, err)
	}
	if err := a.store.decodeDevice(d, []byte(`{"code":"ok"}`), &map[string]any{}); err == nil {
		t.Fatal("codec guessed from state")
	}
	old := []byte(`{"schema":1,"protocol":"tmcp","protocol_version":2,"version":"v2","model":"a1"}`)
	if err := a.discover(topic, old); err != nil {
		t.Fatal(err)
	}
	if codec, err := a.store.deviceCodec(d); err != nil || codec != "json" {
		t.Fatal(codec, err)
	}
	announcement["device_id"] = "001122334455"
	data, _ = msgpack.Marshal(announcement)
	if err := a.discover(topic, data); err == nil {
		t.Fatal("cross-device announcement accepted")
	}
	if codec, _ := a.store.deviceCodec(d); codec != "json" {
		t.Fatal("invalid discovery changed codec")
	}
}
func TestBoundedMessagePack(t *testing.T) {
	for _, data := range [][]byte{
		{0xdf, 0xff, 0xff, 0xff, 0xff},     // impossible map allocation
		{0x82, 0xa1, 'a', 1, 0xa1, 'a', 2}, // ambiguous duplicate key
		{0x81, 0xa1, 'a', 0xc4, 1, 0},      // blob outside profile
		{0x80, 0x80},                       // trailing object
		{0x81, 0xa1, 0xff, 1},              // non UTF8 key
	} {
		var out map[string]any
		if decodeDeviceWire(data, &out) == nil {
			t.Fatalf("accepted %x", data)
		}
	}
	payload, _ := msgpack.Marshal(map[string]any{"nul": "a\x00b"})
	if decodeDeviceWire(payload, &map[string]any{}) == nil {
		t.Fatal("accepted NUL string")
	}
}
func TestMetadataACLScoped(t *testing.T) {
	a := testApp(t)
	d := Device{Namespace: "account", Username: "alice", MAC: "aabbccddeeff", Model: "m1", Version: "v2"}
	if err := a.registerScopedDevice(d, false); err != nil {
		t.Fatal(err)
	}
	if !hasACL(scopeRules(d), "publishClientSend", "phicomm/v2/"+userKey("alice")+"/sensor/"+d.MAC+"/metadata") {
		t.Fatal("metadata publisher denied")
	}
	rules, err := a.accountRules("alice")
	if err != nil {
		t.Fatal(err)
	}
	if !hasACL(rules, "publishClientReceive", "phicomm/v2/"+userKey("alice")+"/sensor/+/metadata") {
		t.Fatal("metadata reader denied")
	}
	if hasACL(rules, "publishClientReceive", "phicomm/v2/"+userKey("bob")+"/sensor/+/metadata") {
		t.Fatal("cross-account metadata exposed")
	}
}

func TestManualRegistrationCannotSelectCodec(t *testing.T) {
	a := testApp(t)
	d := Device{MAC: "aabbccddeeff", Model: "a1", Version: "v2", WireFormat: "msgpack", WireVersion: 1}
	if err := a.addDevice("alice", d); err != nil {
		t.Fatal(err)
	}
	d.Username = "alice"
	d.Namespace = "account"
	if _, err := a.store.deviceCodec(d); err == nil {
		t.Fatal("account API selected codec without discovery")
	}
}

func TestS30DiscoveryKeepsIdentityAndBinaryOnlyCodec(t *testing.T) {
	a := testApp(t)
	mac := "aabbccddeeff"
	topic := "phicomm/v2/" + userKey("alice") + "/discovery/" + mac
	// Captured from the actual firmware C writer, not encoded by this server.
	data, err := os.ReadFile("testdata/s30-announcement.msgpack")
	if err != nil {
		t.Fatal(err)
	}
	announcement := map[string]any{}
	if err = msgpack.Unmarshal(data, &announcement); err != nil {
		t.Fatal(err)
	}
	if err = a.discover(topic, data); err != nil {
		t.Fatal(err)
	}
	d := Device{Namespace: "account", Username: "alice", MAC: mac, Model: "s30-v1b", Version: "v2"}
	if codec, err := a.store.deviceCodec(d); err != nil || codec != "msgpack" {
		t.Fatal(codec, err)
	}
	list, err := a.store.scopedDevices("alice")
	if err != nil || len(list) != 1 || list[0].Model != "s30-v1b" || list[0].WireFormat != "msgpack" {
		t.Fatal(list, err)
	}
	// The exact new model never downgrades to the old JSON discovery contract.
	if err = a.discover(topic, []byte(`{"schema":1,"protocol":"tmcp","protocol_version":2,"version":"v2","model":"s30-v1b"}`)); err == nil {
		t.Fatal("S30 JSON downgrade accepted")
	}
	for key, value := range map[string]any{"device_id": "001122334455", "command_protocol": "unknown", "wire_version": 2, "firmware_version": "", "variant": ""} {
		invalid := map[string]any{}
		for k, v := range announcement {
			invalid[k] = v
		}
		invalid[key] = value
		payload, _ := msgpack.Marshal(invalid)
		if err = a.discover(topic, payload); err == nil {
			t.Fatal("accepted invalid S30 field", key)
		}
	}
	if codec, err := a.store.deviceCodec(d); err != nil || codec != "msgpack" {
		t.Fatal("invalid announcement changed codec", codec, err)
	}
	var reply map[string]any
	if err = a.store.decodeDevice(d, []byte(`{"code":"ok"}`), &reply); err == nil {
		t.Fatal("S30 JSON status accepted")
	}
	d.Namespace = ""
	if _, err = a.store.deviceCodec(d); err == nil {
		t.Fatal("S30 inherited legacy global JSON codec")
	}
}
