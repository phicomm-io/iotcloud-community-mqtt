<script setup>
import { ref, onMounted } from "vue";
import AuthShell from "../components/AuthShell.vue";
import { api } from "../account";
const username = ref(""),
  email = ref(""),
  password = ref(""),
  code = ref(""),
  message = ref(""),
  busy = ref(false),
  level = ref(0),
  contact = ref("");
onMounted(async () => {
  try {
    const s = await api("/register/status");
    level.value = s.register_level;
    contact.value = s.contact_info;
  } catch (e) {
    message.value = e.message;
  }
});
async function verify() {
  try {
    await api("/register/verify", {
      method: "POST",
      body: JSON.stringify({ email: email.value }),
    });
    message.value = "验证码已发送";
  } catch (e) {
    message.value = e.message;
  }
}
async function register() {
  busy.value = true;
  try {
    await api("/register", {
      method: "POST",
      body: JSON.stringify({
        username: username.value,
        email: email.value,
        password: password.value,
        code: code.value,
      }),
    });
    message.value = "注册成功，请登录后添加设备";
    password.value = "";
  } catch (e) {
    message.value = e.message;
  } finally {
    busy.value = false;
  }
}
</script>
<template>
  <AuthShell>
    <span class="eyebrow">开始连接</span>
    <h1>注册账号</h1>
    <p>创建账号，自动同步设备列表。</p>
    <form class="auth-form" v-if="level" @submit.prevent="register">
      <label
        >用户名<input
          v-model="username"
          type="text"
          minlength="3"
          maxlength="32"
          pattern="[A-Za-z0-9_.\-]{3,32}"
          placeholder="3–32 个字母、数字、下划线、点或连字符"
          autocomplete="username"
          required /></label
      ><label
        >密码<input
          v-model="password"
          type="password"
          minlength="6"
          maxlength="256"
          autocomplete="new-password"
          required /></label
      ><template v-if="level === 2"
        ><label>验证邮箱<input v-model="email" type="email" maxlength="254" autocomplete="email" required /></label><button type="button" @click="verify">发送验证码</button
        ><label>验证码<input v-model="code" required /></label></template
      ><button class="primary-button auth-submit" :disabled="busy">注册</button>
    </form>
    <p v-else>注册已关闭 {{ contact }}</p>
    <p role="alert" class="form-message">{{ message }}</p>
    <RouterLink to="/login">登录</RouterLink> ·
    <RouterLink to="/guest">进入访客模式</RouterLink>
  </AuthShell>
</template>
