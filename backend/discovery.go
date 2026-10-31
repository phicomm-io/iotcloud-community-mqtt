package main

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
)

func managerRules() []Rule {
	rules := []Rule{
		{"publishClientSend", "$CONTROL/dynamic-security/v1", true, 1},
		{"publishClientReceive", "$CONTROL/dynamic-security/v1/response", true, 1},
		{"subscribeLiteral", "$CONTROL/dynamic-security/v1/response", true, 1},
		{"publishClientReceive", "phicomm/v2/+/discovery/+", true, 1},
		{"subscribePattern", "phicomm/v2/+/discovery/+", true, 1},
	}
	for _, leaf := range []string{"state", "availability", "result", "capabilities", "metadata"} {
		topic := "phicomm/v2/+/+/+/" + leaf
		rules = append(rules, Rule{"publishClientReceive", topic, true, 1}, Rule{"subscribePattern", topic, true, 1})
	}
	return append(rules, Rule{"publishClientSend", "phicomm/v2/+/+/+/command", true, 1})
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
	rules := []Rule{{"publishClientSend", topic, true, 1}, {"publishClientReceive", topic, true, 1}, {"subscribePattern", topic, true, 1}}
	if d.Model == "tc1" || d.Model == "a1" || d.Model == "m1" || d.Model == "daikin" || d.Model == "s30-v1b" {
		base := "phicomm/v2/" + userKey(topicUsername(d)) + "/sensor/" + d.MAC
		rules = append(rules, Rule{"publishClientSend", base + "/state", true, 1}, Rule{"publishClientSend", base + "/metadata", true, 1})
		if d.Model == "m1" || d.Model == "daikin" || d.Model == "s30-v1b" {
			rules = append(rules, Rule{"publishClientReceive", base + "/command", true, 1}, Rule{"subscribeLiteral", base + "/command", true, 1})
		}
	}
	return rules
}
func (a *Application) accountRules(username string) ([]Rule, error) {
	principals, err := a.store.mqttPrincipals(username)
	if err != nil {
		return nil, err
	}
	// HA subscribes across node IDs. Permit its discovery filters, while the
	// account-scoped publishClientReceive ACLs below still filter every message.
	// Never grant broad receive/send access alongside these subscription rules.
	rules := []Rule{
		{"publishClientSend", "homeassistant/status", true, 1},
		{"subscribeLiteral", "homeassistant/status", true, 1},
		{"publishClientReceive", "homeassistant/status", true, 1},
		{"subscribePattern", "homeassistant/+/+/config", true, 1},
		{"subscribePattern", "homeassistant/+/+/+/config", true, 1},
	}
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
	sensorReaders := map[string]bool{}
	hasM1 := false
	for _, d := range devices {
		if d.Status != "active" && d.Status != "pending" {
			continue
		}
		if d.Model == "a1" {
			key := userKey(topicUsername(d))
			linked[key] = true
			sensorReaders[key] = true
		}
		hasM1 = hasM1 || d.Model == "m1"
	}
	if hasM1 {
		for _, principal := range principals {
			key := userKey(principal)
			sensorReaders[key] = true
			// M1 uses this fixed filter for display-source availability. A SUBACK
			// grants no receive permission: registered device roles still gate
			// every availability message, including retained messages.
			rules = append(rules, Rule{"subscribeLiteral", "phicomm/v2/" + key + "/+/+/availability", true, 1})
		}
	}
	for _, d := range devices {
		if d.Status == "active" || d.Status == "pending" {
			rules = append(rules, Rule{"publishClientSend", "phicomm/v2/" + userKey(topicUsername(d)) + "/address/" + d.MAC, true, 1})
		}
	}
	for key := range sensorReaders {
		base := "phicomm/v2/" + key + "/sensor/"
		if hasM1 {
			// Subscription only: receive ACLs still restrict state/metadata.
			rules = append(rules, Rule{"subscribeLiteral", base + "+/+", true, 1})
		}
		for _, leaf := range []string{"state", "metadata"} {
			rules = append(rules, Rule{"publishClientReceive", base + "+/" + leaf, true, 1}, Rule{"subscribePattern", base + "+/" + leaf, true, 1})
		}
	}
	for key := range linked {
		rules = append(rules, Rule{"publishClientSend", "phicomm/v2/" + key + "/sensor/+/command", true, 1})
		for _, kind := range []string{"publishClientReceive", "subscribePattern"} {
			rules = append(rules, Rule{kind, "phicomm/v2/" + key + "/address/+", true, 1})
		}
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
	q := "SELECT d.username,d.mqtt_username,d.mac,d.model,d.version,d.status,COALESCE(w.format,''),COALESCE(w.wire_version,0),COALESCE(w.firmware_version,''),COALESCE(w.variant,'') FROM scoped_devices d LEFT JOIN device_wire w ON w.username=d.username AND w.mqtt_username=d.mqtt_username AND w.mac=d.mac WHERE d.status!='hidden'"
	args := []interface{}{}
	if username != "" {
		q += " AND d.username=?"
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
		if err = rows.Scan(&d.Username, &d.MQTTUsername, &d.MAC, &d.Model, &d.Version, &d.Status, &d.WireFormat, &d.WireVersion, &d.FirmwareVersion, &d.Variant); err != nil {
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
		if err == nil {
			_, err = a.store.Exec("DELETE FROM device_wire WHERE username=? AND mqtt_username=? AND mac=?", d.Username, topicUsername(d), d.MAC)
		}
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

// Explicit additions can restore hidden devices; retained discovery cannot.
func (a *Application) addDevice(username string, d Device) error {
	// These are observations from discovery, never account-API writable fields.
	d.WireFormat = ""
	d.WireVersion = 0
	d.FirmwareVersion = ""
	d.Variant = ""
	if err := validateDevice(&d); err != nil {
		return err
	}
	if d.Version != "v2" {
		return a.saveDevice(username, d, false)
	}
	d.Username = username
	return a.registerScopedDevice(d, true)
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
		Schema            int      `json:"schema"`
		DeviceID          string   `json:"device_id"`
		FirmwareVersion   string   `json:"firmware_version"`
		Variant           string   `json:"variant"`
		WireVersion       int      `json:"wire_version"`
		WireFormats       []string `json:"wire_formats"`
		DefaultWireFormat string   `json:"default_wire_format"`
		Model             string   `json:"model"`
		Version           string   `json:"version"`
		Protocol          string   `json:"protocol"`
		CommandProtocol   string   `json:"command_protocol"`
		ProtocolVersion   int      `json:"protocol_version"`
	}
	if err = decodeDeviceWire(payload, &announcement); err != nil {
		return err
	}
	protocolOK := announcement.Protocol == "tmcp" || (announcement.Schema == 2 && announcement.Protocol == "openiot" && announcement.CommandProtocol == "tmcp")
	if (announcement.Schema != 1 && announcement.Schema != 2) || !protocolOK || announcement.ProtocolVersion != 2 || announcement.Version != "v2" || (announcement.Model == "s30-v1b" && announcement.Schema != 2) {
		return errors.New("unsupported schema")
	}
	d := Device{Namespace: "account", Username: username, MAC: parts[4], Model: announcement.Model, Version: announcement.Version, Status: "pending", WireFormat: "json"}
	if announcement.Schema == 2 {
		if deviceWireFormat(payload) != "msgpack" || announcement.DeviceID != parts[4] || announcement.WireVersion != 1 || announcement.DefaultWireFormat != "msgpack" || len(announcement.WireFormats) != 1 || announcement.WireFormats[0] != "msgpack" || !validWireIdentity(announcement.FirmwareVersion, 64) || !validWireIdentity(announcement.Variant, 32) {
			return errors.New("invalid wire identity")
		}
		d.WireFormat = "msgpack"
		d.WireVersion = 1
		d.FirmwareVersion = announcement.FirmwareVersion
		d.Variant = announcement.Variant
	} else if deviceWireFormat(payload) != "json" {
		return errors.New("legacy discovery requires JSON")
	}
	if d.Model == "" || parts[4] != normalizeMAC(parts[4]) {
		return errors.New("invalid identity")
	}
	if err = validateDevice(&d); err != nil {
		return err
	}
	if d.Model == "tc1" && d.Version != "v2" {
		return errors.New("scoped TC1 requires v2")
	}
	return a.registerScopedDevice(d, false)
}

func (a *Application) registerScopedDevice(d Device, restoreHidden bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	username := d.Username
	owner, principal, err := a.store.resolveAccount(username)
	if err != nil || principal != username {
		return errCredentials
	}
	d.Username = owner
	d.MQTTUsername = principal
	d.Namespace = "account"
	d.Status = "pending"
	state, err := a.store.state(owner)
	if err != nil || state != "active" {
		return errCredentials
	}
	var existing Device
	err = a.store.QueryRow("SELECT model,version,status FROM scoped_devices WHERE username=? AND mqtt_username=? AND mac=?", owner, principal, d.MAC).Scan(&existing.Model, &existing.Version, &existing.Status)
	if err == nil {
		if !restoreHidden && (existing.Status == "hidden" || existing.Status == "deleting") {
			return nil
		}
		if existing.Model != d.Model || existing.Version != d.Version || existing.Status == "deleting" {
			return errConflict
		}
		// Reannouncements refresh the canonical ACL after server upgrades. An
		// active database row does not imply that its broker role is up to date.
		if existing.Status == "hidden" {
			_, err = a.store.Exec("UPDATE scoped_devices SET status='pending' WHERE username=? AND mqtt_username=? AND mac=?", owner, principal, d.MAC)
			if err != nil {
				return err
			}
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	} else {
		_, err = a.store.Exec("INSERT INTO scoped_devices(username,mqtt_username,mac,model,version,status) VALUES(?,?,?,?,?,'pending')", owner, principal, d.MAC, d.Model, d.Version)
		if err != nil {
			return err
		}
	}
	if d.WireFormat != "" {
		_, err = a.store.Exec("INSERT INTO device_wire(username,mqtt_username,mac,format,wire_version,firmware_version,variant) VALUES(?,?,?,?,?,?,?) ON CONFLICT(username,mqtt_username,mac) DO UPDATE SET format=excluded.format,wire_version=excluded.wire_version,firmware_version=excluded.firmware_version,variant=excluded.variant", owner, principal, d.MAC, d.WireFormat, d.WireVersion, d.FirmwareVersion, d.Variant)
		if err != nil {
			return err
		}
	}
	return a.syncScopedDevice(d)
}
func handleScopedDevice(w http.ResponseWriter, r *http.Request, mac string) {
	if !isValidMAC(mac) {
		jsonError(w, messageText("e.d87efef93be5"), 400)
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
			jsonError(w, messageText("e.69ffa0add155"), 400)
			return
		}
		d.Status = "pending"
		_, err = app.store.Exec("UPDATE scoped_devices SET status='pending' WHERE username=? AND mqtt_username=? AND mac=?", u, principal, mac)
	default:
		jsonError(w, messageText("e.4f34574afee4"), 405)
		return
	}
	if err == nil {
		err = app.syncScopedDevice(d)
	}
	if err != nil {
		operationError(w, err)
		return
	}
	jsonResp(w, map[string]string{"message": messageText("e.0a7684920170")})
}
