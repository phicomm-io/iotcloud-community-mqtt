#!/bin/sh
# Isolated local verification only; never mounts the Docker socket into a container.
set -eu
project_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
test_runtime=$(mktemp -d)
test_container="phicomm-mqtt-test-$$"
cleanup() { if [ "$?" -ne 0 ]; then docker logs --tail 20 "$test_container" >&2 || true; fi; docker rm -f "$test_container" >/dev/null 2>&1 || true; rm -rf "$test_runtime"; }
trap cleanup EXIT INT TERM
(cd "$project_root/backend" && go build -o "$test_runtime/backend" .)
"$test_runtime/backend" init --output "$test_runtime/runtime"
docker run -d --name "$test_container" --user "$(id -u):$(id -g)" -p 127.0.0.1::8883 \
 -v "$project_root/mosquitto/config/mosquitto.conf:/mosquitto/config/mosquitto.conf:ro" \
 -v "$test_runtime/runtime/broker:/mosquitto/security" \
 -v "$test_runtime/runtime/broker-data:/mosquitto/data" eclipse-mosquitto:2.0.22 >/dev/null
test_port=$(docker port "$test_container" 8883/tcp | cut -d: -f2)
export PHICOMM_TEST_RUNTIME="$test_runtime/runtime" PHICOMM_TEST_BROKER="ssl://localhost:$test_port"
(cd "$project_root/backend" && go test -v -run TestRealDynamicSecurity .)
(cd "$project_root/backend" && PHICOMM_TEST_PHASE=seed go test -v -run 'TestRealPersistence|TestRealDynamicSecurityHADiscovery' .)
docker restart "$test_container" >/dev/null
test_port=$(docker port "$test_container" 8883/tcp | cut -d: -f2)
export PHICOMM_TEST_BROKER="ssl://localhost:$test_port"
(cd "$project_root/backend" && PHICOMM_TEST_PHASE=verify go test -v -run 'TestRealPersistence|TestRealDynamicSecurityHADiscovery' .)
# Verify real Mosquitto passwd hashes without ever needing the user's original passwords.
mkdir "$test_runtime/legacy"
docker run --rm --user "$(id -u):$(id -g)" -v "$test_runtime/legacy:/legacy" eclipse-mosquitto:2.0.22 \
 mosquitto_passwd -b -c /legacy/passwd legacy_test original-test-password
printf 'user legacy_test\ntopic readwrite device/ztc1/AA:BB:CC:DD:EE:FF/#\nuser legacy_test\ntopic readwrite homeassistant/+/AABBCCDDEEFF/#\n' > "$test_runtime/legacy/acl"
"$test_runtime/backend" migrate --output "$test_runtime/migrated" --passwd "$test_runtime/legacy/passwd" --acl "$test_runtime/legacy/acl"
docker rm -f "$test_container" >/dev/null
docker run -d --name "$test_container" --user "$(id -u):$(id -g)" -p 127.0.0.1::8883 \
 -v "$project_root/mosquitto/config/mosquitto.conf:/mosquitto/config/mosquitto.conf:ro" \
 -v "$test_runtime/migrated/broker:/mosquitto/security" \
 -v "$test_runtime/migrated/broker-data:/mosquitto/data" eclipse-mosquitto:2.0.22 >/dev/null
test_port=$(docker port "$test_container" 8883/tcp | cut -d: -f2)
export PHICOMM_TEST_RUNTIME="$test_runtime/migrated" PHICOMM_TEST_BROKER="ssl://localhost:$test_port" PHICOMM_TEST_MIGRATED=1
(cd "$project_root/backend" && go test -v -run TestRealMigration .)
