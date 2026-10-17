package main

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func seedVerification(t *testing.T, s *Store, email, code string) int64 {
	t.Helper()
	id, e := s.beginVerification(email, code)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.finishVerificationSend(id, email, true); e != nil {
		t.Fatal(e)
	}
	return id
}
func TestVerificationDurableLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codes.db")
	s, e := openStore(path)
	if e != nil {
		t.Fatal(e)
	}
	first := seedVerification(t, s, "one@example.com", "123456")
	s.Close()
	s, e = openStore(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	failed, _ := s.beginVerification("one@example.com", "999999")
	if e = s.finishVerificationSend(failed, "one@example.com", false); e != nil {
		t.Fatal(e)
	}
	var saved string
	s.QueryRow("SELECT code FROM verification_emails WHERE id=?", first).Scan(&saved)
	if saved != "123456" {
		t.Fatal("failed resend invalidated valid code")
	}
	second := seedVerification(t, s, "one@example.com", "654321")
	for _, code := range []string{"123456", "111111"} {
		if id, e := s.consumeVerification("one@example.com", code); e != nil || id != 0 {
			t.Fatal("old/wrong code", id, e)
		}
	}
	if id, e := s.consumeVerification("one@example.com", "654321"); e != nil || id != second {
		t.Fatal(id, e)
	}
	if id, e := s.consumeVerification("one@example.com", "654321"); e != nil || id != 0 {
		t.Fatal("replay", id, e)
	}
	if e = s.finishVerificationRegistration(second, "new_user", true); e != nil {
		t.Fatal(e)
	}
	var status string
	s.QueryRow("SELECT status,code FROM verification_emails WHERE id=?", second).Scan(&status, &saved)
	if status != "registered" || saved != "" {
		t.Fatal(status, saved)
	}
	expired := seedVerification(t, s, "old@example.com", "222222")
	s.Exec("UPDATE verification_emails SET expires_at=? WHERE id=?", time.Now().Unix()-1, expired)
	if id, e := s.consumeVerification("old@example.com", "222222"); e != nil || id != 0 {
		t.Fatal("expired", id, e)
	}
	old, _ := s.beginVerification("race@example.com", "333333")
	newID := seedVerification(t, s, "race@example.com", "444444")
	s.finishVerificationSend(old, "race@example.com", true)
	if id, _ := s.consumeVerification("race@example.com", "333333"); id != 0 {
		t.Fatal("late old send accepted")
	}
	if id, _ := s.consumeVerification("race@example.com", "444444"); id != newID {
		t.Fatal("latest lost")
	}
}
func TestVerificationHTTPAndAdminRecords(t *testing.T) {
	oldCfg, oldApp, oldSend := cfg, app, deliverVerificationEmail
	t.Cleanup(func() { cfg, app, deliverVerificationEmail = oldCfg, oldApp, oldSend })
	app = testApp(t)
	cfg.AdminUsername = "admin"
	cfg.PublicURL = "http://example.test"
	cfg.RegisterLevel = 2
	rateMu.Lock()
	clear(rates)
	rateMu.Unlock()
	call := func(path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", cfg.PublicURL+path, strings.NewReader(body))
		r.Header.Set("Origin", cfg.PublicURL)
		w := httptest.NewRecorder()
		routes().ServeHTTP(w, r)
		return w
	}
	var delivered string
	deliverVerificationEmail = func(to, code string) error { delivered = code; return nil }
	if w := call("/api/register/verify", `{"email":"New@Example.com"}`); w.Code != 200 || strings.Contains(w.Body.String(), delivered) {
		t.Fatal(w.Code, w.Body)
	}
	body := `{"username":"new_user","email":"new@example.com","password":"secret123","code":"` + delivered + `"}`
	if w := call("/api/register", body); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if w := call("/api/register", body); w.Code != 400 {
		t.Fatal("replay", w.Code, w.Body)
	}
	deliverVerificationEmail = func(to, code string) error { return errors.New("private smtp detail") }
	if w := call("/api/register/verify", `{"email":"fail@example.com"}`); w.Code != 503 {
		t.Fatal(w.Code, w.Body)
	}
	seedVerification(t, app.store, "pending@example.com", "112200")
	seedVerification(t, app.store, "pending@example.com", "112233")
	expired := seedVerification(t, app.store, "expired@example.com", "223344")
	app.store.Exec("UPDATE verification_emails SET expires_at=? WHERE id=?", time.Now().Unix()-1, expired)
	broken := seedVerification(t, app.store, "broken@example.com", "334455")
	_ = broken
	app.broker.(*fakeBroker).fail = true
	if w := call("/api/register", `{"username":"broken_user","email":"broken@example.com","password":"secret123","code":"334455"}`); w.Code != 503 {
		t.Fatal(w.Code, w.Body)
	}
	app.broker.(*fakeBroker).fail = false
	w := httptest.NewRecorder()
	handleVerificationEmails(w, httptest.NewRequest("GET", "/api/admin/verification-emails", nil))
	var data struct {
		Records []verificationEmail `json:"records"`
		Total   int                 `json:"total"`
		Summary map[string]int      `json:"summary"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &data); e != nil {
		t.Fatal(e, w.Body)
	}
	if w.Code != 200 || data.Total != 6 || data.Summary["sent_emails"] != 4 || data.Summary["registered_emails"] != 1 || data.Summary["unregistered_emails"] != 3 || data.Summary["failed_attempts"] != 1 {
		t.Fatal(w.Code, w.Body)
	}
	for _, v := range data.Records {
		if v.Status != "sent" && v.Code != "" {
			t.Fatal("inactive code leaked")
		}
		if v.Email == "pending@example.com" && v.Status == "sent" && v.Code != "112233" {
			t.Fatal("manual code absent")
		}
	}
	if strings.Contains(w.Body.String(), "private smtp") || strings.Contains(w.Body.String(), "223344") {
		t.Fatal("secret leaked")
	}
	w = httptest.NewRecorder()
	handleVerificationEmails(w, httptest.NewRequest("GET", "/api/admin/verification-emails?status=expired&email=EXPIRED", nil))
	if !strings.Contains(w.Body.String(), `"total":1`) {
		t.Fatal(w.Body)
	}
	w = httptest.NewRecorder()
	handleVerificationEmails(w, httptest.NewRequest("GET", "/api/admin/verification-emails?page=2", nil))
	if !strings.Contains(w.Body.String(), `"records":[]`) {
		t.Fatal(w.Body)
	}
	cfg.RegisterLevel = 1
	if w := call("/api/register/verify", `{"email":"disabled@example.com"}`); w.Code != 400 {
		t.Fatal(w.Code)
	}
}

func TestVerificationRetentionAndInterruptedSend(t *testing.T) {
	a := testApp(t)
	s := a.store
	now := time.Now().Unix()
	id := seedVerification(t, s, "expired@example.com", "123456")
	s.Exec("UPDATE verification_emails SET expires_at=? WHERE id=?", now-1, id)
	interrupted, _ := s.beginVerification("interrupted@example.com", "654321")
	s.Exec("UPDATE verification_emails SET created_at=? WHERE id=?", now-61, interrupted)
	ancient, _ := s.beginVerification("ancient@example.com", "111111")
	s.Exec("UPDATE verification_emails SET created_at=? WHERE id=?", now-91*86400, ancient)
	if err := s.cleanVerificationHistory(now); err != nil {
		t.Fatal(err)
	}
	var code, status string
	if err := s.QueryRow("SELECT code FROM verification_emails WHERE id=?", id).Scan(&code); err != nil || code != "" {
		t.Fatal("expired secret retained", err)
	}
	if err := s.QueryRow("SELECT status,code FROM verification_emails WHERE id=?", interrupted).Scan(&status, &code); err != nil || status != "unknown" || code != "" {
		t.Fatal(status, err)
	}
	var count int
	s.QueryRow("SELECT COUNT(*) FROM verification_emails WHERE id=?", ancient).Scan(&count)
	if count != 0 {
		t.Fatal("old history retained")
	}
}
