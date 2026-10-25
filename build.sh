#!/usr/bin/env bash
# Build locally. Publishing and deploying are separate operations.
set -euo pipefail
TAG="${1:-local}"
docker build -t "iotcloud-community-mqtt-backend:${TAG}" backend
docker build -t "iotcloud-community-mqtt-web:${TAG}" -f web/Dockerfile .
