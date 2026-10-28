package main

import (
	"bytes"
	"math"
	"unicode/utf8"
)

// Validate lengths/depth before library decoding can allocate from an attacker
// supplied array header. Device messages contain finite floats but no tags or blobs.
func deviceBinaryPreflight(format string, data []byte) error {
	p, nodes := 0, 0
	read := func(n int) (uint64, error) {
		if n > len(data)-p {
			return 0, invalidDeviceWire()
		}
		var value uint64
		for i := 0; i < n; i++ {
			value = value<<8 | uint64(data[p])
			p++
		}
		return value, nil
	}
	var item func(int) (string, bool, error)
	item = func(depth int) (string, bool, error) {
		nodes++
		if depth > 12 || nodes > 1024 || p >= len(data) {
			return "", false, invalidDeviceWire()
		}
		tag := data[p]
		p++
		kind := ""
		var n uint64
		var err error
		if format == "msgpack" {
			switch {
			case tag < 0x80 || tag >= 0xe0 || tag == 0xc0 || tag == 0xc2 || tag == 0xc3:
				return "", false, nil
			case tag&0xe0 == 0xa0:
				kind = "text"
				n = uint64(tag & 31)
			case tag&0xf0 == 0x80:
				kind = "map"
				n = uint64(tag & 15)
			case tag&0xf0 == 0x90:
				kind = "array"
				n = uint64(tag & 15)
			case tag == 0xca || tag == 0xcb:
				width := 4
				if tag == 0xcb {
					width = 8
				}
				bits, e := read(width)
				value := math.Float64frombits(bits)
				if width == 4 {
					value = float64(math.Float32frombits(uint32(bits)))
				}
				if e != nil || math.IsNaN(value) || math.IsInf(value, 0) {
					return "", false, invalidDeviceWire()
				}
				return "", false, nil
			case tag >= 0xcc && tag <= 0xcf:
				n, err = read(1 << uint(tag-0xcc))
				if err != nil || n > uint64(deviceSafeInteger) {
					return "", false, invalidDeviceWire()
				}
				return "", false, nil
			case tag >= 0xd0 && tag <= 0xd3:
				width := 1 << uint(tag-0xd0)
				bits, e := read(width)
				if e != nil {
					return "", false, e
				}
				if width < 8 && bits&(uint64(1)<<uint(width*8-1)) != 0 {
					bits |= ^uint64(0) << uint(width*8)
				}
				value := int64(bits)
				if value < -deviceSafeInteger || value > deviceSafeInteger {
					return "", false, invalidDeviceWire()
				}
				return "", false, nil
			case tag >= 0xd9 && tag <= 0xdb:
				kind = "text"
				n, err = read(1 << uint(tag-0xd9))
			case tag == 0xdc || tag == 0xdd:
				kind = "array"
				n, err = read(1 << uint(tag-0xdb))
			case tag == 0xde || tag == 0xdf:
				kind = "map"
				n, err = read(1 << uint(tag-0xdd))
			default:
				return "", false, invalidDeviceWire()
			}

		} else {
			return "", false, invalidDeviceWire()
		}
		if err != nil {
			return "", false, err
		}
		if kind == "text" {
			if n > 8192 || n > uint64(len(data)-p) {
				return "", false, invalidDeviceWire()
			}
			value := data[p : p+int(n)]
			p += int(n)
			if !utf8.Valid(value) || bytes.IndexByte(value, 0) >= 0 {
				return "", false, invalidDeviceWire()
			}
			return string(value), true, nil
		}
		limit := uint64(1024)
		if kind == "array" {
			limit = 1024
		}
		if n > limit {
			return "", false, invalidDeviceWire()
		}
		keys := map[string]bool{}
		for i := uint64(0); i < n; i++ {
			if kind == "map" {
				key, text, err := item(depth + 1)
				if err != nil || !text || keys[key] || key == "__proto__" || key == "constructor" || key == "prototype" {
					return "", false, invalidDeviceWire()
				}
				keys[key] = true
			}
			if _, _, err := item(depth + 1); err != nil {
				return "", false, err
			}
		}
		return "", false, nil
	}
	_, _, err := item(0)
	if err != nil || p != len(data) {
		return invalidDeviceWire()
	}
	return nil
}
