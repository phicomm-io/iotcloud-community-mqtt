# Cloud RC4 deployment and wire isolation

## Current deployment status — 2026-10-30

PR #21 merged the discovery-bound codec, narrow M1 subscription and S30 publisher
ACLs as `d2c9ba0e134e48bbd766cbdbfd49179bdf8025be`. The equivalent source tree at
`16f2a93784315cd9dff59512a613d3dc3639ad14` was deployed to the existing backend.
Database, compose, broker security configuration and prior image were backed up.
The broker was not restarted, and account credentials were preserved. Health and
database checks passed. Host/race tests and platform host/browser CI passed.

S30 received its account-scoped role. With the independently tested S30 cache
memory fix installed, 20/20 full `status.get` requests over the production TLS
broker succeeded with zero busy/timeouts (P95 90.88 ms). Schema-3 descriptors and
live voltage/current/power/energy packets arrived on exact sensor topics. The
device stayed on the same boot with its relay OFF. This short check establishes
transport and schema acceptance, not metering accuracy, a load benchmark or
end-to-end physical M1 display acceptance. No M1 field PASS is asserted here.

The additional HA/native test and the historical review below were retained
from PR #20 so its unique wire-isolation evidence is not lost when the overlapping
runtime changes are integrated through PR #21.

## Historical pre-deployment assessment

Before that deployment, read-only field probes recorded these actual SUBACKs
(account/device identifiers omitted): `sensor/+/+` =128,
`sensor/+/metadata` =128, established `sensor/+/state` =1,
and `<device base>/ha/command` =1. Those observations supported the old broker ACL
mismatch; they describe the old deployment and are not current recovery evidence.

### Options considered before the complete deployment

1. **Restore the expanded M1 subscription only:** backport the four-line
   `subscribeLiteral <owned principal>/sensor/+/+` rule inside the existing M1
   account rules. Rebuild/restart only the backend with its existing database,
   Dynamic Security manager identity, and broker. The broker plugin supports this
   grant already; a broker binary/config update or restart is unnecessary.
   This permits subscription only. It does not by itself grant metadata receive
   or publisher rights missing from old main, so it cannot establish full remote
   display acceptance.
2. **Support state plus schema-3 metadata:** additionally backport the narrow
   registered sensor `/metadata` publisher rule and account-scoped metadata
   receive/subscription rules. Preserve the existing state rules. Broker transport
   remains byte-transparent; no JSON/MP conversion is required in the broker.
3. **Deploy the complete PR:** adds the discovery-bound codec store and Bemfa
   JSON/MP conversion alongside the ACL changes. This is a wider backend/database
   rollout, requiring backup of application data plus broker permissions/keys and
   verification of retained discovery, platform UI codec integration, and both
   old/new device flows. It is not equivalent to the four-line ACL-only backport.

The normal repository route is `feature/* -> dev -> main`. Existing main contains
release/tooling commits not yet in dev; this PR is based on dev to avoid bundling
those unrelated changes. A source merge alone does not alter production ACLs.
No release tag, image publication, or deployment is included.

## How existing and new rules refresh

`main.go` calls `Application.reconcile()` immediately and then every 10 seconds.
`reconcile()` calls `syncAccounts()` for **all active accounts**, even when all
of their devices are already active. `syncAccount()` sends Dynamic Security
`modifyRole` (or creates a missing account role) and attaches that canonical
account role to the owner's MQTT principals/aliases. This refresh is not limited
to newly registered accounts or newly online devices. It preserves credentials
and uses the existing broker management API.

The extra literal grant is generated only for accounts with an active/pending M1.
All their owned MQTT principals receive the same scoped filters; foreign
principals receive no grant. A1-only/TC1-only accounts do not receive the expanded
literal filter. Both legacy JSON M1 and new MP M1 get the grant because permissions
are model/account based, independent of wire encoding. Revocation is recalculated
when the last M1 is hidden/deleted. Broker failure or an earlier reconciliation
error delays completion; ten seconds is scheduling cadence, not a guaranteed SLA.

Device publisher roles are a separate refresh path. Active device roles are
updated by validated discovery reannouncements (`registerScopedDevice` calls
`syncScopedDevice` for existing active rows too). The complete deployment consumes
retained discovery after subscribing, while future discovery also refreshes the
roles. The reconciliation loop otherwise syncs non-active device rows only.
An existing active sensor with no retained/repeated discovery may therefore need
an explicit, authorized role reconciliation to obtain the new metadata publisher
rule; merely waiting for the account loop is insufficient for that role.

MQTT clients need to retry a previously denied subscription after policy refresh.
The ACL update cannot retroactively turn a rejected SUBACK into a live subscription.
Confirm the requested filter/code and core readiness after retry; do not infer
recovery from source deployment or a successful manager command alone.

## Firmware compatibility alternative (assessment only)

Using mandatory `sensor/+/state` plus an independent optional
`sensor/+/metadata` subscription is compatible with the established old filter.
A metadata SUBACK rejection should disable the optional external display-source
capability and surface its unavailable status without blocking core MQTT commands,
state, results, and device readiness. It must not silently call remote display ready.

This preserves MP display integrity only if the consumer retains its schema-3
metadata rules: accept complete binary state only after matching
`device_id/boot/descriptor_revision` metadata; invalidate stale descriptors on
reboot/revision change; never render values using old field names or units; do not
fall back to JSON or replay a control in another format. A fresh old broker that
denies metadata cannot provide schema-3 display until its narrow metadata ACLs are
updated. If a valid cached descriptor already matches exactly, its permitted use
must follow the canonical cache/lifetime contract, not a best-effort assumption.

Thus the split-subscription alternative improves basic MQTT availability on old
brokers, while the cloud ACL update enables the new display feature. They solve
related but different requirements and can coexist. This PR does not change firmware.

## Review evidence and limitations

The exact expanded filter is covered by `TestRealDynamicSecurityM1ExpandedSensorFilter`:
old-main-equivalent and pre-fix RC4 policies return SUBACK128; the narrow patch
returns QoS1; foreign filters still return128, and the expanded subscription does
not gain unregistered command receive access. See the committed real-broker/race
logs in `docs/test-results/rc4-cloud/expanded-filter*`.

Host tests and isolated broker simulation are not field-device acceptance. The
pinned platform frontend and protocol release locks remain unchanged in this PR;
full new-device browser acceptance and release pin coordination remain separate
integration gates. Neither the historical artifacts nor current CI should be
reported as a post-deployment M1 PASS.


## HA JSON and native MessagePack topic separation

The existing per-device `phicomm/v2/<owned principal>/<model>/<device>/#` role
already includes `/ha/command` and `/ha/state`, as well as native `/command` and
`/state`. No new HA ACL grant is needed. The same-account/alias access is
intentional; foreign device namespaces remain denied. ACLs restrict topics and
principals, not payload codecs. The platform is not claiming content validation
inside the broker.

The native manager relay subscribes to `phicomm/v2/+/+/+/state` and `/result`,
which do not match the additional `/ha/` level. Bemfa's route lookup also requires
the exact native device base. Its device decoder requires the codec selected by
discovery; JSON from HA cannot be accepted as native MessagePack state. HA stays
an independent JSON compatibility endpoint. Legacy native JSON devices remain
supported on their original native topics, selected by schema-1 discovery.

`TestRealDynamicSecurityHANativeWireSeparation` explicitly checks `/ha/command`,
`/ha/state`, native `/command`, and `/state` on an MP device using a real isolated
broker: owner SUBACK1, foreign SUBACK128, byte-identical HA JSON/native MP delivery,
only native state entering the manager relay, and native decoder rejection of HA
JSON. No production ACL broadening or firmware change was necessary.

Final isolated-broker extension: HA/native separation plus the exact M1 wildcard
regression each ran three times under `-race` (six PASS records,14.765s). The
immutable log is `docs/test-results/rc4-cloud/ha-native-race.log`. The default full
backend suite also passed locally (23.303s). Earlier GitHub host/browser, contract,
and branch-policy checks passed; subsequent pushes rerun the same CI gates.
