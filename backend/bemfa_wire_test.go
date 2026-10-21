package main

// An isolated MQTT 3.1.1 wire fixture. Both sides use real Paho connections;
// neither production brokers nor physical devices are contacted.
import (
	"encoding/json"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/eclipse/paho.mqtt.golang/packets"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type wirePeer struct {
	conn   net.Conn
	mu     sync.Mutex
	topics []string
}
type wireBroker struct {
	listener net.Listener
	mu       sync.Mutex
	peers    map[*wirePeer]bool
	retained map[string][]byte
	closed   bool
	deny     bool
}

func (p *wirePeer) write(packet packets.ControlPacket) {
	p.mu.Lock()
	defer p.mu.Unlock()
	_ = packet.Write(p.conn)
}
func wireMatches(filter, topic string) bool {
	f, t := strings.Split(filter, "/"), strings.Split(topic, "/")
	if len(f) != len(t) {
		return false
	}
	for i := range f {
		if f[i] != "+" && f[i] != t[i] {
			return false
		}
	}
	return true
}
func newWireBroker(t *testing.T) *wireBroker {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	b := &wireBroker{listener: listener, peers: map[*wirePeer]bool{}, retained: map[string][]byte{}}
	t.Cleanup(func() {
		b.mu.Lock()
		b.closed = true
		_ = listener.Close()
		for p := range b.peers {
			_ = p.conn.Close()
		}
		b.mu.Unlock()
	})
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			p := &wirePeer{conn: conn}
			b.mu.Lock()
			if b.closed {
				_ = conn.Close()
				b.mu.Unlock()
				return
			}
			b.peers[p] = true
			b.mu.Unlock()
			go b.serve(p)
		}
	}()
	return b
}
func (b *wireBroker) url() string { return "tcp://" + b.listener.Addr().String() }
func (b *wireBroker) serve(p *wirePeer) {
	defer func() { b.mu.Lock(); delete(b.peers, p); b.mu.Unlock(); _ = p.conn.Close() }()
	for {
		packet, err := packets.ReadPacket(p.conn)
		if err != nil {
			return
		}
		switch packet := packet.(type) {
		case *packets.ConnectPacket:
			ack := packets.NewControlPacket(packets.Connack).(*packets.ConnackPacket)
			p.write(ack)
		case *packets.SubscribePacket:
			ack := packets.NewControlPacket(packets.Suback).(*packets.SubackPacket)
			ack.MessageID = packet.MessageID
			ack.ReturnCodes = make([]byte, len(packet.Topics))
			b.mu.Lock()
			denied := b.deny
			b.mu.Unlock()
			if denied {
				for i := range ack.ReturnCodes {
					ack.ReturnCodes[i] = 128
				}
			}
			p.write(ack)
			if denied {
				continue
			}
			b.mu.Lock()
			p.topics = append(p.topics, packet.Topics...)
			for topic, payload := range b.retained {
				for _, filter := range packet.Topics {
					if wireMatches(filter, topic) {
						message := packets.NewControlPacket(packets.Publish).(*packets.PublishPacket)
						message.TopicName = topic
						message.Payload = payload
						message.Retain = true
						p.write(message)
						break
					}
				}
			}
			b.mu.Unlock()
		case *packets.PublishPacket:
			b.mu.Lock()
			if packet.Retain {
				b.retained[packet.TopicName] = append([]byte(nil), packet.Payload...)
			}
			for peer := range b.peers {
				for _, filter := range peer.topics {
					if wireMatches(filter, packet.TopicName) {
						message := packets.NewControlPacket(packets.Publish).(*packets.PublishPacket)
						message.TopicName = packet.TopicName
						message.Payload = packet.Payload
						peer.write(message)
						break
					}
				}
			}
			b.mu.Unlock()
			if packet.Qos == 1 {
				ack := packets.NewControlPacket(packets.Puback).(*packets.PubackPacket)
				ack.MessageID = packet.MessageID
				p.write(ack)
			}
		case *packets.PingreqPacket:
			p.write(packets.NewControlPacket(packets.Pingresp))
		case *packets.DisconnectPacket:
			return
		}
	}
}
func (b *wireBroker) dropConnections() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for peer := range b.peers {
		_ = peer.conn.Close()
	}
}
func wireClient(t *testing.T, b *wireBroker) mqtt.Client {
	t.Helper()
	c := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(b.url()).SetClientID("fixture_" + randomID()[:16]).SetAutoReconnect(false))
	if err := waitToken(c.Connect()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Disconnect(0) })
	return c
}
func eventuallyBemfa(t *testing.T, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for relay")
}
func assertNoBemfaActuation(t *testing.T, acts <-chan map[string]interface{}) {
	t.Helper()
	select {
	case act := <-acts:
		t.Fatal("unexpected actuation", act)
	case <-time.After(150 * time.Millisecond):
	}
}
func TestBemfaRealWireRelayIsolationAndNoReplay(t *testing.T) {
	a, r, d := testBemfa(t)
	original, cloud := newWireBroker(t), newWireBroker(t)
	b, err := newDynamicBroker(original.url(), "", "manager", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	a.broker = b
	b.watchRelay(r.deviceEvent)
	eventuallyBemfa(t, b.client.IsConnectionOpen)
	key := strings.Repeat("a", 32)
	r.clientFactory = func(opts *mqtt.ClientOptions) mqtt.Client {
		if opts.ClientID != key || opts.Servers[0].String() != "ssl://bemfa.com:9503" || opts.TLSConfig.InsecureSkipVerify || !opts.CleanSession {
			t.Error("unsafe Bemfa production options")
		}
		local, _ := url.Parse(cloud.url())
		opts.Servers = []*url.URL{local}
		return mqtt.NewClient(opts)
	}
	device := wireClient(t, original)
	speaker := wireClient(t, cloud)
	acts := make(chan map[string]interface{}, 16)
	reports := make(chan string, 32)
	var stateMu sync.Mutex
	on := false
	base := bemfaBase(d)
	err = waitToken(device.Subscribe(base+"/command", 0, func(_ mqtt.Client, m mqtt.Message) {
		var request struct {
			Action string                 `json:"action"`
			Params map[string]interface{} `json:"params"`
			ID     string                 `json:"request_id"`
		}
		if json.Unmarshal(m.Payload(), &request) != nil {
			return
		}
		stateMu.Lock()
		if request.Action == "outlet.set" {
			acts <- request.Params
			on = request.Params["on"] == true
		}
		value := 0
		if on {
			value = 1
		}
		stateMu.Unlock()
		states := map[string]interface{}{"socket_states": []int{0, 0, 0, 0, 0, value}}
		if request.Action == "status.get" {
			data, _ := json.Marshal(map[string]interface{}{"request_id": request.ID, "code": "ok", "data": states})
			device.Publish(base+"/result", 0, false, data)
		} else {
			data, _ := json.Marshal(states)
			device.Publish(base+"/state", 0, true, data)
			result, _ := json.Marshal(map[string]interface{}{"request_id": request.ID, "code": "accepted", "data": map[string]interface{}{}})
			device.Publish(base+"/result", 0, false, result)
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err = waitToken(speaker.Subscribe("testplug001/up", 0, func(_ mqtt.Client, m mqtt.Message) { reports <- string(m.Payload()) })); err != nil {
		t.Fatal(err)
	}
	_ = waitToken(device.Publish(base+"/availability", 0, true, "online"))
	// A retained control predates enabling; it must never reach the device.
	_ = waitToken(speaker.Publish("testplug001", 0, true, "on"))
	config := BemfaConfig{Enabled: true, APIKey: key, Mappings: []BemfaMapping{{Device: d, Channel: 5, Topic: "testplug001"}}}
	payload, _ := json.Marshal(config)
	res := bemfaRequest(t, a, "alice", "PUT", string(payload))
	if res.Code != 200 {
		t.Fatal(res.Body.String())
	}
	defer r.Close()
	select {
	case value := <-reports:
		if value != "off" {
			t.Fatal("optimistic report", value)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("initial canonical state missing")
	}
	assertNoBemfaActuation(t, acts)
	_ = waitToken(speaker.Publish("testplug001", 0, false, "on#1"))
	assertNoBemfaActuation(t, acts)
	_ = waitToken(speaker.Publish("testplug001", 0, false, "on"))
	select {
	case p := <-acts:
		if p["index"] != float64(5) || p["on"] != true {
			t.Fatal(p)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("command not forwarded")
	}
	select {
	case value := <-reports:
		if value != "on" {
			t.Fatal(value)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("actual state not reported")
	}
	_ = waitToken(device.Publish(base+"/availability", 0, true, "offline"))
	time.Sleep(50 * time.Millisecond)
	_ = waitToken(speaker.Publish("testplug001", 0, false, "off"))
	assertNoBemfaActuation(t, acts)
	_ = waitToken(device.Publish(base+"/availability", 0, true, "online"))
	time.Sleep(200 * time.Millisecond)
	assertNoBemfaActuation(t, acts)
	r.mu.Lock()
	worker := r.workers["alice"]
	r.mu.Unlock()
	worker.enqueue(bemfaEvent{kind: "command", topic: "testplug001", payload: []byte("off"), epoch: worker.epoch.Load() - 1, at: time.Now()})
	worker.enqueue(bemfaEvent{kind: "command", topic: "testplug001", payload: []byte("off"), epoch: worker.epoch.Load(), at: time.Now().Add(-4 * time.Second)})
	assertNoBemfaActuation(t, acts)
	// A real cloud TCP disconnect must re-subscribe without replaying the
	// retained command that the fixture still holds.
	oldEpoch := worker.epoch.Load()
	cloud.dropConnections()
	eventuallyBemfa(t, func() bool { return worker.epoch.Load() > oldEpoch && r.status("alice") == "connected" })
	assertNoBemfaActuation(t, acts)
	speaker = wireClient(t, cloud)
	// Revocation is checked at dispatch, even before periodic worker reconciliation.
	_, _ = a.store.Exec("UPDATE scoped_devices SET status='hidden' WHERE username='alice'")
	_ = waitToken(speaker.Publish("testplug001", 0, false, "off"))
	assertNoBemfaActuation(t, acts)
	res = bemfaRequest(t, a, "alice", "DELETE", "")
	if res.Code != 200 {
		t.Fatal(res.Body.String())
	}
	_ = waitToken(speaker.Publish("testplug001", 0, false, "off"))
	assertNoBemfaActuation(t, acts)
	if r.status("alice") != "disabled" {
		t.Fatal("worker still enabled")
	}
}

func TestRealDynamicSecurityBemfaRelay(t *testing.T) {
	runtime := os.Getenv("PHICOMM_TEST_RUNTIME")
	if runtime == "" {
		t.Skip("isolated dynamic-security broker required")
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
	if _, err = b.Command("modifyRole", map[string]interface{}{"rolename": "manager", "acls": managerRules()}); err != nil {
		t.Fatal(err)
	}
	s, err := openStore(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := &Application{store: s, broker: b}
	user := "relay_" + randomID()[:12]
	password := "isolated-relay-password"
	if err = a.register(user, password); err != nil {
		t.Fatal(err)
	}
	defer a.deleteUser(user)
	mac := "aa9876543210"
	if err = a.discover("phicomm/v2/"+userKey(user)+"/discovery/"+mac, []byte(`{"schema":1,"protocol":"tmcp","protocol_version":2,"version":"v2","model":"tc1"}`)); err != nil {
		t.Fatal(err)
	}
	device := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(b.url).SetTLSConfig(b.tls).SetUsername(user).SetPassword(password).SetClientID("relay_device_" + randomID()[:8]).SetAutoReconnect(false))
	if err = waitToken(device.Connect()); err != nil {
		t.Fatal(err)
	}
	defer device.Disconnect(0)
	base := "phicomm/v2/" + userKey(user) + "/tc1/" + mac
	states := make(chan string, 4)
	commands := make(chan string, 4)
	b.watchRelay(func(topic string, data []byte, retained bool) {
		if topic == base+"/state" {
			states <- string(data)
		}
	})
	if err = b.subscribeRelay(b.client); err != nil {
		t.Fatal(err)
	}
	if err = waitToken(device.Subscribe(base+"/command", 0, func(_ mqtt.Client, m mqtt.Message) { commands <- string(m.Payload()) })); err != nil {
		t.Fatal(err)
	}
	if err = waitToken(device.Publish(base+"/state", 1, true, `{"socket_states":[0,0,0,0,0,0]}`)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-states:
	case <-time.After(5 * time.Second):
		t.Fatal("manager cannot receive state")
	}
	if err = b.relayPublish(base+"/command", []byte(`{"action":"status.get","params":{},"request_id":"acl-test"}`)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-commands:
	case <-time.After(5 * time.Second):
		t.Fatal("manager cannot send relay command")
	}
}

func TestBemfaDeniedSubscriptionIsNotConnected(t *testing.T) {
	a, r, d := testBemfa(t)
	cloud := newWireBroker(t)
	cloud.deny = true
	r.clientFactory = func(opts *mqtt.ClientOptions) mqtt.Client {
		local, _ := url.Parse(cloud.url())
		opts.Servers = []*url.URL{local}
		return mqtt.NewClient(opts)
	}
	config := BemfaConfig{Enabled: true, APIKey: strings.Repeat("a", 32), Mappings: []BemfaMapping{{Device: d, Channel: 0, Topic: "denied001"}}}
	payload, _ := json.Marshal(config)
	res := bemfaRequest(t, a, "alice", "PUT", string(payload))
	if res.Code != 200 {
		t.Fatal(res.Body.String())
	}
	defer r.Close()
	eventuallyBemfa(t, func() bool { return r.status("alice") == "subscription_error" })
	r.mu.Lock()
	worker := r.workers["alice"]
	r.mu.Unlock()
	if worker.readyEpoch.Load() != 0 {
		t.Fatal("denied subscription accepted")
	}
}
