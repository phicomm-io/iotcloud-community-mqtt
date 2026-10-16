# Phicomm TMCP 平台

本目录与 TC1、M1、A1 固件一起管理。支持当前 TMCP 和 TC1 Legacy；不接受项目早期协议。TMCP 全称为 Tom MQTT Cloud Protocol。

未登录默认显示登录页，可选择账号登录或自建 Broker 模式。首页展示设备核心信息与日常控制；设置包含 MQTT 连接、设备管理、第三方 Broker 和账号安全。平台直接使用 `common/web/style.css` 的公共样式，登录、注册、管理和设置均沿用设备 WebUI 的设计语言。设备控制区域从 TC1、M1、A1 的首页源文件生成，只运行首页组件，不嵌入设备端导航与设置。平台通过 MQTT 适配命令接口，Legacy TC1 复用同一首页布局；未收到状态时禁用控制。设备端专有设置入口会提示前往设备本地 WebUI。

账号主题按 MQTT 用户名编码隔离，设备上线公告触发登记与权限配置。登录页兼容旧版 `mqtt_config`，预填浏览器保存的 MQTT 凭据并提示平台更新。账号与设备归属由服务端迁移数据保留，首次登录沿用原密码。平台 MQTT 密码保存在当前浏览器 localStorage，刷新或重新打开后自动连接，退出或会话失效即清除。默认自动连接，手动断开偏好按账号保存；兼容旧版 `mqtt_auto_connect=false`。自建 Broker 模式的设备列表和连接地址保存在当前浏览器，支持 ws/wss。

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

可独立启动设备 WebUI 模拟器，不需要设备或 MQTT Broker：

```sh
SIM_HOST=0.0.0.0 SIM_PORT=25174 node scripts/device-simulator.mjs
```

访问 `/tc1`、`/a1` 或 `/m1` 打开对应设备页面。模拟器提供设备名称恢复、TC1 插座控制、A1 风速与光效配置、M1 亮度控制；A1 光效设置页可调节速度并查看连续光环预览。

每种型号共用一份内存状态，刷新页面保留配置，重启模拟器后恢复默认值。模拟风速立即生效，用于检查灯光反馈，不模拟电机升降速或实体硬件时序。

```sh
(cd backend && go test ./...)
(cd frontend && npm ci && npm test && npm run build && npx playwright install chromium)
scripts/test-browser-compose.sh
```

浏览器脚本自动创建独立 Broker、临时数据及随机端口，退出后清理。生产快照验证使用 `tests/tmcp-review.spec.js`，凭据通过私有文件环境变量提供；不得在公开 CI 使用生产快照。

- [迁移设计、验证与回退](docs/MIGRATION_TMCP.md)
- [TMCP 主题与接口](docs/MQTT_ACCOUNT_TOPICS.md)
- [统一账号与鉴权](docs/ACCOUNT_SECURITY.md)

## 内置访问统计

管理员页面提供今天、近 7/30/90 天的浏览量、匿名访客、访问次数、近 5 分钟活跃、每日趋势、来源域名、浏览器、操作系统、屏幕类型与设备型号访问。当前有效登记的型号数量单独展示，不随时间筛选。口径参考 [Umami 指标说明](https://docs.umami.is/docs/metric-definitions)，平台自行收集和存储，无外部分析服务依赖。

前端在路由切换、设置页切换、成功登录、MQTT 建立连接和切换设备时异步上报；不等待统计接口，不影响控制。匿名访客由当前浏览器随机标识去重，30 分钟无事件后开始新访问；浏览器清理存储或更换浏览器会被视为新访客。数据保留 90 天，按 UTC 自然日统计，启用前的历史访问不会补算。设备型号访问是查看次数，不代表独立实物数量；当前登记数来自平台设备表。

仅 `/api/analytics/event` 接受公开同源写入，字段和事件类型受白名单限制并限流。`/api/admin/analytics?days=7` 使用现有管理员会话鉴权。统计表仅保存哈希后的随机访客/访问标识、服务器时间、固定页面类别、型号及来源域名与客户端分类；不保存账号、MAC、IP、完整来源 URL、密码或控制参数。该统计衡量浏览器上报访问，不是经审计的计费指标。
