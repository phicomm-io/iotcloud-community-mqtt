# RC4 cloud simulated-device acceptance — 2026-10-28

Base: `17fa2a4` on `feat/rc4-cloud-wire`. No production broker, production deployment,
or physical device was used. Tests create independent temporary accounts and databases.

| Scenario | Before this acceptance | Result |
| --- | --- | --- |
| JSON and MP simultaneous, same platform account | Separate codec runs only | PASS: two mapped devices concurrently accept their fixed codec and return state/results |
| Other account with same MAC | ACL/mapping unit coverage | PASS: mapping rejected; foreign state cannot update mapped readings; real broker refuses foreign receive |
| Live JSON → MP → JSON discovery | Store-level test | PASS: next controls change codec without reloading worker/mapping |
| Malformed discovery and state; wrong-codec state | Parser-level rejection | PASS over TCP MQTT: selected codec and known readings remain unchanged |
| Busy response | No alternate codec in implementation | PASS: one write, no format fallback/replay; later new write succeeds |
| Offline control and quick recovery | Quick reconnect could lose state probe to previous connection's 1s limiter | FIXED and PASS: offline invalidates previous probe timer, dropped control stays dropped; new control works after recovery |
| Retained/stale controls and MQTT reconnect | Existing JSON/MP fixture tests | PASS in both formats, repeated under race detector |
| Mosquitto Dynamic Security real ACLs | Optional environment tests skipped in default suite | PASS with isolated `eclipse-mosquitto:2.0.22`, verified TLS management connection and two account clients |

## Recorded executions

- `full-suite.log`: `go test ./... -v -count=1 -timeout=120s`; PASS, 20.837s,
  106 PASS records including nested cases. Nine environment-gated tests skipped in this run.
- `fixture-race.log`: `go test -race -run 'TestCloudMixedWireDeviceMatrix|TestBemfaRealWireRelayIsolationAndNoReplay|TestDiscoveryCodecUpgradeDowngrade|TestBoundedMessagePack|TestRelayPreservesOpaqueBinary|TestMetadataACLScoped' -v -count=3 -timeout=120s`;
  PASS, 29.662s, 24 PASS records including nested codec cases, no race reports.
- `mosquitto.log`: isolated real-broker runs of `TestRealDynamicSecurityMixedCodecs`,
  `TestRealDynamicSecurityBemfaRelay`, `TestRealDynamicSecurityM1DisplaySources`;
  all three PASS, 14.863s. Uses `PHICOMM_TEST_RUNTIME` and `PHICOMM_TEST_BROKER`.
- `mosquitto-race.log`: real-broker mixed-codec test repeated three times with
  `-race`; all three PASS, 8.246s, no race reports.

The broker was bound only on localhost with an ephemeral host port and a newly generated
runtime. It was removed after testing. Credentials are excluded from these artifacts.

The mixed application fixture stubs only Dynamic Security command replies. Its discovery,
SQLite account/device registration, mapping authorization, codec selection, and Bemfa
worker are production code; both MQTT sides use real Paho/TCP connections. The separate
Mosquitto run verifies broker-enforced ACLs rather than inferring them from the stub.

Scope limits: no frontend/browser run, WebSocket-specific test, firmware flash, device P95,
physical controls, OTA installation, or production acceptance is claimed. The platform
HTTP/account API remains JSON. This lane does not own the firmware HTTP two-waiter limit.
