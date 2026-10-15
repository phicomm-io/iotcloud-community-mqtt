package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAnalyticsCollectionAndAdminBoundary(t *testing.T) {
	app = testApp(t)
	cfg.PublicURL = "http://example.test"
	h := routes()
	request := func(method, path, body, origin string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://example.test"+path, strings.NewReader(body))
		r.Header.Set("Origin", origin)
		r.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 14) Chrome/120.0")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	payload := `{"visitor":"0123456789abcdef0123456789abcdef","session":"abcdef0123456789abcdef0123456789","event":"pageview","page":"/login","model":"","referrer":"https://search.example/private?password=secret","screen":"mobile"}`
	if w := request("POST", "/api/analytics/event", payload, "https://evil.test", nil); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := request("POST", "/api/analytics/event", payload, cfg.PublicURL, nil); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	var visitor, ref, browser, os string
	if err := app.store.QueryRow("SELECT visitor,referrer,browser,os FROM analytics_events").Scan(&visitor, &ref, &browser, &os); err != nil {
		t.Fatal(err)
	}
	if visitor == "0123456789abcdef0123456789abcdef" || ref != "search.example" || browser != "Chrome" || os != "Android" {
		t.Fatal(visitor, ref, browser, os)
	}
	for _, bad := range []string{strings.Replace(payload, "/login", "/login?password=x", 1), strings.Replace(payload, `"model":""`, `"model":"unsupported"`, 1), strings.Replace(payload, `"event":"pageview"`, `"event":"outlet.set"`, 1), strings.Replace(payload, `"model":""`, `"model":"tc1"`, 1), strings.Replace(payload, `"screen":"mobile"`, `"screen":"mobile","password":"secret"`, 1)} {
		if w := request("POST", "/api/analytics/event", bad, cfg.PublicURL, nil); w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	login := request("POST", "/api/auth/login", `{"username":"alice","password":"secret123"}`, cfg.PublicURL, nil)
	if login.Code != 200 {
		t.Fatal(login.Code, login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	if w := request("GET", "/api/admin/analytics", "", "", nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := request("GET", "/api/admin/analytics", "", "", cookie); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if _, err := app.store.Exec("INSERT INTO user_roles(username,role) VALUES('alice','admin')"); err != nil {
		t.Fatal(err)
	}
	w := request("GET", "/api/admin/analytics?days=7", "", "", cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result struct {
		Pageviews, Visitors, Visits, Active int
		Referrers                           []metricRow
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Pageviews != 1 || result.Visitors != 1 || result.Visits != 1 || result.Active != 1 || len(result.Referrers) != 1 {
		t.Fatal(w.Body.String())
	}
	if w := request("GET", "/api/admin/analytics?days=999", "", "", cookie); w.Code != 400 {
		t.Fatal(w.Code)
	}
}
func TestAnalyticsUniqueVisitsModelsAndRetention(t *testing.T) {
	app = testApp(t)
	cfg.PublicURL = "http://example.test"
	now := time.Now().Unix()
	for _, row := range []struct {
		visitor, visit, event, model string
		ts                           int64
	}{{"v1", "s1", "pageview", "", now}, {"v1", "s1", "pageview", "", now}, {"v1", "s2", "device_view", "tc1", now}, {"v2", "s3", "device_view", "m1", now}, {"expired", "old", "pageview", "", now - 91*86400}} {
		if _, err := app.store.Exec(`INSERT INTO analytics_events(ts,visitor,visit,event,page,model,referrer,browser,os,screen) VALUES(?,?,?,?,? ,?,'直接访问','Chrome','Linux','desktop')`, row.ts, row.visitor, row.visit, row.event, "/", row.model); err != nil {
			t.Fatal(err)
		}
	}
	r := httptest.NewRequest("POST", "http://example.test/api/analytics/event", strings.NewReader(`{"visitor":"0123456789abcdef0123456789abcdef","session":"abcdef0123456789abcdef0123456789","event":"login","page":"/login","model":"","referrer":"","screen":"desktop"}`))
	r.Header.Set("Origin", cfg.PublicURL)
	w := httptest.NewRecorder()
	handleAnalyticsEvent(w, r)
	if w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	var n int
	app.store.QueryRow("SELECT COUNT(*) FROM analytics_events WHERE visitor='expired'").Scan(&n)
	if n != 0 {
		t.Fatal("retention did not remove old rows")
	}
	w = httptest.NewRecorder()
	handleAnalytics(w, httptest.NewRequest("GET", "/api/admin/analytics?days=7", nil))
	var result struct {
		Pageviews, Visitors, Visits, Events int
		Models                              []metricRow
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Pageviews != 2 || result.Visitors != 3 || result.Visits != 4 || result.Events != 3 || len(result.Models) != 2 {
		t.Fatal(w.Body.String())
	}
}
