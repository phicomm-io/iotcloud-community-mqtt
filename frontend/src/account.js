import { reactive } from "vue";
export const account = reactive({
  username: null,
  mqttUsername: null,
  aliases: [],
  isAdmin: false,
  password: "",
  loaded: false,
});
export async function api(path, options = {}) {
  const response = await fetch(`/api${path}`, {
    credentials: "same-origin",
    ...options,
    headers: { "Content-Type": "application/json", ...options.headers },
  });
  const data = await response.json().catch(() => ({}));
  if (!response.ok) {
    if (response.status === 401 && !path.startsWith("/admin")) {
      account.username = null;
      account.mqttUsername = null;
      account.aliases = [];
      account.isAdmin = false;
      account.password = "";
      window.dispatchEvent(new Event("phicomm-session-expired"));
    }
    if (response.status === 403 && path.startsWith("/admin") && data.error === "没有管理权限") account.isAdmin = false;
    throw new Error(
      data.error || data.message || `请求失败 (${response.status})`,
    );
  }
  return data;
}
export async function restoreAccount() {
  if (account.loaded) return;
  try {
    const data = await api("/auth/me");
    applyAccount(data);
  } catch {
    /* Guest access remains available. */
  }
  account.loaded = true;
}

export async function refreshAccountRole() {
  const owner = account.username;
  if (!owner) return;
  const data = await api("/auth/me");
  if (account.username === owner) applyAccount(data);
}

export function applyAccount(data) {
  account.username = data.username;
  account.mqttUsername = data.mqtt_username || data.username;
  account.aliases = data.aliases || [];
  account.isAdmin = data.is_admin === true;
}
