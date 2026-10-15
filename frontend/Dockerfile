# ------ 构建阶段（始终在宿主平台运行 Node，无需模拟）------
FROM --platform=$BUILDPLATFORM node:22-alpine AS builder
WORKDIR /platform/frontend
COPY platform/frontend/package*.json ./
RUN npm ci
COPY platform/frontend/ ./
COPY common/web/ /common/web/
COPY TC1/http_server/web/ /TC1/http_server/web/
COPY A1/web/ /A1/web/
COPY M1/web/ /M1/web/
RUN npm run build

# ------ 运行阶段 ------
FROM nginx:alpine
COPY --from=builder /platform/frontend/dist /usr/share/nginx/html
COPY platform/nginx/nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]
