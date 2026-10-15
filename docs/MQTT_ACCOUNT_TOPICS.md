# TMCP 主题与接口

TMCP：Tom MQTT Cloud Protocol。用户界面只显示 TMCP。协议使用 `protocol: "tmcp"`、`protocol_version: 2`、`version: "v2"`。Legacy 标记为 `v1`，仅限 TC1。

## 设备身份

型号为 `tc1`、`m1`、`a1`。MAC 为去掉分隔符的小写 12 位十六进制。账号键为 `u_` 加 MQTT 用户名 UTF-8 字节的十六进制编码。没有用户名的自建 Broker 使用 `local`。

```text
phicomm/v2/<scope>/discovery/<mac>
{"schema":1,"model":"m1","protocol":"tmcp","protocol_version":2,"version":"v2"}
```

上线公告 retained 发布。平台从主题解析账号，不信任载荷中的归属。公告需要完整协议字段；早期 `phicomm/discovery/...` 与 `version: community` 不接受。不同账号可各自登记相同 MAC；明确型号不可由后续公告覆盖。解绑隐藏记录不会被 retained 公告自动恢复。

## 状态和命令

根主题为 `phicomm/v2/<scope>/<model>/<mac>`。

| 后缀 | 内容 |
| --- | --- |
| `availability` | `online` / `offline`，retained 和 LWT |
| `state` | 共享 HTTP 状态字段的 JSON 快照，retained |
| `capabilities` | 支持动作列表、产品和协议元数据，retained |
| `command` | JSON 请求，不得 retained |
| `result` | 带请求编号的 JSON 响应，不得 retained |
| `sensor` | TC1 电量/功率及产品采样遥测 |

```json
{"request_id":"web_123","action":"status.get","params":{}}
```

```json
{"request_id":"web_123","code":"ok","message":"操作成功","data":{}}
```

动作和参数以固件 capabilities 为准。示例：TC1 `outlet.set {index:0,on:true}`；M1 `display.set {brightness:75}`；A1 `fan.set {on:true,speed:50}`。结果 `accepted` 表示已接受，不等同于硬件完成。平台用 request_id 匹配查询响应；重试同一请求应复用相同编号与内容。

M1 采样需同时检查 `sensor_valid`、设备在线和接收时间；超过 15 秒不展示为实时有效。A1 `sensors.average.get` 的平均值仅包含已绑定、有效且新鲜的 M1。M1 的联动链路只响应查询，规则、绑定和控制主要由 A1 管理。设备对设备查询在已授权设备根下的 `link/...`；A1 虚拟联动实体仍使用 `phicomm/linkage/<scope>/<root>/...`，不等于兼容早期原生协议。

## HA 与权限

HA 发现为 `homeassistant/<component>/<scope>/<model>_<mac>_<entity>/config`；固件提供状态模板和独立实体。`homeassistant/status` 仅允许读取。设备自身完整状态通过 json_attributes_topic 声明；平台发现与 HA 发现分别验证。

账号只允许发布自己的上线公告、访问明确登记的设备根与本账号 HA 发现配置；不能订阅其他账号设备或 `$CONTROL` 管理主题。用户名别名使用自身编码的 scope，与主账号共享归属，但不混淆线上的 scope。

## Legacy

TC1 保留原有 `device/ztc1/<MAC>/...` 与原 HA 标识，订阅保留大小写及分隔符变体。平台支持原始 JSON 状态与命令，以及当前固件 Legacy 模式保留的 HA 文本状态与控制；收到文本状态后沿用大写 MAC 的文本命令。Legacy 不会自动变成 TMCP。M1/A1 不提供 Legacy 或早期项目协议。
