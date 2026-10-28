# Cloud RC4 source review and rollout boundary

This change is source only. It does not deploy a backend, change a running broker,
publish an image, merge a branch, or declare the field M1 accepted. Field M1 remains
`waiting_acl`/BLOCKED until actual post-change runtime evidence exists. The device
logs available in this session did not expose its rejected SUBACK filter/code.

## Smallest production options (not performed)

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
