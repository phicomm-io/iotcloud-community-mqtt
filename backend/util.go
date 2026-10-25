package main

import (
	"encoding/json"
	"net/http"
)

func jsonResp(w http.ResponseWriter, data interface{}) {
	// Only this response field is a platform message. Account names, banners,
	// device labels and other user-provided strings must remain unchanged.
	if fields, ok := data.(map[string]string); ok && fields["message"] != "" {
		translated, key := localizedMessage(fields["message"], responseLanguage(w))
		copy := make(map[string]string, len(fields)+1)
		for name, value := range fields {
			copy[name] = value
		}
		copy["message"] = translated
		if key != "" {
			copy["message_key"] = key
		}
		data = copy
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	key := messageKey(msg)
	stable := key
	if msg == messageText("e.90343cedc288") {
		stable = "admin_forbidden"
	}
	if stable == "" {
		stable = "request_failed"
	}
	translated, _ := localizedMessage(msg, responseLanguage(w))
	json.NewEncoder(w).Encode(map[string]string{"error": translated, "code": stable, "message_key": key})
}
