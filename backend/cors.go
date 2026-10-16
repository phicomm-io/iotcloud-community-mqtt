package main

import (
	"net/http"
	"net/url"
	"strings"
)

// Only explicit origins can read credentials or perform cross-origin writes.
func allowedFrontend(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || origin == "" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	for _, allowed := range cfg.AllowedOrigins {
		if origin == allowed {
			return true
		}
	}
	return false
}
func requestSession(r *http.Request) string {
	if header := r.Header.Get("Authorization"); strings.HasPrefix(header, "Bearer ") {
		return strings.TrimPrefix(header, "Bearer ")
	}
	if cookie, err := r.Cookie("phicomm_session"); err == nil {
		return cookie.Value
	}
	return ""
}
