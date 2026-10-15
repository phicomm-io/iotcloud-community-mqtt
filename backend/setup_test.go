package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrationHashesAndRepeatedACL(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "passwd")
	acl := filepath.Join(dir, "acl")
	c := newBootstrapClient("alice", "original-password", nil)
	line := "alice:$7$100000$" + c.Salt + "$" + c.Password + "\n"
	os.WriteFile(p, []byte(line), 0600)
	os.WriteFile(acl, []byte("user alice\ntopic readwrite device/ztc1/AA:BB:CC:DD:EE:FF/#\nuser alice\ntopic readwrite homeassistant/+/AABBCCDDEEFF/#\n"), 0600)
	users, devices, e := parseLegacy(p, acl)
	if e != nil {
		t.Fatal(e)
	}
	if users["alice"].Password != c.Password || users["alice"].Salt != c.Salt || len(devices) != 1 || devices[0].MAC != "aabbccddeeff" {
		t.Fatal("migration differs")
	}
	os.WriteFile(acl, []byte("user alice\ntopic readwrite #\n"), 0600)
	if _, _, e = parseLegacy(p, acl); e == nil {
		t.Fatal("broad ACL accepted")
	}
	c2 := newBootstrapClient("bob", "another-password", nil)
	os.WriteFile(p, []byte(line+"bob:$7$100000$"+c2.Salt+"$"+c2.Password+"\n"), 0600)
	os.WriteFile(acl, []byte("user alice\ntopic readwrite device/ztc1/aabbccddeeff/#\nuser bob\ntopic readwrite device/ztc1/AABBCCDDEEFF/#\n"), 0600)
	if _, _, e = parseLegacy(p, acl); e == nil {
		t.Fatal("conflicting owners accepted")
	}
}
func TestBootstrapRefusesOverwrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "runtime")
	if e := setupCommand("init", []string{"--output", dir}); e != nil {
		t.Fatal(e)
	}
	if e := setupCommand("init", []string{"--output", dir}); e == nil {
		t.Fatal("overwrite allowed")
	}
	info, e := os.Stat(filepath.Join(dir, "secrets", "broker-password"))
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("secret permissions", e)
	}
}

func TestMigrationExplicitProductOverridesSameOwnerGenericStub(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "passwd")
	acl := filepath.Join(dir, "acl")
	c := newBootstrapClient("alice", "migration-password", nil)
	os.WriteFile(p, []byte("alice:$7$100000$"+c.Salt+"$"+c.Password+"\n"), 0600)
	for _, entries := range []string{
		"topic readwrite device/ztc1/AABBCCDDEEFF/#\ntopic readwrite phicomm/m1/aabbccddeeff/#\n",
		"topic readwrite phicomm/m1/aabbccddeeff/#\ntopic readwrite device/ztc1/AABBCCDDEEFF/#\n",
	} {
		os.WriteFile(acl, []byte("user alice\n"+entries), 0600)
		users, devices, err := parseLegacy(p, acl)
		if err != nil || len(devices) != 1 {
			t.Fatal(devices, err)
		}
		d := devices[0]
		if d.Model != "m1" || d.Version != "v2" || d.Namespace != "account" || d.MQTTUsername != "alice" {
			t.Fatal(d)
		}
		if users["alice"].Password != c.Password {
			t.Fatal("hash changed")
		}
	}
	os.WriteFile(acl, []byte("user alice\ntopic readwrite phicomm/m1/aabbccddeeff/#\ntopic readwrite phicomm/a1/aabbccddeeff/#\n"), 0600)
	if _, _, err := parseLegacy(p, acl); err == nil {
		t.Fatal("conflicting explicit products accepted")
	}
	os.WriteFile(acl, []byte("user alice\ntopic readwrite device/ztc1/AABBCCDDEEFF/#\n"), 0600)
	_, devices, err := parseLegacy(p, acl)
	if err != nil || devices[0].Version != "v1" {
		t.Fatal("Legacy changed", err)
	}
}
