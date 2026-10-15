# Phicomm TMCP 平台

本目录与 TC1、M1、A1 固件一起管理。支持当前 TMCP 和 TC1 Legacy；不接受项目早期协议。TMCP 全称为 Tom MQTT Cloud Protocol。

未登录默认显示登录页，可选择账号登录或自建 Broker 模式。首页展示设备核心信息与日常控制；设置包含 MQTT 连接、设备管理、第三方 Broker 和账号安全。平台与设备共用 `common/web/style.css` 的设计变量，更新后运行 `python3 platform/scripts/sync-design.py` 同步。

账号主题按 MQTT 用户名编码隔离，设备上线公告触发登记与权限配置。密码仅保存在页面内存；刷新页面后，可在设置中重新输入 MQTT 密码。自建 Broker 模式的设备列表和连接地址保存在当前浏览器，支持 ws/wss。

## 初始化和部署

需要 Docker Compose。以下命令从本目录执行：

```sh
docker compose build backend nginx
docker compose --profile tools run --rm setup
# 首次管理员密码位于 runtime/secrets/admin-password，不要提交或公开。
docker compose up -d mosquitto backend nginx
```

默认 Web 为 80、MQTT 为 1883，可通过 `HTTP_PORT`、`MQTT_PORT` 修改。公网部署应配置 HTTPS、公开地址及安全 Cookie，并核对注册策略与 SMTP。后端只能通过可信 Nginx 访问；内部 Broker 管理使用独立账号和校验 CA 的 TLS 连接，不挂载 Docker socket。普通账号默认拒绝所有未授权主题。

备份时同时保存 SQLite、Dynamic Security JSON、配置和 secrets。只运行一份后端。Broker 操作失败时保留 pending/deleting 状态，重试完成后才视为成功，不能仅复制 SQLite 后宣称权限恢复。

## 验证

```sh
(cd backend && go test ./...)
(cd frontend && npm ci && npm test && npm run build && npx playwright install chromium)
scripts/test-browser-compose.sh
```

浏览器脚本自动创建独立 Broker、临时数据及随机端口，退出后清理。生产快照验证使用 `tests/tmcp-review.spec.js`，凭据通过私有文件环境变量提供；不得在公开 CI 使用生产快照。

- [迁移设计、验证与回退](docs/MIGRATION_TMCP.md)
- [TMCP 主题与接口](docs/MQTT_ACCOUNT_TOPICS.md)
- [统一账号与鉴权](docs/ACCOUNT_SECURITY.md)
