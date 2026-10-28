package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestDiscoveryRefreshesExistingSensorACL(t *testing.T) {
	for _, model := range []string{"tc1", "a1", "m1", "daikin"} {
		t.Run(model, func(t *testing.T) {
			a := testApp(t)
			b := a.broker.(*fakeBroker)
			const mac = "aabbccddeeff"
			topic := "phicomm/v2/" + userKey("alice") + "/discovery/" + mac
			payload := []byte(fmt.Sprintf(`{"schema":1,"protocol":"tmcp","protocol_version":2,"version":"v2","model":%q}`, model))
			if err := a.discover(topic, payload); err != nil {
				t.Fatal(err)
			}
			devices, err := a.store.scopedDevices("alice")
			if err != nil || len(devices) != 1 || devices[0].Status != "active" {
				t.Fatal(devices, err)
			}
			d := devices[0]
			role := deviceRole(d)
			// Simulate a role created before generic sensor topics were supported.
			b.acl[role] = append([]Rule(nil), scopeRules(d)[:3]...)
			for i := 0; i < 2; i++ {
				if err := a.discover(topic, payload); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(b.acl[role], scopeRules(d)) || !b.attached["alice"][role] || b.attached["bob"][role] {
					t.Fatal("existing sensor ACL was not repaired with isolated ownership", b.acl[role], b.attached)
				}
				got, err := a.store.scopedDevices("alice")
				if err != nil || !reflect.DeepEqual(got, devices) {
					t.Fatal("reannouncement changed device identity or status", got, err)
				}
			}
		})
	}
}

func addDeviceRequest(t *testing.T, admin bool, model, version string) *httptest.ResponseRecorder {
	t.Helper()
	body := fmt.Sprintf(`{"mac":"AA:BB:CC:DD:EE:FF","model":%q,"version":%q`, model, version)
	path := "/api/devices"
	if admin {
		path = "/api/admin/devices"
		body += `,"username":"alice"`
	}
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body+"}"))
	r = r.WithContext(context.WithValue(r.Context(), userContextKey{}, "alice"))
	w := httptest.NewRecorder()
	if admin {
		handleAdminDevices(w, r)
	} else {
		handleDevices(w, r)
	}
	return w
}

func TestManualTMCPRegistrationAndRestore(t *testing.T) {
	for _, admin := range []bool{false, true} {
		for _, model := range []string{"tc1", "m1", "a1", "daikin"} {
			t.Run(fmt.Sprintf("admin=%t/%s", admin, model), func(t *testing.T) {
				a := testApp(t)
				app = a
				t.Cleanup(func() { app = nil })
				b := a.broker.(*fakeBroker)
				const mac = "aabbccddeeff"
				role := "scoped_" + userKey("alice") + "_" + mac
				assertActive := func() {
					t.Helper()
					list, err := a.store.devices("alice")
					if err != nil || len(list) != 1 || list[0].Namespace != "account" || list[0].MQTTUsername != "alice" || list[0].Status != "active" || list[0].Model != model {
						t.Fatalf("TMCP device not visible and active: %+v, %v", list, err)
					}
					if !b.attached["alice"][role] || b.attached["bob"][role] || b.roles["device_"+mac] {
						t.Fatal("incorrect device role assignment", b.attached)
					}
					want := "phicomm/v2/" + userKey("alice") + "/" + model + "/" + mac + "/#"
					expected := 5
					if model == "m1" || model == "daikin" {
						expected = 7
					}
					if rules := b.acl[role]; len(rules) != expected || rules[2].ACLType != "subscribePattern" || rules[2].Topic != want || !rules[2].Allow {
						t.Fatal("incorrect TMCP ACL", rules)
					}
				}
				for i := 0; i < 2; i++ {
					w := addDeviceRequest(t, admin, model, "v2")
					if w.Code != 200 {
						t.Fatal(w.Code, w.Body.String())
					}
					assertActive()
				}
				w := httptest.NewRecorder()
				handleScopedDeviceFor(w, httptest.NewRequest("DELETE", "/", nil), mac, "alice")
				if w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
				payload := []byte(fmt.Sprintf(`{"schema":1,"protocol":"tmcp","protocol_version":2,"version":"v2","model":%q}`, model))
				if err := a.discover("phicomm/v2/"+userKey("alice")+"/discovery/"+mac, payload); err != nil {
					t.Fatal(err)
				}
				if list, err := a.store.devices("alice"); err != nil || len(list) != 0 || b.roles[role] {
					t.Fatal("passive discovery restored removed device", list, err)
				}
				wrongModel := "m1"
				if model == wrongModel {
					wrongModel = "a1"
				}
				if w := addDeviceRequest(t, admin, wrongModel, "v2"); w.Code != 409 {
					t.Fatal("conflicting model accepted", w.Code, w.Body.String())
				}
				if w := addDeviceRequest(t, admin, model, "v2"); w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
				assertActive()
			})
		}
	}
}

func TestManualTMCPRestoreBrokerFailure(t *testing.T) {
	a := testApp(t)
	app = a
	t.Cleanup(func() { app = nil })
	b := a.broker.(*fakeBroker)
	if w := addDeviceRequest(t, false, "tc1", "v2"); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w := httptest.NewRecorder()
	handleScopedDeviceFor(w, httptest.NewRequest("DELETE", "/", nil), "aabbccddeeff", "alice")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	b.fail = true
	if w := addDeviceRequest(t, true, "tc1", "v2"); w.Code != 503 {
		t.Fatal("failed permission sync reported success", w.Code, w.Body.String())
	}
	list, err := a.store.scopedDevices("alice")
	if err != nil || len(list) != 1 || list[0].Status != "pending" {
		t.Fatal("restore not pending for retry", list, err)
	}
	b.fail = false
	if err := a.reconcile(); err != nil {
		t.Fatal(err)
	}
	list, err = a.store.scopedDevices("alice")
	if err != nil || len(list) != 1 || list[0].Status != "active" || !b.attached["alice"][deviceRole(list[0])] {
		t.Fatal("restore did not recover", list, err)
	}
}

func TestManualTMCPAdditionDuringUnbind(t *testing.T) {
	a := testApp(t)
	app = a
	t.Cleanup(func() { app = nil })
	if w := addDeviceRequest(t, false, "tc1", "v2"); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	b := a.broker.(*fakeBroker)
	b.fail = true
	w := httptest.NewRecorder()
	handleScopedDeviceFor(w, httptest.NewRequest("DELETE", "/", nil), "aabbccddeeff", "alice")
	if w.Code != 503 {
		t.Fatal(w.Code, w.Body.String())
	}
	b.fail = false
	if w := addDeviceRequest(t, true, "tc1", "v2"); w.Code != 409 {
		t.Fatal("unfinished unbind reported as added", w.Code, w.Body.String())
	}
	if err := a.reconcile(); err != nil {
		t.Fatal(err)
	}
	if w := addDeviceRequest(t, true, "tc1", "v2"); w.Code != 200 {
		t.Fatal("could not re-add after unbind completed", w.Code, w.Body.String())
	}
}

func TestManualLegacyTC1Registration(t *testing.T) {
	for _, admin := range []bool{false, true} {
		t.Run(fmt.Sprintf("admin=%t", admin), func(t *testing.T) {
			a := testApp(t)
			app = a
			t.Cleanup(func() { app = nil })
			if w := addDeviceRequest(t, admin, "tc1", "v1"); w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			list, err := a.store.devices("alice")
			if err != nil || len(list) != 1 || list[0].Namespace != "" || list[0].Version != "v1" || list[0].Status != "active" {
				t.Fatal("legacy TC1 registration changed", list, err)
			}
		})
	}
}
