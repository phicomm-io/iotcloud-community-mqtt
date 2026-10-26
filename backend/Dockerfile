FROM alpine:3.21 AS version-metadata
ARG BUILD_VERSION_JSON
RUN if [ -n "$BUILD_VERSION_JSON" ]; then \
      printf '%s\n' "$BUILD_VERSION_JSON"; \
    else \
      printf '%s\n' '{"schema":1,"version":"development","channel":"development","prerelease":false}'; \
    fi > /version.json

FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder
ARG TARGETARCH
ARG TARGETOS=linux
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o mqtt_mgr .
FROM alpine:3.21
RUN apk add --no-cache ca-certificates && addgroup -g 1000 app && adduser -D -u 1000 -G app app
WORKDIR /app
COPY --from=builder /app/mqtt_mgr .
COPY --from=version-metadata /version.json /app/version.json
COPY config.yaml .
USER app
EXPOSE 8080
ENTRYPOINT ["./mqtt_mgr"]
