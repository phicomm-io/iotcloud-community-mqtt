# ------ 构建阶段（始终在宿主平台运行 Node，无需模拟）------
FROM --platform=$BUILDPLATFORM node:22-alpine AS builder
WORKDIR /app
COPY frontend/package*.json ./frontend/
RUN npm ci --prefix frontend
COPY frontend/ ./frontend/
COPY common/ ./common/
COPY TC1/ ./TC1/
COPY A1/ ./A1/
COPY M1/ ./M1/
RUN npm run build --prefix frontend

# ------ 运行阶段 ------
FROM nginx:alpine
COPY --from=builder /app/frontend/dist /usr/share/nginx/html
COPY nginx/nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]
