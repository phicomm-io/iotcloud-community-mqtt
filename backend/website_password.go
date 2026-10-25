package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"golang.org/x/crypto/pbkdf2"
	"net/http"
	"strings"
)

func websiteHash(password, salt string) string {
	return hex.EncodeToString(pbkdf2.Key([]byte(password), []byte(salt), 120000, 32, sha256.New))
}
func (a *Application) saveWebsitePassword(user, password string) error {
	salt := randomID()
	_, err := a.store.Exec(`INSERT INTO account_passwords(username,hash) VALUES(?,?) ON CONFLICT(username) DO NOTHING`, user, salt+":"+websiteHash(password, salt))
	return err
}
func (a *Application) authenticateWebsite(user, principal, password string) error {
	var hash string
	err := a.store.QueryRow("SELECT hash FROM account_passwords WHERE username=?", user).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		if err = a.broker.Authenticate(principal, password); err != nil {
			return err
		}
		return a.saveWebsitePassword(user, password)
	}
	if err != nil {
		return err
	}
	parts := strings.Split(hash, ":")
	if len(parts) != 2 || subtle.ConstantTimeCompare([]byte(parts[1]), []byte(websiteHash(password, parts[0]))) != 1 {
		return errCredentials
	}
	return nil
}
func (a *Application) changePassword(user, password string) error {
	if len(password) < 6 || len(password) > 256 {
		return errors.New(messageText("e.447af662b999"))
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	state, err := a.store.state(user)
	if err != nil || state != "active" {
		return sql.ErrNoRows
	}
	tx, err := a.store.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	salt := randomID()
	if _, err = tx.Exec(`INSERT INTO account_passwords(username,hash,mqtt_separate) VALUES(?,?,1) ON CONFLICT(username) DO UPDATE SET hash=excluded.hash,mqtt_separate=1`, user, salt+":"+websiteHash(password, salt)); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM sessions WHERE username=?", user); err != nil {
		return err
	}
	return tx.Commit()
}
func handleMQTTPassword(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "POST") {
		return
	}
	var req struct {
		CurrentPassword string `json:"current_password"`
		Password        string `json:"password"`
	}
	if err := decode(r, &req); err != nil {
		jsonError(w, err.Error(), 400)
		return
	}
	if len(req.Password) < 6 || len(req.Password) > 256 {
		jsonError(w, messageText("e.447af662b999"), 400)
		return
	}
	u := currentUser(r)
	if err := app.authenticateWebsite(u, u, req.CurrentPassword); err != nil {
		operationError(w, err)
		return
	}
	if err := app.changeMQTTPassword(u, req.Password); err != nil {
		operationError(w, err)
		return
	}
	jsonResp(w, map[string]string{"message": messageText("e.14c896de34db")})
}
