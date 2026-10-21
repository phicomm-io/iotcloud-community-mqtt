package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
)

// Credentials are write-only over the API and encrypted using a persistent,
// installation-specific key, independent of account or MQTT passwords.
type BemfaMapping struct {
	Device  Device `json:"device"`
	Channel int    `json:"channel"` // TC1: 0..5; A1: 0 fan, 1 ring light.
	Topic   string `json:"topic"`
}
type BemfaConfig struct {
	Enabled  bool           `json:"enabled"`
	APIKey   string         `json:"api_key,omitempty"`
	Mappings []BemfaMapping `json:"mappings"`
	Revision string         `json:"-"`
}
type BemfaRelay struct {
	routes        map[string][]*bemfaWorker
	app           *Application
	seal          cipher.AEAD
	mu            sync.Mutex
	workers       map[string]*bemfaWorker
	done          chan struct{}
	clientFactory func(*mqtt.ClientOptions) mqtt.Client
}

var bemfaTopic = regexp.MustCompile(`^[A-Za-z0-9_]{1,61}(001|002|003)$`)
var bemfaKey = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)

func openBemfaKey(path string) (cipher.AEAD, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		data = make([]byte, 32)
		if _, err = rand.Read(data); err != nil {
			return nil, err
		}
		f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if os.IsExist(e) {
			data, err = os.ReadFile(path)
		} else if e != nil {
			return nil, e
		} else {
			_, err = f.Write(data)
			if err == nil {
				err = f.Sync()
			}
			closeErr := f.Close()
			if err == nil {
				err = closeErr
			}
		}
	}
	if err != nil {
		return nil, err
	}
	if len(data) != 32 {
		return nil, errors.New("invalid Bemfa encryption key")
	}
	block, err := aes.NewCipher(data)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
func (r *BemfaRelay) readConfig(user string) (BemfaConfig, error) {
	var c BemfaConfig
	var data []byte
	err := r.app.store.QueryRow("SELECT config,revision FROM bemfa_integrations WHERE username=?", user).Scan(&data, &c.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		c.Mappings = []BemfaMapping{}
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if len(data) < r.seal.NonceSize() {
		return c, errors.New("invalid encrypted config")
	}
	plain, err := r.seal.Open(nil, data[:r.seal.NonceSize()], data[r.seal.NonceSize():], []byte(user))
	if err != nil {
		return c, err
	}
	err = json.Unmarshal(plain, &c)
	return c, err
}
func bemfaBase(d Device) string {
	return "phicomm/v2/" + userKey(topicUsername(d)) + "/" + d.Model + "/" + d.MAC
}
func sameBemfaDevice(a, b Device) bool {
	return a.Namespace == b.Namespace && a.MAC == b.MAC && a.Model == b.Model && a.Version == b.Version && topicUsername(a) == topicUsername(b)
}
func (r *BemfaRelay) validate(user string, c *BemfaConfig) error {
	if c.Enabled && c.APIKey == "" {
		return errors.New("请填写巴法云私钥")
	}
	if c.APIKey != "" && !bemfaKey.MatchString(c.APIKey) {
		return errors.New("巴法云私钥格式不正确")
	}
	if len(c.Mappings) > 64 {
		return errors.New("最多添加 64 个主题映射")
	}
	devices, err := r.app.store.devices(user)
	if err != nil {
		return err
	}
	topics, channels := map[string]bool{}, map[string]bool{}
	for i := range c.Mappings {
		m := &c.Mappings[i]
		m.Topic = strings.TrimSpace(m.Topic)
		found := false
		for _, d := range devices {
			if sameBemfaDevice(m.Device, d) && d.Status == "active" && d.Version == "v2" && (d.Model == "tc1" || d.Model == "a1") {
				m.Device = d
				found = true
				break
			}
		}
		if !found {
			return errors.New("只能映射当前账号下已就绪的 TMCP TC1/A1 设备")
		}
		suffix := "001"
		if m.Device.Model == "a1" {
			suffix = "003"
			if m.Channel == 1 {
				suffix = "002"
			}
		}
		if m.Channel < 0 || (m.Device.Model == "tc1" && m.Channel > 5) || (m.Device.Model == "a1" && m.Channel > 1) || !bemfaTopic.MatchString(m.Topic) || !strings.HasSuffix(m.Topic, suffix) {
			return errors.New("通道或主题格式不正确：插座 001、风扇 003、灯 002")
		}
		key := bemfaBase(m.Device) + "/" + string(rune('0'+m.Channel))
		if topics[m.Topic] || channels[key] {
			return errors.New("主题及设备通道不能重复映射")
		}
		topics[m.Topic] = true
		channels[key] = true
	}
	if c.Enabled && len(c.Mappings) == 0 {
		return errors.New("请至少添加一个主题映射")
	}
	// A private key is also the MQTT client ID: sharing it across platform accounts
	// would disconnect the other account. Store only its digest for uniqueness.
	if c.APIKey != "" {
		var owner string
		err = r.app.store.QueryRow("SELECT username FROM bemfa_integrations WHERE key_hash=? AND username<>?", digest(c.APIKey), user).Scan(&owner)
		if err == nil {
			return errors.New("该巴法云私钥已被其他账号使用")
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	return nil
}
func handleBemfa(w http.ResponseWriter, req *http.Request) {
	r := app.bemfa
	if r == nil {
		jsonError(w, "当前节点未启用巴法云接入", 503)
		return
	}
	user := currentUser(req)
	if req.Method == http.MethodDelete {
		app.mu.Lock()
		_, err := app.store.Exec("DELETE FROM bemfa_integrations WHERE username=?", user)
		app.mu.Unlock()
		if err != nil {
			jsonError(w, "清除失败", 500)
			return
		}
		r.reload()
		jsonResp(w, map[string]bool{"ok": true})
		return
	}
	if req.Method != http.MethodGet && req.Method != http.MethodPut {
		jsonError(w, "方法不允许", 405)
		return
	}
	app.mu.Lock()
	c, err := r.readConfig(user)
	if err != nil {
		app.mu.Unlock()
		jsonError(w, "读取巴法云配置失败，请检查节点密钥", 500)
		return
	}
	if req.Method == http.MethodPut {
		var next BemfaConfig
		if err = decode(req, &next); err != nil {
			app.mu.Unlock()
			jsonError(w, err.Error(), 400)
			return
		}
		next.APIKey = strings.TrimSpace(next.APIKey)
		if next.APIKey == "" {
			next.APIKey = c.APIKey
		}
		if err = r.validate(user, &next); err != nil {
			app.mu.Unlock()
			jsonError(w, err.Error(), 400)
			return
		}
		// Config and unique key index are updated atomically.
		var tx *sql.Tx
		tx, err = app.store.Begin()
		if err == nil {
			data, _ := json.Marshal(next)
			nonce := make([]byte, r.seal.NonceSize())
			_, err = rand.Read(nonce)
			if err == nil {
				data = r.seal.Seal(nonce, nonce, data, []byte(user))
				var hash interface{}
				if next.APIKey != "" {
					hash = digest(next.APIKey)
				}
				_, err = tx.Exec("INSERT INTO bemfa_integrations(username,config,revision,key_hash) VALUES(?,?,?,?) ON CONFLICT(username) DO UPDATE SET config=excluded.config,revision=excluded.revision,key_hash=excluded.key_hash", user, data, randomID(), hash)
			}
			if err == nil {
				err = tx.Commit()
			} else {
				_ = tx.Rollback()
			}
		}
		if err != nil {
			app.mu.Unlock()
			jsonError(w, "保存巴法云配置失败", 500)
			return
		}
		c = next
	}
	app.mu.Unlock()
	if req.Method == http.MethodPut {
		r.reload()
	}
	status := r.status(user)
	if c.Enabled && status == "disabled" {
		status = "configuration_error"
	}
	jsonResp(w, map[string]interface{}{"enabled": c.Enabled, "key_set": c.APIKey != "", "mappings": c.Mappings, "status": status})
}
