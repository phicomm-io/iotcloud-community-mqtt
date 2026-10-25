package main

import (
	"fmt"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

func TestRealDynamicSecurityM1DisplaySources(t *testing.T) {
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
	s, err := openStore(runtime + "/m1-display-acl.db")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := &Application{store: s, broker: b}
	app = a
	t.Cleanup(func() { app = nil })
	suffix := randomID()[:10]
	owner, alias, other := "display_owner_"+suffix, "display_alias_"+suffix, "display_other_"+suffix
	const password = "m1-display-test-password"
	for _, u := range []string{owner, other} {
		if err := a.register(u, password); err != nil {
			t.Fatal(err)
		}
		defer a.deleteUser(u)
	}
	if err := a.addMQTTAlias(owner, alias, password); err != nil {
		t.Fatal(err)
	}
	type source struct{ principal, model, mac, metric, unit string }
	m1 := source{owner, "m1", "111111111111", "pm25", "ug/m3"}
	tc1 := source{alias, "tc1", "222222222222", "power", "W"}
	daikin := source{alias, "daikin", "333333333333", "co2", "ppm"}
	a1 := source{alias, "a1", "444444444444", "fan_speed_percent", "%"}
	foreign := source{other, "m1", m1.mac, "pm25", "ug/m3"}
	root := func(principal string) string { return "phicomm/v2/" + userKey(principal) }
	state := func(d source) string { return root(d.principal) + "/sensor/" + d.mac + "/state" }
	availability := func(d source) string { return root(d.principal) + "/" + d.model + "/" + d.mac + "/availability" }
	announce := func(d source) {
		t.Helper()
		if err := a.discover(root(d.principal)+"/discovery/"+d.mac,
			[]byte(fmt.Sprintf(`{"schema":1,"protocol":"tmcp","protocol_version":2,"version":"v2","model":%q}`, d.model))); err != nil {
			t.Fatal(err)
		}
	}
	remove := func(d source) {
		t.Helper()
		w := httptest.NewRecorder()
		handleScopedDeviceFor(w, httptest.NewRequest("DELETE", "/?mqtt_username="+d.principal, nil), d.mac, owner)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	for _, d := range []source{m1, tc1, daikin, foreign} {
		announce(d)
	}

	// A separate publisher can deliberately inject unregistered/foreign data.
	// This proves receive filtering independently of normal send ACL denial.
	probeUser, probeRole := "acl_probe_"+suffix, "acl_probe_role_"+suffix
	probeTopics := []string{
		availability(m1), availability(tc1), availability(daikin), availability(a1), availability(foreign),
		root(owner) + "/tc1/ffffffffffff/availability", root(alias) + "/m1/ffffffffffff/availability",
		root(owner) + "/sensor/ffffffffffff/state", state(tc1), state(a1),
		root(owner) + "/sensor/ffffffffffff/command", root(alias) + "/sensor/ffffffffffff/command",
		root(alias) + "/sensor/" + tc1.mac + "/command", root(owner) + "/address/ffffffffffff",
		"phicomm/linkage/" + userKey(owner) + "/probe/fan/set", "phicomm/linkage/" + userKey(alias) + "/probe/fan/set",
	}
	probeRules := []Rule{}
	for _, topic := range probeTopics {
		for _, kind := range []string{"publishClientSend", "publishClientReceive", "subscribeLiteral"} {
			probeRules = append(probeRules, Rule{kind, topic, true, 1})
		}
	}
	command := func(name string, values map[string]interface{}) {
		t.Helper()
		if _, err := b.Command(name, values); err != nil {
			t.Fatal(name, err)
		}
	}
	command("createRole", map[string]interface{}{"rolename": probeRole, "acls": probeRules})
	defer b.Command("deleteRole", map[string]interface{}{"rolename": probeRole})
	command("createClient", map[string]interface{}{"username": probeUser, "password": password})
	defer b.Command("deleteClient", map[string]interface{}{"username": probeUser})
	command("addClientRole", map[string]interface{}{"username": probeUser, "rolename": probeRole})
	type delivery struct {
		topic, payload string
		retained       bool
	}
	connect := func(u string, inbox chan delivery) mqtt.Client {
		t.Helper()
		opts := mqtt.NewClientOptions().AddBroker(b.url).SetTLSConfig(b.tls).
			SetUsername(u).SetPassword(password).SetClientID("display_" + randomID()[:16]).SetAutoReconnect(false)
		if inbox != nil {
			opts.SetDefaultPublishHandler(func(_ mqtt.Client, m mqtt.Message) {
				inbox <- delivery{m.Topic(), string(m.Payload()), m.Retained()}
			})
		}
		c := mqtt.NewClient(opts)
		if err := waitToken(c.Connect()); err != nil {
			t.Fatal(u, err)
		}
		t.Cleanup(func() { c.Disconnect(0) })
		return c
	}
	subscribe := func(c mqtt.Client, filter string, allowed bool) {
		t.Helper()
		tok := c.Subscribe(filter, 1, nil)
		if err := waitToken(tok); err != nil {
			t.Fatal(filter, err)
		}
		if got := tok.(*mqtt.SubscribeToken).Result()[filter] != 128; got != allowed {
			t.Fatalf("SUBACK %q = %t, want %t", filter, got, allowed)
		}
	}
	publish := func(c mqtt.Client, topic, body string, retained bool) {
		t.Helper()
		if err := waitToken(c.Publish(topic, 1, retained, body)); err != nil {
			t.Fatal(topic, err)
		}
	}
	probeInbox := make(chan delivery, 64)
	probe := connect(probeUser, probeInbox)
	for _, topic := range probeTopics[7:] {
		subscribe(probe, topic, true)
	}
	clients := map[string]mqtt.Client{}
	inboxes := map[string]chan delivery{}
	disconnect := func() {
		for _, c := range clients {
			c.Disconnect(0)
		}
	}
	reconnect := func() {
		t.Helper()
		disconnect()
		for _, u := range []string{owner, alias, other} {
			inboxes[u] = make(chan delivery, 64)
			clients[u] = connect(u, inboxes[u])
		}
	}
	subscribeSources := func(withAvailability bool) {
		t.Helper()
		for _, u := range []string{owner, alias, other} {
			principals := []string{owner, alias}
			if u == other {
				principals = []string{other}
			}
			for _, principal := range principals {
				subscribe(clients[u], root(principal)+"/sensor/+/state", true)
				if withAvailability {
					subscribe(clients[u], root(principal)+"/+/+/availability", true)
				}
			}
		}
	}
	expect := func(inbox chan delivery, want map[string]string, retained bool) {
		t.Helper()
		timeout := time.After(5 * time.Second)
		for len(want) > 0 {
			select {
			case got := <-inbox:
				body, ok := want[got.topic]
				if !ok || got.payload != body || got.retained != retained {
					t.Fatalf("unexpected delivery %+v; remaining %v", got, want)
				}
				delete(want, got.topic)
			case <-timeout:
				t.Fatal("missing deliveries", want)
			}
		}
	}
	expectNone := func() {
		t.Helper()
		select {
		case got := <-inboxes[owner]:
			t.Fatal("owner received unexpected message", got)
		case got := <-inboxes[alias]:
			t.Fatal("alias received unexpected message", got)
		case got := <-inboxes[other]:
			t.Fatal("other received unexpected message", got)
		case got := <-probeInbox:
			t.Fatal("forbidden publish reached independent observer", got)
		case <-time.After(400 * time.Millisecond):
		}
	}
	checkStates := func(sources []source) {
		t.Helper()
		ownWant, otherWant, probeWant := map[string]string{}, map[string]string{}, map[string]string{}
		for _, d := range sources {
			body := fmt.Sprintf(`{"schema":1,"model":%q,"device_id":%q,"boot":1,"sequence":1,"sensors":{%q:{"value":42,"unit":%q,"valid":true,"age_ms":0}}}`, d.model, d.mac, d.metric, d.unit)
			publish(clients[d.principal], state(d), body, false)
			if d.principal == other {
				otherWant[state(d)] = body
			} else {
				ownWant[state(d)] = body
			}
			if d == tc1 || d == a1 {
				probeWant[state(d)] = body
			}
		}
		for _, u := range []string{owner, alias} {
			copyWant := map[string]string{}
			for topic, body := range ownWant {
				copyWant[topic] = body
			}
			expect(inboxes[u], copyWant, false)
		}
		expect(inboxes[other], otherWant, false)
		expect(probeInbox, probeWant, false)
		expectNone()
	}

	// Include unregistered retained messages in both owned prefixes. Broad
	// availability SUBACKs must only deliver the registered device roles.
	for _, topic := range probeTopics[:7] {
		publish(probe, topic, "online", true)
	}
	reconnect()
	subscribeSources(true)
	for _, u := range []string{owner, alias} {
		expect(inboxes[u], map[string]string{availability(m1): "online", availability(tc1): "online", availability(daikin): "online"}, true)
		for _, filter := range []string{root(other) + "/sensor/+/state", root(other) + "/+/+/availability", "phicomm/v2/+/+/+/availability", root(owner) + "/sensor/ffffffffffff/command", root(alias) + "/sensor/" + tc1.mac + "/command"} {
			subscribe(clients[u], filter, false)
		}
	}
	expect(inboxes[other], map[string]string{availability(foreign): "online"}, true)
	subscribe(clients[other], root(owner)+"/sensor/+/state", false)
	subscribe(clients[other], root(owner)+"/+/+/availability", false)
	expectNone()
	checkStates([]source{m1, tc1, daikin, foreign})
	// MQTT 3 PUBACK does not report ACL denial. Observe all authorized receivers
	// and the independent probe instead of treating Publish's return as proof.
	for _, attempt := range []struct{ sender, topic string }{
		{owner, root(owner) + "/sensor/ffffffffffff/state"},
		{owner, state(foreign)}, {other, state(tc1)},
		{owner, root(owner) + "/sensor/ffffffffffff/command"},
		{owner, root(alias) + "/sensor/ffffffffffff/command"},
		{owner, root(alias) + "/sensor/" + tc1.mac + "/command"},
		{owner, root(owner) + "/address/ffffffffffff"},
		{owner, "phicomm/linkage/" + userKey(owner) + "/probe/fan/set"},
		{owner, "phicomm/linkage/" + userKey(alias) + "/probe/fan/set"},
	} {
		publish(clients[attempt.sender], attempt.topic, "forbidden-before-a1", false)
	}
	expectNone()
	for _, topic := range probeTopics[:7] {
		publish(probe, topic, "offline", false)
	}
	for _, u := range []string{owner, alias} {
		expect(inboxes[u], map[string]string{availability(m1): "offline", availability(tc1): "offline", availability(daikin): "offline"}, false)
	}
	expect(inboxes[other], map[string]string{availability(foreign): "offline"}, false)
	expectNone()

	disconnect()
	announce(a1)
	reconnect()
	subscribeSources(false)
	checkStates([]source{tc1, a1})
	// An A1 under the alias keeps its original automation scope; an M1 under
	// the primary username must not extend command permissions to that prefix.
	publish(clients[owner], root(alias)+"/sensor/ffffffffffff/command", "a1-command", false)
	expect(probeInbox, map[string]string{root(alias) + "/sensor/ffffffffffff/command": "a1-command"}, false)
	publish(clients[owner], root(owner)+"/sensor/ffffffffffff/command", "forbidden-primary-command", false)
	expectNone()

	// Rebuild already-active roles created by a server without typed publishers.
	// Account reads stay enabled, so missing send ACLs are observable on the wire.
	for _, d := range []source{tc1, a1} {
		disconnect()
		device := Device{Username: owner, MQTTUsername: d.principal, Namespace: "account", Model: d.model, MAC: d.mac}
		command("modifyRole", map[string]interface{}{"rolename": deviceRole(device), "acls": scopeRules(device)[:3]})
		reconnect()
		subscribeSources(false)
		publish(clients[alias], state(d), "old-role-denied", false)
		expectNone()
		disconnect()
		announce(d)
		reconnect()
		subscribeSources(false)
		checkStates([]source{d})
	}

	disconnect()
	remove(tc1)
	reconnect()
	subscribeSources(false)
	publish(clients[alias], state(tc1), "revoked-publisher", false)
	expectNone()
	// Its retained availability remains in the broker but its receive role is gone.
	for _, u := range []string{owner, alias} {
		subscribe(clients[u], root(alias)+"/+/+/availability", true)
		expect(inboxes[u], map[string]string{availability(daikin): "online", availability(a1): "online"}, true)
	}
	expectNone()

	disconnect()
	remove(m1)
	reconnect()
	for _, u := range []string{owner, alias} {
		subscribe(clients[u], root(owner)+"/sensor/+/state", false)
		subscribe(clients[u], root(alias)+"/sensor/+/state", true)
		subscribe(clients[u], root(owner)+"/+/+/availability", false)
		subscribe(clients[u], root(alias)+"/+/+/availability", false)
	}
	publish(clients[owner], root(alias)+"/sensor/ffffffffffff/command", "remaining-a1-command", false)
	expect(probeInbox, map[string]string{root(alias) + "/sensor/ffffffffffff/command": "remaining-a1-command"}, false)
	expectNone()
	disconnect()
	remove(a1)
	reconnect()
	for _, u := range []string{owner, alias} {
		subscribe(clients[u], root(alias)+"/sensor/+/state", false)
	}
	publish(clients[owner], root(alias)+"/sensor/ffffffffffff/command", "revoked-a1-command", false)
	expectNone()
	// Remove retained fixtures while the dedicated publisher still owns them.
	for _, topic := range probeTopics[:7] {
		publish(probe, topic, "", true)
	}
}
