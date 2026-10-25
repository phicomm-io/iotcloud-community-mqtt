package main

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Replaceable for isolated tests: never send real email during regression tests.
var deliverVerificationEmail = sendVerificationEmail

func (s *Store) cleanVerificationHistory(now int64) error {
	tx, err := s.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("DELETE FROM verification_emails WHERE created_at < ?", now-90*86400); err != nil {
		return err
	}
	if _, err = tx.Exec("UPDATE verification_emails SET code='' WHERE expires_at>0 AND expires_at<=?", now); err != nil {
		return err
	}
	if _, err = tx.Exec("UPDATE verification_emails SET status='unknown',code='',detail=? WHERE status='sending' AND created_at<?", "verification.interrupted", now-60); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) beginVerification(email, code string) (int64, error) {
	now := time.Now().Unix()
	if err := s.cleanVerificationHistory(now); err != nil {
		return 0, err
	}
	result, err := s.Exec("INSERT INTO verification_emails(email,code,created_at,status) VALUES(?,?,?,'sending')", email, code, now)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}
func (s *Store) finishVerificationSend(id int64, email string, success bool) error {
	if !success {
		_, err := s.Exec("UPDATE verification_emails SET status='failed',code='',detail=? WHERE id=? AND status='sending'", "verification.delivery_failed", id)
		return err
	}
	now := time.Now().Unix()
	tx, err := s.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec("UPDATE verification_emails SET status='superseded',code='' WHERE email=? AND status='sent' AND id<? AND expires_at>?", email, id, now)
	if err != nil {
		return err
	}
	// A delayed older delivery cannot replace a newer, successfully sent code.
	_, err = tx.Exec(`UPDATE verification_emails SET sent_at=?,expires_at=?,status=CASE WHEN EXISTS(SELECT 1 FROM verification_emails WHERE email=? AND sent_at>0 AND id>?) THEN 'superseded' ELSE 'sent' END WHERE id=? AND status='sending'`, now, now+300, email, id, id)
	if err != nil {
		return err
	}
	_, err = tx.Exec("UPDATE verification_emails SET code='' WHERE id=? AND status='superseded'", id)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) consumeVerification(email, code string) (int64, error) {
	if len(code) != 6 {
		return 0, nil
	}
	tx, err := s.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var id int64
	err = tx.QueryRow("SELECT id FROM verification_emails WHERE email=? AND code=? AND status='sent' AND expires_at>? ORDER BY id DESC LIMIT 1", email, code, time.Now().Unix()).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	_, err = tx.Exec("UPDATE verification_emails SET status='verified',code='' WHERE id=?", id)
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}
func (s *Store) finishVerificationRegistration(id int64, username string, success bool) error {
	status, detail, registeredAt := "registration_failed", "e.66ba1c71fe55", int64(0)
	if success {
		status, detail, registeredAt = "registered", "", time.Now().Unix()
	}
	_, err := s.Exec("UPDATE verification_emails SET status=?,detail=?,registered_at=?,username=? WHERE id=? AND status='verified'", status, detail, registeredAt, username, id)
	return err
}

// Expiry is computed at read time, so idle services still report the right state.
const verificationStatusSQL = `CASE WHEN status='sent' AND expires_at<=? THEN 'expired' ELSE status END`

type verificationEmail struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	CreatedAt    int64  `json:"created_at"`
	SentAt       int64  `json:"sent_at"`
	ExpiresAt    int64  `json:"expires_at"`
	Status       string `json:"status"`
	Code         string `json:"code,omitempty"`
	Detail       string `json:"detail,omitempty"`
	DetailKey    string `json:"detail_key,omitempty"`
	RegisteredAt int64  `json:"registered_at"`
	Username     string `json:"username,omitempty"`
}

func handleVerificationEmails(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "GET") {
		return
	}
	now := time.Now().Unix()
	if err := app.store.cleanVerificationHistory(now); err != nil {
		operationError(w, err)
		return
	}
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days != 1 && days != 7 && days != 30 && days != 90 {
		days = 30
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	if page > 100000 {
		jsonError(w, messageText("e.2c5882fc7b7b"), 400)
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("email")))
	if len(email) > 254 {
		jsonError(w, messageText("e.e8cbd0e96486"), 400)
		return
	}
	status := r.URL.Query().Get("status")
	switch status {
	case "", "sent", "expired", "failed", "registered", "superseded", "sending", "unknown", "verified", "registration_failed":
	default:
		jsonError(w, messageText("e.3bb42a1e90f0"), 400)
		return
	}
	since := now - int64(days)*86400
	// The summary counts unique addresses, not resend attempts, to reveal drop-off.
	var requested, sent, registered, pending, expired, failed int
	err := app.store.QueryRow(`SELECT COUNT(DISTINCT email),COUNT(DISTINCT CASE WHEN sent_at>0 THEN email END),COUNT(DISTINCT CASE WHEN status='registered' THEN email END),COUNT(DISTINCT CASE WHEN sent_at>0 AND NOT EXISTS(SELECT 1 FROM verification_emails done WHERE done.email=v.email AND done.status='registered') THEN email END),COUNT(DISTINCT CASE WHEN status='sent' AND expires_at<=? AND NOT EXISTS(SELECT 1 FROM verification_emails active WHERE active.email=v.email AND active.status='sent' AND active.expires_at>?) AND NOT EXISTS(SELECT 1 FROM verification_emails done WHERE done.email=v.email AND done.status='registered') THEN email END),COALESCE(SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END),0) FROM verification_emails v WHERE created_at>=?`, now, now, since).Scan(&requested, &sent, &registered, &pending, &expired, &failed)
	if err != nil {
		operationError(w, err)
		return
	}
	where := "created_at>=?"
	args := []interface{}{since}
	if email != "" {
		where += " AND instr(email,?)>0"
		args = append(args, email)
	}
	if status != "" {
		where += " AND " + verificationStatusSQL + "=?"
		args = append(args, now, status)
	}
	var total int
	if err = app.store.QueryRow("SELECT COUNT(*) FROM verification_emails WHERE "+where, args...).Scan(&total); err != nil {
		operationError(w, err)
		return
	}
	params := append([]interface{}{now, now}, args...)
	params = append(params, 50, (page-1)*50)
	rows, err := app.store.Query("SELECT id,email,created_at,sent_at,expires_at,"+verificationStatusSQL+",CASE WHEN status='sent' AND expires_at>? THEN code ELSE '' END,detail,registered_at,username FROM verification_emails WHERE "+where+" ORDER BY id DESC LIMIT ? OFFSET ?", params...)
	if err != nil {
		operationError(w, err)
		return
	}
	records := []verificationEmail{}
	for rows.Next() {
		var record verificationEmail
		if err = rows.Scan(&record.ID, &record.Email, &record.CreatedAt, &record.SentAt, &record.ExpiresAt, &record.Status, &record.Code, &record.Detail, &record.RegisteredAt, &record.Username); err != nil {
			break
		}
		// Old databases contain the Chinese text; new records store the stable
		// key. Both forms render in the request language without a DB rewrite.
		record.Detail, record.DetailKey = localizedMessage(record.Detail, responseLanguage(w))
		records = append(records, record)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		operationError(w, err)
		return
	}
	jsonResp(w, map[string]interface{}{"records": records, "page": page, "page_size": 50, "total": total, "days": days, "server_time": now, "summary": map[string]int{"requested_emails": requested, "sent_emails": sent, "registered_emails": registered, "unregistered_emails": pending, "expired_unregistered_emails": expired, "failed_attempts": failed}})
}
