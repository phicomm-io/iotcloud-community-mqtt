package main

import (
	"crypto/rand"
	"crypto/tls"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/smtp"
	"strconv"
	"time"
)

// 生成 6 位数字验证码
func genCode() (string, error) {
	const digits = "0123456789"
	code := make([]byte, 6)
	for i := range code {
		n, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		code[i] = digits[n.Int64()]
	}
	return string(code), nil
}

// 发送验证码邮件
func sendVerificationEmail(to, code, language string) error {
	cfgMu.RLock()
	s := cfg.SMTP
	cfgMu.RUnlock()
	addr := net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
	log.Printf("[email] 尝试发送验证码 to=%s smtp=%s tls=%v", to, addr, s.TLS)

	subject := messageForLanguage("e.69499cf86fc1", language)
	body := fmt.Sprintf(messageForLanguage("e.ab00888baac7", language), code)
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		s.From, to, subject, body)

	auth := smtp.PlainAuth("", s.Username, s.Password, s.Host)

	if s.TLS {
		// SSL/TLS 直连（端口 465）
		dialer := &net.Dialer{Timeout: 10 * time.Second}
		tlsCfg := &tls.Config{ServerName: s.Host}
		conn, err := tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
		if err != nil {
			log.Printf("[email] TLS连接失败 smtp=%s err=%v", addr, err)
			return fmt.Errorf("TLS连接失败: %w", err)
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(15 * time.Second))

		client, err := smtp.NewClient(conn, s.Host)
		if err != nil {
			return fmt.Errorf("SMTP客户端创建失败: %w", err)
		}
		defer client.Close()

		if err = client.Auth(auth); err != nil {
			log.Printf("[email] SMTP认证失败(TLS) user=%s err=%v", s.Username, err)
			return fmt.Errorf("SMTP认证失败: %w", err)
		}
		if err = client.Mail(s.From); err != nil {
			return err
		}
		if err = client.Rcpt(to); err != nil {
			return err
		}
		w, err := client.Data()
		if err != nil {
			return err
		}
		_, err = fmt.Fprint(w, msg)
		if err != nil {
			return err
		}
		if err = w.Close(); err != nil {
			return err
		}
		log.Printf("[email] 发送成功(TLS) to=%s", to)
		return nil
	}

	// STARTTLS（端口 587）
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		log.Printf("[email] STARTTLS连接失败 smtp=%s err=%v", addr, err)
		return fmt.Errorf("连接SMTP失败: %w", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(15 * time.Second))
	client, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		return err
	}
	defer client.Close()

	if err = client.StartTLS(&tls.Config{ServerName: s.Host}); err != nil {
		return fmt.Errorf("STARTTLS失败: %w", err)
	}
	if err = client.Auth(auth); err != nil {
		log.Printf("[email] SMTP认证失败(STARTTLS) user=%s err=%v", s.Username, err)
		return fmt.Errorf("SMTP认证失败: %w", err)
	}
	if err = client.Mail(s.From); err != nil {
		return err
	}
	if err = client.Rcpt(to); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(w, msg)
	if err != nil {
		return err
	}
	if err = w.Close(); err != nil {
		return err
	}
	log.Printf("[email] 发送成功(STARTTLS) to=%s", to)
	return nil
}
