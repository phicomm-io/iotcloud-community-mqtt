# phicomm-community-mqtt

支持 [phicomm-community-firmware](https://github.com/phicomm-io/phicomm-community-firmware) 的 MQTT 远程集中管理平台。设备显示为 **tc1(a1)**、**m1**、**a1**。tc1(a1) 表示 TC1 的 A1 硬件版本，与 A1 净化器是不同型号。

平台支持当前 TMCP 和 TC1 Legacy；不接受项目早期协议。TMCP 全称为 Tom MQTT Cloud Protocol。

本仓库原为固件仓库的 `platform/` 子目录，2026 年再次分离为独立仓库，历史由原
[phicomm-community-mqtt](https://github.com/FUjr/phicomm-community-mqtt) 与固件仓库的 `platform/` 合并而成。
设备 WebUI、Vue 管理平台和 demo 的唯一源码在 [phicomm-community-frontend](https://github.com/phicomm-io/phicomm-community-frontend)。本仓库只保留服务端、Broker、nginx 和部署配置，通过 `frontend.lock.json` 固定前端完整 commit 与 Release SHA-256。平台镜像从该 Release 读取预构建 `platform/`，不从 main 或 latest 构建。

## 账号与设备

- 网站登录与 MQTT 使用统一账号，密码可独立修改。浏览器保存 MQTT 密码后可自动连接，断网后自动重连；连接状态可见，重连/断开操作收在“连接设置”二级菜单。可先注册，再绑定设备；支持开放注册、邮箱验证码注册及关闭注册。
- 登录后设备列表保存到服务器 SQLite，可跨浏览器恢复。云端仅保存 MAC、型号、协议版本、归属和同步状态，不保存备注、MQTT 明文密码或实时数据。
- 新账号主题按“用户名 + MAC”隔离，固件发布发现声明即可自动加入云端列表并配置 ACL；同一 MAC 可属于多个账号的新主题。
- 旧纯 MAC 主题仍需手动绑定，全局先到先得；解绑完成后撤销权限并释放 MAC，待同步期间不会允许他人绑定。旧主题不会自动发现。
- 未登录时默认进入登录页，点击“进入访客模式”可独立使用本地设备列表，即使平台账号接口不可用也可进入。访客可连接任意 WebSocket MQTT 服务，本地列表、备注和连接配置继续保存在浏览器。登录不会自动上传本地设备；可明确点击“导入本地设备到账号”，不会覆盖云端列表。
- 登录页兼容旧版 `mqtt_config`，预填浏览器保存的 MQTT 凭据并提示平台更新。平台 MQTT 密码保存在当前浏览器 localStorage，刷新或重新打开后自动连接，退出或会话失效即清除。默认自动连接，手动断开偏好按账号保存；兼容旧版 `mqtt_auto_connect=false`。自建 Broker 模式的设备列表和连接地址保存在当前浏览器，支持 ws/wss。
- [统一账号与鉴权说明](docs/ACCOUNT_SECURITY.md)：管理员是统一账号中的 `admin` 角色，与网站及 MQTT 使用同一用户名、密码和网站会话。仅有管理权限时显示管理入口；可创建、重置或删除账号、授予/撤销管理角色、管理设备归属及注册模式。后端每次请求查询有效会话和当前角色，写请求执行同源校验；角色撤销立即阻止后续管理请求，最后一个管理员不能删除或降级。旧独立管理员登录和 Bearer token 已移除。

配置 [GitHub 登录](docs/GITHUB_LOGIN.md) 后，可用 GitHub 注册、登录，也可给已有账号绑定或解除绑定。默认关闭；需要为每个节点创建 OAuth App 并配置服务器端密钥。GitHub 登录不会自动合并同名或相同邮箱的账号，也不改变设备的 MQTT 密码。

## 权限更新方式

使用 Mosquitto 2.0 的官方 **Dynamic Security** 插件，后端通过验证服务器证书的内部 TLS 连接发送管理命令，并等待带关联 ID 的实际响应。新增设备、解绑、改密即时更新权限，不需要重载容器。

后端使用独立 Broker 管理账号和 CA 公钥连接管理接口。普通账号默认拒绝所有主题，仅获得自己设备对应的主题权限，不能访问 `$CONTROL` 管理主题。M1/A1 可只读订阅 Home Assistant 启动通知。

操作失败不会当作成功：设备保留 `pending` 或 `deleting` 状态，每 10 秒重试；成功收到权限撤销响应后才释放 MAC。两份状态（SQLite 与插件 JSON）需要一起备份、恢复。不要多实例运行后端；当前同步锁及限流为单实例设计。`trust_proxy: true` 仅适用于后端只可由可信 Nginx 访问的部署，Nginx 会覆盖 `X-Real-IP`；直接开放后端时必须关闭该选项。

## 界面

未登录默认显示登录页，可选择账号登录或自建 Broker 模式。首页展示设备核心信息与日常控制；设置包含 MQTT 连接、设备管理、第三方 Broker 和账号安全。平台直接使用设备 WebUI 的公共样式，登录、注册、管理和设置均沿用设备 WebUI 的设计语言。设备控制区域从 TC1、M1、A1 的首页源文件生成，只运行首页组件，不嵌入设备端导航与设置。平台通过 MQTT 适配命令接口，Legacy TC1 复用同一首页布局；未收到状态时禁用控制。设备端专有设置入口会提示前往设备本地 WebUI。

## 本地初始化

要求 Docker Compose、可用的 TCP 1883 和 HTTP 80 端口。首次运行：

```sh
mkdir -p runtime legacy
printf 'APP_UID=%s\nAPP_GID=%s\n' "$(id -u)" "$(id -g)" > .env
docker compose run --rm --build setup init --output /runtime
docker compose up -d --build mosquitto backend nginx
```

打开 `http://服务器地址/`。管理员用户名默认 `admin`，初始随机密码在 **runtime/secrets/admin-password**，请在本机私下查看。管理员从普通登录页登录，该密码也用于管理员自己的 MQTT 设备；MQTT 仍只允许其设备主题，平台管理角色不会授予 Broker 管理主题权限。首次启动自动迁移旧管理员为统一账号，之后该文件不会覆盖修改后的密码；若名称已属于已有用户，则拒绝自动提权。Broker 管理密码、密码哈希、证书私钥和数据库均在被 Git 忽略的 `runtime/` 中。初始化不会输出密码，也不会覆盖已有配置。若初始化中途失败，请保留错误信息，使用新的空目录重新初始化；不要将初始化命令用于正在运行的数据目录。

设备固件设置 MQTT 地址、1883 端口和自己的网站账号密码。网页默认连接同站点 `/mqtt` WebSocket；账号模式使用站点公布的 MQTT 地址。`.env` 可设 `HTTP_PORT`、`MQTT_PORT`、`PUBLIC_URL`、`MQTT_URI`、`MQTT_WS_URI`。自定义远端 WebSocket 地址必须指向同一套账号 Broker。

生产环境在前置代理终止 HTTPS，设置 `PUBLIC_URL=https://你的域名`，并将 `backend/config.yaml` 的 `cookie_secure` 设为 `true`。仅发布 HTTP 与设备所需 MQTT 端口；管理 TLS 8883、原始 WebSocket 9001 和后端 8080 不对外发布。1883 为兼容旧设备使用明文 TCP，请限制到可信设备网络。内部生成证书仅覆盖 `mosquitto`、`localhost`，服务器证书有效期两年；外部 MQTT TLS 需另行配置公网证书和监听器。

邮箱模式需配置 `backend/config.yaml` 的 SMTP 项，支持 TLS 直连或 STARTTLS。验证码仅驻留内存，有效期五分钟；重启后需要重发。

## 从旧 tc1_mqtt 迁移

迁移是离线工具，不读取或修改原服务，也不触发原容器重启：

1. 在原环境停止账号/设备修改，安全备份原 `passwd`、`acl`。不要把它们提交到 Git。
2. 将副本放入本项目 `legacy/passwd`、`legacy/acl`；使用新的空 `runtime/`。
3. 运行：

```sh
mkdir -p runtime legacy
printf 'APP_UID=%s\nAPP_GID=%s\n' "$(id -u)" "$(id -g)" > .env
docker compose run --rm --build setup migrate --output /runtime --passwd /legacy/passwd --acl /legacy/acl
```

4. 检查迁移账号数量、设备数量，再启动新服务。保留原服务与备份，切换地址前验证客户端登录及设备权限。

支持 Mosquitto `$7$` PBKDF2-SHA512 密码哈希，原密码无需重置；保留所有重复 `user` ACL 段中的设备，统一大小写和 MAC 分隔符，检测重复归属。未知规则、宽泛通配符、旧管理账号冲突或不支持的哈希会直接报错，需人工处理，避免默默丢弃权限。支持原 `device/ztc1/<MAC>/#`、`homeassistant/+/<MAC>/#` 及 M1/A1 的设备主题；TC1 迁移后默认 v2，可在网页调整为 v1。旧密码/ACL 的私有快照保存在 `runtime/backup/`。

浏览器旧 `ztc1_devices` 字符串数组和 `{mac,version}` 数组均可读取、按规范 MAC 去重。访客模式仍支持二维码和配置导入导出；导出的访客连接配置可能包含密码，应只在可信设备之间传递。

## 账号主题与自动发现

新协议与 Home Assistant 自动发现格式、用户名编码、固件连接时序及解绑恢复规则见 [MQTT_ACCOUNT_TOPICS.md](docs/MQTT_ACCOUNT_TOPICS.md)。例如 alice 的 M1 使用 `phicomm/m1/u_616c696365/aabbccddeeff/...`，发布 `phicomm/discovery/u_616c696365/aabbccddeeff` 的 retained 声明后即可登记；网页每 3 秒自动刷新云端列表。固件需要实现账号主题模式，当前旧固件继续手动添加。

## 协议与界面

| 显示型号 | 协议 / 主题 | 控制与状态 |
| --- | --- | --- |
| tc1(a1) | v1 JSON 或 v2 `device/ztc1/<MAC>/set`、HA 状态 | 六路插座、总开关、童锁、指示灯及原 v1 功能 |
| m1 | `phicomm/m1/<小写无分隔符MAC>/...` | 温湿度、甲醛、PM2.5、0–4 屏幕亮度 |
| a1 | `phicomm/a1/<小写无分隔符MAC>/...` | 风扇开关/转速、RGB 灯光、童锁、指示灯常亮 |

显示名去掉 zTC 前缀，但 **TC1 原有 `ztc1` 线协议主题保持兼容**。v2 命令使用大写无分隔符 MAC。M1 传感器超过 15 秒没有新数据、收到过期通知或 MQTT 断线后不再显示为有效数据；亮度状态表示设备请求值，不是硬件执行确认。所有命令 `retain=false`，避免固件丢弃保留命令或重连执行旧命令。

## 内置访问统计

管理员页面提供今天、近 7/30/90 天的浏览量、匿名访客、访问次数、近 5 分钟活跃、每日趋势、来源域名、浏览器、操作系统、屏幕类型与设备型号访问。当前有效登记的型号数量单独展示，不随时间筛选。口径参考 [Umami 指标说明](https://docs.umami.is/docs/metric-definitions)，平台自行收集和存储，无外部分析服务依赖。

前端在路由切换、设置页切换、成功登录、MQTT 建立连接和切换设备时异步上报；不等待统计接口，不影响控制。匿名访客由当前浏览器随机标识去重，30 分钟无事件后开始新访问；浏览器清理存储或更换浏览器会被视为新访客。数据保留 90 天，按 UTC 自然日统计，启用前的历史访问不会补算。设备型号访问是查看次数，不代表独立实物数量；当前登记数来自平台设备表。

仅 `/api/analytics/event` 接受公开同源写入，字段和事件类型受白名单限制并限流。`/api/admin/analytics?days=7` 使用现有管理员会话鉴权。统计表仅保存哈希后的随机访客/访问标识、服务器时间、固定页面类别、型号及来源域名与客户端分类；不保存账号、MAC、IP、完整来源 URL、密码或控制参数。该统计衡量浏览器上报访问，不是经审计的计费指标。

## 验证与开发

可独立启动设备 WebUI 模拟器，不需要设备或 MQTT Broker：

```sh
SIM_HOST=0.0.0.0 SIM_PORT=25174 node scripts/device-simulator.mjs
```

访问 `/tc1`、`/a1` 或 `/m1` 打开对应设备页面。模拟器提供设备名称恢复、TC1 插座控制、A1 风速与光效配置、M1 亮度控制；A1 光效设置页可调节速度并查看连续光环预览。

每种型号共用一份内存状态，刷新页面保留配置，重启模拟器后恢复默认值。A1 当前输出按固件规则每 100ms 向目标推进最多 10 个百分点。页面底部的“模拟实体触摸条”在操作期间显示目标速度，松手后灯环恢复跟随当前输出；该模拟不代表实测风扇转速或实体硬件时序。

```sh
(cd backend && go test -race ./... && go vet ./...)
python3 scripts/frontend_release.py
scripts/test-integration.sh
scripts/test-browser-compose.sh
```

集成测试使用临时 Broker 和随机本机端口，结束后自动清理测试容器与数据。

浏览器脚本自动创建独立 Broker、临时数据及随机端口，退出后清理。生产快照验证使用 前端 Release 中的 `source/frontend/tests/tmcp-review.spec.js`，凭据通过私有文件环境变量提供；不得在公开 CI 使用生产快照。

前端开发服务支持 `API_TARGET` 和 `MQTT_TARGET`：

```sh
git clone https://github.com/phicomm-io/phicomm-community-frontend.git
cd phicomm-community-frontend
npm ci --prefix frontend
npm --prefix frontend run dev
```

浏览器测试会创建测试账号，请使用独立测试数据目录。`build.sh` 用于构建本地镜像。

## 更新前端版本

在前端仓库修改并提交代码，等待对应 `frontend-<完整commit>` Release。将该 Release 的 `frontend.lock.json` 复制到此仓库，执行 `python3 scripts/frontend_release.py` 和 `./build.sh`，测试后提交锁文件。离线下载可用 `--archive` 指定压缩包，仍校验 SHA-256。

浏览器集成测试 `scripts/test-browser-compose.sh` 从同一 Release 的 `source/frontend/` 安装测试依赖，针对独立临时后端测试；源码开发、样式同步、前端单元测试归前端仓库管理。`node scripts/device-simulator.mjs` 继续启动锁定版本的设备模拟器。

## GHCR 自动发布

推送 `main`、推送 `v*` 标签或在 `main` 手动运行 **Test and publish TMCP platform**，会先执行平台、真实 Broker 和隔离浏览器测试，再发布两个支持 `linux/amd64`、`linux/arm64` 的镜像：

- `ghcr.io/phicomm-io/phicomm-community-mqtt-backend`
- `ghcr.io/phicomm-io/phicomm-community-mqtt-web`

`main` 更新 `latest`；每次发布都有 `sha-<完整平台commit>` 标签；`v*` 标签按原名发布。PR 只测试，不推送镜像。工作流使用仓库自带的 `GITHUB_TOKEN`，无需配置额外发布密码。镜像包含平台源码和前端锁定版本标签，工作流摘要记录发布 digest。Web 镜像从 `frontend.lock.json` 指定 Release 下载并校验前端，不跟随前端仓库的最新版本。

首次发布的 GHCR 包默认可能是私有包。公开分发时，仓库管理员需在两个包的 **Package settings → Change visibility** 中设为 Public；在此之前，拉取端需要使用具备 `read:packages` 权限且能访问对应包的账号登录（不要将令牌提交到仓库）：

```sh
printf '%s' "$GHCR_TOKEN" | docker login ghcr.io -u "$GHCR_USERNAME" --password-stdin
```

在已有初始化数据和配置的服务器上使用预构建镜像：

```sh
# 建议替换为工作流发布的 sha-<完整平台commit>，以固定版本。
export IMAGE_TAG=latest
docker compose -f docker-compose.yaml -f compose.ghcr.yaml pull backend nginx
docker compose -f docker-compose.yaml -f compose.ghcr.yaml up -d --no-build --no-deps backend nginx
```

上述命令复用现有配置、账号与设备数据。首次安装仍需按前文初始化；初始化工具也可通过同一 Compose 覆盖文件使用后端镜像，例如 `docker compose -f docker-compose.yaml -f compose.ghcr.yaml run --rm --no-deps setup init --output /runtime`。发布镜像不会自动替换任何运行中的服务器。

## 开源许可

本项目以 [GNU GPL v3.0](LICENSE) 开源。第三方组件遵循各自的许可证。

## Release 与 Wiki demo

推送 `v*` 标签会依次运行平台测试、发布 amd64/arm64 GHCR 镜像、创建 GitHub Release，
并由官网 CI 每 5 分钟检查新 Release，将该标签 `frontend.lock.json` 锁定且校验通过的 `demos/platform/` 同步至
[Wiki 平台 demo](https://phicomm-io.github.io/demos/platform/)。普通 main 推送不会更新 demo。
Release 附带前端锁文件；demo 的 `release.json` 记录平台标签、平台 commit 和前端 commit。

官网通过公开 Release 读取前端，不保存跨仓库访问令牌。同步只替换平台 demo，保留官网、指南、设备 demo 和固件归档。



## 独立静态前端（IotCloud）

新版独立前端支持 GitHub Pages / 静态托管及 PWA。跨域前端在 `backend/config.yaml` 增加：

```yaml
allowed_origins:
  - https://your-account.github.io
```

来源必须是精确的 scheme + host（可带端口），不含路径或末尾 `/`，不要使用通配符。前端配置后端根域和该后端配套的 MQTT WSS 地址。后端保留 Cookie 登录，同时支持同一可撤销会话的 Bearer 传递；白名单来源可读取 `X-IotCloud-Session`，不依赖第三方 Cookie。来源白名单并不授予账号或 MQTT 权限。公网部署使用 HTTPS / WSS。

前端通过 frontend.lock.json 固定到经过校验的独立 Release；相邻前端工作区不会自动进入镜像。

### 网站与 MQTT 密码

MQTT 密码默认使用注册密码。用户可在设置中输入网站当前密码，独立修改 MQTT 密码；修改网站密码不会影响设备 MQTT 连接。修改 MQTT 密码后须更新设备连接凭据，MQTT 别名同步使用新 MQTT 密码。已有账号首次成功登录时自动建立独立的网站密码哈希。

接口：`POST /api/auth/mqtt-password`，需要登录和同源校验，JSON 为 `current_password`（网站密码）和 `password`（新 MQTT 密码，6–256 字节）。`/api/auth/password` 仅修改网站密码并撤销网站会话。

## China / US 独立节点

| 节点 | 网站 / API | 设备 MQTT | 浏览器 MQTT |
| --- | --- | --- | --- |
| China | https://mqtt.fjrcn.cn | mqtt.fjrcn.cn:1883 | wss://mqtt.fjrcn.cn/mqtt |
| US | https://mqtt.fjrcn.com | mqtt.fjrcn.com:1883 | wss://mqtt.fjrcn.com/mqtt |

登录和注册页顶部可选择节点；登录后请进入“设置 → MQTT 连接 → 平台节点”切换。两个节点独立注册、登录和绑定设备，用户名相同也不会合并；不会复制账号、密码、设备、会话、数据库或 Broker 权限。设备需配置所选节点的 MQTT 主机与该节点账号。切换节点会重新加载页面、断开原 MQTT，并使用目标节点自己的登录状态。浏览器保存的会话与 MQTT 凭据也按后端来源隔离。

新增节点时，在独立服务器创建新的 `runtime/` 并初始化，**不要从其他节点复制运行数据或 secrets**。
`deploy/node.env.example` 提供 US 地址示例；使用独立 HTTPS 证书，按 `deploy/nginx-node.conf.example` 代理本机 19001 端口。域名使用 DNS-only 记录，设备 MQTT TCP 1883 不经过 Cloudflare HTTP 代理。

每个后端设置 `cookie_secure: true`，并在 `allowed_origins` 中逐项允许可信前端：

```yaml
allowed_origins:
  - https://phicomm-io.github.io
  - https://mqtt.fjrcn.cn
  - https://mqtt.fjrcn.com
```

`public_url` / `PUBLIC_URL` 必须为当前节点自己的地址。独立数据库与 Broker 才是数据隔离边界，CORS 仅允许前端选择访问哪个节点。US 初始管理员随机密码仅存于服务器的 `runtime/secrets/admin-password`，不会写入仓库。

### 注册验证码记录

邮箱验证注册的每次发送会写入应用数据库，记录申请邮箱、发送时间、有效期、发送结果及注册结果。管理后台的“注册验证码记录”支持近 1/7/30/90 天、邮箱搜索、状态筛选和分页；概况按邮箱去重，显示已发送、已注册、已发送但未注册、已过期未注册，以及发送失败次数。邮件服务器接受发送不代表进入收件箱，无法据此确认是否进入垃圾箱。

仅管理员可查看记录和有效期内的验证码，以便人工协助申请人。验证码仍然 5 分钟有效、一次性使用；重发成功后旧码失效，重发失败保留原有效码。后台默认隐藏验证码；已过期、失效、验证后不再返回验证码。历史保留 90 天，过期验证码及历史在发送或查询记录时清理；中断的发送显示结果未知。记录从功能上线后开始，无法补回旧内存验证码。后端重启不再丢失有效验证码。
