package main

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

func managerRules() []Rule {
	return []Rule{
		{"publishClientSend", "$CONTROL/dynamic-security/v1", true, 1},
		{"publishClientReceive", "$CONTROL/dynamic-security/v1/response", true, 1},
		{"subscribeLiteral", "$CONTROL/dynamic-security/v1/response", true, 1},
		{"publishClientReceive", "phicomm/v2/+/discovery/+", true, 1},
		{"subscribePattern", "phicomm/v2/+/discovery/+", true, 1},
	}
}

// Encoding is bijective and never introduces MQTT wildcard/path characters.
func userKey(username string) string { return "u_" + hex.EncodeToString([]byte(username)) }
func topicUsername(d Device) string {
	if d.MQTTUsername != "" {
		return d.MQTTUsername
	}
	return d.Username
}
func deviceRole(d Device) string {
	if d.Namespace != "" {
		return "scoped_" + userKey(topicUsername(d)) + "_" + d.MAC
	}
	return "device_" + d.MAC
}
func scopeRules(d Device) []Rule {
	topic := "phicomm/v2/" + userKey(topicUsername(d)) + "/" + d.Model + "/" + d.MAC + "/#"
	return []Rule{{"publishClientSend", topic, true, 1}, {"publishClientReceive", topic, true, 1}, {"subscribePattern", topic, true, 1}}
}
func (a *Application) accountRules(username string) ([]Rule, error) {
	principals, err := a.store.mqttPrincipals(username)
	if err != nil {
		return nil, err
	}
	rules := []Rule{{"subscribeLiteral", "homeassistant/status", true, 1}, {"publishClientReceive", "homeassistant/status", true, 1}}
	for _, principal := range principals {
		for _, topic := range []string{"homeassistant/+/" + userKey(principal) + "/#", "phicomm/v2/" + userKey(principal) + "/discovery/+"} {
			for _, kind := range []string{"publishClientSend", "publishClientReceive", "subscribePattern"} {
				rules = append(rules, Rule{kind, topic, true, 1})
			}
		}
	}
	devices, err := a.store.scopedDevices(username)
	if err != nil {
		return nil, err
	}
	linked := map[string]bool{}
	for _, d := range devices {
		if d.Model == "a1" && (d.Status == "active" || d.Status == "pending") {
			linked[userKey(topicUsername(d))] = true
		}
	}
	for key := range linked {
		for _, kind := range []string{"publishClientSend", "publishClientReceive", "subscribePattern"} {
			rules = append(rules, Rule{kind, "phicomm/linkage/" + key + "/#", true, 1})
		}
	}
	return rules, nil
}
func (a *Application) syncAccount(username string) error {
	role := "account_" + userKey(username)
	rules, err := a.accountRules(username)
	if err != nil {
		return err
	}
	_, err = a.broker.Command("getRole", map[string]interface{}{"rolename": role})
	if notFound(err) {
		_, err = a.broker.Command("createRole", map[string]interface{}{"rolename": role, "acls": rules})
	} else if err == nil {
		_, err = a.broker.Command("modifyRole", map[string]interface{}{"rolename": role, "acls": rules})
	}
	if err != nil {
		return err
	}
	return a.attachRole(username, role)
}

// All MQTT identities of an account share its device roles.
func (a *Application) attachRole(username, role string) error {
	principals, err := a.store.mqttPrincipals(username)
	if err != nil {
		return err
	}
	for _, principal := range principals {
		data, e := a.broker.Command("getClient", map[string]interface{}{"username": principal})
		if notFound(e) && principal != username {
			continue
		}
		if e != nil {
			return e
		}
		if !hasRole(data, role) {
			if _, e = a.broker.Command("addClientRole", map[string]interface{}{"username": principal, "rolename": role}); e != nil {
				return e
			}
		}
	}
	return nil
}

func (a *Application) syncAccounts() error {
	rows, err := a.store.Query("SELECT username FROM users WHERE state='active'")
	if err != nil {
		return err
	}
	var users []string
	for rows.Next() {
		var u string
		if err = rows.Scan(&u); err != nil {
			rows.Close()
			return err
		}
		users = append(users, u)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, u := range users {
		if err = a.syncAccount(u); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) scopedDevices(username string) ([]Device, error) {
	q := "SELECT username,mqtt_username,mac,model,version,status FROM scoped_devices WHERE status!='hidden'"
	args := []interface{}{}
	if username != "" {
		q += " AND username=?"
		args = append(args, username)
	}
	rows, err := s.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Device{}
	for rows.Next() {
		d := Device{Namespace: "account"}
		if err = rows.Scan(&d.Username, &d.MQTTUsername, &d.MAC, &d.Model, &d.Version, &d.Status); err != nil {
			return nil, err
		}
		list = append(list, d)
	}
	return list, rows.Err()
}
func (a *Application) syncScopedDevice(d Device) error {
	role := deviceRole(d)
	if d.Status == "deleting" {
		if err := a.syncAccount(d.Username); err != nil {
			return err
		}
		_, err := a.broker.Command("removeClientRole", map[string]interface{}{"username": d.Username, "rolename": role})
		if err != nil && !notFound(err) {
			return err
		}
		_, err = a.broker.Command("deleteRole", map[string]interface{}{"rolename": role})
		if err != nil && !notFound(err) {
			return err
		}
		_, err = a.store.Exec("UPDATE scoped_devices SET status='hidden' WHERE username=? AND mqtt_username=? AND mac=?", d.Username, topicUsername(d), d.MAC)
		return err
	}
	_, err := a.broker.Command("getRole", map[string]interface{}{"rolename": role})
	if notFound(err) {
		_, err = a.broker.Command("createRole", map[string]interface{}{"rolename": role, "acls": scopeRules(d)})
	} else if err == nil {
		_, err = a.broker.Command("modifyRole", map[string]interface{}{"rolename": role, "acls": scopeRules(d)})
	}
	if err != nil {
		return err
	}
	if err = a.attachRole(d.Username, role); err != nil {
		return err
	}
	if err = a.syncAccount(d.Username); err != nil {
		return err
	}
	_, err = a.store.Exec("UPDATE scoped_devices SET status='active' WHERE username=? AND mqtt_username=? AND mac=?", d.Username, topicUsername(d), d.MAC)
	return err
}
func (a *Application) discover(topic string, payload []byte) error {
	parts := strings.Split(topic, "/")
	if len(parts) != 5 || parts[0] != "phicomm" || parts[1] != "v2" || parts[3] != "discovery" || !strings.HasPrefix(parts[2], "u_") || len(payload) == 0 || len(payload) > 4096 {
		return errors.New("invalid discovery")
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(parts[2], "u_"))
	if err != nil {
		return err
	}
	username := string(decoded)
	if userKey(username) != parts[2] || !usernameRegex.MatchString(username) || reservedUser(username) {
		return errors.New("invalid owner")
	}
	var announcement struct {
		Schema          int    `json:"schema"`
		Model           string `json:"model"`
		Version         string `json:"version"`
		Protocol        string `json:"protocol"`
		ProtocolVersion int    `json:"protocol_version"`
	}
	if err = json.Unmarshal(payload, &announcement); err != nil {
		return err
	}
	if announcement.Schema != 1 || announcement.Protocol != "tmcp" || announcement.ProtocolVersion != 2 || announcement.Version != "v2" {
		return errors.New("unsupported schema")
	}
	d := Device{Namespace: "account", Username: username, MAC: parts[4], Model: announcement.Model, Version: announcement.Version, Status: "pending"}
	if d.Model == "" || parts[4] != normalizeMAC(parts[4]) {
		return errors.New("invalid identity")
	}
	if err = validateDevice(&d); err != nil {
		return err
	}
	if d.Model == "tc1" && d.Version != "v2" {
		return errors.New("scoped TC1 requires v2")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	owner, principal, err := a.store.resolveAccount(username)
	if err != nil || principal != username {
		return errCredentials
	}
	d.Username = owner
	d.MQTTUsername = principal
	state, err := a.store.state(owner)
	if err != nil || state != "active" {
		return errCredentials
	}
	var existing Device
	err = a.store.QueryRow("SELECT model,version,status FROM scoped_devices WHERE username=? AND mqtt_username=? AND mac=?", owner, principal, d.MAC).Scan(&existing.Model, &existing.Version, &existing.Status)
	if err == nil {
		if existing.Status == "hidden" || existing.Status == "deleting" {
			return nil
		}
		if existing.Model != d.Model || existing.Version != d.Version {
			return errConflict
		}
		if existing.Status == "active" {
			return nil
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	} else {
		_, err = a.store.Exec("INSERT INTO scoped_devices(username,mqtt_username,mac,model,version,status) VALUES(?,?,?,?,?,'pending')", owner, principal, d.MAC, d.Model, d.Version)
		if err != nil {
			return err
		}
	}
	return a.syncScopedDevice(d)
}
func handleScopedDevice(w http.ResponseWriter, r *http.Request, mac string) {
	if !isValidMAC(mac) {
		jsonError(w, "MAC 地址格式不正确", 400)
		return
	}
	mac = normalizeMAC(mac)
	handleScopedDeviceFor(w, r, mac, currentUser(r))
}
func handleScopedDeviceFor(w http.ResponseWriter, r *http.Request, mac, u string) {
	app.mu.Lock()
	defer app.mu.Unlock()
	principal := r.URL.Query().Get("mqtt_username")
	if principal == "" {
		principal = u
	}
	d := Device{Namespace: "account", Username: u, MQTTUsername: principal, MAC: mac}
	err := app.store.QueryRow("SELECT model,version,status FROM scoped_devices WHERE username=? AND mqtt_username=? AND mac=?", u, principal, mac).Scan(&d.Model, &d.Version, &d.Status)
	if err != nil {
		operationError(w, err)
		return
	}
	switch r.Method {
	case "DELETE":
		d.Status = "deleting"
		_, err = app.store.Exec("UPDATE scoped_devices SET status='deleting' WHERE username=? AND mqtt_username=? AND mac=?", u, principal, mac)
	case "PUT":
		if d.Status != "hidden" {
			jsonError(w, "自动发现设备型号由固件声明，不能手动修改", 400)
			return
		}
		d.Status = "pending"
		_, err = app.store.Exec("UPDATE scoped_devices SET status='pending' WHERE username=? AND mqtt_username=? AND mac=?", u, principal, mac)
	default:
		jsonError(w, "方法不允许", 405)
		return
	}
	if err == nil {
		err = app.syncScopedDevice(d)
	}
	if err != nil {
		operationError(w, err)
		return
	}
	jsonResp(w, map[string]string{"message": "设备已更新"})
}
