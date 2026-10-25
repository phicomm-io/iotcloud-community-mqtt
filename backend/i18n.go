package main

import (
	"net/http"
	"strconv"
	"strings"
)

// Stable message IDs are part of the API. Keep IDs when editing copy.
var messageCatalog = map[string][2]string{
	"e.d2909f1647e7":               {"Other", "其他"},
	"e.9be2505cb51c":               {"Too many requests", "请求过于频繁"},
	"e.2ea3a69ca256":               {"Invalid analytics event", "统计事件格式错误"},
	"e.657f8459718b":               {"Unsupported device model", "设备型号不支持"},
	"e.d11c6512f82b":               {"Analytics model does not match the event", "统计型号与事件不匹配"},
	"e.baf9b4ab24e3":               {"Unsupported screen type", "屏幕类型不支持"},
	"e.762cd9c2de69":               {"Direct", "直接访问"},
	"e.885f47a0d4e2":               {"Invalid referrer", "来源格式错误"},
	"e.1d9d8f93ea27":               {"Unsupported analytics range", "统计范围不支持"},
	"e.d87efef93be5":               {"Invalid MAC address", "MAC 地址格式不正确"},
	"e.150d651d5679":               {"TC1 protocol must be v1 or v2", "TC1 协议必须为 v1/v2"},
	"e.5c1e0d5e1217":               {"Sensors and purifiers support TMCP only", "传感器与净化器仅支持 TMCP"},
	"e.8f87e412bb4f":               {"Account topics can only be discovered from MQTT declarations", "账号主题只能通过 MQTT 声明自动发现"},
	"e.a8d0239a72c3":               {"Device is being unbound. Try again later.", "设备正在解绑，请稍后重试"},
	"e.4c193cda5dd8":               {"This MAC or account is already bound", "该 MAC 或账号已被绑定"},
	"e.7709ac19da8a":               {"Enter the Bemfa private key", "请填写巴法云私钥"},
	"e.0ce87282dfbb":               {"Invalid Bemfa private key format", "巴法云私钥格式不正确"},
	"e.11f74b9c9966":               {"At most 64 topic mappings are allowed", "最多添加 64 个主题映射"},
	"e.769090dbbb54":               {"Only ready TMCP TC1/A1 devices owned by this account can be mapped", "只能映射当前账号下已就绪的 TMCP TC1/A1 设备"},
	"e.d2ed1e828e67":               {"Invalid channel or topic: switches use 001, fans 003 and lights 002", "通道或主题格式不正确：插座 001、风扇 003、灯 002"},
	"e.cba6e923c026":               {"Topic and device channel mappings must be unique", "主题及设备通道不能重复映射"},
	"e.fc5807a70e5d":               {"Add at least one topic mapping", "请至少添加一个主题映射"},
	"e.f513aea04248":               {"This Bemfa key is already used by another account", "该巴法云私钥已被其他账号使用"},
	"e.449a12016770":               {"Bemfa integration is disabled on this node", "当前节点未启用巴法云接入"},
	"e.2952f4853185":               {"Clear failed", "清除失败"},
	"e.4f34574afee4":               {"Method not allowed", "方法不允许"},
	"e.9a8c13083c68":               {"Could not read Bemfa configuration. Check the node key.", "读取巴法云配置失败，请检查节点密钥"},
	"e.06b2307f3506":               {"Could not save Bemfa configuration", "保存巴法云配置失败"},
	"e.916c788017f9":               {"Username or alias already in use", "用户名或别名已被使用"},
	"e.1ef6a7c0d805":               {"Username must contain 3–32 letters, numbers, underscores, dots or hyphens, and must not be reserved", "用户名须为 3–32 个字母、数字、下划线、点或连字符，且不能为保留账号"},
	"e.83fae2be3f27":               {"Each account supports one MQTT alias. Use the existing alias.", "每个账号只支持一个 MQTT 别名，请使用已有别名"},
	"e.e7cd8a7a0397":               {"Choose an announcement before or after sign-in", "请选择登录前或登录后公告"},
	"e.0eb14a5fe05f":               {"Enter announcement text (up to 300 characters) and button text (up to 40 characters)", "请填写公告正文（最多 300 字），按钮文字最多 40 字"},
	"e.bc91627d3b31":               {"Enter both the link and button text, or leave both empty", "链接地址和按钮文字需要同时填写，或同时留空"},
	"e.2acf49559231":               {"Use a complete HTTP or HTTPS link", "链接需要使用完整的 HTTP 或 HTTPS 地址"},
	"e.447af662b999":               {"Password must contain 6–256 bytes", "密码长度须为 6–256 字节"},
	"e.14c896de34db":               {"MQTT password changed. Update the password on your devices.", "MQTT 密码已修改，请更新设备连接密码"},
	"e.d4f7d0bf6262":               {"User created", "用户已创建"},
	"e.d3d0c2c1c246":               {"Reserved accounts cannot be modified", "不能操作保留账号"},
	"e.6d97c2d92199":               {"Select a valid account", "请选择有效账号"},
	"e.ff5b91812add":               {"Invalid device identifier", "设备标识不正确"},
	"e.478b671121de":               {"Invalid configuration format", "配置格式错误"},
	"e.74bb6ba74027":               {"Broker unavailable; synchronization is pending. Try again later.", "Broker 不可用，操作待同步，请稍后重试"},
	"e.a9a6a83a7d22":               {"Incorrect username or password", "用户名或密码错误"},
	"e.8f1fa2479432":               {"Invalid return address", "返回地址无效"},
	"e.a860e18970a9":               {"Return address is not a trusted frontend", "返回地址不在可信前端中"},
	"e.caf05a2ad17d":               {"GitHub sign-in is disabled", "GitHub 登录尚未启用"},
	"e.0c5a83d0e3bc":               {"Invalid return address or browser verification", "返回地址或浏览器验证信息无效"},
	"e.b44953a2b3c9":               {"Sign in to the account you want to link first", "请先登录要绑定的账号"},
	"e.2650498d5197":               {"GitHub sign-in request expired. Start again.", "GitHub 登录请求已失效，请重新开始"},
	"e.af4854ffbbbf":               {"GitHub sign-in request already used", "GitHub 登录请求已使用"},
	"e.9334077996f7":               {"GitHub authorization incomplete. Try again.", "GitHub 授权未完成，请重新尝试"},
	"e.6a978bf88a03":               {"Could not complete GitHub verification. Try again.", "无法完成 GitHub 验证，请重新尝试"},
	"e.c8b7463c89de":               {"GitHub sign-in request expired or belongs to another browser. Start again.", "GitHub 登录请求已失效或不属于此浏览器，请重新开始"},
	"e.026effbde3cd":               {"Request origin does not match", "请求来源不匹配"},
	"e.495c0e57a489":               {"Create the account using the platform username selected earlier", "请使用此前选择的平台用户名完成账号创建"},
	"e.a8eac6146daf":               {"The linked account's sign-in state changed. Start again.", "绑定账号的登录状态已变化，请重新开始"},
	"e.ed7faac67984":               {"The account session expired. Sign in again before linking.", "绑定账号的登录已失效，请重新登录"},
	"e.5119665361dd":               {"This GitHub account is linked to another platform account", "此 GitHub 已绑定其他平台账号"},
	"e.c47314603a76":               {"A GitHub account is already linked. Unlink it first.", "账号已绑定其他 GitHub，请先解除绑定"},
	"e.12a6d18e21bf":               {"Registration is closed. Sign in to an existing account with its password before linking GitHub.", "注册已关闭；已有账号请使用密码登录后绑定 GitHub"},
	"e.9fe5e5854355":               {"This node requires a verified email. Verify your primary email on GitHub, or register by email before linking.", "此节点要求验证邮箱，请先在 GitHub 验证主邮箱，或使用邮箱注册后绑定"},
	"e.ff56818dd391":               {"Username already in use. Sign in to the existing account with its password before linking GitHub.", "用户名已被使用；已有账号请使用密码登录后绑定 GitHub"},
	"e.850ca422df35":               {"This GitHub account is already linked. Sign in with GitHub again.", "此 GitHub 已绑定账号，请重新使用 GitHub 登录"},
	"e.c54193e580e8":               {"Account unavailable. Contact an administrator or sign in with a password to finish initialization.", "账号不可用，请联系管理员或使用密码登录完成初始化"},
	"e.15d0e272cae1":               {"The last administrator cannot be deleted or demoted", "不能删除或降级最后一个管理员"},
	"e.3dc876c8ff1e":               {"Role must be admin or user", "角色必须为 admin 或 user"},
	"e.a2a9d4e096e5":               {"Active account not found", "有效账号不存在"},
	"e.2529b19bc41c":               {"Invalid request format", "请求格式错误"},
	"e.2f31aac81862":               {"Request origin is not allowed", "请求来源不允许"},
	"e.48144f96c150":               {"Sign in first", "请先登录"},
	"e.48c14e645b3b":               {"Session expired. Sign in again.", "会话已失效，请重新登录"},
	"e.9c0b05f1e23d":               {"Website password changed. Sign in again. Your MQTT password stays the same.", "网站密码已修改，请重新登录；MQTT 密码保持不变"},
	"e.90343cedc288":               {"Administrator permission required", "没有管理权限"},
	"e.e4ba03868d92":               {"Record not found", "记录不存在"},
	"e.f0a422fd901c":               {"Broker permission synchronization is incomplete. Try again later.", "Broker 权限同步未完成，请稍后重试"},
	"e.5b81b37e66d1":               {"Operation incomplete. Check the service logs.", "操作未完成，请检查服务日志"},
	"e.cb5ee4993128":               {"Replaced by structured device permission management", "已改为结构化设备权限管理"},
	"e.c49d8684e46f":               {"Endpoint not found", "接口不存在"},
	"e.1e06fa3ced5f":               {"Administrator initialization incomplete: %v", "管理员账号初始化未完成: %v"},
	"e.74b6d213d243":               {"Permission synchronization incomplete: %v", "权限同步未完成: %v"},
	"e.6c2899d8f501":               {"Device ownership and status cannot be specified", "不能指定设备归属或状态"},
	"e.f7bb1748ec1e":               {"Device added", "设备已添加"},
	"e.449ee80d2263":               {"MAC, ownership and status cannot be changed", "不能修改 MAC、归属或状态"},
	"e.0a7684920170":               {"Device updated", "设备已更新"},
	"e.507b4da76f2f":               {"Device unbound", "设备已解绑"},
	"e.69ffa0add155":               {"Discovered device models are declared by firmware and cannot be changed manually", "自动发现设备型号由固件声明，不能手动修改"},
	"e.9613d5708c95":               {"Invalid or reserved username", "用户名格式不正确或为保留账号"},
	"e.dc994e342a79":               {"Username must contain 3–32 characters", "用户名长度须为 3–32 个字符"},
	"e.fb3a60f71b0b":               {"Registration is closed", "注册功能已关闭"},
	"e.132431225954":               {"Email verification code is incorrect or expired", "邮箱验证码错误或已过期"},
	"e.0bf253bee02b":               {"Registered. Sign in to add devices.", "注册成功，请登录后添加设备"},
	"e.815349ee7216":               {"This registration mode does not require email verification", "当前注册模式不需要邮箱验证"},
	"e.4e992d96f947":               {"Invalid email address", "邮箱格式不正确"},
	"e.a2bec2867f2e":               {"Could not send email. Try again later.", "邮件发送失败，请稍后重试"},
	"e.aa3f3413dfdd":               {"Verification code sent. Valid for 5 minutes.", "验证码已发送，5 分钟内有效"},
	"e.66ba1c71fe55":               {"Verification succeeded but registration failed. Request a new code.", "验证码已通过，但注册失败；请重新获取验证码"},
	"e.2c5882fc7b7b":               {"Page number is out of range", "页码超出范围"},
	"e.e8cbd0e96486":               {"Email query is too long", "邮箱查询过长"},
	"e.3bb42a1e90f0":               {"Invalid status", "状态不正确"},
	"e.69499cf86fc1":               {"MQTT account verification code", "MQTT 用户注册验证码"},
	"e.ab00888baac7":               {"Your verification code is: %s\n\nValid for 5 minutes. Do not share this code.", "您的验证码为：%s\n\n有效期 5 分钟，请勿泄露。"},
	"verification.interrupted":     {"Email delivery may have been interrupted. Request a new verification code.", "发送过程可能被中断，无法确认邮件服务器是否接受；请重新获取验证码"},
	"verification.delivery_failed": {"The mail server could not send the message. Check the SMTP logs.", "邮件服务器发送失败，请检查 SMTP 日志"},
}

// routes sets this response header for each request. Never use a package-wide
// current language: simultaneous browsers may select different languages.
func responseLanguage(w http.ResponseWriter) string {
	if w.Header().Get("Content-Language") == "en" {
		return "en"
	}
	return "zh-CN"
}

func localizedMessage(text, language string) (string, string) {
	key := text
	if _, ok := messageCatalog[key]; !ok {
		key = messageKey(text)
	}
	if key == "" {
		return text, ""
	}
	return messageForLanguage(key, language), key
}

func messageText(key string) string {
	return messageForLanguage(key, "zh-CN")
}

func messageForLanguage(key, language string) string {
	if pair, ok := messageCatalog[key]; ok {
		if language == "en" {
			return pair[0]
		}
		return pair[1]
	}
	return key
}

func preferredLanguage(header string) string {
	selected, best := "zh-CN", 0.0
	for _, item := range strings.Split(header, ",") {
		parts := strings.Split(strings.TrimSpace(item), ";")
		tag := strings.ToLower(strings.TrimSpace(parts[0]))
		language := ""
		if tag == "en" || strings.HasPrefix(tag, "en-") {
			language = "en"
		} else if tag == "zh" || strings.HasPrefix(tag, "zh-") {
			language = "zh-CN"
		}
		quality := 1.0
		for _, parameter := range parts[1:] {
			parameter = strings.TrimSpace(parameter)
			if strings.HasPrefix(parameter, "q=") {
				parsed, err := strconv.ParseFloat(strings.TrimPrefix(parameter, "q="), 64)
				if err != nil || parsed < 0 || parsed > 1 || parsed != parsed {
					quality = 0
				} else {
					quality = parsed
				}
			}
		}
		if language != "" && quality > best {
			selected, best = language, quality
		}
	}
	return selected
}
func messageKey(text string) string {
	for key, pair := range messageCatalog {
		if pair[0] == text || pair[1] == text {
			return key
		}
	}
	return ""
}
