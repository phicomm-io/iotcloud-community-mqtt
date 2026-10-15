package main

import (
	"net/http"
	"strconv"
	"strings"
)

func handleUsers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		rows, err := app.store.Query("SELECT u.username,u.state,CASE WHEN r.role='admin' THEN 'admin' ELSE 'user' END FROM users u LEFT JOIN user_roles r ON r.username=u.username ORDER BY u.username")
		if err != nil {
			operationError(w, err)
			return
		}
		list := []map[string]string{}
		for rows.Next() {
			var u, s, role string
			if err = rows.Scan(&u, &s, &role); err != nil {
				rows.Close()
				operationError(w, err)
				return
			}
			list = append(list, map[string]string{"username": u, "status": s, "role": role})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			operationError(w, err)
			return
		}
		jsonResp(w, list)
	case "POST":
		var req RegisterReq
		if err := decode(r, &req); err != nil {
			jsonError(w, err.Error(), 400)
			return
		}
		req.Username = strings.TrimSpace(req.Username)
		if err := validateUsername(req.Username); err != nil {
			jsonError(w, err.Error(), 400)
			return
		}
		if err := validateAccount(req.Username, req.Password); err != nil {
			jsonError(w, err.Error(), 400)
			return
		}
		if err := app.register(req.Username, req.Password); err != nil {
			operationError(w, err)
			return
		}
		jsonResp(w, map[string]string{"message": "用户已创建"})
	default:
		jsonError(w, "方法不允许", 405)
	}
}
func handleUserOps(w http.ResponseWriter, r *http.Request) {
	u, role := roleTarget(strings.TrimPrefix(r.URL.Path, "/api/admin/user/"))
	if role {
		handleUserRole(w, r, u)
		return
	}
	if reservedUser(u) {
		jsonError(w, "不能操作保留账号", 403)
		return
	}
	switch r.Method {
	case "DELETE":
		if err := app.deleteUser(u); err != nil {
			operationError(w, err)
			return
		}
	case "PUT":
		var req struct {
			Password string `json:"password"`
		}
		if err := decode(r, &req); err != nil {
			jsonError(w, err.Error(), 400)
			return
		}
		if len(req.Password) < 6 || len(req.Password) > 256 {
			jsonError(w, "密码长度须为 6–256 字节", 400)
			return
		}
		if err := app.changePassword(u, req.Password); err != nil {
			operationError(w, err)
			return
		}
	default:
		jsonError(w, "方法不允许", 405)
		return
	}
	jsonResp(w, map[string]bool{"ok": true})
}
func handleAdminDevices(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		list, err := app.store.devices("")
		if err != nil {
			operationError(w, err)
			return
		}
		jsonResp(w, list)
	case "POST":
		var d Device
		if err := decode(r, &d); err != nil {
			jsonError(w, err.Error(), 400)
			return
		}
		if d.Username == "" || reservedUser(d.Username) || d.Status != "" || (d.Namespace != "" || d.MQTTUsername != "") {
			jsonError(w, "请选择有效账号", 400)
			return
		}
		if err := validateDevice(&d); err != nil {
			jsonError(w, err.Error(), 400)
			return
		}
		if err := app.saveDevice(d.Username, d, false); err != nil {
			operationError(w, err)
			return
		}
		jsonResp(w, map[string]bool{"ok": true})
	default:
		jsonError(w, "方法不允许", 405)
	}
}
func handleAdminDevice(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "DELETE") {
		return
	}
	mac := strings.TrimPrefix(r.URL.Path, "/api/admin/device/")
	if strings.HasPrefix(mac, "scoped/") {
		mac = strings.TrimPrefix(mac, "scoped/")
		if !isValidMAC(mac) || reservedUser(r.URL.Query().Get("username")) {
			jsonError(w, "设备标识不正确", 400)
			return
		}
		handleScopedDeviceFor(w, r, normalizeMAC(mac), r.URL.Query().Get("username"))
		return
	}
	if !isValidMAC(mac) {
		jsonError(w, "MAC 地址格式不正确", 400)
		return
	}
	mac = normalizeMAC(mac)
	d, err := app.store.device(mac)
	if err != nil {
		operationError(w, err)
		return
	}
	if err = app.removeDevice(d.Username, mac); err != nil {
		operationError(w, err)
		return
	}
	jsonResp(w, map[string]bool{"ok": true})
}
func loadSettings() {
	rows, err := app.store.Query("SELECT key,value FROM settings")
	if err != nil {
		return
	}
	defer rows.Close()
	cfgMu.Lock()
	defer cfgMu.Unlock()
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) != nil {
			continue
		}
		if k == "register_level" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 && n <= 2 {
				cfg.RegisterLevel = n
			}
		} else if k == "contact_info" {
			cfg.ContactInfo = v
		}
	}
}
func handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		cfgMu.RLock()
		defer cfgMu.RUnlock()
		jsonResp(w, map[string]interface{}{"register_level": cfg.RegisterLevel, "contact_info": cfg.ContactInfo, "mqtt_uri": cfg.MQTTUri, "mqtt_ws_uri": cfg.MQTTWSUri, "permission_backend": "dynamic-security"})
	case "PUT":
		var req struct {
			RegisterLevel int    `json:"register_level"`
			ContactInfo   string `json:"contact_info"`
		}
		if err := decode(r, &req); err != nil {
			jsonError(w, err.Error(), 400)
			return
		}
		if req.RegisterLevel < 0 || req.RegisterLevel > 2 || len(req.ContactInfo) > 2000 {
			jsonError(w, "配置格式错误", 400)
			return
		}
		tx, err := app.store.Begin()
		if err != nil {
			operationError(w, err)
			return
		}
		defer tx.Rollback()
		for k, v := range map[string]string{"register_level": strconv.Itoa(req.RegisterLevel), "contact_info": req.ContactInfo} {
			if _, err = tx.Exec("INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", k, v); err != nil {
				operationError(w, err)
				return
			}
		}
		if err = tx.Commit(); err != nil {
			operationError(w, err)
			return
		}
		cfgMu.Lock()
		cfg.RegisterLevel = req.RegisterLevel
		cfg.ContactInfo = req.ContactInfo
		cfgMu.Unlock()
		jsonResp(w, map[string]bool{"ok": true})
	default:
		jsonError(w, "方法不允许", 405)
	}
}
