package main

import (
	"encoding/json"
	"testing"
)

// Record role writes while delegating authentication and membership to the
// existing broker fixture. Exercise the actual account reconciliation.
type haBirthBroker struct {
	*fakeBroker
	writes []string
	rules  map[string][]Rule
}

func (b *haBirthBroker) Command(command string, params map[string]interface{}) (json.RawMessage, error) {
	if !b.fail && (command == "createRole" || command == "modifyRole") {
		role := params["rolename"].(string)
		b.writes = append(b.writes, command)
		b.rules[role] = params["acls"].([]Rule)
	}
	return b.fakeBroker.Command(command, params)
}
func TestHomeAssistantBirthNewAndExistingAccounts(t *testing.T) {
	a := testApp(t)
	b := &haBirthBroker{fakeBroker: a.broker.(*fakeBroker), rules: map[string][]Rule{}}
	a.broker = b
	for _, username := range []string{"alice", "bob"} {
		role := "account_" + userKey(username)
		for _, existing := range []bool{false, true} {
			b.roles[role] = existing
			b.writes = nil
			if err := a.syncAccount(username); err != nil {
				t.Fatal(err)
			}
			expected := "createRole"
			if existing {
				expected = "modifyRole"
			}
			if len(b.writes) != 1 || b.writes[0] != expected {
				t.Fatalf("existing=%v writes=%v", existing, b.writes)
			}
			birth := false
			for _, r := range b.rules[role] {
				if r.ACLType != "publishClientSend" || !r.Allow {
					continue
				}
				switch r.Topic {
				case "homeassistant/status":
					birth = true
				case "homeassistant/+/" + userKey(username) + "/#", "phicomm/v2/" + userKey(username) + "/discovery/+":
				default:
					t.Fatalf("unexpected publish grant: %s", r.Topic)
				}
			}
			if !birth || !b.attached[username][role] {
				t.Fatalf("missing birth permission or role for %s", username)
			}
		}
	}
	b.fail = true
	if err := a.syncAccount("alice"); err == nil {
		t.Fatal("broker failure must propagate")
	}
}
