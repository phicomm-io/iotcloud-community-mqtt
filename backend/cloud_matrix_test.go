package main

import (
	"encoding/json"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Dynamic Security replies are stubbed; all device and cloud application bytes
// cross real TCP MQTT sockets and use the platform discovery/account/relay code.
type matrixBroker struct {
	Broker
	wire *DynamicBroker
}

func (b matrixBroker) relayPublish(topic string, body []byte) error {
	return b.wire.relayPublish(topic, body)
}
func (b matrixBroker) refreshRelay() { b.wire.refreshRelay() }

type matrixDevice struct {
	client     mqtt.Client
	device     Device
	mu         sync.Mutex
	format     string
	on, reject bool
	controls   chan string
	errors     chan string
}

func newMatrixDevice(t *testing.T, fixture *wireBroker, d Device, format string) *matrixDevice {
	t.Helper()
	s := &matrixDevice{client: wireClient(t, fixture), device: d, format: format, controls: make(chan string, 32), errors: make(chan string, 32)}
	if err := waitToken(s.client.Subscribe(bemfaBase(d)+"/command", 0, func(_ mqtt.Client, m mqtt.Message) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if m.Retained() || deviceWireFormat(m.Payload()) != s.format {
			s.errors <- "wrong codec or retained command"
			return
		}
		var q struct {
			Action string         `json:"action"`
			Params map[string]any `json:"params"`
			ID     string         `json:"request_id"`
		}
		if decodeDeviceWire(m.Payload(), &q) != nil {
			s.errors <- "malformed command"
			return
		}
		if q.Action != "status.get" {
			s.controls <- s.format
			if s.reject {
				body, _ := encodeDeviceWire(s.format, map[string]any{"request_id": q.ID, "code": "busy"})
				s.client.Publish(bemfaBase(d)+"/result", 0, false, body)
				return
			}
			if q.Action != "outlet.set" || q.Params["index"] != float64(5) {
				s.errors <- "wrong business parameters"
				return
			}
			s.on = q.Params["on"] == true
		}
		value := 0
		if s.on {
			value = 1
		}
		state := map[string]any{"socket_states": []int{0, 0, 0, 0, 0, value}}
		if q.Action != "status.get" {
			body, _ := encodeDeviceWire(s.format, state)
			s.client.Publish(bemfaBase(d)+"/state", 0, false, body)
		}
		body, _ := encodeDeviceWire(s.format, map[string]any{"request_id": q.ID, "code": "ok", "data": state})
		s.client.Publish(bemfaBase(d)+"/result", 0, false, body)
	})); err != nil {
		t.Fatal(err)
	}
	return s
}
func (s *matrixDevice) announcement(format string) []byte {
	s.mu.Lock()
	s.format = format
	s.mu.Unlock()
	a := map[string]any{"schema": 1, "protocol": "tmcp", "protocol_version": 2, "version": "v2", "model": "tc1"}
	if format == "msgpack" {
		a = map[string]any{"schema": 2, "protocol": "tmcp", "protocol_version": 2, "version": "v2", "model": "tc1", "device_id": s.device.MAC, "firmware_version": "1.0-rc.4", "variant": "tc1", "wire_version": 1, "wire_formats": []string{"msgpack"}, "default_wire_format": "msgpack"}
	}
	body, _ := encodeDeviceWire(format, a)
	return body
}
func (s *matrixDevice) availability(t *testing.T, online bool) {
	t.Helper()
	body := "offline"
	if online {
		body = "online"
	}
	if err := waitToken(s.client.Publish(bemfaBase(s.device)+"/availability", 0, true, body)); err != nil {
		t.Fatal(err)
	}
}
func expectMatrixControl(t *testing.T, s *matrixDevice, want string) {
	t.Helper()
	select {
	case got := <-s.controls:
		if got != want {
			t.Fatalf("codec=%s want=%s", got, want)
		}
	case err := <-s.errors:
		t.Fatal(err)
	case <-time.After(4 * time.Second):
		t.Fatal("no control delivered")
	}
}
func noMatrixControl(t *testing.T, devices ...*matrixDevice) {
	t.Helper()
	until := time.Now().Add(180 * time.Millisecond)
	for time.Now().Before(until) {
		for _, s := range devices {
			select {
			case got := <-s.controls:
				t.Fatalf("unexpected %s control for %s", got, s.device.MAC)
			case err := <-s.errors:
				t.Fatal(err)
			default:
			}
		}
		time.Sleep(time.Millisecond)
	}
}
func TestCloudMixedWireDeviceMatrix(t *testing.T) {
	a, r, first := testBemfa(t)
	original, cloud := newWireBroker(t), newWireBroker(t)
	wire, err := newDynamicBroker(original.url(), "", "manager", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	defer wire.Close()
	a.broker = matrixBroker{Broker: a.broker, wire: wire}
	wire.watchRelay(r.deviceEvent)
	discovered := make(chan error, 32)
	wire.watchDiscovery(func(topic string, body []byte) { discovered <- a.discover(topic, body) })
	eventuallyBemfa(t, wire.client.IsConnectionOpen)
	if err = wire.subscribeDiscovery(wire.client); err != nil {
		t.Fatal(err)
	}
	second := first
	second.MAC = "001122334455"
	foreign := first
	foreign.Username = "bob"
	foreign.MQTTUsername = "bob"
	old, modern, other := newMatrixDevice(t, original, first, "json"), newMatrixDevice(t, original, second, "msgpack"), newMatrixDevice(t, original, foreign, "msgpack")
	discover := func(s *matrixDevice, format string) {
		t.Helper()
		topic := "phicomm/v2/" + userKey(topicUsername(s.device)) + "/discovery/" + s.device.MAC
		if err := waitToken(s.client.Publish(topic, 0, false, s.announcement(format))); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-discovered:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(4 * time.Second):
			t.Fatal("discovery not processed")
		}
	}
	discover(old, "json")
	discover(modern, "msgpack")
	discover(other, "msgpack")
	key := strings.Repeat("a", 32)
	r.clientFactory = func(opts *mqtt.ClientOptions) mqtt.Client {
		local, _ := url.Parse(cloud.url())
		opts.Servers = []*url.URL{local}
		return mqtt.NewClient(opts)
	}
	bad, _ := json.Marshal(BemfaConfig{APIKey: key, Mappings: []BemfaMapping{{Device: foreign, Channel: 5, Topic: "foreign001"}}})
	if res := bemfaRequest(t, a, "alice", "PUT", string(bad)); res.Code == 200 {
		t.Fatal("foreign account mapping accepted")
	}
	config := BemfaConfig{Enabled: true, APIKey: key, Mappings: []BemfaMapping{{Device: first, Channel: 5, Topic: "old001"}, {Device: second, Channel: 5, Topic: "modern001"}}}
	body, _ := json.Marshal(config)
	res := bemfaRequest(t, a, "alice", "PUT", string(body))
	if res.Code != 200 {
		t.Fatal(res.Body.String())
	}
	defer r.Close()
	speaker := wireClient(t, cloud)
	reports := make(chan string, 64)
	if err = waitToken(speaker.Subscribe("+/up", 0, func(_ mqtt.Client, m mqtt.Message) { reports <- m.Topic() + ":" + string(m.Payload()) })); err != nil {
		t.Fatal(err)
	}
	old.availability(t, true)
	modern.availability(t, true)
	other.availability(t, true)
	expectReports := func(wants ...string) {
		t.Helper()
		remaining := map[string]bool{}
		for _, want := range wants {
			remaining[want] = true
		}
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		for len(remaining) > 0 {
			select {
			case got := <-reports:
				delete(remaining, got)
			case <-timer.C:
				t.Fatalf("missing canonical state %v", remaining)
			}
		}
	}
	command := func(topic, body string) {
		t.Helper()
		if err := waitToken(speaker.Publish(topic, 0, false, body)); err != nil {
			t.Fatal(err)
		}
	}
	expectReports("old001/up:off", "modern001/up:off")
	command("old001", "on")
	command("modern001", "on")
	expectMatrixControl(t, old, "json")
	expectMatrixControl(t, modern, "msgpack")
	expectReports("old001/up:on", "modern001/up:on")
	noMatrixControl(t, old, modern, other)
	t.Log("PASS mixed JSON/MP online, result/state and cross-account mapping isolation")
	// Malformed and mismatched-codec state cannot overwrite established readings.
	for _, payload := range [][]byte{[]byte(`{"socket_states":[0,0,0,0,0,0]}`), {0xdf, 255, 255, 255, 255}} {
		if err = waitToken(modern.client.Publish(bemfaBase(second)+"/state", 0, false, payload)); err != nil {
			t.Fatal(err)
		}
	}
	foreignState, _ := encodeDeviceWire("msgpack", map[string]any{"socket_states": []int{0, 0, 0, 0, 0, 0}})
	if err = waitToken(other.client.Publish(bemfaBase(foreign)+"/state", 0, false, foreignState)); err != nil {
		t.Fatal(err)
	}
	select {
	case report := <-reports:
		t.Fatalf("malformed/foreign state changed report: %s", report)
	case <-time.After(180 * time.Millisecond):
	}
	t.Log("PASS malformed/wrong-codec/foreign state cannot replace known readings")

	topic := "phicomm/v2/" + userKey("alice") + "/discovery/" + second.MAC
	if err = waitToken(modern.client.Publish(topic, 0, false, []byte{0xdf, 255, 255, 255, 255})); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-discovered:
		if err == nil {
			t.Fatal("malformed discovery accepted")
		}
	case <-time.After(4 * time.Second):
		t.Fatal("malformed discovery not processed")
	}
	if codec, err := a.store.deviceCodec(second); err != nil || codec != "msgpack" {
		t.Fatal(codec, err)
	}
	discover(old, "msgpack")
	command("old001", "off")
	expectMatrixControl(t, old, "msgpack")
	expectReports("old001/up:off")
	noMatrixControl(t, old, modern, other)
	discover(old, "json")
	command("old001", "on")
	expectMatrixControl(t, old, "json")
	expectReports("old001/up:on")
	t.Log("PASS live codec upgrade/rollback without worker reload; malformed discovery rejected")
	modern.mu.Lock()
	modern.reject = true
	modern.mu.Unlock()
	command("modern001", "off")
	expectMatrixControl(t, modern, "msgpack")
	noMatrixControl(t, old, modern, other)
	modern.mu.Lock()
	modern.reject = false
	modern.mu.Unlock()
	command("modern001", "off")
	expectMatrixControl(t, modern, "msgpack")
	expectReports("modern001/up:off")
	modern.availability(t, false)
	time.Sleep(60 * time.Millisecond)
	command("modern001", "on")
	noMatrixControl(t, modern)
	modern.availability(t, true)
	time.Sleep(250 * time.Millisecond)
	noMatrixControl(t, modern)
	command("modern001", "on")
	expectMatrixControl(t, modern, "msgpack")
	expectReports("modern001/up:on")
	t.Log("PASS busy no-replay, offline drop, recovery without delayed write")
	noMatrixControl(t, old, modern, other)
}

// This lane uses an isolated real Mosquitto Dynamic Security service when supplied.
// Unlike the TCP fixture it verifies broker-enforced cross-account receive ACLs.
func TestRealDynamicSecurityMixedCodecs(t *testing.T) {
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
	if _, err = b.Command("modifyRole", map[string]any{"rolename": "manager", "acls": managerRules()}); err != nil {
		t.Fatal(err)
	}
	store, err := openStore(filepath.Join(t.TempDir(), "cloud.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a := &Application{store: store, broker: b}
	users := []string{"mixed_" + randomID()[:12], "mixed_" + randomID()[:12]}
	formats := []string{"json", "msgpack"}
	clients := make([]mqtt.Client, 2)
	devices := make([]Device, 2)
	commands := make(chan string, 8)
	states := make(chan string, 8)
	foreign := make(chan bool, 8)
	b.watchRelay(func(topic string, body []byte, _ bool) {
		if strings.HasSuffix(topic, "/state") {
			for _, u := range users {
				if strings.HasPrefix(topic, "phicomm/v2/"+userKey(u)+"/") {
					states <- topic + ":" + deviceWireFormat(body)
					break
				}
			}
		}
	})
	if err = b.subscribeRelay(b.client); err != nil {
		t.Fatal(err)
	}
	for i, u := range users {
		if err = a.register(u, "matrix-fixture-password"); err != nil {
			t.Fatal(err)
		}
		defer a.deleteUser(u)
		d := Device{Namespace: "account", Username: u, MQTTUsername: u, MAC: "aa9876543210", Model: "m1", Version: "v2"}
		devices[i] = d
		simulated := &matrixDevice{device: d}
		body := simulated.announcement(formats[i])
		var announcement map[string]any
		if err = decodeDeviceWire(body, &announcement); err != nil {
			t.Fatal(err)
		}
		announcement["model"] = "m1"
		if i == 1 {
			announcement["variant"] = "m1"
		}
		body, _ = encodeDeviceWire(formats[i], announcement)
		if err = a.discover("phicomm/v2/"+userKey(u)+"/discovery/"+d.MAC, body); err != nil {
			t.Fatal(err)
		}
		c := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(b.url).SetTLSConfig(b.tls).SetUsername(u).SetPassword("matrix-fixture-password").SetClientID("mixed_" + randomID()[:12]).SetAutoReconnect(false))
		if err = waitToken(c.Connect()); err != nil {
			t.Fatal(err)
		}
		defer c.Disconnect(0)
		clients[i] = c
		format := formats[i]
		if err = waitToken(c.Subscribe(bemfaBase(d)+"/command", 0, func(_ mqtt.Client, m mqtt.Message) {
			if m.Retained() {
				commands <- "retained"
				return
			}
			commands <- format + ":" + deviceWireFormat(m.Payload())
		})); err != nil {
			t.Fatal(err)
		}
	}
	// Account A may ask to subscribe broadly, but ACLs must deny B's data.
	_ = waitToken(clients[0].Subscribe(bemfaBase(devices[1])+"/state", 0, func(_ mqtt.Client, _ mqtt.Message) { foreign <- true }))
	for i, d := range devices {
		body, _ := encodeDeviceWire(formats[i], map[string]any{"action": "status.get", "params": map[string]any{}, "request_id": "matrix"})
		if err = b.relayPublish(bemfaBase(d)+"/command", body); err != nil {
			t.Fatal(err)
		}
		body, _ = encodeDeviceWire(formats[i], map[string]any{"temperature": 235})
		if err = waitToken(clients[i].Publish(bemfaBase(d)+"/state", 1, false, body)); err != nil {
			t.Fatal(err)
		}
	}
	got := map[string]bool{}
	for range users {
		select {
		case value := <-commands:
			got[value] = true
		case <-time.After(4 * time.Second):
			t.Fatal("command missing")
		}
	}
	if !got["json:json"] || !got["msgpack:msgpack"] {
		t.Fatal(got)
	}
	seen := map[string]bool{}
	for range users {
		select {
		case value := <-states:
			seen[value] = true
		case <-time.After(4 * time.Second):
			t.Fatal("state missing")
		}
	}
	for i, d := range devices {
		if !seen[bemfaBase(d)+"/state:"+formats[i]] {
			t.Fatal(seen)
		}
	}
	select {
	case <-foreign:
		t.Fatal("foreign account state leaked")
	case <-time.After(200 * time.Millisecond):
	}
	t.Log("PASS actual Mosquitto Dynamic Security: JSON and MP same-MAC devices online, fixed control/state codecs, foreign receive denied")
}
