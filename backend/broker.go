package main

import (
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/eclipse/paho.mqtt.golang/packets"
	"os"
	"strings"
	"sync"
	"time"
)

var errUnavailable = errors.New("Broker 不可用，操作待同步，请稍后重试")
var errCredentials = errors.New("用户名或密码错误")

type brokerReply struct {
	Command     string          `json:"command"`
	Error       string          `json:"error"`
	Correlation string          `json:"correlationData"`
	Data        json.RawMessage `json:"data"`
}
type brokerFailure struct{ message string }

func (e *brokerFailure) Error() string { return e.message }
func notFound(err error) bool {
	var e *brokerFailure
	return errors.As(err, &e) && strings.Contains(strings.ToLower(e.message), "not found")
}

type Broker interface {
	Command(string, map[string]interface{}) (json.RawMessage, error)
	Authenticate(string, string) error
}
type DynamicBroker struct {
	client    mqtt.Client
	url       string
	tls       *tls.Config
	mu        sync.Mutex
	pending   map[string]chan brokerReply
	discovery func(string, []byte)
}

func randomID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func waitToken(t mqtt.Token) error {
	if !t.WaitTimeout(6 * time.Second) {
		return errUnavailable
	}
	if err := t.Error(); err != nil {
		return fmt.Errorf("%w: %v", errUnavailable, err)
	}
	return nil
}
func newDynamicBroker(url, ca, user, pass string) (*DynamicBroker, error) {
	if !strings.HasPrefix(url, "ssl://") && !strings.HasPrefix(url, "tls://") && !strings.HasPrefix(url, "tcp://127.0.0.1:") {
		return nil, errors.New("broker_url must use verified TLS (loopback TCP allowed for tests)")
	}
	b := &DynamicBroker{url: url, pending: make(map[string]chan brokerReply)}
	if ca != "" {
		data, err := os.ReadFile(ca)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(data) {
			return nil, errors.New("invalid broker CA")
		}
		b.tls = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	}
	opts := mqtt.NewClientOptions().AddBroker(url).SetUsername(user).SetPassword(pass).SetClientID("manager_" + randomID()[:16]).SetConnectTimeout(5 * time.Second).SetAutoReconnect(true).SetConnectRetry(true).SetConnectRetryInterval(time.Second).SetMaxReconnectInterval(2 * time.Second).SetOrderMatters(false)
	if b.tls != nil {
		opts.SetTLSConfig(b.tls)
	}
	opts.OnConnect = func(c mqtt.Client) {
		_ = b.subscribeDiscovery(c)
		if err := waitToken(c.Subscribe("$CONTROL/dynamic-security/v1/response", 1, b.receive)); err != nil {
			c.Disconnect(0)
		}
	}
	b.client = mqtt.NewClient(opts)
	b.client.Connect()
	return b, nil
}
func (b *DynamicBroker) watchDiscovery(handler func(string, []byte)) {
	b.mu.Lock()
	b.discovery = handler
	b.mu.Unlock()
}
func (b *DynamicBroker) subscribeDiscovery(c mqtt.Client) error {
	return waitToken(c.Subscribe("phicomm/v2/+/discovery/+", 1, func(_ mqtt.Client, m mqtt.Message) {
		b.mu.Lock()
		handler := b.discovery
		b.mu.Unlock()
		if handler != nil {
			handler(m.Topic(), append([]byte(nil), m.Payload()...))
		}
	}))
}
func (b *DynamicBroker) Close() { b.client.Disconnect(100) }
func (b *DynamicBroker) receive(c mqtt.Client, m mqtt.Message) {
	var data struct {
		Responses []brokerReply `json:"responses"`
	}
	if json.Unmarshal(m.Payload(), &data) != nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, r := range data.Responses {
		if ch := b.pending[r.Correlation]; ch != nil {
			select {
			case ch <- r:
			default:
			}
		}
	}
}
func (b *DynamicBroker) Command(name string, args map[string]interface{}) (json.RawMessage, error) {
	if !b.client.IsConnectionOpen() {
		return nil, errUnavailable
	}
	// Subscribe before publishing; startup and reconnect cannot race a command response.
	if err := waitToken(b.client.Subscribe("$CONTROL/dynamic-security/v1/response", 1, b.receive)); err != nil {
		return nil, err
	}
	id := randomID()
	ch := make(chan brokerReply, 1)
	b.mu.Lock()
	b.pending[id] = ch
	b.mu.Unlock()
	defer func() { b.mu.Lock(); delete(b.pending, id); b.mu.Unlock() }()
	command := map[string]interface{}{"command": name, "correlationData": id}
	for k, v := range args {
		command[k] = v
	}
	payload, err := json.Marshal(map[string]interface{}{"commands": []interface{}{command}})
	if err != nil {
		return nil, err
	}
	if err = waitToken(b.client.Publish("$CONTROL/dynamic-security/v1", 1, false, payload)); err != nil {
		return nil, err
	}
	select {
	case r := <-ch:
		if r.Error != "" {
			return nil, &brokerFailure{r.Error}
		}
		return r.Data, nil
	case <-time.After(6 * time.Second):
		return nil, errUnavailable
	}
}
func (b *DynamicBroker) Authenticate(user, pass string) error {
	opts := mqtt.NewClientOptions().AddBroker(b.url).SetUsername(user).SetPassword(pass).SetClientID("auth_" + randomID()[:16]).SetConnectTimeout(5 * time.Second).SetAutoReconnect(false)
	if b.tls != nil {
		opts.SetTLSConfig(b.tls)
	}
	c := mqtt.NewClient(opts)
	defer c.Disconnect(0)
	token := c.Connect()
	if !token.WaitTimeout(6 * time.Second) {
		return errUnavailable
	}
	if err := token.Error(); err != nil {
		if errors.Is(err, packets.ErrorRefusedBadUsernameOrPassword) || errors.Is(err, packets.ErrorRefusedNotAuthorised) {
			return errCredentials
		}
		return errUnavailable
	}
	return nil
}

type Rule struct {
	ACLType  string `json:"acltype"`
	Topic    string `json:"topic"`
	Allow    bool   `json:"allow"`
	Priority int    `json:"priority"`
}

func rulesFor(d Device) []Rule {
	var topics []string
	switch {
	case d.Version == "v2":
		topics = []string{"phicomm/v2/" + userKey(topicUsername(d)) + "/" + d.Model + "/" + d.MAC + "/#"}
	case d.Model == "tc1":
		for _, id := range []string{d.MAC, strings.ToUpper(d.MAC), colonMAC(d.MAC), strings.ToUpper(colonMAC(d.MAC))} {
			topics = append(topics, "device/ztc1/"+id+"/#", "homeassistant/+/"+id+"/#")
		}
	}
	rules := []Rule{}
	for _, topic := range topics {
		for _, kind := range []string{"publishClientSend", "publishClientReceive", "subscribePattern"} {
			rules = append(rules, Rule{kind, topic, true, 1})
		}
	}
	if d.Model != "tc1" {
		rules = append(rules, Rule{"subscribeLiteral", "homeassistant/status", true, 1}, Rule{"publishClientReceive", "homeassistant/status", true, 1})
	}
	return rules
}

func hasRole(data json.RawMessage, role string) bool {
	var parsed struct {
		Client struct {
			Roles []struct {
				Role string `json:"rolename"`
			} `json:"roles"`
		} `json:"client"`
	}
	if json.Unmarshal(data, &parsed) != nil {
		return false
	}
	for _, r := range parsed.Client.Roles {
		if r.Role == role {
			return true
		}
	}
	return false
}
