package main

import (
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestRealDynamicSecurity(t *testing.T) {
	runtime := os.Getenv("PHICOMM_TEST_RUNTIME")
	if runtime == "" {
		t.Skip("isolated broker required")
	}
	pass, e := readSecret(runtime + "/secrets/broker-password")
	if e != nil {
		t.Fatal(e)
	}
	b, e := newDynamicBroker(os.Getenv("PHICOMM_TEST_BROKER"), runtime+"/broker/certs/ca.crt", serviceUser, pass)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	deadline := time.Now().Add(10 * time.Second)
	for !b.client.IsConnectionOpen() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	s, e := openStore(runtime + "/integration.db")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	a := &Application{store: s, broker: b}
	u := "test_" + randomID()[:12]
	v := "test_" + randomID()[:12]
	pw := "integration-password"
	if e = a.register(u, pw); e != nil {
		t.Fatal("register", e)
	}
	if e = a.register(v, pw); e != nil {
		t.Fatal(e)
	}
	defer a.deleteUser(u)
	defer a.deleteUser(v)
	if e = b.Authenticate(u, "wrongpassword"); e != errCredentials {
		t.Fatal("bad password", e)
	}
	if e = b.Authenticate(u, pw); e != nil {
		t.Fatal("auth", e)
	}
	d := Device{MAC: "aa1122334455", Model: "tc1", Version: "v1"}
	if e = a.saveDevice(u, d, false); e != nil {
		t.Fatal("bind", e)
	}
	connect := func(user string) mqtt.Client {
		opts := mqtt.NewClientOptions().AddBroker(b.url).SetTLSConfig(b.tls).SetUsername(user).SetPassword(pw).SetClientID("test_" + randomID()[:16]).SetAutoReconnect(false)
		c := mqtt.NewClient(opts)
		if e := waitToken(c.Connect()); e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { c.Disconnect(0) })
		return c
	}
	own, other := connect(u), connect(v)
	grant := func(c mqtt.Client, topic string) byte {
		tok := c.Subscribe(topic, 1, nil)
		if !tok.WaitTimeout(5 * time.Second) {
			t.Fatal("subscribe timeout")
		}
		if tok.Error() != nil {
			return 128
		}
		return tok.(*mqtt.SubscribeToken).Result()[topic]
	}
	if g := grant(own, "device/ztc1/aa1122334455/#"); g == 128 {
		t.Fatal("owner denied")
	}
	if g := grant(other, "device/ztc1/aa1122334455/#"); g != 128 {
		t.Fatal("other allowed", g)
	}
	if g := grant(own, "$CONTROL/dynamic-security/v1/response"); g != 128 {
		t.Fatal("control accessible", g)
	}
	if g := grant(own, "phicomm/a1/aa1122334455/#"); g != 128 {
		t.Fatal("model accessible", g)
	}
	received := make(chan string, 1)
	tok := own.Subscribe("device/ztc1/aa1122334455/set", 1, func(_ mqtt.Client, m mqtt.Message) { received <- string(m.Payload()) })
	if e = waitToken(tok); e != nil {
		t.Fatal(e)
	}
	if e = waitToken(own.Publish("device/ztc1/aa1122334455/set", 1, false, "0")); e != nil {
		t.Fatal(e)
	}
	select {
	case value := <-received:
		if value != "0" {
			t.Fatal(value)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("owner command not delivered")
	}
	if e = a.removeDevice(u, "aa1122334455"); e != nil {
		t.Fatal("unbind", e)
	}
	own.Disconnect(0)
	own = connect(u)
	if grant(own, "device/ztc1/aa1122334455/#") != 128 {
		t.Fatal("permissions retained")
	}
	if e = a.saveDevice(v, d, false); e != nil {
		t.Fatal("rebind", e)
	}
	if e = a.changePassword(u, "replacement-password"); e != nil {
		t.Fatal(e)
	}
	if e = b.Authenticate(u, pw); e != errCredentials {
		t.Fatal("old password accepted", e)
	}
	if e = b.Authenticate(u, "replacement-password"); e != nil {
		t.Fatal("new password", e)
	}
}

func TestRealPersistence(t *testing.T) {
	runtime := os.Getenv("PHICOMM_TEST_RUNTIME")
	phase := os.Getenv("PHICOMM_TEST_PHASE")
	if runtime == "" || phase == "" {
		t.Skip("isolated restart phase required")
	}
	pass, e := readSecret(runtime + "/secrets/broker-password")
	if e != nil {
		t.Fatal(e)
	}
	b, e := newDynamicBroker(os.Getenv("PHICOMM_TEST_BROKER"), runtime+"/broker/certs/ca.crt", serviceUser, pass)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	deadline := time.Now().Add(10 * time.Second)
	for !b.client.IsConnectionOpen() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	s, e := openStore(runtime + "/restart.db")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	a := &Application{store: s, broker: b}
	const user = "restart_test"
	const password = "restart-test-password"
	if phase == "seed" {
		if e = a.register(user, password); e != nil {
			t.Fatal(e)
		}
		for i, model := range []string{"tc1", "m1", "a1"} {
			mac := []string{"aa1111111111", "aa2222222222", "aa3333333333"}[i]
			if model == "tc1" {
				e = a.saveDevice(user, Device{MAC: mac, Model: model, Version: "v1"}, false)
			} else {
				e = a.discover("phicomm/v2/"+userKey(user)+"/discovery/"+mac, []byte(`{"schema":1,"protocol":"tmcp","protocol_version":2,"version":"v2","model":"`+model+`"}`))
			}
			if e != nil {
				t.Fatal(e)
			}
		}
		if e = a.discover("phicomm/v2/"+userKey(user)+"/discovery/aa1111111111", []byte(`{"schema":1,"protocol":"tmcp","protocol_version":2,"version":"v2","model":"tc1"}`)); e != nil {
			t.Fatal(e)
		}
		return
	}
	if e = b.Authenticate(user, password); e != nil {
		t.Fatal("password not persisted", e)
	}
	ds, e := s.devices(user)
	if e != nil || len(ds) != 4 {
		t.Fatal("database not persisted", ds, e)
	}
	data, e := b.Command("getClient", map[string]interface{}{"username": user})
	if e != nil {
		t.Fatal(e)
	}
	for _, d := range ds {
		if !hasRole(data, deviceRole(d)) {
			t.Fatal("role not persisted", d)
		}
	}
	if e = a.deleteUser(user); e != nil {
		t.Fatal(e)
	}
	if e = b.Authenticate(user, password); e != errCredentials {
		t.Fatal("deleted user can authenticate", e)
	}
}

func TestRealMigration(t *testing.T) {
	runtime := os.Getenv("PHICOMM_TEST_RUNTIME")
	if runtime == "" || os.Getenv("PHICOMM_TEST_MIGRATED") != "1" {
		t.Skip("isolated migrated broker required")
	}
	pass, e := readSecret(runtime + "/secrets/broker-password")
	if e != nil {
		t.Fatal(e)
	}
	b, e := newDynamicBroker(os.Getenv("PHICOMM_TEST_BROKER"), runtime+"/broker/certs/ca.crt", serviceUser, pass)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	deadline := time.Now().Add(15 * time.Second)
	for !b.client.IsConnectionOpen() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if e = b.Authenticate("legacy_test", "original-test-password"); e != nil {
		t.Fatal("legacy password changed", e)
	}
	s, e := openStore(runtime + "/app/app.db")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ds, e := s.devices("legacy_test")
	if e != nil || len(ds) != 1 || ds[0].MAC != "aabbccddeeff" {
		t.Fatal(ds, e)
	}
	data, e := b.Command("getClient", map[string]interface{}{"username": "legacy_test"})
	if e != nil || !hasRole(data, "device_aabbccddeeff") {
		t.Fatal("migrated role missing", e)
	}
}

func TestRealDynamicSecurityDiscovery(t *testing.T) {
	runtime := os.Getenv("PHICOMM_TEST_RUNTIME")
	if runtime == "" {
		t.Skip("isolated broker required")
	}
	pass, e := readSecret(runtime + "/secrets/broker-password")
	if e != nil {
		t.Fatal(e)
	}
	b, e := newDynamicBroker(os.Getenv("PHICOMM_TEST_BROKER"), runtime+"/broker/certs/ca.crt", serviceUser, pass)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	deadline := time.Now().Add(10 * time.Second)
	for !b.client.IsConnectionOpen() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	s, e := openStore(runtime + "/discovery-test.db")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	a := &Application{store: s, broker: b}
	events := make(chan error, 20)
	b.watchDiscovery(func(topic string, payload []byte) { events <- a.discover(topic, payload) })
	if e = b.subscribeDiscovery(b.client); e != nil {
		t.Fatal(e)
	}
	users := []string{"a+b@example.com", "scoped_bob"}
	pw := "namespace-test-password"
	for _, u := range users {
		if e = a.register(u, pw); e != nil {
			t.Fatal(e)
		}
		defer a.deleteUser(u)
	}
	connect := func(u string) mqtt.Client {
		c := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(b.url).SetTLSConfig(b.tls).SetUsername(u).SetPassword(pw).SetClientID("scope_" + randomID()[:12]).SetAutoReconnect(false))
		if e := waitToken(c.Connect()); e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { c.Disconnect(0) })
		return c
	}
	grant := func(c mqtt.Client, topic string) byte {
		tok := c.Subscribe(topic, 1, nil)
		if !tok.WaitTimeout(5 * time.Second) {
			t.Fatal("timeout")
		}
		if tok.Error() != nil {
			return 128
		}
		return tok.(*mqtt.SubscribeToken).Result()[topic]
	}
	const mac = "abcdeffedcba"
	for _, u := range users {
		c := connect(u)
		topic := "phicomm/v2/" + userKey(u) + "/discovery/" + mac
		if e = waitToken(c.Publish(topic, 1, true, `{"schema":1,"protocol":"tmcp","protocol_version":2,"version":"v2","model":"m1"}`)); e != nil {
			t.Fatal(e)
		}
		select {
		case e = <-events:
			if e != nil {
				t.Fatal(e)
			}
		case <-time.After(6 * time.Second):
			t.Fatal("discovery not delivered")
		}
		c.Disconnect(0)
		c = connect(u)
		if grant(c, "phicomm/v2/"+userKey(u)+"/m1/"+mac+"/#") == 128 {
			t.Fatal("scoped owner denied")
		}
		if grant(c, "phicomm/v2/"+userKey(users[1])+"/m1/"+mac+"/#") != 128 && u != users[1] {
			t.Fatal("other namespace allowed")
		}
		if grant(c, "phicomm/m1/"+mac+"/#") != 128 {
			t.Fatal("legacy implicitly granted")
		}
		if grant(c, "homeassistant/+/"+userKey(u)+"/+/config") == 128 {
			t.Fatal("HA subscription denied")
		}
	}
	own := connect(users[0])
	otherTopic := "phicomm/v2/" + userKey(users[1]) + "/discovery/001122334455"
	_ = waitToken(own.Publish(otherTopic, 1, true, `{"schema":1,"protocol":"tmcp","protocol_version":2,"version":"v2","model":"a1"}`))
	select {
	case e = <-events:
		t.Fatal("spoof reached manager", e)
	case <-time.After(300 * time.Millisecond):
	}
	list, e := s.devices(users[1])
	if e != nil || len(list) != 1 {
		t.Fatal("spoof created device", list, e)
	}
	// A fresh manager connection must replay retained announcements safely.
	b2, e := newDynamicBroker(b.url, runtime+"/broker/certs/ca.crt", serviceUser, pass)
	if e != nil {
		t.Fatal(e)
	}
	defer b2.Close()
	replay := make(chan string, 10)
	b2.watchDiscovery(func(topic string, payload []byte) {
		if err := a.discover(topic, payload); err != nil {
			t.Error(err)
		}
		replay <- topic
	})
	for i := 0; i < 2; i++ {
		select {
		case <-replay:
		case <-time.After(6 * time.Second):
			t.Fatal("retained replay missing")
		}
	}
}

func TestRealDynamicSecurityAliasesAndLinkage(t *testing.T) {
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
	deadline := time.Now().Add(10 * time.Second)
	for !b.client.IsConnectionOpen() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	s, err := openStore(runtime + "/aliases-test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := &Application{store: s, broker: b}
	user := "alias_" + randomID()[:8] + "@example.com"
	alias := "short_" + randomID()[:8]
	other := "other_" + randomID()[:8]
	pw := "alias-test-password"
	for _, u := range []string{user, other} {
		if err = a.register(u, pw); err != nil {
			t.Fatal(err)
		}
		defer a.deleteUser(u)
	}
	if err = a.addMQTTAlias(user, alias, pw); err != nil {
		t.Fatal(err)
	}
	if err = b.Authenticate(alias, pw); err != nil {
		t.Fatal("MQTT alias rejected", err)
	}
	for _, entry := range []struct{ u, mac, model string }{{alias, "aabbccddeeff", "a1"}, {alias, "001122334455", "m1"}, {other, "aabbccddeeff", "a1"}} {
		if err = a.discover("phicomm/v2/"+userKey(entry.u)+"/discovery/"+entry.mac, []byte(`{"schema":1,"protocol":"tmcp","protocol_version":2,"version":"v2","model":"`+entry.model+`"}`)); err != nil {
			t.Fatal(err)
		}
	}
	connect := func(u string) mqtt.Client {
		c := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(b.url).SetTLSConfig(b.tls).SetUsername(u).SetPassword(pw).SetClientID("link_" + randomID()[:12]).SetAutoReconnect(true).SetConnectRetryInterval(100 * time.Millisecond).SetMaxReconnectInterval(time.Second))
		if e := waitToken(c.Connect()); e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { c.Disconnect(0) })
		return c
	}
	own, foreign := connect(alias), connect(other)
	grant := func(c mqtt.Client, topic string, handler mqtt.MessageHandler) byte {
		deadline := time.Now().Add(5 * time.Second)
		for !c.IsConnectionOpen() && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		tok := c.Subscribe(topic, 1, handler)
		if !tok.WaitTimeout(5 * time.Second) {
			t.Fatal("timeout")
		}
		if tok.Error() != nil {
			return 128
		}
		return tok.(*mqtt.SubscribeToken).Result()[topic]
	}
	key := userKey(alias)
	foreignKey := userKey(other)
	bind := "phicomm/v2/" + key + "/m1/001122334455/link/aabbccddeeff/result"
	address := "phicomm/v2/" + key + "/a1/aabbccddeeff/link/001122334455/query"
	group := "phicomm/linkage/" + key + "/living-room:00:00:00:00:00:00/fan/set"
	for _, topic := range []string{bind, address, group, "phicomm/v2/" + key + "/a1/aabbccddeeff/link/+/query"} {
		if grant(own, topic, nil) == 128 {
			t.Fatal("own linkage denied", topic)
		}
	}
	for _, topic := range []string{"phicomm/bind/" + key + "/ffffffffffff", "phicomm/bind/" + foreignKey + "/aabbccddeeff", "phicomm/address/" + foreignKey + "/+", "phicomm/linkage/" + foreignKey + "/#", "phicomm/bind/aabbccddeeff", "living-room:00:00:00:00:00:00/#", "$CONTROL/dynamic-security/v1/response"} {
		if grant(own, topic, nil) != 128 {
			t.Fatal("unexpected permission", topic)
		}
	}
	deliveries := make(chan string, 10)
	handler := func(_ mqtt.Client, m mqtt.Message) { deliveries <- string(m.Payload()) }
	for _, topic := range []string{bind, address, group} {
		if grant(own, topic, handler) == 128 {
			t.Fatal(topic)
		}
		if err = waitToken(own.Publish(topic, 1, false, "allowed")); err != nil {
			t.Fatal(err)
		}
		select {
		case value := <-deliveries:
			if value != "allowed" {
				t.Fatal(value)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("authorized publish not delivered", topic)
		}
	}
	// Cross-account and unregistered address spoofing must not reach a subscribed receiver.
	if err = waitToken(foreign.Publish(group, 1, false, "foreign")); err != nil {
		t.Fatal(err)
	}
	select {
	case value := <-deliveries:
		t.Fatal("cross-account publish delivered", value)
	case <-time.After(300 * time.Millisecond):
	}
	app = a
	t.Cleanup(func() { app = nil })
	req := httptest.NewRequest("DELETE", "/?mqtt_username="+alias, nil)
	w := httptest.NewRecorder()
	handleScopedDeviceFor(w, req, "001122334455", user)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if grant(own, bind, nil) != 128 {
		t.Fatal("unbound binding still allowed")
	}
	if err = waitToken(own.Publish(bind, 1, false, "after-unbind")); err != nil {
		t.Fatal(err)
	}
	select {
	case value := <-deliveries:
		t.Fatal("unbound binding publish delivered", value)
	case <-time.After(300 * time.Millisecond):
	}
	if grant(own, group, nil) == 128 {
		t.Fatal("remaining A1 lost its group permission")
	}
	req = httptest.NewRequest("DELETE", "/?mqtt_username="+alias, nil)
	w = httptest.NewRecorder()
	handleScopedDeviceFor(w, req, "aabbccddeeff", user)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if grant(own, group, nil) != 128 {
		t.Fatal("empty account kept group ACL")
	}
	own.Disconnect(0)
	foreign.Disconnect(0)
	if err = a.changePassword(user, "replacement-password"); err != nil {
		t.Fatal(err)
	}
	if b.Authenticate(alias, pw) != errCredentials {
		t.Fatal("old alias password accepted")
	}
	if err = b.Authenticate(alias, "replacement-password"); err != nil {
		t.Fatal(err)
	}
	if err = a.deleteUser(user); err != nil {
		t.Fatal(err)
	}
	if b.Authenticate(alias, "replacement-password") != errCredentials {
		t.Fatal("deleted alias accepted")
	}
}
