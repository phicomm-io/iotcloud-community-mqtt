# M1 expanded sensor subscription compatibility

Source baseline: cloud main `0ec9324`, MP implementation `17fa2a4`, acceptance `0912db9`.
This diagnosis changes no production broker or DUT.

## Confirmed source gap

Old cloud main grants `subscribePattern sensor/+/state`. Commit `0912db9` also
grants `subscribePattern sensor/+/metadata`, but neither authorizes the broader
requested MQTT filter `sensor/+/+`. The existing M1 real-broker acceptance test
subscribes only to `sensor/+/state`; its previous PASS did not cover this new filter.

On an isolated real Mosquitto 2.0.22 broker, requested QoS is deliberately 1:

| Account policy | Requested suffix | Actual SUBACK |
| --- | --- | --- |
| Old main equivalent | `sensor/+/state` | 1 |
| Old main equivalent | `sensor/+/+` | 128 (rejected) |
| 0912 equivalent, metadata separately permitted | `sensor/+/metadata` | 1 |
| 0912 equivalent | `sensor/+/+` | 128 (rejected) |
| Minimal patch | `sensor/+/+` | 1 |
| Minimal patch, foreign namespace | `sensor/+/+` | 128 (rejected) |

The patch adds only `subscribeLiteral sensor/+/+` for accounts containing an active
or pending M1. It does not grant `publishClientReceive sensor/+/+`. Existing scoped
state/metadata receive ACLs remain responsible for message delivery. The real broker
test verifies that a manager-injected command for an unregistered sensor is not
received through this filter, while registered state and metadata are received.
Unit tests retain M1 principal scoping and remove this grant when M1 is removed.

## Field evidence boundary

Luna could read `waiting_acl`, a rising MQTT error counter, and `session: -1`, but
there was no readable SUBACK/filter trace in device logs. The matching source gap
is confirmed; the field rejection's precise filter/code is not captured, so this
report does not claim that this is the proven sole cause of the DUT failure.
This reproduction is independent of any MQTT QoS enum bug: the test passes explicit
QoS1 and inspects the actual broker SUBACK bytes exposed by Paho.

## Deployment and compatibility choices

The minimal cloud patch requires normal platform deployment and account ACL
reconciliation before the new grant reaches a running production broker. No such
action was performed here. Merely merging source does not repair persisted ACLs.

For firmware compatibility with an unchanged old cloud, keep the established
`+/state` subscription and request metadata separately. Treat an unavailable
optional display-source subscription as a capability limitation rather than
blocking core control MQTT readiness. Full schema-3 remote display still requires
metadata receive permission and therefore a corresponding platform update.

## Evidence

`expanded-filter.log`: three repetitions of the real-broker test and the M1 scope/
revocation unit test under `-race`; all six cases PASS, 9.036s. It records sanitized
filter names and SUBACK codes without credentials. `expanded-filter-full-suite.log`
records the complete default backend test run. The broker is an independent
localhost-only disposable container; its generated runtime is removed afterward.
