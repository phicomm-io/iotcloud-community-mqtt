package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Banner content is plain text. It is configured independently on each node.
type Banner struct {
	Enabled bool   `json:"enabled" yaml:"enabled"`
	Text    string `json:"text" yaml:"text"`
	URL     string `json:"url" yaml:"url"`
	Label   string `json:"label" yaml:"label"`
}

func (b *Banner) validate() error {
	b.Text, b.URL, b.Label = strings.TrimSpace(b.Text), strings.TrimSpace(b.URL), strings.TrimSpace(b.Label)
	if !utf8.ValidString(b.Text+b.URL+b.Label) || utf8.RuneCountInString(b.Text) > 300 || len(b.URL) > 1000 || utf8.RuneCountInString(b.Label) > 40 || (b.Enabled && b.Text == "") {
		return errors.New("请填写公告正文（最多 300 字），按钮文字最多 40 字")
	}
	if (b.URL == "") != (b.Label == "") {
		return errors.New("链接地址和按钮文字需要同时填写，或同时留空")
	}
	if b.URL != "" {
		u, err := url.Parse(b.URL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || strings.ContainsAny(b.URL, "\\ \t\r\n") || strings.IndexFunc(b.URL, unicode.IsControl) >= 0 {
			return errors.New("链接需要使用完整的 HTTP 或 HTTPS 地址")
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
		return b, b.validate()
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
		jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
	}
}
