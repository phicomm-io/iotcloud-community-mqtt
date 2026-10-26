# 部署与维护 / Deployment and maintenance

[中文首页](../README.md) · [English overview](../README.en.md)

## 账号、数据与备份

管理员使用普通登录页，其 `admin` 角色只授予平台管理权限，不授予 Broker 管理主题。网站与 MQTT 可独立改密；修改 MQTT 密码后，应同步更新设备和浏览器连接凭据。管理员可配置注册模式、管理设备归属及角色；最后一个管理员不能被删除或降级。

新设备按账号主题发现和登记；旧纯 MAC 主题手动绑定，解绑权限撤销完成后才释放 MAC。平台只保存设备登记和权限信息，实时状态仍来自 MQTT。浏览器内的访客列表、备注和 MQTT 连接凭据与账号设备列表分开保存，登录不会自动上传本地列表。

应用数据库、Broker Dynamic Security JSON、Broker 持久化数据和 `runtime/secrets/` 必须一起备份恢复；巴法云密钥加密使用的数据文件也应保存。请停止写入后取得一致快照。不要多实例运行后端，当前同步锁和限流按单实例设计。

Administrators use the regular sign-in page. Platform roles do not grant access to MQTT management topics. Back up and restore the application database, broker permissions/data and keys together. Run a single backend instance. Guest lists and browser MQTT credentials remain local to that browser.

## 网络与独立静态前端

在前置代理终止 HTTPS，将 `PUBLIC_URL` 设置为本站完整 HTTPS 来源，启用 `cookie_secure`。`trust_proxy: true` 仅适用于后端只可由可信 Nginx 访问的部署；直接开放后端时应关闭。内部 Broker 管理连接校验 CA 和服务器证书，初始化证书覆盖 `mosquitto`、`localhost`，有效期两年；外部 MQTT TLS 需要单独配置监听和证书。

独立静态前端的来源逐项加入后端配置：

```yaml
allowed_origins:
  - https://your-account.github.io
```

来源只包含 scheme、host 和可选端口，不含路径或末尾 `/`。后端保留 Cookie 登录，并支持同一可撤销会话的 Bearer 传递；白名单前端可读取 `X-IotCloud-Session`。CORS 不授予账号或 MQTT 权限。

Terminate HTTPS at a trusted proxy and use HTTPS/WSS publicly. List exact static frontend origins in `allowed_origins`; CORS does not grant account or MQTT permissions. Enable `trust_proxy` only when the backend is reachable through a trusted proxy that replaces client-IP headers.

## 节点与邮箱

| 节点 / Node | 网站 / API | 设备 MQTT | 浏览器 MQTT |
| --- | --- | --- | --- |
| China | https://mqtt.fjrcn.cn | mqtt.fjrcn.cn:1883 | wss://mqtt.fjrcn.cn/mqtt |
| US | https://mqtt.fjrcn.com | mqtt.fjrcn.com:1883 | wss://mqtt.fjrcn.com/mqtt |

两个节点独立注册、登录和绑定设备，不会合并同名账号或复制密码、设备和会话。新增节点需在新服务器初始化独立 `runtime/`，不能复制其他节点的 secrets。参考 `deploy/node.env.example` 和 `deploy/nginx-node.conf.example`。

邮箱验证码注册需配置 `backend/config.yaml` 的 SMTP，支持 TLS 直连或 STARTTLS。验证码五分钟有效、一次性使用；成功重发会撤销旧码，发送失败保留原有效码。管理员可查看近 90 天发送/注册记录及尚有效的验证码。发送记录表示 SMTP 接受情况，不代表进入收件箱；邮件和界面使用请求语言。

Nodes have independent accounts, credentials, devices and sessions. Initialize separate runtime data for each node. Email registration requires SMTP with TLS or STARTTLS. Codes expire after five minutes and can be used once. The administrator history retains 90 days; mail-server acceptance does not prove inbox delivery.

## 公告、统计与巴法云

默认关闭公告。可在配置文件或管理员页面管理，数据库保存值优先于 YAML。正文最多 300 字，按钮最多 40 字，链接支持 HTTP/HTTPS；`audience` 为 `before_login` 或 `after_login`。公告属于管理员输入内容，语言切换不会改写正文。

内置统计按 UTC 聚合，保留 90 天，只记录匿名访客/访问哈希、固定页面与型号、来源域名及客户端类别；不记录账号、MAC、IP、完整来源 URL 或控制参数。统计和设备登记数量分别展示。

巴法云使用用户自己的密钥，由后台中转；设备继续连接原 MQTT。配置、主题类型、密钥备份及验证见[巴法云转发](bemfa-cloud.md)。

Announcements are disabled by default and preserve administrator-provided text. Analytics retains anonymous aggregates for 90 days and does not store account names, MAC/IP addresses, complete referrers or command parameters. See the Bemfa guide for per-account forwarding and encrypted key storage.

## 预构建镜像与更新

发布标签通过 CI 发布 `ghcr.io/phicomm-io/iotcloud-community-mqtt-backend` 和 `ghcr.io/phicomm-io/iotcloud-community-mqtt-web`。`VERSION` 只保存数字核心版本；标签须匹配该版本并指向已合入 `main` 的同一干净提交。支持 `v1.0.0`、`v1.0.0-rc` 和 `v1.0.0-rc.N`，编号允许 `0`，不允许前导零。候选版是非 latest 的 GitHub prerelease，镜像仅发布精确候选标签，不修改稳定版的 `latest`。使用已发布版本或工作流记录的 digest；不要把同一标签当作可替换的镜像。

```sh
export IMAGE_TAG=v1.0.0-rc # 试用候选版；按需选择已发布版本 / select a published version
# 保留现有 runtime 和配置 / retain existing runtime data and configuration
docker compose -f docker-compose.yaml -f compose.ghcr.yaml pull backend nginx
docker compose -f docker-compose.yaml -f compose.ghcr.yaml up -d --no-build --no-deps backend nginx
```

镜像发布与部署分开，不会自动重启服务器。GHCR 包需要公开读取权限；私有包拉取可使用当前账号的 `read:packages` 凭据，禁止将令牌写入仓库。

Release 的 `release-manifest.json` 绑定完整源码提交、前端与协议锁，以及两个多架构镜像的 digest。镜像内 `/app/version.json`（backend）和站点 `/version.json`（web）提供同一准确版本及锁信息；普通本地构建显示 `development`。前端继续使用独立的 `frontend-<完整commit>` 制品，不另打平台 RC 版本。试用候选版时显式设置例如 `IMAGE_TAG=v1.0.0-rc`，并保留原有配置和数据。

已存在同名候选 Release 时，CI 在构建镜像前停止；应复用原 manifest/digest，修改后发布新的 `rc.N`。候选资产补传只允许增加缺失文件，已有文件须逐字节相同；冲突会失败，不覆盖已公开资产。

Publishing and deployment are separate. `VERSION` contains the numeric core; release tags must match it and the exact clean source on `main`. `-rc` and `-rc.N` candidates are GitHub prereleases, never latest, and publish only their exact image tags. An existing candidate Release stops image rebuilding: reuse its original manifest/digests or publish a new `rc.N`. Existing candidate assets must match byte for byte; only missing assets can be added. `release-manifest.json` binds source, frontend/protocol pins and both multi-architecture image digests. Inspect `/app/version.json` inside the backend image or `/version.json` on the web service; ordinary local builds identify as `development`. The frontend keeps its independent immutable commit release. Set an explicit candidate `IMAGE_TAG` when testing, retain configuration/data, and verify login, MQTT permissions, device status and rollback before changing a production endpoint.
