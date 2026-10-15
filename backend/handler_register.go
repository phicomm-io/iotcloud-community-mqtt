package main

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"
)

type RegisterReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Code     string `json:"code,omitempty"`
	Email    string `json:"email,omitempty"`
}

func reservedUser(u string) bool {
	return u == cfg.BrokerAdminUsername || strings.HasPrefix(u, "__")
}
func validateAccount(user, password string) error {
	if !usernameRegex.MatchString(user) || reservedUser(user) || user == cfg.AdminUsername {
		return errors.New("用户名格式不正确或为保留账号")
	}
	if !emailRegex.MatchString(user) && len(user) > 32 {
		return errors.New("用户名长度须为 3–32 个字符")
	}
	if len(password) < 6 || len(password) > 256 {
		return errors.New("密码长度须为 6–256 字节")
	}
	return nil
}
func (a *Application) register(user, password string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	state, err := a.store.state(user)
	if err == nil {
		if state == "pending" {
			if authErr := a.broker.Authenticate(user, password); authErr == nil {
				_, err = a.store.Exec("UPDATE users SET state='active' WHERE username=?", user)
				if err != nil {
					return err
				}
				return a.syncAccount(user)
			}
			_, lookup := a.broker.Command("getClient", map[string]interface{}{"username": user})
			if lookup != nil && !notFound(lookup) {
				return lookup
			}
			if lookup == nil {
				return errConflict
			}
		} else {
			return errConflict
		}
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if state == "" {
		available, e := a.store.nameAvailable(user)
		if e != nil {
			return e
		}
		if !available {
			return errAccountName
		}
		if _, err = a.store.Exec("INSERT INTO users(username,state) VALUES(?,'pending')", user); err != nil {
			return err
		}
	}
	_, err = a.broker.Command("createClient", map[string]interface{}{"username": user, "password": password})
	if err != nil {
		return err
	}
	_, err = a.store.Exec("UPDATE users SET state='active' WHERE username=?", user)
	if err != nil {
		return err
	}
	return a.syncAccount(user)
}
func handleRegisterStatus(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "GET") {
		return
	}
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	jsonResp(w, map[string]interface{}{"register_level": cfg.RegisterLevel, "contact_info": cfg.ContactInfo, "mqtt_uri": cfg.MQTTUri, "mqtt_ws_uri": cfg.MQTTWSUri})
}
func handleRegister(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "POST") || !sameOrigin(w, r) {
		return
	}
	if !allowAttempt(r, "register", 10, time.Minute) {
		jsonError(w, "请求过于频繁", 429)
		return
	}
	cfgMu.RLock()
	level := cfg.RegisterLevel
	cfgMu.RUnlock()
	if level == 0 {
		jsonError(w, "注册功能已关闭", 403)
		return
	}
	var req RegisterReq
	if err := decode(r, &req); err != nil {
		jsonError(w, err.Error(), 400)
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if err := validateUsername(req.Username); err != nil {
		jsonError(w, err.Error(), 400)
		return
	}
	if err := validateAccount(req.Username, req.Password); err != nil {
		jsonError(w, err.Error(), 400)
		return
	}
	if level == 2 && (!emailRegex.MatchString(req.Email) || len(req.Email) > 254 || !verifyCode(req.Email, req.Code)) {
		jsonError(w, "邮箱验证码错误或已过期", 400)
		return
	}
	if err := app.registerWithEmail(req.Username, req.Password, req.Email, level == 2); err != nil {
		operationError(w, err)
		return
	}
	jsonResp(w, map[string]string{"message": "注册成功，请登录后添加设备", "username": req.Username})
}
func handleVerifyEmail(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "POST") || !sameOrigin(w, r) {
		return
	}
	cfgMu.RLock()
	level := cfg.RegisterLevel
	cfgMu.RUnlock()
	if level != 2 {
		jsonError(w, "当前注册模式不需要邮箱验证", 400)
		return
	}
	var req struct {
		Email string `json:"email"`
	}
	if err := decode(r, &req); err != nil {
		jsonError(w, err.Error(), 400)
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if !emailRegex.MatchString(req.Email) || len(req.Email) > 254 {
		jsonError(w, "邮箱格式不正确", 400)
		return
	}
	if !allowAttempt(r, "verify", 5, time.Minute) || !allowAttempt(r, "email:"+req.Email, 1, time.Minute) {
		jsonError(w, "请求过于频繁", 429)
		return
	}
	code, err := genCode()
	if err != nil {
		operationError(w, err)
		return
	}
	if err = sendVerificationEmail(req.Email, code); err != nil {
		jsonError(w, "邮件发送失败，请稍后重试", 503)
		return
	}
	storeCode(req.Email, code)
	jsonResp(w, map[string]string{"message": "验证码已发送，5 分钟内有效"})
}
