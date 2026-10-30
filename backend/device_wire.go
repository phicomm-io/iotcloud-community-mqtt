package main

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/vmihailenco/msgpack/v5"
)

const deviceSafeInteger int64 = 9007199254740991

func invalidDeviceWire() error { return errors.New("invalid device wire message") }

// Detection only accepts a root map, matching the firmware wire profile.
func deviceWireFormat(data []byte) string {
	if len(data) == 0 {
		return "json"
	}
	c := data[0]
	if c >= 0x80 && c <= 0x8f || c == 0xde || c == 0xdf {
		return "msgpack"
	}
	return "json"
}

// Binary decoding is bounded before allocating a graph from untrusted headers.
// Rebinding via JSON gives existing cloud consumers identical field names and
// float64 numeric semantics for both old JSON and new binary firmware.
func decodeDeviceWire(data []byte, out any) error {
	if len(data) == 0 || len(data) > 8192 {
		return invalidDeviceWire()
	}
	format := deviceWireFormat(data)
	if format == "json" {
		return json.Unmarshal(data, out)
	}
	if err := deviceBinaryPreflight(format, data); err != nil {
		return err
	}
	var value any
	if format == "msgpack" {
		dec := msgpack.NewDecoder(bytes.NewReader(data))
		dec.SetMapDecoder(func(d *msgpack.Decoder) (interface{}, error) { return d.DecodeMap() })
		if err := dec.Decode(&value); err != nil {
			return err
		}
	} else {
		return invalidDeviceWire()
	}
	if _, ok := value.(map[string]any); !ok {
		return invalidDeviceWire()
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(canonical, out)
}
func encodeDeviceWire(format string, value any) ([]byte, error) {
	switch format {
	case "msgpack":
		return msgpack.Marshal(value)
	case "json":
		return json.Marshal(value)
	default:
		return nil, invalidDeviceWire()
	}
}

func validWireIdentity(s string, limit int) bool {
	if len(s) == 0 || len(s) > limit {
		return false
	}
	for _, c := range s {
		if c < 0x21 || c > 0x7e {
			return false
		}
	}
	return true
}

// Only discovery chooses a codec. A write never guesses or falls back after an error.
func (s *Store) deviceCodec(d Device) (string, error) {
	if d.Namespace == "" {
		if d.Model == "s30-v1b" {
			return "", errUnavailable
		}
		return "json", nil
	}
	var format string
	err := s.QueryRow("SELECT format FROM device_wire WHERE username=? AND mqtt_username=? AND mac=?", d.Username, topicUsername(d), d.MAC).Scan(&format)
	if err != nil || (format != "json" && format != "msgpack") || (d.Model == "s30-v1b" && format != "msgpack") {
		return "", errUnavailable
	}
	return format, nil
}
func (s *Store) decodeDevice(d Device, data []byte, out any) error {
	format, err := s.deviceCodec(d)
	if err != nil {
		return err
	}
	if deviceWireFormat(data) != format {
		return invalidDeviceWire()
	}
	return decodeDeviceWire(data, out)
}
