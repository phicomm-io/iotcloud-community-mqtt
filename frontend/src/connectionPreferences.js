// Legacy credentials stay untouched until a successful platform login.
export function legacyCredentials(storage = localStorage) {
 try {
  const value = JSON.parse(storage.getItem('mqtt_config') || 'null');
  if (typeof value?.username !== 'string' || !value.username.trim() || typeof value.password !== 'string' || !value.password) return null;
  return {username:value.username, password:value.password};
 } catch { return null; }
}
const key = owner => 'phicomm.connection.' + owner;
export function autoConnect(owner, storage = localStorage) {
 try {
  const value = storage.getItem(key(owner));
  return value === null ? storage.getItem('mqtt_auto_connect') !== 'false' : value !== 'false';
 } catch { return true; }
}
export function setAutoConnect(owner, enabled, storage = localStorage) {
 try { storage.setItem(key(owner), String(enabled)); } catch { /* Storage may be disabled. */ }
}
export function rememberLogin(owner, password, storage = localStorage) {
 try { storage.setItem('phicomm.mqtt-login', JSON.stringify({owner,password})); } catch { /* Keep in-memory credentials. */ }
}
export function savedPassword(owner, storage = localStorage) {
 try { const saved=JSON.parse(storage.getItem('phicomm.mqtt-login') || 'null'); return saved?.owner===owner && typeof saved.password==='string' ? saved.password : ''; } catch { return ''; }
}
export function forgetLogin(storage = localStorage) {
 try { storage.removeItem('phicomm.mqtt-login'); } catch { /* Storage may be disabled. */ }
}
