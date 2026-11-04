FROM alpine:3.21 AS version-metadata
ARG BUILD_VERSION_JSON_BASE64
ARG BUILD_VERSION_JSON
RUN if [ -n "$BUILD_VERSION_JSON_BASE64" ]; then \
      printf '%s' "$BUILD_VERSION_JSON_BASE64" | base64 -d || exit 1; \
      printf '\n'; \
    elif [ -n "$BUILD_VERSION_JSON" ]; then \
      printf '%s\n' "$BUILD_VERSION_JSON"; \
    else \
      printf '%s\n' '{"schema":1,"version":"development","channel":"development","prerelease":false}'; \
    fi > /version.json

# Consume a verified prebuilt frontend; backend builds do not compile Vue.
FROM --platform=$BUILDPLATFORM python:3.12-alpine AS frontend
WORKDIR /app
COPY frontend.lock.json ./
COPY scripts/frontend_release.py ./scripts/
RUN python3 scripts/frontend_release.py --output /frontend-release

FROM nginx:alpine
COPY --from=frontend /frontend-release/platform /usr/share/nginx/html
COPY --from=version-metadata /version.json /usr/share/nginx/html/version.json
COPY nginx/nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]
