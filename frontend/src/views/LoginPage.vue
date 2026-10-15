<script setup>
import { ref } from "vue";
import { useRouter } from "vue-router";
import AuthShell from "../components/AuthShell.vue";
import AppIcon from "../components/AppIcon.vue";
import { api, account, applyAccount } from "../account";
import {track} from "../analytics";
import {legacyCredentials, rememberLogin} from "../connectionPreferences";
const candidate = legacyCredentials();
const username = ref(candidate?.username || ""),
  password = ref(candidate?.password || ""),
  message = ref(""),
  busy = ref(false),
  showPassword = ref(false),
  router = useRouter();
async function login() {
  busy.value = true;
  message.value = "";
  try {
    const result = await api("/auth/login", {
      method: "POST",
      body: JSON.stringify({
        username: username.value,
        password: password.value,
      }),
    });
    applyAccount(result);
    account.password = password.value;
    rememberLogin(account.username, password.value);
    track("login","/login");
    account.loaded = true;
    await router.push("/");
  } catch (e) {
    message.value = e.message;
  } finally {
    busy.value = false;
  }
}
</script>
<template>
 <AuthShell>
  <div class="auth-card-heading"><span class="eyebrow">欢迎回来</span><h1>登录</h1><p>登录账号，查看并管理你的云端设备。</p></div>
  <div v-if="candidate" class="migration-notice" role="status"><b>平台已更新</b><p>已填入此浏览器保存的 MQTT 账号。首次登录会自动迁移到新版平台，原账号和设备归属保持不变。</p><button type="button" class="btn ghost" @click="username=candidate.username;password=candidate.password">使用已保存账号：{{candidate.username}}</button></div>
  <form class="auth-form" @submit.prevent="login">
   <label>用户名<input v-model="username" autocomplete="username" placeholder="用户名、别名或验证邮箱" required /></label>
   <label>密码<span class="password-field"><input v-model="password" :type="showPassword ? 'text' : 'password'" autocomplete="current-password" placeholder="输入账号密码" required /><button class="icon-button" type="button" :aria-label="showPassword ? '隐藏密码' : '显示密码'" @click="showPassword = !showPassword"><AppIcon name="eye" :size="18" /></button></span></label>
   <p v-if="message" role="alert" class="form-message error">{{ message }}</p><p v-else role="alert" class="sr-only"></p>
   <button class="primary-button auth-submit" :disabled="busy">{{ busy ? '登录中…' : '登录' }}<AppIcon name="arrow" :size="18" /></button>
  </form>
  <p class="auth-register">还没有账号？<RouterLink to="/register">注册账号</RouterLink></p>
  <div class="auth-divider"><span>或者</span></div>
  <RouterLink to="/guest" class="guest-entry"><span class="guest-entry-icon"><AppIcon name="guest" :size="22" /></span><span><b>自建 Broker 模式</b><small>填写自己的 MQTT 服务器，无需注册</small></span><AppIcon name="arrow" :size="18" /></RouterLink>
  <p class="auth-footnote">网站与平台 MQTT 使用同一账号密码。</p>
 </AuthShell>
</template>

<style scoped>
.migration-notice{padding:16px;margin:20px 0;background:var(--accent-soft);border:1px solid var(--border);border-radius:var(--radius-s);font-size:13px;line-height:1.7}.migration-notice b{color:var(--accent)}.migration-notice p{margin:8px 0;color:var(--muted)}.migration-notice button{max-width:100%;white-space:normal;overflow-wrap:anywhere;text-align:left}
</style>
