import { createServer } from 'node:http';
import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '../..');
const pages = {
  tc1: 'TC1/http_server/web/index.html',
  a1: 'A1/web/index.html',
  m1: 'M1/web/index.html',
};
const macs = { tc1: '020000000002', a1: '020000000003', m1: '020000000001' };
const defaults = { tc1: 'TC1_A1', a1: 'A1', m1: 'M1' };
const host = process.env.SIM_HOST || '0.0.0.0';
const port = Number(process.env.SIM_PORT || 25174);
const defaultName = model => `${defaults[model]}-${macs[model].slice(-4).toUpperCase()}`;

function makeState(model) {
  const common = {
    name: defaultName(model),
    device_id: macs[model], version: 'simulated', ip: host,
    ssid: 'simulated-wifi', wifi_connected: true, mqtt_status: 'ready',
    mqtt_host: 'simulated-broker', mqtt_user: 'simulator', mqtt_port: 1883,
    time_synced: true, timezone_minutes: 480, ntp_host: 'pool.ntp.org',
    ntp_status: 'synced', ntp_synced: true, free_memory: 32768,
    ota_progress: 0, ota_state: 'idle', uptime_seconds: 3600,
  };
  if (model === 'tc1') return {
    ...common, mqtt_topic_mode: 'legacy', socket_names: Array.from({ length: 6 }, (_, i) => `插座 ${i + 1}`),
    socket_states: [1, 0, 1, 0, 1, 0], child_lock: false, led_enabled: true,
    report_seconds: 2, powerCoefficientX100: 1710, power_watts: 42, energy_kwh: 1.25,
    powers: [35, 40, 38, 42],
  };
  const sensors = {
    sensor_received: true, sensor_valid: true, uart_ready: true, sensorFresh: true,
    age_ms: 500, temperature: 25, humidity: 50, formaldehyde: 0.02, pm25: 20,
    brightness_requested: 4, power_watts: 0, energy_kwh: 0,
  };
  if (model === 'a1') return {
    ...common, ...sensors, model: 'a1', fan_on: true, requested_fan_on: true,
    fan_ready: true, panel_ready: true, child_lock: false, speed: 45,
    percentage: 45, requested_speed: 45, applied_speed: 45, led_enabled: true,
    led_visible: true, led_always_on: true, brightness: 255, red: 30, green: 150, blue: 255,
    light_effect: { effect: 0, segments: 1, follow_fan: 0, quality_pm: 35, quality_hcho: 0.08,
      segment: [{ source: 0, display: 2, minimum: -10, maximum: 50, suitable_low: 18, suitable_high: 26,
        colors: [[0, 0, 255], [0, 255, 0], [255, 0, 0]] }], revision: 1 },
    binding_status: 'disabled',
  };
  return { ...common, ...sensors, model: 'm1', brightness: 50 };
}

function dataFor(state, model, action) {
  if (action === 'status.get') return state;
  if (action === 'capabilities.get') return { model, actions: [] };
  if (action === 'power.get') return { power_watts: state.power_watts || 0, energy_kwh: state.energy_kwh || 0, powers: state.powers || [0] };
  if (action === 'binding.status.get') return { linkage_status: 'disabled', peers: [], members: [], clock_valid: true };
  if (action === 'binding.get') return { response_ids: [], peers: [], members: [], revision: 1 };
  if (action === 'linkage.get') return { enabled: 0, revision: 1 };
  if (action === 'light_effect.get') return state.light_effect || { effect: 0, segments: 1, follow_fan: 0, revision: 1 };
  if (action === 'schedules.get' || action === 'night.get' || action === 'buttons.get') return { items: [], windows: [], revision: 1, capacity: 8, count: 0, next: 1 };
  if (action === 'time.get' || action === 'time/config.get') return { timezone_minutes: state.timezone_minutes, ntp_host: state.ntp_host, ntp_status: state.ntp_status, ntp_synced: true };
  if (action === 'logs.get') return { records: [], next: 1, latest: 0 };
  if (action === 'config.get') return { name: state.name };
  return {};
}

function apply(state, action, params = {}) {
  if (action === 'config.set') {
    if (params.section === 'device' && Object.hasOwn(params.values || {}, 'name')) {
      state.name = params.values.name || defaultName(state.model);
    }
    if (params.section === 'socket') {
      const i = Number(params.values?.index);
      if (Number.isInteger(i) && i >= 0 && i < 6) state.socket_names[i] = params.values.name;
    }
  } else if (action === 'outlet.set') {
    if (params.index === undefined) state.socket_states = state.socket_states.map(() => Number(!!params.on));
    else if (Number.isInteger(params.index) && params.index >= 0 && params.index < 6) state.socket_states[params.index] = Number(!!params.on);
  } else if (action === 'child_lock.set') state.child_lock = !!params.enabled;
  else if (action === 'indicator.set') state.led_enabled = !!params.enabled;
  else if (action === 'fan.set') {
    if (params.enabled !== undefined || params.on !== undefined) state.fan_on = !!(params.enabled ?? params.on);
    if (params.speed !== undefined || params.percentage !== undefined) {
      const speed = Math.max(0, Math.min(100, Number(params.speed ?? params.percentage)));
      state.speed = state.requested_speed = speed;
    }
    state.requested_fan_on = state.fan_on;
    state.percentage = state.applied_speed = state.fan_on ? state.speed : 0;
  }
  else if (action === 'fan.percentage.set') state.percentage = state.speed = Number(params.percentage) || state.speed;
  else if (action === 'light.set') {
    if (params.enabled !== undefined) state.led_enabled = !!params.enabled;
    if (params.always_on !== undefined) state.led_always_on = !!params.always_on;
    if (params.brightness !== undefined) state.brightness = Math.round(Number(params.brightness) * 255 / 100);
    for (const key of ['red', 'green', 'blue']) if (params[key] !== undefined) state[key] = Number(params[key]);
    if (params.color) for (const [key, channel] of Object.entries({ red: 'r', green: 'g', blue: 'b' })) {
      if (params.color[channel] !== undefined) state[key] = Number(params.color[channel]);
    }
  } else if (action === 'light_effect.set') {
    state.light_effect = { ...state.light_effect, ...params };
  } else if (action === 'display.set') state.brightness_requested = Math.round(Number(params.brightness) / 25);
}

function render(model) {
  return execFileSync('python3', ['common/web/page.py', pages[model]], { cwd: root, encoding: 'utf8' });
}

const server = createServer((req, res) => {
  const url = new URL(req.url, `http://${req.headers.host || 'localhost'}`);
  const match = url.pathname.match(/^\/(tc1|a1|m1)\/?$/);
  if (req.method === 'GET' && (url.pathname === '/' || match)) {
    if (!match) {
      res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' });
      res.end('<!doctype html><meta charset="utf-8"><title>Phicomm 设备模拟</title><h1>选择模拟设备</h1><ul><li><a href="/tc1">TC1 插座</a></li><li><a href="/a1">A1 空气净化器</a></li><li><a href="/m1">M1 环境监测仪</a></li></ul>');
      return;
    }
    const model = match[1];
    server.states ||= new Map();
    if (!server.states.has(model)) server.states.set(model, { ...makeState(model), model });
    res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' });
    res.end(render(model));
    return;
  }
  if (req.method === 'POST' && url.pathname === '/api/v1/command') {
    let body = '';
    req.setEncoding('utf8');
    req.on('data', chunk => { body += chunk; if (body.length > 65536) req.destroy(); });
    req.on('end', () => {
      try {
        const request = JSON.parse(body);
        const state = server.states?.get(url.searchParams.get('model') || req.headers.referer?.match(/\/(tc1|a1|m1)/)?.[1]);
        if (!state) throw new Error('请先打开模拟设备首页');
        if (!request.action?.endsWith('.get')) apply(state, request.action, request.params);
        const data = dataFor(state, state.model, request.action);
        res.writeHead(200, { 'Content-Type': 'application/json; charset=utf-8' });
        res.end(JSON.stringify({ code: request.action?.endsWith('.get') ? 'ok' : 'accepted', data }));
      } catch (error) {
        res.writeHead(400, { 'Content-Type': 'application/json; charset=utf-8' });
        res.end(JSON.stringify({ code: 'error', message: error.message }));
      }
    });
    return;
  }
  res.writeHead(404).end('Not found');
});

server.listen(port, host, () => console.log(`Device simulator listening on http://${host}:${port}/`));
