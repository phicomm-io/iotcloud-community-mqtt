# rc.4 cloud device transport

The broker transports device bytes unchanged. Account, login, integration configuration,
Dynamic Security, and platform administration HTTP APIs remain JSON. Device topics use
JSON for schema-1 firmware and MessagePack for schema-2 firmware.

## Selection

Only `phicomm/v2/u_<hex mqtt identity>/discovery/<normalized MAC>` selects a codec.
The topic identity must resolve to an active account/principal and the registered model
cannot change. A retained announcement cannot resurrect a removed device.

Legacy JSON announcements keep `schema:1`, `protocol:"tmcp"`, `protocol_version:2`,
`version:"v2"`, and `model`. New announcements are MessagePack root maps with
`schema:2`, the same protocol/version fields, plus `device_id` matching the topic,
nonempty printable `firmware_version` (64 bytes max), `variant` (32 bytes max),
`wire_version:1`, `wire_formats:["msgpack"]`, and `default_wire_format:"msgpack"`.

Codec and firmware identity persist per canonical account, MQTT principal, and MAC.
Every valid discovery replaces this record, including upgrade and downgrade. The
account device list exposes `wire_format`, `wire_version`, `firmware_version`, and
`variant`. A manually registered scoped device without validated discovery has no codec
and cannot issue commands through the server integration. Deletion clears the codec;
restoration waits for discovery again. Existing unscoped legacy devices remain JSON.

A device response must match its selected codec. Busy, timeout, authentication failure,
or broker reconnection must never trigger a write in another format. The Bemfa adapter
uses the selected codec for status probes, commands, state, and results. Standard
availability remains the literal MQTT `online`/`offline` payload.

MessagePack parsing is bounded to 8192 bytes, depth 12, 1024 nodes, unique UTF-8 map
keys, finite numbers, JavaScript-safe integers, and the device map profile. Binary,
extension values, ambiguous/prototype keys, trailing objects, NUL text, and invalid
UTF-8 are rejected before allocating a decoded object graph. Discovery is limited to
4096 bytes. This release adds no CBOR default or protocol guessing after discovery.

## Remote display

Registered sensors may publish `sensor/<MAC>/metadata` and `sensor/<MAC>/state`.
Existing account-scoped A1/M1 sensor readers may subscribe to both leaves; the ACL
never grants a foreign account or wildcard publisher. Manager subscriptions also
include capabilities and metadata and preserve opaque payload bytes.

The frontend owns the descriptor cache keyed by account/device/boot/revision; protocol
schema 3 uses retained metadata and nonretained complete snapshots. A frontend must
reject state without matching metadata rather than rendering an old descriptor.
The canonical schema and device-side validation belong to the protocols repository.

## Validation

`cd backend && go test ./... -count=1 -timeout=90s`

Coverage includes schema-1 to schema-2 upgrades and rollback, invalid identity rejection,
unknown-device command refusal, strict response codec binding, bounded malicious
MessagePack headers, metadata ACL isolation, and real TCP MQTT fixture tests for both
JSON and MessagePack Bemfa state/control/reconnect/no-replay behavior. Opaque binary
relay tests include embedded NUL and invalid UTF-8. Tests using a real Dynamic Security
broker remain gated by `PHICOMM_TEST_BROKER`; local fixture results are not production
or physical-device acceptance evidence.
