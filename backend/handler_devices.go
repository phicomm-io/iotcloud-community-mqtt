package main

import (
	"net/http"
	"strings"
)

func handleDevices(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	switch r.Method {
	case "GET":
		list, err := app.store.devices(u)
		if err != nil {
			operationError(w, err)
			return
		}
		for i := range list {
			if list[i].Namespace == "" && list[i].Version == "v1" {
				list[i].Username = ""
			}
		}
		jsonResp(w, list)
	case "POST":
		var d Device
		if err := decode(r, &d); err != nil {
			jsonError(w, err.Error(), 400)
			return
		}
		if d.Username != "" || d.Status != "" || (d.Namespace != "" || d.MQTTUsername != "") {
			jsonError(w, messageText("e.6c2899d8f501"), 400)
			return
		}
		if err := validateDevice(&d); err != nil {
			jsonError(w, err.Error(), 400)
			return
		}
		if err := app.addDevice(u, d); err != nil {
			operationError(w, err)
			return
		}
		jsonResp(w, map[string]string{"message": messageText("e.f7bb1748ec1e")})
	default:
		jsonError(w, messageText("e.4f34574afee4"), 405)
	}
}
func handleDevice(w http.ResponseWriter, r *http.Request) {
	mac := strings.TrimPrefix(r.URL.Path, "/api/devices/")
	if strings.HasPrefix(mac, "scoped/") {
		handleScopedDevice(w, r, strings.TrimPrefix(mac, "scoped/"))
		return
	}
	if !isValidMAC(mac) {
		jsonError(w, messageText("e.d87efef93be5"), 400)
		return
	}
	mac = normalizeMAC(mac)
	switch r.Method {
	case "PUT":
		var d Device
		if err := decode(r, &d); err != nil {
			jsonError(w, err.Error(), 400)
			return
		}
		if d.MAC != "" && normalizeMAC(d.MAC) != mac || d.Username != "" || d.Status != "" || (d.Namespace != "" || d.MQTTUsername != "") {
			jsonError(w, messageText("e.449ee80d2263"), 400)
			return
		}
		d.MAC = mac
		if err := validateDevice(&d); err != nil {
			jsonError(w, err.Error(), 400)
			return
		}
		if err := app.saveDevice(currentUser(r), d, true); err != nil {
			operationError(w, err)
			return
		}
		jsonResp(w, map[string]string{"message": messageText("e.0a7684920170")})
	case "DELETE":
		if err := app.removeDevice(currentUser(r), mac); err != nil {
			operationError(w, err)
			return
		}
		jsonResp(w, map[string]string{"message": messageText("e.507b4da76f2f")})
	default:
		jsonError(w, messageText("e.4f34574afee4"), 405)
	}
}
