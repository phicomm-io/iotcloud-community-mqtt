package main

import (
	"database/sql"
	"errors"
	"net/http"
	"os"
	"strings"
)

var errLastAdmin = errors.New("不能删除或降级最后一个管理员")

func (s *Store) isAdmin(user string) bool {
	var count int
	err := s.QueryRow("SELECT COUNT(*) FROM user_roles r JOIN users u ON u.username=r.username WHERE r.username=? AND u.state='active' AND r.role='admin'", user).Scan(&count)
	return err == nil && count == 1
}
func (a *Application) canRemoveAdmin(user string) error {
	if !a.store.isAdmin(user) {
		return nil
	}
	var count int
	if err := a.store.QueryRow("SELECT COUNT(*) FROM user_roles r JOIN users u ON u.username=r.username WHERE u.state='active' AND r.role='admin'").Scan(&count); err != nil {
		return err
	}
	if count <= 1 {
		return errLastAdmin
	}
	return nil
}

// Upgrade the former administrator once; never adopt an existing unprivileged username.
func (a *Application) bootstrapAdmin() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	var done string
	err := a.store.QueryRow("SELECT value FROM settings WHERE key='unified_admin_bootstrapped'").Scan(&done)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	user := cfg.AdminUsername
	if !usernameRegex.MatchString(user) || reservedUser(user) {
		return errors.New("invalid bootstrap admin username")
	}
	state, err := a.store.state(user)
	if err == nil && state != "bootstrap" {
		return errors.New("bootstrap admin username already belongs to an account; refusing privilege adoption")
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	rawPassword, err := os.ReadFile(cfg.AdminPasswordFile)
	password := strings.TrimSpace(string(rawPassword))
	if err == nil && (len(password) < 6 || len(password) > 256) {
		err = errors.New("admin password must be 6-256 bytes")
	}
	if err != nil {
		return err
	}
	if state == "" {
		_, lookup := a.broker.Command("getClient", map[string]interface{}{"username": user})
		if lookup == nil {
			return errors.New("bootstrap admin MQTT username already exists")
		}
		if !notFound(lookup) {
			return lookup
		}
		if _, err = a.store.Exec("INSERT INTO users(username,state) VALUES(?,'bootstrap')", user); err != nil {
			return err
		}
	}
	_, lookup := a.broker.Command("getClient", map[string]interface{}{"username": user})
	if notFound(lookup) {
		if _, err = a.broker.Command("createClient", map[string]interface{}{"username": user, "password": password}); err != nil {
			return err
		}
	} else if lookup != nil {
		return lookup
	} else if err = a.broker.Authenticate(user, password); err != nil {
		return err
	}
	if err = a.syncAccount(user); err != nil {
		return err
	}
	tx, err := a.store.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE users SET state='active' WHERE username=?", user); err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT INTO user_roles(username,role) VALUES(?,'admin')", user); err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT INTO settings(key,value) VALUES('unified_admin_bootstrapped','1')"); err != nil {
		return err
	}
	return tx.Commit()
}
func handleUserRole(w http.ResponseWriter, r *http.Request, user string) {
	if !method(w, r, "PUT") {
		return
	}
	var req struct {
		Role string `json:"role"`
	}
	if err := decode(r, &req); err != nil {
		jsonError(w, err.Error(), 400)
		return
	}
	if req.Role != "admin" && req.Role != "user" {
		jsonError(w, "角色必须为 admin 或 user", 400)
		return
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	state, err := app.store.state(user)
	if err != nil || state != "active" {
		jsonError(w, "有效账号不存在", 404)
		return
	}
	if req.Role == "user" {
		if err = app.canRemoveAdmin(user); err == nil {
			_, err = app.store.Exec("DELETE FROM user_roles WHERE username=?", user)
		}
	} else {
		_, err = app.store.Exec("INSERT INTO user_roles(username,role) VALUES(?,'admin') ON CONFLICT(username) DO NOTHING", user)
	}
	if err != nil {
		operationError(w, err)
		return
	}
	jsonResp(w, map[string]bool{"ok": true})
}
func roleTarget(path string) (string, bool) {
	const suffix = "/role"
	if !strings.HasSuffix(path, suffix) {
		return path, false
	}
	return strings.TrimSuffix(path, suffix), true
}
