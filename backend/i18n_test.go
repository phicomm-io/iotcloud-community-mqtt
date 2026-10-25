package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestPreferredLanguage(t *testing.T) {
	for _, tc := range []struct{ header, want string }{
		{"", "zh-CN"}, {"fr,en-US;q=0.8,zh-CN;q=0.5", "en"},
		{"en;q=0,zh;q=1", "zh-CN"}, {"en;q=bad", "zh-CN"},
		{"en;q=NaN,zh;q=0.2", "zh-CN"}, {"zh-TW,en;q=0.8", "zh-CN"},
		{"en-GB,zh-CN", "en"},
	} {
		if got := preferredLanguage(tc.header); got != tc.want {
			t.Errorf("%q: got %q, want %q", tc.header, got, tc.want)
		}
	}
	for key, pair := range messageCatalog {
		if messageForLanguage(key, "en") != pair[0] || messageForLanguage(key, "zh-CN") != pair[1] {
			t.Fatalf("missing translation for %s", key)
		}
	}
}

func TestAPIResponseLanguages(t *testing.T) {
	// Exercise the real route and error helpers, including concurrent requests.
	// The stable fields must match while translated display text may differ.
	handler := routes()
	var group sync.WaitGroup
	for i := 0; i < 20; i++ {
		for _, language := range []string{"en", "zh-CN"} {
			group.Add(1)
			go func(language string) {
				defer group.Done()
				r := httptest.NewRequest(http.MethodGet, "/api/unknown", nil)
				r.Header.Set("Accept-Language", language)
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				var data map[string]string
				if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
					t.Error(err)
					return
				}
				if w.Code != http.StatusNotFound || data["code"] != "e.c49d8684e46f" || data["message_key"] != "e.c49d8684e46f" || data["error"] != messageForLanguage("e.c49d8684e46f", language) {
					t.Errorf("%s: status=%d data=%v", language, w.Code, data)
				}
				if w.Header().Get("Content-Language") != language || !strings.Contains(strings.Join(w.Header().Values("Vary"), ","), "Accept-Language") {
					t.Errorf("%s: headers=%v", language, w.Header())
				}
			}(language)
		}
	}
	group.Wait()
}

func TestSuccessMessagesPreserveUserValues(t *testing.T) {
	for _, language := range []string{"en", "zh-CN"} {
		original := map[string]string{"message": messageText("e.f7bb1748ec1e"), "username": "其他", "name": "直接访问"}
		recorder := httptest.NewRecorder()
		recorder.Header().Set("Content-Language", language)
		jsonResp(recorder, original)
		var data map[string]string
		if err := json.Unmarshal(recorder.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
		if data["message"] != messageForLanguage("e.f7bb1748ec1e", language) || data["message_key"] != "e.f7bb1748ec1e" || data["username"] != "其他" || data["name"] != "直接访问" || len(original) != 3 {
			t.Fatalf("%s: localized or mutated user data: %v / %v", language, data, original)
		}
	}
	// Legacy clients and direct handler calls still receive the Chinese default.
	w := httptest.NewRecorder()
	jsonError(w, messageText("e.90343cedc288"), http.StatusForbidden)
	var data map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil || data["code"] != "admin_forbidden" || data["error"] != "没有管理权限" {
		t.Fatal(data, err)
	}
}
