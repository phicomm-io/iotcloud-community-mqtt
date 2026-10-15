package main

import (
	"database/sql"
	"errors"
	"fmt"
	_ "modernc.org/sqlite"
	"regexp"
	"strings"
	"sync"
)

var macRegex = regexp.MustCompile(`^([0-9A-Fa-f]{12}|([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}|([0-9A-Fa-f]{2}-){5}[0-9A-Fa-f]{2})$`)
var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
var usernameRegex = regexp.MustCompile(`^[A-Za-z0-9_.@+\-]{3,254}$`)

func normalizeMAC(mac string) string {
	return strings.ToLower(strings.NewReplacer(":", "", "-", "").Replace(strings.TrimSpace(mac)))
}
func colonMAC(mac string) string {
	var b strings.Builder
	for i := 0; i < len(mac); i += 2 {
		if i > 0 {
			b.WriteByte(':')
		}
		b.WriteString(mac[i : i+2])
	}
	return b.String()
}
func isValidMAC(mac string) bool { return macRegex.MatchString(strings.TrimSpace(mac)) }

type Device struct {
	Namespace    string `json:"namespace,omitempty"`
	MAC          string `json:"mac"`
	Model        string `json:"model"`
	Version      string `json:"version"`
	Status       string `json:"status"`
	Username     string `json:"username,omitempty"`
	MQTTUsername string `json:"mqtt_username,omitempty"`
}

func validateDevice(d *Device) error {
	if !isValidMAC(d.MAC) {
		return errors.New("MAC 地址格式不正确")
	}
	d.MAC = normalizeMAC(d.MAC)
	if d.Model == "" {
		d.Model = "tc1"
	}
	switch d.Model {
	case "tc1":
		if d.Version == "" {
			d.Version = "v2"
		}
		if d.Version != "v1" && d.Version != "v2" {
			return errors.New("TC1 协议必须为 v1/v2")
		}
	case "m1", "a1":
		if d.Version == "" {
			d.Version = "v2"
		}
		if d.Version != "v2" {
			return errors.New("M1/A1 仅支持 TMCP")
		}
	default:
		return errors.New("设备型号不支持")
	}
	return nil
}

type Store struct{ *sql.DB }

func openStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db}
	_, err = db.Exec(`PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000; PRAGMA journal_mode=WAL;
 CREATE TABLE IF NOT EXISTS users(username TEXT PRIMARY KEY,state TEXT NOT NULL DEFAULT 'active');
 CREATE TABLE IF NOT EXISTS user_roles(username TEXT PRIMARY KEY REFERENCES users(username) ON DELETE CASCADE,role TEXT NOT NULL CHECK(role='admin'));
 CREATE TABLE IF NOT EXISTS devices(mac TEXT PRIMARY KEY,username TEXT NOT NULL REFERENCES users(username) ON DELETE CASCADE,model TEXT NOT NULL,version TEXT NOT NULL,status TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS sessions(token TEXT PRIMARY KEY,username TEXT NOT NULL REFERENCES users(username) ON DELETE CASCADE,expires INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS scoped_devices(username TEXT NOT NULL REFERENCES users(username) ON DELETE CASCADE,mac TEXT NOT NULL,model TEXT NOT NULL,version TEXT NOT NULL,status TEXT NOT NULL,PRIMARY KEY(username,mac));
 CREATE TABLE IF NOT EXISTS account_aliases(alias TEXT PRIMARY KEY,username TEXT NOT NULL REFERENCES users(username) ON DELETE CASCADE,kind TEXT NOT NULL CHECK(kind IN ('mqtt','email')),state TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS settings(key TEXT PRIMARY KEY,value TEXT NOT NULL);`)
	if err != nil {
		db.Close()
		return nil, err
	}
	if err = s.migrateScopedIdentities(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) state(username string) (string, error) {
	var state string
	err := s.QueryRow("SELECT state FROM users WHERE username=?", username).Scan(&state)
	return state, err
}
func (s *Store) devices(username string) ([]Device, error) {
	q := "SELECT mac,username,model,version,status FROM devices"
	args := []interface{}{}
	if username != "" {
		q += " WHERE username=?"
		args = append(args, username)
	}
	q += " ORDER BY mac"
	rows, err := s.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Device{}
	for rows.Next() {
		var d Device
		if err = rows.Scan(&d.MAC, &d.Username, &d.Model, &d.Version, &d.Status); err != nil {
			return nil, err
		}
		list = append(list, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	scoped, err := s.scopedDevices(username)
	return append(list, scoped...), err
}
func (s *Store) device(mac string) (Device, error) {
	var d Device
	err := s.QueryRow("SELECT mac,username,model,version,status FROM devices WHERE mac=?", mac).Scan(&d.MAC, &d.Username, &d.Model, &d.Version, &d.Status)
	return d, err
}

type Application struct {
	store  *Store
	broker Broker
	mu     sync.Mutex
}

func (a *Application) syncDevice(d Device) error {
	if d.Namespace != "" {
		return a.syncScopedDevice(d)
	}
	role := deviceRole(d)
	if d.Status == "deleting" {
		_, err := a.broker.Command("removeClientRole", map[string]interface{}{"username": d.Username, "rolename": role})
		if err != nil && !notFound(err) {
			return err
		}
		_, err = a.broker.Command("deleteRole", map[string]interface{}{"rolename": role})
		if err != nil && !notFound(err) {
			return err
		}
		_, err = a.store.Exec("DELETE FROM devices WHERE mac=?", d.MAC)
		return err
	}
	_, err := a.broker.Command("getRole", map[string]interface{}{"rolename": role})
	if notFound(err) {
		_, err = a.broker.Command("createRole", map[string]interface{}{"rolename": role, "acls": rulesFor(d)})
	} else if err == nil {
		_, err = a.broker.Command("modifyRole", map[string]interface{}{"rolename": role, "acls": rulesFor(d)})
	}
	if err != nil {
		return err
	}
	if err = a.attachRole(d.Username, role); err != nil {
		return err
	}
	_, err = a.store.Exec("UPDATE devices SET status='active' WHERE mac=?", d.MAC)
	return err
}
func (a *Application) saveDevice(username string, d Device, update bool) error {
	if d.Namespace != "" || d.MQTTUsername != "" {
		return errors.New("账号主题只能通过 MQTT 声明自动发现")
	}
	if err := validateDevice(&d); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	state, err := a.store.state(username)
	if err != nil || state != "active" {
		return errCredentials
	}
	existing, err := a.store.device(d.MAC)
	if update {
		if err != nil || existing.Username != username {
			return sql.ErrNoRows
		}
		if existing.Status == "deleting" {
			return errors.New("设备正在解绑，请稍后重试")
		}
		_, err = a.store.Exec("UPDATE devices SET model=?,version=?,status='pending' WHERE mac=?", d.Model, d.Version, d.MAC)
	} else {
		if err == nil {
			if existing.Username == username && existing.Status != "deleting" && existing.Model == d.Model && existing.Version == d.Version {
				return a.syncDevice(existing)
			}
			return errConflict
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		_, err = a.store.Exec("INSERT INTO devices(mac,username,model,version,status) VALUES(?,?,?,?,'pending')", d.MAC, username, d.Model, d.Version)
	}
	if err != nil {
		return err
	}
	d.Username = username
	d.Status = "pending"
	return a.syncDevice(d)
}

var errConflict = errors.New("该 MAC 或账号已被绑定")

func (a *Application) removeDevice(username, mac string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	d, err := a.store.device(mac)
	if err != nil || d.Username != username {
		return sql.ErrNoRows
	}
	if _, err = a.store.Exec("UPDATE devices SET status='deleting' WHERE mac=?", mac); err != nil {
		return err
	}
	d.Status = "deleting"
	return a.syncDevice(d)
}
func (a *Application) deleteUser(username string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, err := a.store.state(username); err != nil {
		return err
	}
	if err := a.canRemoveAdmin(username); err != nil {
		return err
	}
	if _, err := a.store.Exec("UPDATE users SET state='deleting' WHERE username=?", username); err != nil {
		return err
	}
	a.store.Exec("DELETE FROM sessions WHERE username=?", username)
	return a.syncDeleteUser(username)
}
func (a *Application) syncDeleteUser(username string) error {
	aliases, err := a.store.aliases(username)
	if err != nil {
		return err
	}
	for _, alias := range aliases {
		if alias.Kind == "mqtt" {
			_, e := a.broker.Command("deleteClient", map[string]interface{}{"username": alias.Name})
			if e != nil && !notFound(e) {
				return e
			}
		}
	}
	_, err = a.broker.Command("deleteClient", map[string]interface{}{"username": username})
	if err != nil && !notFound(err) {
		return err
	}
	devices, err := a.store.devices(username)
	if err != nil {
		return err
	}
	for _, d := range devices {
		_, err = a.broker.Command("deleteRole", map[string]interface{}{"rolename": deviceRole(d)})
		if err != nil && !notFound(err) {
			return err
		}
	}
	_, err = a.broker.Command("deleteRole", map[string]interface{}{"rolename": "account_" + userKey(username)})
	if err != nil && !notFound(err) {
		return err
	}
	_, err = a.store.Exec("DELETE FROM users WHERE username=?", username)
	return err
}
func (a *Application) reconcile() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	rows, err := a.store.Query("SELECT username FROM users WHERE state='deleting'")
	if err != nil {
		return err
	}
	var usernames []string
	for rows.Next() {
		var u string
		if err = rows.Scan(&u); err != nil {
			rows.Close()
			return err
		}
		usernames = append(usernames, u)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, u := range usernames {
		if err = a.syncDeleteUser(u); err != nil {
			return err
		}
	}
	devices, err := a.store.devices("")
	if err != nil {
		return err
	}
	if err = a.syncAccounts(); err != nil {
		return err
	}
	for _, d := range devices {
		if d.Status != "active" {
			if err = a.syncDevice(d); err != nil {
				return fmt.Errorf("device %s: %w", d.MAC, err)
			}
		}
	}
	return nil
}
