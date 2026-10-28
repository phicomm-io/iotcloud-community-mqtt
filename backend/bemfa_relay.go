package main

import (
	"crypto/cipher"
	"crypto/tls"
	"errors"
	"fmt"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type bemfaEvent struct {
	topic    string
	payload  []byte
	retained bool
	epoch    uint64
	at       time.Time
	kind     string
}
type bemfaWorker struct {
	relay       *BemfaRelay
	user        string
	config      BemfaConfig
	client      mqtt.Client
	events      chan bemfaEvent
	done        chan struct{}
	lifecycle   sync.Mutex
	stopped     bool
	epoch       atomic.Uint64
	readyEpoch  atomic.Uint64
	sourceEpoch atomic.Uint64
	statusMu    sync.Mutex
	state       string
}
type bemfaDeviceState struct {
	online    bool
	data      map[string]interface{}
	probe     string
	lastProbe time.Time
}

func newBemfaRelay(a *Application, seal cipher.AEAD) *BemfaRelay {
	return &BemfaRelay{app: a, seal: seal, workers: map[string]*bemfaWorker{}, done: make(chan struct{}), clientFactory: mqtt.NewClient}
}
func (r *BemfaRelay) start() {
	go func() {
		r.reload()
		tick := time.NewTicker(5 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-r.done:
				return
			case <-tick.C:
				r.reload()
			}
		}
	}()
}
func (r *BemfaRelay) Close() {
	close(r.done)
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, w := range r.workers {
		w.stop()
	}
	r.workers = map[string]*bemfaWorker{}
	r.routes = map[string][]*bemfaWorker{}
}
func (r *BemfaRelay) status(user string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if w := r.workers[user]; w != nil {
		w.statusMu.Lock()
		defer w.statusMu.Unlock()
		return w.state
	}
	return "disabled"
}
func (w *bemfaWorker) setStatus(s string) { w.statusMu.Lock(); w.state = s; w.statusMu.Unlock() }
func (w *bemfaWorker) enqueue(e bemfaEvent) {
	select {
	case w.events <- e:
	default:
		w.setStatus("busy")
		w.sourceEpoch.Add(1)
		if b, ok := w.relay.app.broker.(interface{ refreshRelay() }); ok {
			b.refreshRelay()
		}
	}
}
func (w *bemfaWorker) stop() {
	w.lifecycle.Lock()
	defer w.lifecycle.Unlock()
	if !w.stopped {
		w.stopped = true
		close(w.done)
		if w.client != nil {
			w.client.Disconnect(0)
		}
	}
}
func (r *BemfaRelay) reload() {
	r.mu.Lock()
	defer r.mu.Unlock()
	select {
	case <-r.done:
		return
	default:
	}
	rows, err := r.app.store.Query("SELECT b.username,b.revision FROM bemfa_integrations b JOIN users u ON u.username=b.username WHERE u.state='active'")
	if err != nil {
		return
	}
	versions := map[string]string{}
	for rows.Next() {
		var u, v string
		if rows.Scan(&u, &v) == nil {
			versions[u] = v
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return
	}
	for u, w := range r.workers {
		if versions[u] != w.config.Revision {
			w.stop()
			delete(r.workers, u)
		}
	}
	for u := range versions {
		if r.workers[u] != nil {
			continue
		}
		c, e := r.readConfig(u)
		if e != nil || !c.Enabled {
			continue
		}
		// Re-check ownership on startup; each dispatch and report also revalidates it.
		if r.validate(u, &c) != nil {
			continue
		}
		w := &bemfaWorker{relay: r, user: u, config: c, events: make(chan bemfaEvent, 64), done: make(chan struct{}), state: "connecting"}
		r.workers[u] = w
		w.connect()
		go w.run()
	}
	r.routes = map[string][]*bemfaWorker{}
	for _, w := range r.workers {
		seen := map[string]bool{}
		for _, m := range w.config.Mappings {
			base := bemfaBase(m.Device)
			if !seen[base] {
				r.routes[base] = append(r.routes[base], w)
				seen[base] = true
			}
		}
	}
}
func (w *bemfaWorker) connect() {
	// Private key authentication uses the private key as client ID, per Bemfa's
	// documented MQTT API. Never include it in logs, status or HTTP responses.
	opts := mqtt.NewClientOptions().AddBroker("ssl://bemfa.com:9503").SetClientID(w.config.APIKey).SetProtocolVersion(4).SetCleanSession(true).SetAutoReconnect(true).SetConnectRetry(true).SetConnectTimeout(5 * time.Second).SetConnectRetryInterval(10 * time.Second).SetMaxReconnectInterval(time.Minute).SetOrderMatters(true).SetTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12}).SetWriteTimeout(3 * time.Second)
	opts.OnConnectionLost = func(_ mqtt.Client, _ error) { w.epoch.Add(1); w.setStatus("disconnected") }
	opts.OnConnect = func(c mqtt.Client) {
		generation := w.epoch.Add(1)
		w.setStatus("connecting")
		topics := map[string]byte{}
		for _, m := range w.config.Mappings {
			topics[m.Topic] = 1
		}
		token := c.SubscribeMultiple(topics, func(_ mqtt.Client, m mqtt.Message) {
			if m.Retained() || m.Duplicate() || len(m.Payload()) > 16 {
				return
			}
			w.enqueue(bemfaEvent{kind: "command", topic: m.Topic(), payload: append([]byte(nil), m.Payload()...), epoch: generation, at: time.Now()})
		})
		err := waitToken(token)
		if err == nil {
			for topic := range topics {
				if grant, ok := token.(*mqtt.SubscribeToken).Result()[topic]; !ok || grant > 1 {
					err = errors.New("subscription rejected")
					break
				}
			}
		}
		if err != nil {
			w.setStatus("subscription_error")
			w.readyEpoch.Store(0)
			return
		}
		w.readyEpoch.Store(generation)
		w.setStatus("connected")
		w.enqueue(bemfaEvent{kind: "connected"})
	}
	w.client = w.relay.clientFactory(opts)
	w.client.Connect()
}
func (r *BemfaRelay) deviceEvent(topic string, payload []byte, retained bool) {
	if len(payload) > 64<<10 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if topic == "" {
		for _, w := range r.workers {
			w.sourceEpoch.Add(1)
			w.enqueue(bemfaEvent{kind: "reset"})
		}
		return
	}
	cut := strings.LastIndex(topic, "/")
	if cut < 0 {
		return
	}
	workers := r.routes[topic[:cut]]
	if len(workers) == 0 {
		return
	}
	data := append([]byte(nil), payload...)
	for _, w := range workers {
		w.enqueue(bemfaEvent{kind: "device", epoch: w.sourceEpoch.Load(), topic: topic, payload: data, retained: retained})
	}
}
func (w *bemfaWorker) authorized(m BemfaMapping) bool {
	var revision, state string
	err := w.relay.app.store.QueryRow("SELECT b.revision,u.state FROM bemfa_integrations b JOIN users u ON u.username=b.username WHERE b.username=?", w.user).Scan(&revision, &state)
	if err != nil || revision != w.config.Revision || state != "active" {
		return false
	}
	devices, err := w.relay.app.store.devices(w.user)
	if err != nil {
		return false
	}
	for _, d := range devices {
		if sameBemfaDevice(d, m.Device) && d.Status == "active" {
			return true
		}
	}
	return false
}
func (w *bemfaWorker) publishCommand(m BemfaMapping, action string, params map[string]interface{}) (string, error) {
	w.lifecycle.Lock()
	defer w.lifecycle.Unlock()
	if w.stopped || !w.authorized(m) {
		return "", errors.New("mapping unavailable")
	}
	id := "bm_" + randomID()[:24]
	format, err := w.relay.app.store.deviceCodec(m.Device)
	if err != nil {
		return "", err
	}
	data, err := encodeDeviceWire(format, map[string]interface{}{"action": action, "params": params, "request_id": id})
	if err != nil {
		return "", err
	}
	b, ok := w.relay.app.broker.(interface{ relayPublish(string, []byte) error })
	if !ok {
		return "", errUnavailable
	}
	return id, b.relayPublish(bemfaBase(m.Device)+"/command", data)
}
func bemfaCommand(m BemfaMapping, text string) (string, map[string]interface{}, error) {
	if text != "on" && text != "off" && !strings.HasPrefix(text, "on#") {
		return "", nil, errors.New("invalid command")
	}
	on := text != "off"
	action := "outlet.set"
	params := map[string]interface{}{"on": on}
	if m.Device.Model == "tc1" {
		if text != "on" && text != "off" {
			return "", nil, errors.New("invalid socket command")
		}
		params["index"] = m.Channel
		return action, params, nil
	}
	if m.Channel == 1 {
		action = "light.set"
		params = map[string]interface{}{"enabled": on}
	} else {
		action = "fan.set"
		params["mode"] = "manual"
	}
	if strings.HasPrefix(text, "on#") {
		raw := text[3:]
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 || strconv.Itoa(n) != raw {
			return "", nil, errors.New("invalid level")
		}
		if m.Channel == 0 {
			if n > 4 {
				return "", nil, errors.New("invalid gear")
			}
			params["speed"] = []int{0, 20, 50, 75, 100}[n]
		} else {
			params["brightness"] = (n*255 + 50) / 100
		}
	}
	return action, params, nil
}
func bemfaReport(m BemfaMapping, s map[string]interface{}) string {
	if m.Device.Model == "tc1" {
		states, ok := s["socket_states"].([]interface{})
		if !ok || m.Channel >= len(states) {
			return ""
		}
		v, ok := states[m.Channel].(float64)
		if !ok || (v != 0 && v != 1) {
			return ""
		}
		if v == 1 {
			return "on"
		}
		return "off"
	}
	if m.Channel == 1 {
		on, ok := s["led_visible"].(bool)
		v, vok := s["brightness"].(float64)
		if !ok || !vok || v < 0 || v > 255 {
			return ""
		}
		if !on || v == 0 {
			return "off"
		}
		n := int((v*100 + 127) / 255)
		if n < 1 {
			n = 1
		}
		return fmt.Sprintf("on#%d", n)
	}
	on, ok := s["fan_on"].(bool)
	v, vok := s["percentage"].(float64)
	if !ok || !vok || v < 0 || v > 100 {
		return ""
	}
	if !on || v == 0 {
		return "off"
	}
	best, distance := 1, 101
	for i, speed := range []int{0, 20, 50, 75, 100} {
		if i == 0 {
			continue
		}
		delta := int(v) - speed
		if delta < 0 {
			delta = -delta
		}
		if delta <= distance {
			best = i
			distance = delta
		}
	}
	return fmt.Sprintf("on#%d", best)
}
func (w *bemfaWorker) run() {
	devices := map[string]*bemfaDeviceState{}
	reported := map[string]string{}
	pending := map[string]BemfaMapping{}
	pendingAt := map[string]time.Time{}
	sourceEpoch := w.sourceEpoch.Load()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for _, m := range w.config.Mappings {
		devices[bemfaBase(m.Device)] = &bemfaDeviceState{}
	}
	report := func(base string) {
		if w.client == nil || !w.client.IsConnectionOpen() || w.readyEpoch.Load() != w.epoch.Load() {
			return
		}
		for _, m := range w.config.Mappings {
			if base != "" && bemfaBase(m.Device) != base {
				continue
			}
			s := devices[bemfaBase(m.Device)]
			if !s.online || s.data == nil {
				continue
			}
			wire := bemfaReport(m, s.data)
			if wire == "" || reported[m.Topic] == wire {
				continue
			}
			w.lifecycle.Lock()
			if !w.stopped && w.authorized(m) {
				if waitToken(w.client.Publish(m.Topic+"/up", 0, false, wire)) == nil {
					reported[m.Topic] = wire
				}
			}
			w.lifecycle.Unlock()
		}
	}
	// Probe only on connection/availability and after a command receipt. There is
	// no background status polling, so idle devices do not acquire extra load.
	probe := func(m BemfaMapping) {
		s := devices[bemfaBase(m.Device)]
		if time.Since(s.lastProbe) < time.Second {
			return
		}
		s.lastProbe = time.Now()
		s.probe, _ = w.publishCommand(m, "status.get", map[string]interface{}{})
	}
	// A newly enabled integration may have missed retained availability before
	// its worker existed. Ask the shared connection to replay its subscriptions.
	if b, ok := w.relay.app.broker.(interface{ refreshRelay() }); ok {
		b.refreshRelay()
	}
	for {
		select {
		case <-w.done:
			return
		case <-tick.C:
			for id, at := range pendingAt {
				if time.Since(at) > 10*time.Second {
					delete(pendingAt, id)
					delete(pending, id)
					w.setStatus("device_timeout")
				}
			}
		case e := <-w.events:
			if sourceEpoch != w.sourceEpoch.Load() {
				sourceEpoch = w.sourceEpoch.Load()
				for _, s := range devices {
					s.online = false
					s.data = nil
					s.probe = ""
					s.lastProbe = time.Time{}
				}
				reported = map[string]string{}
				pending = map[string]BemfaMapping{}
				pendingAt = map[string]time.Time{}
			}
			if e.kind == "device" && e.epoch != sourceEpoch {
				continue
			}
			switch e.kind {
			case "reset":
				for _, s := range devices {
					s.online = false
					s.data = nil
					s.probe = ""
				}
				reported = map[string]string{}
				pending = map[string]BemfaMapping{}
				pendingAt = map[string]time.Time{}
			case "connected":
				reported = map[string]string{}
				report("")
			case "command":
				if time.Since(e.at) > 3*time.Second || e.epoch != w.epoch.Load() || e.epoch != w.readyEpoch.Load() || w.client == nil || !w.client.IsConnectionOpen() {
					continue
				}
				for _, m := range w.config.Mappings {
					if m.Topic != e.topic {
						continue
					}
					s := devices[bemfaBase(m.Device)]
					if !s.online || s.data == nil {
						if s.online {
							probe(m)
						}
						w.setStatus("device_offline")
						continue
					}
					action, p, err := bemfaCommand(m, string(e.payload))
					if err != nil {
						continue
					}
					// Only one outstanding actuation per device. Busy requests are
					// dropped, never added to a delayed control queue.
					occupied := false
					for _, active := range pending {
						if bemfaBase(active.Device) == bemfaBase(m.Device) {
							occupied = true
							break
						}
					}
					if occupied || len(pending) >= 32 {
						w.setStatus("busy")
						continue
					}
					id, err := w.publishCommand(m, action, p)
					if err == nil {
						pending[id] = m
						pendingAt[id] = time.Now()
						w.setStatus("connected")
					} else {
						w.setStatus("device_offline")
					}
				}
			case "device":
				cut := strings.LastIndex(e.topic, "/")
				if cut < 0 {
					continue
				}
				base, leaf := e.topic[:cut], e.topic[cut+1:]
				s := devices[base]
				if s == nil {
					continue
				}
				var mapping BemfaMapping
				for _, m := range w.config.Mappings {
					if bemfaBase(m.Device) == base {
						mapping = m
						break
					}
				}
				switch leaf {
				case "availability":
					s.online = string(e.payload) == "online"
					if !s.online {
						s.data = nil
						s.probe = ""
						// Offline invalidates the old probe. A quick reconnect
						// must be able to acquire state immediately, even inside
						// the previous connection's one-second probe limit.
						s.lastProbe = time.Time{}
						for id, m := range pending {
							if bemfaBase(m.Device) == base {
								delete(pending, id)
								delete(pendingAt, id)
							}
						}
					} else {
						probe(mapping)
					}
				case "state":
					if !e.retained && s.online {
						var data map[string]interface{}
						if w.relay.app.store.decodeDevice(mapping.Device, e.payload, &data) == nil {
							s.data = data
						}
					}
				case "result":
					if e.retained {
						continue
					}
					var result struct {
						RequestID string                 `json:"request_id"`
						Code      string                 `json:"code"`
						Data      map[string]interface{} `json:"data"`
					}
					if w.relay.app.store.decodeDevice(mapping.Device, e.payload, &result) != nil {
						continue
					}
					if result.RequestID == s.probe && result.RequestID != "" {
						s.probe = ""
						if result.Code == "ok" && s.online {
							s.data = result.Data
						}
					}
					if m, ok := pending[result.RequestID]; ok && bemfaBase(m.Device) == base {
						delete(pending, result.RequestID)
						delete(pendingAt, result.RequestID)
						if result.Code == "ok" || result.Code == "accepted" {
							s.lastProbe = time.Time{}
							probe(m)
						} else {
							w.setStatus("device_rejected")
						}
					}
				}
				report(base)
			}
		}
	}
}
