package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"gopkg.in/yaml.v3"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type SMTPConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	From     string `yaml:"from"`
	TLS      bool   `yaml:"tls"`
}
type Config struct {
	DBPath                  string     `yaml:"db_path"`
	BrokerURL               string     `yaml:"broker_url"`
	BrokerCA                string     `yaml:"broker_ca"`
	BrokerAdminUsername     string     `yaml:"broker_admin_username"`
	BrokerAdminPasswordFile string     `yaml:"broker_admin_password_file"`
	AdminUsername           string     `yaml:"admin_username"`
	AdminPasswordFile       string     `yaml:"admin_password_file"`
	PublicURL               string     `yaml:"public_url"`
	TrustProxy              bool       `yaml:"trust_proxy"`
	CookieSecure            bool       `yaml:"cookie_secure"`
	RegisterLevel           int        `yaml:"register_level"`
	ContactInfo             string     `yaml:"contact_info"`
	MQTTUri                 string     `yaml:"mqtt_uri"`
	MQTTWSUri               string     `yaml:"mqtt_ws_uri"`
	SMTP                    SMTPConfig `yaml:"smtp"`
	ListenAddr              string     `yaml:"listen_addr"`
}

var cfg Config
var cfgMu sync.RWMutex
var app *Application

func safeReturnTo(raw string) string {
	if len(raw) == 0 || raw[0] != '/' || (len(raw) > 1 && raw[1] == '/') {
		return ""
	}
	for _, c := range raw {
		if c == '\\' || c < 32 || c == 127 {
			return ""
		}
	}
	return raw
}
func loadConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err = yaml.Unmarshal(data, &cfg); err != nil {
		return err
	}
	if cfg.DBPath == "" {
		cfg.DBPath = "/data/app.db"
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":8080"
	}
	for _, item := range []struct {
		key string
		dst *string
	}{{"MQTT_URI", &cfg.MQTTUri}, {"MQTT_WS_URI", &cfg.MQTTWSUri}, {"PUBLIC_URL", &cfg.PublicURL}, {"BROKER_URL", &cfg.BrokerURL}} {
		if v := os.Getenv(item.key); v != "" {
			*item.dst = v
		}
	}
	if cfg.RegisterLevel < 0 || cfg.RegisterLevel > 2 {
		return errors.New("invalid register_level")
	}
	return nil
}
func readSecret(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	s := string(data)
	if len(s) > 0 && s[len(s)-1] == '\n' {
		s = s[:len(s)-1]
	}
	if len(s) < 16 {
		return "", errors.New("secret must be at least 16 bytes")
	}
	return s, nil
}
func digest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/analytics/event", handleAnalyticsEvent)
	mux.HandleFunc("/api/admin/analytics", adminAuth(handleAnalytics))
	mux.HandleFunc("/api/register", handleRegister)
	mux.HandleFunc("/api/register/status", handleRegisterStatus)
	mux.HandleFunc("/api/register/verify", handleVerifyEmail)
	mux.HandleFunc("/api/auth/login", handleLogin)
	mux.HandleFunc("/api/auth/logout", userAuth(handleLogout))
	mux.HandleFunc("/api/auth/me", userAuth(handleMe))
	mux.HandleFunc("/api/auth/password", userAuth(handlePassword))
	mux.HandleFunc("/api/auth/alias", userAuth(handleAliases))
	mux.HandleFunc("/api/devices", userAuth(handleDevices))
	mux.HandleFunc("/api/devices/", userAuth(handleDevice))
	mux.HandleFunc("/api/admin/users", adminAuth(handleUsers))
	mux.HandleFunc("/api/admin/user/", adminAuth(handleUserOps))
	mux.HandleFunc("/api/admin/devices", adminAuth(handleAdminDevices))
	mux.HandleFunc("/api/admin/device/", adminAuth(handleAdminDevice))
	mux.HandleFunc("/api/admin/config", adminAuth(handleConfig))
	mux.HandleFunc("/api/admin/acl", adminAuth(func(w http.ResponseWriter, r *http.Request) {
		jsonError(w, "已改为结构化设备权限管理", http.StatusGone)
	}))
	mux.HandleFunc("/api/ping", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonError(w, "方法不允许", 405)
			return
		}
		if target := safeReturnTo(r.URL.Query().Get("return_to")); target != "" {
			http.Redirect(w, r, target, 302)
			return
		}
		jsonResp(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { jsonError(w, "接口不存在", 404) })
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		mux.ServeHTTP(w, r)
	})
}
func main() {
	if len(os.Args) > 1 && (os.Args[1] == "init" || os.Args[1] == "migrate") {
		if err := setupCommand(os.Args[1], os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	}
	path := "config.yaml"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}
	if err := loadConfig(path); err != nil {
		log.Fatal(err)
	}
	var err error
	pass, err := readSecret(cfg.BrokerAdminPasswordFile)
	if err != nil {
		log.Fatalf("read broker secret: %v", err)
	}
	if err = os.MkdirAll(filepath.Dir(cfg.DBPath), 0700); err != nil {
		log.Fatal(err)
	}
	store, err := openStore(cfg.DBPath)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	broker, err := newDynamicBroker(cfg.BrokerURL, cfg.BrokerCA, cfg.BrokerAdminUsername, pass)
	if err != nil {
		log.Fatal(err)
	}
	defer broker.Close()
	app = &Application{store: store, broker: broker}
	// Upgrade the manager role in place, including installations initialized before discovery support.
	go func() {
		for {
			_, err := broker.Command("modifyRole", map[string]interface{}{"rolename": "manager", "acls": managerRules()})
			if err == nil {
				broker.watchDiscovery(func(topic string, payload []byte) {
					if err := app.discover(topic, payload); err != nil {
						log.Printf("discovery rejected: %v", err)
					}
				})
				if err = broker.subscribeDiscovery(broker.client); err == nil {
					break
				}
			}
			time.Sleep(time.Second)
		}
	}()
	go func() {
		for {
			if err := app.bootstrapAdmin(); err == nil {
				return
			} else {
				log.Printf("管理员账号初始化未完成: %v", err)
			}
			time.Sleep(2 * time.Second)
		}
	}()
	loadSettings()
	go func() {
		for {
			if err := app.reconcile(); err != nil {
				log.Printf("权限同步未完成: %v", err)
			}
			time.Sleep(10 * time.Second)
		}
	}()
	server := &http.Server{Addr: cfg.ListenAddr, Handler: routes(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	log.Printf("Listening on %s", cfg.ListenAddr)
	log.Fatal(server.ListenAndServe())
}
