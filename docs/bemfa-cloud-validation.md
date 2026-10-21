# 云端转发分支验证记录

分支：`feat/bemfa-cloud-relay`。前端锁定提交：`2a4f2fbd83e34307b9a2a6dc4abfbac52be5a605`。

| 验证 | 结果 | 范围 |
| --- | --- | --- |
| `go test -race ./...` | PASS | 全部 Go 用例；外部运行环境用例按原条件跳过 |
| `go vet ./...` | PASS | 后台静态检查 |
| 新增本地 MQTT wire 链路 | PASS | 实际 Paho 收发，逐插孔路由、实际状态回传、断线重连、过期/retained 指令拦截、归属撤销、停用及订阅拒绝 |
| 新增配置/API 用例 | PASS | 账号隔离、来源检查、凭据加密/持久化、主题合法性和 A1 风速/亮度换算 |
| `scripts/test-integration.sh` | PASS | 临时真实 Mosquitto 动态权限、迁移、重启持久化，含 manager 转发权限 |
| 前端 `npm test` | PASS | 38 项 |
| 前端 `tests/bemfa-cloud.mjs` | PASS | Chromium 375px / 1280px；密钥填写与留空保留、主题配置、启停、清除、错误恢复、无浏览器密钥持久化 |
| 前端完整发布打包 | PASS | 平台、本地版、演示和设备页面全部生成；锁定包通过 SHA-256 / manifest 校验 |
| `FRONTEND_ARCHIVE=… scripts/test-browser-compose.sh` | PASS | Docker 后台 + nginx + Mosquitto，浏览器 15 项通过、3 项按既有条件跳过；此轮使用前端 `2fe830b`，其后前端仅增加订阅拒绝提示文案，最终 `2a4f2fb` 重新完整打包并校验 |
| 真实巴法云 / 小爱同学 | NOT_RUN | 未使用用户凭据，未向外部账号写入 |
| 实机控制、刷机、生产部署 | NOT_RUN | 本次只完成分支、代码和隔离环境验证 |

最后完整锁定包 SHA-256 以 `frontend.lock.json` 为准。本地包位于相邻前端工作区 `build/frontend.tar.gz`；常规 web 镜像所需的远端前端 Release 尚未发布，离线验证方式见 `bemfa-cloud.md`。

固件新分支相对当前 `origin/main` 仅新增 `docs/bemfa-cloud.md`。原模块方案和实机测试产物留在原工作区，未搬入本分支。
