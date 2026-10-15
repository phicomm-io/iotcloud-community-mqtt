package main

import (
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var shortUsernameRegex = regexp.MustCompile(`^[A-Za-z0-9_.\-]{3,32}$`)
var errAccountName = errors.New("用户名或别名已被使用")
var errInvalidUsername = errors.New("用户名须为 3–32 个字母、数字、下划线、点或连字符，且不能为保留账号")
var errAliasLimit = errors.New("每个账号只支持一个 MQTT 别名，请使用已有别名")

func validateUsername(name string) error {
	if !shortUsernameRegex.MatchString(name) || reservedUser(name) || name == cfg.AdminUsername {
		return errInvalidUsername
	}
	return nil
}

type AccountAlias struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
}

func (s *Store) aliases(user string) ([]AccountAlias, error) {
	rows, err := s.Query("SELECT alias,kind,state FROM account_aliases WHERE username=? ORDER BY alias", user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []AccountAlias{}
	for rows.Next() {
		var a AccountAlias
		if err = rows.Scan(&a.Name, &a.Kind, &a.Status); err != nil {
			return nil, err
		}
		list = append(list, a)
	}
	return list, rows.Err()
}
func (s *Store) resolveAccount(name string) (string, string, error) {
	var owner, kind string
	err := s.QueryRow("SELECT username,'primary' FROM users WHERE username=? UNION ALL SELECT username,kind FROM account_aliases WHERE alias=? AND state='active' LIMIT 1", name, name).Scan(&owner, &kind)
	if err != nil {
		return "", "", err
	}
	principal := name
	if kind == "email" {
		principal = owner
	}
	return owner, principal, nil
}
func (s *Store) nameAvailable(name string) (bool, error) {
	var count int
	err := s.QueryRow("SELECT (SELECT COUNT(*) FROM users WHERE username=?)+(SELECT COUNT(*) FROM account_aliases WHERE alias=?)", name, name).Scan(&count)
	return count == 0, err
}
func (s *Store) mqttPrincipals(user string) ([]string, error) {
	aliases, err := s.aliases(user)
	if err != nil {
		return nil, err
	}
	names := []string{user}
	for _, a := range aliases {
		if a.Kind == "mqtt" && (a.Status == "active" || a.Status == "pending") {
			names = append(names, a.Name)
		}
	}
	return names, nil
}
func (s *Store) accountInfo(user string) (map[string]interface{}, error) {
	aliases, err := s.aliases(user)
	if err != nil {
		return nil, err
	}
	mqttUser := user
	for _, a := range aliases {
		if a.Kind == "mqtt" && a.Status == "active" {
			mqttUser = a.Name
			break
		}
	}
	return map[string]interface{}{"username": user, "mqtt_username": mqttUser, "is_admin": s.isAdmin(user), "aliases": aliases}, nil
}
func (a *Application) addMQTTAlias(user, name, password string) error {
	if err := validateUsername(name); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var aliasCount int
	if err := a.store.QueryRow("SELECT COUNT(*) FROM account_aliases WHERE username=? AND kind='mqtt' AND alias!=?", user, name).Scan(&aliasCount); err != nil {
		return err
	}
	if aliasCount > 0 {
		return errAliasLimit
	}
	state, err := a.store.state(user)
	if err != nil || state != "active" {
		return errCredentials
	}
	if err = a.broker.Authenticate(user, password); err != nil {
		return err
	}
	var owner, kind string
	err = a.store.QueryRow("SELECT username,kind FROM account_aliases WHERE alias=?", name).Scan(&owner, &kind)
	if err == nil && (owner != user || kind != "mqtt") {
		return errAccountName
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if errors.Is(err, sql.ErrNoRows) {
		available, e := a.store.nameAvailable(name)
		if e != nil {
			return e
		}
		if !available {
			return errAccountName
		}
		// Reserve the name before a Broker mutation so retries cannot adopt another user's client.
		_, e = a.broker.Command("getClient", map[string]interface{}{"username": name})
		if e == nil {
			return errAccountName
		}
		if !notFound(e) {
			return e
		}
		_, err = a.store.Exec("INSERT INTO account_aliases(alias,username,kind,state) VALUES(?,?,'mqtt','pending')", name, user)
		if err != nil {
			return err
		}
	}
	_, err = a.broker.Command("getClient", map[string]interface{}{"username": name})
	if notFound(err) {
		_, err = a.broker.Command("createClient", map[string]interface{}{"username": name, "password": password})
	} else if err == nil {
		_, err = a.broker.Command("setClientPassword", map[string]interface{}{"username": name, "password": password})
	}
	if err != nil {
		return err
	}
	// Password recovery is explicit; the periodic reconciler never enables an alias with a stale password.
	if _, err = a.store.Exec("UPDATE account_aliases SET state='pending' WHERE alias=?", name); err != nil {
		return err
	}
	if err = a.syncAccount(user); err != nil {
		return err
	}
	devices, err := a.store.devices(user)
	if err != nil {
		return err
	}
	for _, d := range devices {
		if d.Status == "active" {
			if err = a.attachRole(user, deviceRole(d)); err != nil {
				return err
			}
		}
	}
	if _, err = a.broker.Command("enableClient", map[string]interface{}{"username": name}); err != nil {
		return err
	}
	_, err = a.store.Exec("UPDATE account_aliases SET state='active' WHERE alias=?", name)
	return err
}
func handleAliases(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "PUT") {
		return
	}
	var req struct {
		Alias    string `json:"alias"`
		Password string `json:"password"`
	}
	if err := decode(r, &req); err != nil {
		jsonError(w, err.Error(), 400)
		return
	}
	if !allowAttempt(r, "alias", 10, time.Minute) {
		jsonError(w, "请求过于频繁", 429)
		return
	}
	if err := app.addMQTTAlias(currentUser(r), strings.TrimSpace(req.Alias), req.Password); err != nil {
		if errors.Is(err, errInvalidUsername) || errors.Is(err, errAliasLimit) {
			jsonError(w, err.Error(), 400)
		} else {
			operationError(w, err)
		}
		return
	}
	info, err := app.store.accountInfo(currentUser(r))
	if err != nil {
		operationError(w, err)
		return
	}
	jsonResp(w, info)
}

func (s *Store) migrateScopedIdentities() error {
	rows, err := s.Query("PRAGMA table_info(scoped_devices)")
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var value interface{}
		if err = rows.Scan(&cid, &name, &typ, &notnull, &value, &pk); err != nil {
			rows.Close()
			return err
		}
		found = found || name == "mqtt_username"
	}
	err = rows.Err()
	rows.Close()
	if err != nil || found {
		return err
	}
	tx, err := s.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`CREATE TABLE scoped_devices_v2(username TEXT NOT NULL REFERENCES users(username) ON DELETE CASCADE,mqtt_username TEXT NOT NULL,mac TEXT NOT NULL,model TEXT NOT NULL,version TEXT NOT NULL,status TEXT NOT NULL,PRIMARY KEY(username,mqtt_username,mac));
 INSERT INTO scoped_devices_v2 SELECT username,username,mac,model,version,status FROM scoped_devices;
 DROP TABLE scoped_devices;
 ALTER TABLE scoped_devices_v2 RENAME TO scoped_devices;`)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// Reserve the verified email alongside the username before creating the Broker client.
func (a *Application) registerWithEmail(user, password, email string, verified bool) error {
	if verified {
		a.mu.Lock()
		var owner, kind string
		err := a.store.QueryRow("SELECT username,kind FROM account_aliases WHERE alias=?", email).Scan(&owner, &kind)
		if err == nil && (owner != user || kind != "email") {
			a.mu.Unlock()
			return errAccountName
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			a.mu.Unlock()
			return err
		}
		if errors.Is(err, sql.ErrNoRows) {
			available, e := a.store.nameAvailable(email)
			if e != nil || !available {
				a.mu.Unlock()
				if e != nil {
					return e
				}
				return errAccountName
			}
			available, e = a.store.nameAvailable(user)
			if e != nil || !available {
				a.mu.Unlock()
				if e != nil {
					return e
				}
				return errAccountName
			}
			tx, e := a.store.Begin()
			if e != nil {
				a.mu.Unlock()
				return e
			}
			_, e = tx.Exec("INSERT INTO users(username,state) VALUES(?,'pending')", user)
			if e == nil {
				_, e = tx.Exec("INSERT INTO account_aliases(alias,username,kind,state) VALUES(?,?,'email','active')", email, user)
			}
			if e == nil {
				e = tx.Commit()
			} else {
				tx.Rollback()
			}
			if e != nil {
				a.mu.Unlock()
				return e
			}
		}
		a.mu.Unlock()
	}
	return a.register(user, password)
}
