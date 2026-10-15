package main

import (
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var analyticsID = regexp.MustCompile(`^[a-f0-9]{32}$`)
var analyticsPages = map[string]bool{"/": true, "/login": true, "/register": true, "/guest": true, "/admin": true, "/settings": true, "/guest/settings": true}
var analyticsEvents = map[string]bool{"pageview": true, "login": true, "mqtt_connect": true, "device_view": true}

type analyticsEvent struct {
	Visitor  string `json:"visitor"`
	Session  string `json:"session"`
	Event    string `json:"event"`
	Page     string `json:"page"`
	Model    string `json:"model"`
	Referrer string `json:"referrer"`
	Screen   string `json:"screen"`
}
type metricRow struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func analyticsAgent(ua string) (string, string) {
	browser, os := "其他", "其他"
	switch {
	case strings.Contains(ua, "Edg/"):
		browser = "Edge"
	case strings.Contains(ua, "Firefox/"):
		browser = "Firefox"
	case strings.Contains(ua, "Chrome/"):
		browser = "Chrome"
	case strings.Contains(ua, "Safari/"):
		browser = "Safari"
	}
	switch {
	case strings.Contains(ua, "Android"):
		os = "Android"
	case strings.Contains(ua, "iPhone") || strings.Contains(ua, "iPad"):
		os = "iOS"
	case strings.Contains(ua, "Windows"):
		os = "Windows"
	case strings.Contains(ua, "Mac OS"):
		os = "macOS"
	case strings.Contains(ua, "Linux"):
		os = "Linux"
	}
	return browser, os
}
func handleAnalyticsEvent(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "POST") || !sameOrigin(w, r) {
		return
	}
	if !allowAttempt(r, "analytics", 120, time.Minute) {
		jsonError(w, "请求过于频繁", 429)
		return
	}
	var e analyticsEvent
	if decode(r, &e) != nil || !analyticsID.MatchString(e.Visitor) || !analyticsID.MatchString(e.Session) || !analyticsEvents[e.Event] || !analyticsPages[e.Page] {
		jsonError(w, "统计事件格式错误", 400)
		return
	}
	if e.Model != "" && e.Model != "tc1" && e.Model != "m1" && e.Model != "a1" {
		jsonError(w, "设备型号不支持", 400)
		return
	}
	if (e.Event == "device_view") != (e.Model != "") {
		jsonError(w, "统计型号与事件不匹配", 400)
		return
	}
	if e.Screen != "desktop" && e.Screen != "tablet" && e.Screen != "mobile" {
		jsonError(w, "屏幕类型不支持", 400)
		return
	}
	referrer := "直接访问"
	if e.Referrer != "" {
		u, err := url.Parse(e.Referrer)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || len(u.Hostname()) > 253 {
			jsonError(w, "来源格式错误", 400)
			return
		}
		if !strings.EqualFold(u.Host, r.Host) {
			referrer = strings.ToLower(u.Hostname())
		}
	}
	browser, os := analyticsAgent(r.UserAgent())
	now := time.Now().Unix()
	// No account name, MAC, IP, raw URL/query, payload, or full user agent is stored.
	_, err := app.store.Exec(`INSERT INTO analytics_events(ts,visitor,visit,event,page,model,referrer,browser,os,screen) VALUES(?,?,?,?,?,?,?,?,?,?)`, now, digest(e.Visitor), digest(e.Session), e.Event, e.Page, e.Model, referrer, browser, os, e.Screen)
	if err != nil {
		operationError(w, err)
		return
	}
	if _, err = app.store.Exec("DELETE FROM analytics_events WHERE ts<?", now-90*86400); err != nil {
		operationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func analyticsRows(query string, args ...interface{}) ([]metricRow, error) {
	rows, err := app.store.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []metricRow{}
	for rows.Next() {
		var row metricRow
		if err = rows.Scan(&row.Name, &row.Count); err != nil {
			return nil, err
		}
		list = append(list, row)
	}
	return list, rows.Err()
}
func handleAnalytics(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "GET") {
		return
	}
	days := 7
	if value := r.URL.Query().Get("days"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || (n != 1 && n != 7 && n != 30 && n != 90) {
			jsonError(w, "统计范围不支持", 400)
			return
		}
		days = n
	}
	now := time.Now().UTC()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1-days).Unix()
	var views, visitors, visits, active, events int
	err := app.store.QueryRow(`SELECT COALESCE(SUM(event='pageview'),0),COUNT(DISTINCT visitor),COUNT(DISTINCT visit),COUNT(DISTINCT CASE WHEN ts>=? THEN visitor END),COALESCE(SUM(event!='pageview'),0) FROM analytics_events WHERE ts>=?`, now.Unix()-300, start).Scan(&views, &visitors, &visits, &active, &events)
	if err != nil {
		operationError(w, err)
		return
	}
	result := map[string]interface{}{"days": days, "timezone": "UTC", "pageviews": views, "visitors": visitors, "visits": visits, "active": active, "events": events, "retention_days": 90}
	queries := map[string]string{
		"pages":     `SELECT page,COUNT(*) FROM analytics_events WHERE ts>=? AND event='pageview' GROUP BY page ORDER BY COUNT(*) DESC`,
		"models":    `SELECT upper(model),COUNT(*) FROM analytics_events WHERE ts>=? AND event='device_view' GROUP BY model ORDER BY COUNT(*) DESC`,
		"browsers":  `SELECT browser,COUNT(*) FROM analytics_events WHERE ts>=? AND event='pageview' GROUP BY browser ORDER BY COUNT(*) DESC`,
		"systems":   `SELECT os,COUNT(*) FROM analytics_events WHERE ts>=? AND event='pageview' GROUP BY os ORDER BY COUNT(*) DESC`,
		"screens":   `SELECT screen,COUNT(*) FROM analytics_events WHERE ts>=? AND event='pageview' GROUP BY screen ORDER BY COUNT(*) DESC`,
		"referrers": `SELECT referrer,COUNT(*) FROM analytics_events WHERE ts>=? AND event='pageview' GROUP BY referrer ORDER BY COUNT(*) DESC LIMIT 20`,
		"actions":   `SELECT event,COUNT(*) FROM analytics_events WHERE ts>=? AND event!='pageview' GROUP BY event ORDER BY COUNT(*) DESC`,
		"trend":     `SELECT strftime('%Y-%m-%d',ts,'unixepoch'),COUNT(*) FROM analytics_events WHERE ts>=? AND event='pageview' GROUP BY 1 ORDER BY 1`,
	}
	for name, query := range queries {
		rows, e := analyticsRows(query, start)
		if e != nil {
			operationError(w, e)
			return
		}
		result[name] = rows
	}
	models, e := analyticsRows(`SELECT upper(model),COUNT(*) FROM (SELECT model FROM devices WHERE status='active' UNION ALL SELECT model FROM scoped_devices WHERE status='active') GROUP BY model ORDER BY COUNT(*) DESC`)
	if e != nil {
		operationError(w, e)
		return
	}
	result["registered_models"] = models
	jsonResp(w, result)
}
