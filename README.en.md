# IoTCloud MQTT Platform

An MQTT management platform for TC1, M1, A1 and Daikin air monitors. It provides local platform accounts, device registration, access control, remote operation and Bemfa forwarding. Its Chinese/English WebUI can run with the platform or on static hosting.

[中文](README.md) · [User guides](https://phicomm-io.github.io/en/) · [Developer contracts](https://phicomm-io.github.io/iotcloud-community-protocols/en/)

## Features

- Shared website/MQTT accounts with independently changeable passwords, administrator roles, open or email-verified registration, and optional GitHub sign-in.
- Account-scoped MQTT topics and device permissions. Current firmware supports automatic discovery; TC1 Legacy supports manual registration. Existing v0.9.x devices remain supported.
- Chinese/English UI, request-language API messages and verification emails, with stable error codes and wire identifiers.
- Account device lists, guest mode, external WebSocket brokers, Home Assistant discovery and Bemfa forwarding.
- SQLite account/device storage and Mosquitto Dynamic Security permissions. Failed permission updates remain pending and are retried.

Device firmware is in [iotcloud-community-firmware](https://github.com/phicomm-io/iotcloud-community-firmware). The only WebUI source is [iotcloud-community-frontend](https://github.com/phicomm-io/iotcloud-community-frontend). The platform consumes a frontend Release pinned by its complete commit and SHA-256.

## Installation

Install Docker Compose and make HTTP/MQTT TCP ports available. From the repository directory:

```sh
mkdir -p runtime legacy
printf 'APP_UID=%s\nAPP_GID=%s\n' "$(id -u)" "$(id -g)" > .env
docker compose run --rm --build setup init --output /runtime
docker compose up -d --build mosquitto backend nginx
```

Open `http://server-address/`. The initial administrator is `admin`; read its random password locally from `runtime/secrets/admin-password`. Initialize only a new empty directory. Initialization does not replace existing configuration.

Configure each device with the server MQTT address, port 1883 and its platform account credentials. The browser uses the same origin's `/mqtt` WebSocket endpoint. `.env` accepts `HTTP_PORT`, `MQTT_PORT`, `PUBLIC_URL`, `MQTT_URI` and `MQTT_WS_URI`.

Use HTTPS/WSS for public deployments and set `cookie_secure: true` in `backend/config.yaml`. For a separate static frontend, list each exact origin in `allowed_origins`, such as `https://example.github.io`. Port 1883 is plaintext TCP for device compatibility and should stay on a trusted device network. Do not directly expose internal management TLS 8883, raw WebSocket 9001 or backend 8080.

The ignored `runtime/` directory contains databases, password hashes, certificates and keys. Back up application data, broker permissions and keys together. See [deployment and maintenance](docs/OPERATIONS.md) for configuration details.

## Usage and migration

Sign in to register or discover devices. Guest mode connects independently to a WebSocket broker. User-provided device names, notes and connection settings keep their original values when the display language changes.

New usernames and MQTT aliases contain 3–32 ASCII letters, digits, underscores, dots or hyphens. Verified email addresses are separate login aliases of up to 254 bytes. Existing long account names keep working and can add a short MQTT alias.

The platform accepts TMCP and TC1 Legacy. TMCP uses `protocol: tmcp` and `protocol_version: 2`; TC1's existing `ztc1` topics remain compatible. Repository and firmware names do not change these wire identifiers.

- [Accounts and authentication](docs/ACCOUNT_SECURITY.md) · [Account topics and discovery](docs/MQTT_ACCOUNT_TOPICS.md)
- [Migrating from tc1_mqtt, validation and rollback](docs/MIGRATION_TMCP.md)
- [GitHub sign-in](docs/GITHUB_LOGIN.md) · [Bemfa forwarding](docs/bemfa-cloud.md)

Historical v0.9.x source is frozen in [`archive/0.9.x`](https://github.com/phicomm-io/iotcloud-community-mqtt/tree/archive/0.9.x); published artifacts are preserved. Back up the existing deployment and validate an upgrade with separate data and ports before switching. New platform writes are not copied back to the old platform.

## Development and releases

```sh
(cd backend && go test -race ./... && go vet ./...)
python3 scripts/frontend_release.py
scripts/test-integration.sh
scripts/test-browser-compose.sh
```

The last two scripts use temporary brokers, separate data and random local ports. Frontend development and unit tests belong in the frontend repository. Run the pinned device simulator with `node scripts/device-simulator.mjs`.

Use `feature/* → dev → main`; see [CONTRIBUTING.md](CONTRIBUTING.md). Interface and configuration changes must cover old clients, migration, write failures and rollback. Cross-repository implementations pin the same specification through `protocols.lock.json`.

To update the frontend, copy `frontend.lock.json` from a verified `frontend-<complete commit>` Release, then verify its download and run platform/browser checks before committing. Offline `--archive` builds verify the same hash. Adjacent worktrees are not included automatically.

`VERSION` stores the numeric core, such as `1.0.0`. Matching `v1.0.0`, `v1.0.0-rc` or `v1.0.0-rc.N` tags on `main` publish GitHub Releases and amd64/arm64 GHCR images after checks pass. Candidates are prereleases, publish only their exact image tag and leave `latest` unchanged. PRs, `dev` and ordinary `main` pushes run checks only. Image names are `iotcloud-community-mqtt-backend` and `iotcloud-community-mqtt-web`; `release-manifest.json` records source, frontend pins and both image digests. See [operations](docs/OPERATIONS.md) for deployment and version inspection. The website consumes the locked [platform demo](https://phicomm-io.github.io/demos/platform/) from formal Releases only.

## License

[GPL-3.0](LICENSE). Third-party components retain their own licenses.
