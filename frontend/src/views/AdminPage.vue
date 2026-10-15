<script setup>
import AdminAnalytics from "../components/AdminAnalytics.vue";
import ThemeToggle from "../components/ThemeToggle.vue";
import { ref, onMounted, onBeforeUnmount, watch } from "vue";
import { useRouter } from "vue-router";
import { api, account, refreshAccountRole } from "../account";
import { modelName, deviceKey } from "../deviceProtocol";
const message = ref(""),
  users = ref([]),
  devices = ref([]),
  newUser = ref(""),
  newPass = ref(""),
  owner = ref(""),
  mac = ref(""),
  model = ref("tc1"),
  version = ref("v2"),
  level = ref(1),
  contact = ref("");
const request = (path, method = "GET", body) =>
  api("/admin" + path, {
    method,
    ...(body ? { body: JSON.stringify(body) } : {}),
  });
async function refresh() {
  const [u, d, c] = await Promise.all([
    request("/users"),
    request("/devices"),
    request("/config"),
  ]);
  users.value = u;
  devices.value = d;
  level.value = c.register_level;
  contact.value = c.contact_info;
}
async function run(fn) {
  try {
    await fn();
    message.value = "";
    await refresh();
  } catch (e) {
    message.value = e.message;
  }
}
const router = useRouter();
let roleTimer;
onMounted(() => { run(async () => {}); roleTimer = setInterval(() => refreshAccountRole().catch(() => {}), 3000); });
onBeforeUnmount(() => clearInterval(roleTimer));
watch(() => account.isAdmin, allowed => { if (!allowed) router.replace("/"); });
function setRole(u) {
 const role = u.role === "admin" ? "user" : "admin";
 if (confirm(`${role === "admin" ? "授予" : "撤销"} ${u.username} 的管理权限？`))
  run(() => request("/user/" + encodeURIComponent(u.username) + "/role", "PUT", { role }));
}
function addUser() {
  return run(async () => {
    await request("/users", "POST", {
      username: newUser.value,
      password: newPass.value,
    });
    newUser.value = "";
    newPass.value = "";
  });
}
function reset(u) {
  const p = prompt("输入新密码（至少 6 字节）");
  if (p)
    run(() =>
      request("/user/" + encodeURIComponent(u), "PUT", { password: p }),
    );
}
function removeUser(u) {
  if (confirm("删除账号及其所有设备权限：" + u + "？"))
    run(() => request("/user/" + encodeURIComponent(u), "DELETE"));
}
function removeDevice(d) {
  if (confirm("解绑并撤销权限：" + d.mac + "？"))
    run(() => request("/device/" + (d.namespace ? "scoped/" : "") + d.mac + (d.namespace ? "?username=" + encodeURIComponent(d.username) + "&mqtt_username=" + encodeURIComponent(d.mqtt_username || d.username) : ""), "DELETE"));
}
function bind() {
  return run(async () => {
    await request("/devices", "POST", {
      username: owner.value,
      mac: mac.value,
      model: model.value,
      version: model.value === "tc1" ? version.value : "v2",
    });
    mac.value = "";
  });
}
</script>
<template>
  <main class="console-page">
    <header class="topbar console-topbar"><div class="brand"><span class="kind">MQTT 管理平台</span><h1>账号与设备管理</h1></div><nav class="console-nav"><ThemeToggle/><RouterLink class="btn" to="/">返回控制台</RouterLink></nav></header>
    <p class="error" role="alert">{{ message }}</p>
    <template v-if="account.isAdmin">
      <p class="helper-text">当前账号：{{ account.username }} · 管理员。与设备控制台、MQTT 使用同一登录账号。</p>
      <AdminAnalytics/>
      <button @click="run(async () => {})">刷新账号与设备</button>
      <section class="card">
        <h2>账号</h2>
        <form @submit.prevent="addUser">
          <label>用户名<input v-model="newUser" minlength="3" maxlength="32" pattern="[A-Za-z0-9_.\-]{3,32}" required /></label
          ><label
            >初始密码<input
              v-model="newPass"
              type="password"
              minlength="6"
              required /></label
          ><button>创建账号</button>
        </form>
        <ul class="admin-list">
          <li v-for="u in users" :key="u.username">
            <div class="admin-item-copy"><b>{{ u.username }}</b><p class="help">{{ u.status === 'active' ? '已启用' : u.status }} · {{ u.role === "admin" ? "管理员" : "普通用户" }}</p></div><div class="admin-actions">
            <button @click="setRole(u)">{{ u.role === "admin" ? "撤销管理权限" : "设为管理员" }}</button>
            <button @click="reset(u.username)">重置密码</button>
            <button class="btn danger" @click="removeUser(u.username)">删除</button></div>
          </li>
        </ul>
      </section>
      <section class="card">
        <h2>设备归属</h2>
        <form @submit.prevent="bind">
          <label
            >账号<select v-model="owner" required>
              <option
                v-for="u in users.filter((u) => u.status === 'active')"
                :key="u.username"
              >
                {{ u.username }}
              </option>
            </select></label
          ><label>MAC<input v-model="mac" required /></label
          ><label
            >型号<select v-model="model" aria-label="型号">
              <option value="tc1">TC1</option>
              <option value="m1">M1</option>
              <option value="a1">A1</option>
            </select></label
          ><label v-if="model === 'tc1'">协议<select v-model="version">
            <option value="v1">Legacy</option>
            <option value="v2">TMCP</option></select></label
          ><button>绑定</button>
        </form>
        <ul class="admin-list">
          <li v-for="d in devices" :key="deviceKey(d)">
            <div class="admin-item-copy"><b>{{ modelName(d.model) }} · {{ d.mac }}</b><p class="help">{{ d.username }} · {{ d.version === 'v1' ? 'Legacy' : 'TMCP' }} · {{ d.status === 'active' ? '已启用' : d.status }} · {{ d.namespace ? "账号主题" : "旧主题" }}</p></div>
            <button class="btn danger" @click="removeDevice(d)">解绑</button>
          </li>
        </ul>
      </section>
      <section class="card">
        <h2>注册配置</h2>
        <form
          @submit.prevent="
            run(() =>
              request('/config', 'PUT', {
                register_level: level,
                contact_info: contact,
              }),
            )
          "
        >
          <label>注册方式<select v-model="level">
            <option :value="0">关闭注册</option>
            <option :value="1">开放注册</option>
            <option :value="2">邮箱验证</option></select></label
          ><label>联系信息<input v-model="contact" /></label
          ><button>保存</button>
        </form>
      </section></template
    >
  </main>
</template>
