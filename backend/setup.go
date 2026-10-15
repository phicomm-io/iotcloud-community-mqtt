package main

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha512"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"golang.org/x/crypto/pbkdf2"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const serviceUser = "__mqtt_manager"

type bootstrapClient struct {
	Username   string                   `json:"username"`
	Password   string                   `json:"password"`
	Salt       string                   `json:"salt"`
	Iterations int                      `json:"iterations"`
	Roles      []map[string]interface{} `json:"roles"`
}
type bootstrapRole struct {
	RoleName string `json:"rolename"`
	ACLs     []Rule `json:"acls"`
}
type bootstrapConfig struct {
	DefaultACL map[string]bool   `json:"defaultACLAccess"`
	Clients    []bootstrapClient `json:"clients"`
	Roles      []bootstrapRole   `json:"roles"`
}

func newBootstrapClient(user, password string, roles []string) bootstrapClient {
	salt := make([]byte, 12)
	if _, err := rand.Read(salt); err != nil {
		panic(err)
	}
	c := bootstrapClient{Username: user, Iterations: 100000, Salt: base64.StdEncoding.EncodeToString(salt), Password: base64.StdEncoding.EncodeToString(pbkdf2.Key([]byte(password), salt, 100000, 64, sha512.New)), Roles: []map[string]interface{}{}}
	for _, r := range roles {
		c.Roles = append(c.Roles, map[string]interface{}{"rolename": r, "priority": 1})
	}
	return c
}
func privateWrite(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
func setupCommand(command string, args []string) error {
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	output := fs.String("output", "runtime", "new runtime directory")
	passwd := fs.String("passwd", "", "legacy passwd file")
	acl := fs.String("acl", "", "legacy ACL file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	// Refuse to overwrite a running deployment. Parse all legacy input before generating anything.
	users := map[string]bootstrapClient{}
	devices := []Device{}
	var err error
	if command == "migrate" {
		if *passwd == "" || *acl == "" {
			return errors.New("migrate requires --passwd and --acl")
		}
		users, devices, err = parseLegacy(*passwd, *acl)
		if err != nil {
			return err
		}
	}
	if _, err = os.Stat(filepath.Join(*output, "broker", "dynamic-security.json")); err == nil {
		return errors.New("output already initialized; use a new directory")
	}
	for _, dir := range []string{"app", "broker-data", "broker/certs", "secrets", "backup"} {
		if err = os.MkdirAll(filepath.Join(*output, dir), 0700); err != nil {
			return err
		}
	}
	brokerPass, adminPass := randomID(), randomID()
	if err = privateWrite(filepath.Join(*output, "secrets", "broker-password"), []byte(brokerPass+"\n")); err != nil {
		return err
	}
	if err = privateWrite(filepath.Join(*output, "secrets", "admin-password"), []byte(adminPass+"\n")); err != nil {
		return err
	}
	if err = createCertificates(filepath.Join(*output, "broker", "certs")); err != nil {
		return err
	}
	b := bootstrapConfig{DefaultACL: map[string]bool{"publishClientSend": false, "publishClientReceive": false, "subscribe": false, "unsubscribe": true}, Clients: []bootstrapClient{newBootstrapClient(serviceUser, brokerPass, []string{"manager"})}, Roles: []bootstrapRole{{"manager", managerRules()}}}
	store, err := openStore(filepath.Join(*output, "app", "app.db"))
	if err != nil {
		return err
	}
	defer store.Close()
	tx, err := store.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for user, c := range users {
		if _, err = tx.Exec("INSERT INTO users(username,state) VALUES(?,'active')", user); err != nil {
			return err
		}
		b.Clients = append(b.Clients, c)
	}
	for _, d := range devices {
		role := deviceRole(d)
		rules := rulesFor(d)
		if d.Namespace != "" {
			rules = scopeRules(d)
		}
		b.Roles = append(b.Roles, bootstrapRole{role, rules})
		for i := range b.Clients {
			if b.Clients[i].Username == d.Username {
				b.Clients[i].Roles = append(b.Clients[i].Roles, map[string]interface{}{"rolename": role, "priority": 1})
			}
		}
		if d.Namespace != "" {
			_, err = tx.Exec("INSERT INTO scoped_devices(username,mqtt_username,mac,model,version,status) VALUES(?,?,?,?,?,'active')", d.Username, d.MQTTUsername, d.MAC, d.Model, d.Version)
		} else {
			_, err = tx.Exec("INSERT INTO devices(mac,username,model,version,status) VALUES(?,?,?,?,'active')", d.MAC, d.Username, d.Model, d.Version)
		}
		if err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	if err = privateWrite(filepath.Join(*output, "broker", "dynamic-security.json"), data); err != nil {
		return err
	}
	if command == "migrate" {
		for name, path := range map[string]string{"passwd": *passwd, "acl": *acl} {
			data, err = os.ReadFile(path)
			if err != nil {
				return err
			}
			if err = privateWrite(filepath.Join(*output, "backup", name), data); err != nil {
				return err
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	fmt.Printf("Initialized %s (%d users, %d devices). Admin password is in secrets/admin-password; no passwords printed.\n", *output, len(users), len(devices))
	return nil
}
func createCertificates(dir string) error {
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(now.UnixNano()), Subject: pkix.Name{CommonName: "phicomm MQTT internal CA"}, NotBefore: now.Add(-time.Hour), NotAfter: now.AddDate(10, 0, 0), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		return err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(now.UnixNano() + 1), Subject: pkix.Name{CommonName: "mosquitto"}, DNSNames: []string{"mosquitto", "localhost"}, NotBefore: now.Add(-time.Hour), NotAfter: now.AddDate(2, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, cert, ca, &key.PublicKey, caKey)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	for name, data := range map[string][]byte{"ca.crt": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), "server.crt": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), "server.key": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})} {
		if err = privateWrite(filepath.Join(dir, name), data); err != nil {
			return err
		}
	}
	return nil
}
func parseLegacy(passwdPath, aclPath string) (map[string]bootstrapClient, []Device, error) {
	f, err := os.Open(passwdPath)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	users := map[string]bootstrapClient{}
	var entries []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, ":") {
			entries = append(entries, line)
		} else if len(entries) > 0 {
			entries[len(entries)-1] += line
		} else {
			return nil, nil, errors.New("invalid passwd continuation")
		}
	}
	if err = scanner.Err(); err != nil {
		return nil, nil, err
	}
	for _, line := range entries {
		parts := strings.SplitN(line, ":", 2)
		u := parts[0]
		if !usernameRegex.MatchString(u) || u == serviceUser || u == "admin" || strings.HasPrefix(u, "__") {
			return nil, nil, fmt.Errorf("invalid/reserved legacy username %q", u)
		}
		if _, exists := users[u]; exists {
			return nil, nil, fmt.Errorf("duplicate username %q", u)
		}
		hash := strings.Split(parts[1], "$")
		if len(hash) != 5 || hash[1] != "7" {
			return nil, nil, fmt.Errorf("unsupported hash for %q (requires $7$)", u)
		}
		iterations, err := strconv.Atoi(hash[2])
		if err != nil || iterations < 1 || iterations > 10000000 {
			return nil, nil, errors.New("invalid iterations")
		}
		salt, err := base64.StdEncoding.DecodeString(hash[3])
		if err != nil || len(salt) != 12 {
			return nil, nil, errors.New("invalid salt")
		}
		key, err := base64.StdEncoding.DecodeString(hash[4])
		if err != nil || len(key) != 64 {
			return nil, nil, errors.New("invalid hash")
		}
		users[u] = bootstrapClient{u, hash[4], hash[3], iterations, []map[string]interface{}{}}
	}
	data, err := os.ReadFile(aclPath)
	if err != nil {
		return nil, nil, err
	}
	owner := ""
	found := map[string]Device{}
	for n, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "user" {
			owner = fields[1]
			if _, ok := users[owner]; !ok {
				return nil, nil, fmt.Errorf("ACL user missing from passwd: %q", owner)
			}
			continue
		}
		if owner == "" || len(fields) != 3 || fields[0] != "topic" || fields[1] != "readwrite" {
			return nil, nil, fmt.Errorf("unsupported ACL line %d", n+1)
		}
		parts := strings.Split(fields[2], "/")
		model := "tc1"
		mac := ""
		if len(parts) == 4 && parts[0] == "device" && parts[1] == "ztc1" && parts[3] == "#" {
			mac = parts[2]
		} else if len(parts) == 4 && parts[0] == "phicomm" && (parts[1] == "m1" || parts[1] == "a1") && parts[3] == "#" {
			model = parts[1]
			mac = parts[2]
		} else if len(parts) == 4 && parts[0] == "homeassistant" && parts[1] == "+" && parts[3] == "#" {
			mac = parts[2]
			for _, m := range []string{"m1", "a1"} {
				if strings.HasPrefix(mac, m+"_") {
					model = m
					mac = strings.TrimPrefix(mac, m+"_")
					break
				}
			}
		} else {
			return nil, nil, fmt.Errorf("unsupported ACL topic on line %d", n+1)
		}
		if !isValidMAC(mac) {
			return nil, nil, fmt.Errorf("invalid MAC on ACL line %d", n+1)
		}
		mac = normalizeMAC(mac)
		version := "v2"
		if model == "tc1" {
			version = "v1"
		}
		d := Device{MAC: mac, Model: model, Version: version, Status: "active", Username: owner}
		if old, ok := found[mac]; ok {
			if old.Username != owner || (old.Model != model && old.Model != "tc1" && model != "tc1") {
				return nil, nil, fmt.Errorf("conflicting ownership/model on ACL line %d", n+1)
			}
			// The old console registered every model under the generic TC1 root.
			// Explicit same-owner M1/A1 entries are authoritative over those stubs.
			if old.Model != "tc1" && model == "tc1" {
				continue
			}
		}
		if model != "tc1" {
			d.Namespace = "account"
			d.MQTTUsername = owner
		}
		found[mac] = d
	}
	devices := []Device{}
	for _, d := range found {
		devices = append(devices, d)
	}
	return users, devices, nil
}
