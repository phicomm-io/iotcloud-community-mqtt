# GitHub 登录

GitHub 登录是可选功能；未配置时登录页隐藏按钮，原有密码登录不受影响。个人 GitHub 账号即可创建 OAuth App，无需公司认证或付费开发者会员。

## 为每个节点配置 OAuth App

China 与 US 使用独立账号库，建议各自创建一个 OAuth App 和独立的 Client Secret。

1. 打开 [GitHub → Settings → Developer settings → OAuth Apps → New OAuth App](https://github.com/settings/applications/new)。
2. 填写应用名称和网站地址，Authorization callback URL 必须填写后端地址：

   | 节点 | Homepage URL | Authorization callback URL |
   | --- | --- | --- |
   | China | `https://mqtt.fjrcn.cn` | `https://mqtt.fjrcn.cn/api/auth/github/callback` |
   | US | `https://mqtt.fjrcn.com` | `https://mqtt.fjrcn.com/api/auth/github/callback` |

3. 生成 Client Secret，将其单独保存到该节点 `runtime/secrets/github-client-secret`，限制文件权限，仅让运维人员及后端运行用户读取。此目录已被 Git 忽略；不要将密钥放入前端、镜像构建参数、日志或仓库。
4. 在该节点实际使用的后端配置文件中填写：

   ```yaml
   public_url: https://mqtt.fjrcn.cn
   cookie_secure: true
   github:
     client_id: "OAuth App 的 Client ID"
     client_secret_file: /run/oauth-secrets/github-client-secret
   ```

   Compose 将本机 `runtime/secrets/` 只读挂载至 `/run/oauth-secrets/`。使用二进制运行时，填写其可读取的本机文件路径。`PUBLIC_URL` 环境变量会覆盖 `public_url`，必须与当前节点一致，并且是 HTTPS 根域，不能带路径。
5. 更新后端和锁定前端版本并重启后端。检查 `GET /api/auth/github/status` 返回 `{"enabled":true}`，再用真实 GitHub 账号走一次登录及绑定流程。配置不完整、密钥不可读或后端地址不符合要求时，服务会拒绝启动。

独立静态前端 / Pages 的来源需在后端 `allowed_origins` 中精确允许，例如 `https://phicomm-io.github.io`。回调地址始终是后端 `/api/auth/github/callback`；系统自动返回发起登录的前端，支持 `/iotcloud/` 子路径和 China / US 选择，无需把 Pages 地址注册为 GitHub 回调。

## 用户使用

- 新用户点击“使用 GitHub 登录”，授权后选择平台用户名和初始密码。初始密码同时用于网站和 MQTT，之后可分别修改；设备使用平台用户名及 MQTT 密码连接，不使用 GitHub 密码或令牌。
- 已有账号先用原密码登录，在“设置 → 账号安全”绑定 GitHub，即可保留原设备并增加一种登录方式。系统不会按 GitHub 用户名或相同邮箱自动合并账号。
- GitHub 登录仅建立网站会话。新浏览器中没有保存 MQTT 密码时，可先查看账号设备列表，再在“设置 → MQTT 连接”填写 MQTT 密码连接设备；后端不会返回 MQTT 明文密码。
- 一个 GitHub 身份只能绑定该节点的一个平台账号，一个平台账号也只绑定一个 GitHub 身份。China / US 的绑定互不影响。
- 解除绑定需要网站当前密码，完成后撤销该账号全部网站会话并退出，仍可用原密码登录。GitHub 改名不影响绑定，身份按 GitHub 数字 ID 识别。

注册模式继续生效：关闭注册时仅允许已有绑定登录或已登录账号绑定；开放注册时可创建新账号；邮箱验证模式要求 GitHub 返回已验证的主邮箱，作为该新账号的邮箱登录别名，邮箱冲突须先用原账号登录后绑定。普通密码注册流程不变。

## 授权与验证边界

仅申请 `user:email`，不申请仓库权限。Client Secret 与授权码交换均在后端；GitHub access token 仅用于本次读取身份及邮箱，不落库、不返回浏览器。OAuth 使用随机 state、PKCE S256、10 分钟有效期及一次性兑换；浏览器标签页保存独立随机校验信息，回跳 URL 不携带网站会话令牌。发起与完成必须处于同一标签页、同一平台节点，绑定还须保持发起时的网站会话有效。

本地自动测试使用隔离的 GitHub HTTP fixture，覆盖身份稳定性、注册/绑定冲突、邮箱策略、Broker 故障恢复、过期/重放、解绑与会话撤销。浏览器测试覆盖 Chromium / WebKit、手机 / 桌面、静态子路径、跨域会话和节点隔离；它们不能替代配置真实 OAuth App 后的生产验收。

参考：[GitHub OAuth web application flow](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/authorizing-oauth-apps)、[创建 OAuth App](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/creating-an-oauth-app)。
