# Consume a verified prebuilt frontend; backend builds do not compile Vue.
FROM --platform=$BUILDPLATFORM python:3.12-alpine AS frontend
WORKDIR /app
COPY frontend.lock.json ./
COPY scripts/frontend_release.py ./scripts/
RUN python3 scripts/frontend_release.py --output /frontend-release

FROM nginx:alpine
COPY --from=frontend /frontend-release/platform /usr/share/nginx/html
COPY nginx/nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]
