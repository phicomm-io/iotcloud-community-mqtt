package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Banner content is plain text. It is configured independently on each node.
type Banner struct {
	Audience string `json:"audience" yaml:"audience"`
	Enabled  bool   `json:"enabled" yaml:"enabled"`
	Text     string `json:"text" yaml:"text"`
	URL      string `json:"url" yaml:"url"`
	Label    string `json:"label" yaml:"label"`
}

func (b *Banner) validate() error {
	if b.Audience == "" {
		b.Audience = "before_login"
	}
	if b.Audience != "before_login" && b.Audience != "after_login" {
		return errors.New(messageText("e.e7cd8a7a0397"))
	}
	b.Text, b.URL, b.Label = strings.TrimSpace(b.Text), strings.TrimSpace(b.URL), strings.TrimSpace(b.Label)
	if !utf8.ValidString(b.Text+b.URL+b.Label) || utf8.RuneCountInString(b.Text) > 300 || len(b.URL) > 1000 || utf8.RuneCountInString(b.Label) > 40 || (b.Enabled && b.Text == "") {
		return errors.New(messageText("e.0eb14a5fe05f"))
	}
	if (b.URL == "") != (b.Label == "") {
		return errors.New(messageText("e.bc91627d3b31"))
	}
	if b.URL != "" {
		u, err := url.Parse(b.URL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || strings.ContainsAny(b.URL, "\\ \t\r\n") || strings.IndexFunc(b.URL, unicode.IsControl) >= 0 {
			return errors.New(messageText("e.2acf49559231"))
		}
	}
	return nil
}

func readBanner() (Banner, error) {
	var b Banner
	var raw string
	err := app.store.QueryRow("SELECT value FROM settings WHERE key='platform_banner'").Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		cfgMu.RLock()
		b = cfg.Banner
		cfgMu.RUnlock()
		err = b.validate()
		return b, err
	}
	if err != nil {
		return b, err
	}
	if err = json.Unmarshal([]byte(raw), &b); err != nil {
		return Banner{}, err
	}
	err = b.validate()
	return b, err
}

func handleBanner(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	b, err := readBanner()
	if err != nil {
		operationError(w, err)
		return
	}
	if !b.Enabled {
		b = Banner{}
	} else if b.Audience == "after_login" {
		// Post-login announcements are only disclosed to an active session.
		var user string
		err = app.store.QueryRow("SELECT s.username FROM sessions s JOIN users u ON s.username=u.username WHERE s.token=? AND s.expires>? AND u.state='active'", digest(requestSession(r)), time.Now().Unix()).Scan(&user)
		if errors.Is(err, sql.ErrNoRows) {
			b = Banner{}
		} else if err != nil {
			operationError(w, err)
			return
		}
	}
	jsonResp(w, b)
}

func handleAdminBanner(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b, err := readBanner()
		if err != nil {
			operationError(w, err)
			return
		}
		jsonResp(w, b)
	case http.MethodPut:
		var b Banner
		if err := decode(r, &b); err != nil {
			jsonError(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := b.validate(); err != nil {
			jsonError(w, err.Error(), http.StatusBadRequest)
			return
		}
		raw, err := json.Marshal(b)
		if err == nil {
			_, err = app.store.Exec("INSERT INTO settings(key,value) VALUES('platform_banner',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", string(raw))
		}
		if err != nil {
			operationError(w, err)
			return
		}
		jsonResp(w, b)
	default:
		jsonError(w, messageText("e.4f34574afee4"), http.StatusMethodNotAllowed)
	}
}
