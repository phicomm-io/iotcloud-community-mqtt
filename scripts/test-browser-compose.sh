#!/bin/sh
# Full local Compose/browser verification with private temporary data and random ports.
set -eu
project_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
if [ -n "${FRONTEND_ARCHIVE:-}" ]; then
 frontend_bundle=$(python3 "$project_root/scripts/frontend_release.py" --archive "$FRONTEND_ARCHIVE")
else
 frontend_bundle=$(python3 "$project_root/scripts/frontend_release.py")
fi
frontend_src="$frontend_bundle/source/frontend"
npm --prefix "$frontend_src" ci
if [ "${CI:-}" = true ]; then
 (cd "$frontend_src" && npx playwright install --with-deps chromium)
else
 (cd "$frontend_src" && npx playwright install chromium)
fi
test_runtime=$(mktemp -d)
test_project="phicomm-browser-$$"
compose() { HTTP_PORT=127.0.0.1:0 MQTT_PORT=127.0.0.1:0 PUBLIC_URL= MQTT_URI= MQTT_WS_URI= docker compose -p "$test_project" -f "$project_root/docker-compose.yaml" -f "$test_runtime/override.yaml" "$@"; }
cleanup() { if [ "$?" -ne 0 ]; then compose logs --tail 25 >&2 || true; fi; compose down >/dev/null 2>&1 || true; rm -rf "$test_runtime"; }
trap cleanup EXIT INT TERM
if [ -n "${FRONTEND_ARCHIVE:-}" ]; then
 (cd "$project_root" && docker compose build backend)
 cp "$project_root/nginx/nginx.conf" "$frontend_bundle/nginx.conf"
 docker build -f "$project_root/web/Dockerfile.offline" -t phicomm-community-mqtt-web:local "$frontend_bundle"
else
 (cd "$project_root" && docker compose build backend nginx)
fi
mkdir "$test_runtime/runtime"
docker run --rm --user "$(id -u):$(id -g)" -v "$test_runtime/runtime:/runtime" phicomm-community-mqtt-backend:local init --output /runtime
cat > "$test_runtime/override.yaml" <<YAML
services:
  mosquitto:
    user: "$(id -u):$(id -g)"
    restart: "no"
    volumes:
      - $test_runtime/runtime/broker:/mosquitto/security
      - $test_runtime/runtime/broker-data:/mosquitto/data
  backend:
    user: "$(id -u):$(id -g)"
    restart: "no"
    volumes:
      - $test_runtime/runtime/app:/data
      - $test_runtime/runtime/secrets/broker-password:/run/secrets/broker-password:ro
      - $test_runtime/runtime/secrets/admin-password:/run/secrets/admin-password:ro
      - $test_runtime/runtime/broker/certs/ca.crt:/run/broker-ca.crt:ro
  nginx:
    restart: "no"
YAML
compose up -d mosquitto backend nginx
test_address=$(compose port nginx 80)
# Wait for HTTP proxy and backend readiness, without touching existing services.
attempt=0
until curl -fsS "http://$test_address/api/register/status" >/dev/null 2>&1; do
 attempt=$((attempt+1)); if [ "$attempt" -ge 30 ]; then exit 1; fi
 sleep 1
done
export PHICOMM_WEB_URL="http://$test_address" PHICOMM_MQTT_WS="ws://$test_address/mqtt" PHICOMM_TEST_ADMIN_FILE="$test_runtime/runtime/secrets/admin-password"
(cd "$frontend_src" && npx playwright test)
