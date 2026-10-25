# IoTCloud 云平台

用于 TC1、M1、A1 和大金空气检测仪的 MQTT 管理平台。提供独立账号、设备列表、权限管理、远程控制与巴法云转发；配套中英文 WebUI 可静态部署，也可随平台一起运行。

[English](README.en.md) · [使用文档](https://phicomm-io.github.io/) · [开发协议](https://phicomm-io.github.io/iotcloud-community-protocols/)

## 功能

- 网站与 MQTT 共用账号，网站密码和 MQTT 密码可独立修改；支持管理员角色、开放注册、邮箱验证码和可选 GitHub 登录。
- 按账号隔离 MQTT 主题和设备权限。新固件自动发现，旧版 TC1 Legacy 可手动添加；现有 v0.9.x 设备继续接入。
- 浏览器选择中文或英文；API 的显示消息和验证码邮件随请求语言返回，错误码与线协议保持稳定。
- 支持账号设备列表、访客模式、独立 WebSocket Broker、Home Assistant 发现与巴法云转发。
- SQLite 保存账号和设备登记，Mosquitto Dynamic Security 管理 MQTT 权限；权限操作失败时保留待同步状态并重试。

设备固件见 [iotcloud-community-firmware](https://github.com/phicomm-io/iotcloud-community-firmware)，WebUI 唯一源码见 [iotcloud-community-frontend](https://github.com/phicomm-io/iotcloud-community-frontend)。平台通过完整 commit 和 SHA-256 固定前端 Release，不从最新分支临时构建。

## 安装

需要 Docker Compose，以及可用的 HTTP 和 MQTT TCP 端口。在仓库目录运行：

```sh
mkdir -p runtime legacy
printf 'APP_UID=%s\nAPP_GID=%s\n' "$(id -u)" "$(id -g)" > .env
docker compose run --rm --build setup init --output /runtime
docker compose up -d --build mosquitto backend nginx
```

打开 `http://服务器地址/`。初始管理员名为 `admin`，随机密码保存在 `runtime/secrets/admin-password`，请在本机查看。初始化只用于新的空目录，不覆盖现有配置。

在设备中设置服务器的 MQTT 地址、1883 端口及自己的平台账号密码。网页默认使用同站点 `/mqtt` WebSocket。`.env` 可设置 `HTTP_PORT`、`MQTT_PORT`、`PUBLIC_URL`、`MQTT_URI` 和 `MQTT_WS_URI`。

公网部署使用 HTTPS / WSS，在 `backend/config.yaml` 中设置 `cookie_secure: true`。跨域静态前端须逐项配置 `allowed_origins`，只填写完整来源，如 `https://example.github.io`。设备兼容端口 1883 使用明文 TCP，应限制到可信设备网络；内部管理 TLS 8883、原始 WebSocket 9001 和后端 8080 不直接对外开放。

`runtime/` 包含数据库、密码哈希、证书和密钥，已被 Git 忽略。备份和恢复需同时保存应用数据库、Broker 权限数据及密钥。详细配置见[部署与维护](docs/OPERATIONS.md)。

## 使用与迁移

网站登录后添加或自动发现设备；访客模式可独立连接自建 WebSocket Broker。用户设置中的设备名称、备注与本地连接配置保留原文，不随语言切换修改。

新用户名与 MQTT 别名为 3–32 个 ASCII 字母、数字、下划线、点或连字符。验证邮箱仅作登录别名，最长 254 字节；已有长账号保留登录，可增加短 MQTT 别名。

平台接收 TMCP 和 TC1 Legacy。TMCP 的 `protocol: tmcp`、`protocol_version: 2`，以及旧 TC1 的 `ztc1` 主题保持兼容；固件或仓库更名不会改动这些标识。

- [账号与鉴权](docs/ACCOUNT_SECURITY.md) · [账号主题与自动发现](docs/MQTT_ACCOUNT_TOPICS.md)
- [旧 tc1_mqtt 迁移、验证与回退](docs/MIGRATION_TMCP.md)
- [GitHub 登录](docs/GITHUB_LOGIN.md) · [巴法云转发](docs/bemfa-cloud.md)

历史 v0.9.x 源码保存在 [`archive/0.9.x`](https://github.com/phicomm-io/iotcloud-community-mqtt/tree/archive/0.9.x)，已发布制品保持原样。升级前备份原部署，在独立目录和端口验证新版本后再切换；新平台写入不会自动回写旧平台。

## 开发与发布

```sh
(cd backend && go test -race ./... && go vet ./...)
python3 scripts/frontend_release.py
scripts/test-integration.sh
scripts/test-browser-compose.sh
```

后两项使用临时 Broker、独立数据目录与随机本机端口，不挂载生产数据。前端开发和单元测试在前端仓库进行；设备模拟器可用 `node scripts/device-simulator.mjs` 启动。

采用 `feature/* → dev → main` 工作流，详见 [CONTRIBUTING.md](CONTRIBUTING.md)。接口与配置变化须覆盖旧客户端、迁移、写入失败及回退边界。跨仓库实现固定同一个 `protocols.lock.json` 规范提交。

更新前端时，从已验证的 `frontend-<完整commit>` Release 复制 `frontend.lock.json`，运行下载校验、平台及浏览器测试后提交；离线构建使用 `--archive`，同样校验哈希。相邻工作区不会自动进入镜像。

稳定版本由 `main` 上的 `v1.0.0` 及后续正式版本标签发布 GitHub Release 和 amd64/arm64 GHCR 镜像；PR、`dev` 和普通 `main` 提交只运行检查。镜像名为 `iotcloud-community-mqtt-backend` 与 `iotcloud-community-mqtt-web`。部署固定版本或 digest，并复用原配置和数据；发布不会自动替换运行中的服务。官网仅从正式 Release 同步锁定的[平台演示](https://phicomm-io.github.io/demos/platform/)。

## 许可

本项目采用 [GPL-3.0](LICENSE)，第三方组件保留各自许可。
