'use strict';

// Exercises the mini-app's API client against a real CoolHome server.
//
// The bundle cannot run outside the Tuya IDE, so the runtime is stubbed the same
// way the host exposes it: a global `ty` with WeChat-style callbacks and sync
// storage. Everything below the transport — settings, headers, error mapping,
// command payloads — is the code that ships in the bundle.

const test = require('node:test');
const assert = require('node:assert/strict');
const { execFileSync, spawn } = require('node:child_process');
const fs = require('node:fs');
const net = require('node:net');
const os = require('node:os');
const path = require('node:path');

const REPO = path.resolve(__dirname, '..');
const TOKEN = 'miniapp-test-token';

let server = null;
let baseUrl = '';
let binaryPath = '';

function freePort() {
  return new Promise((resolve, reject) => {
    const srv = net.createServer();
    srv.once('error', reject);
    srv.listen(0, '127.0.0.1', () => {
      const { port } = srv.address();
      srv.close(() => resolve(port));
    });
  });
}

async function waitForHealth(url, timeoutMs = 20000) {
  const deadline = Date.now() + timeoutMs;
  let lastError = '';
  while (Date.now() < deadline) {
    try {
      const res = await fetch(url + '/api/health', {
        headers: { Authorization: 'Bearer ' + TOKEN },
      });
      if (res.ok) return;
      lastError = 'status ' + res.status;
    } catch (err) {
      lastError = String(err.message || err);
    }
    await new Promise((r) => setTimeout(r, 150));
  }
  throw new Error('server did not become healthy: ' + lastError);
}

// installRuntime mirrors how the mini-app runtime hands `ty` to the bundle.
function installRuntime() {
  const storage = {};
  const runtime = {
    request(options) {
      const method = options.method || 'GET';
      const init = { method, headers: options.header || {} };
      if (options.data !== undefined && method !== 'GET') {
        // The client serializes JSON itself; a string body goes on the wire
        // verbatim, exactly like the real runtime.
        init.body = typeof options.data === 'string' ? options.data : JSON.stringify(options.data);
      }
      fetch(options.url, init)
        .then(async (res) => {
          const text = await res.text();
          let data = text;
          try {
            data = JSON.parse(text);
          } catch (err) {
            // A non-JSON body stays a string, exactly like a real response.
          }
          options.success({ statusCode: res.status, data });
        })
        .catch((err) => {
          if (options.fail) options.fail({ errMsg: 'request:fail ' + (err.message || err) });
        });
    },
    getStorageSync(key) {
      return Object.prototype.hasOwnProperty.call(storage, key) ? storage[key] : '';
    },
    setStorageSync(key, value) {
      storage[key] = value;
    },
    removeStorageSync(key) {
      delete storage[key];
    },
  };
  globalThis.ty = runtime;
  return storage;
}

test.before(async () => {
  binaryPath = path.join(os.tmpdir(), 'coolhome-miniapp-test-' + process.pid);
  execFileSync('go', ['build', '-o', binaryPath, './cmd/coolhome'], { cwd: REPO, stdio: 'inherit' });
  const port = await freePort();
  baseUrl = 'http://127.0.0.1:' + port;
  server = spawn(binaryPath, ['-addr', '127.0.0.1:' + String(port)], {
    cwd: REPO,
    env: Object.assign({}, process.env, {
      COOLHOME_DEMO: 'true',
      COOLHOME_AUTH_TOKEN: TOKEN,
    }),
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  server.stdout.on('data', () => {});
  server.stderr.on('data', () => {});
  await waitForHealth(baseUrl);
});

test.after(() => {
  if (server) server.kill('SIGTERM');
  if (binaryPath && fs.existsSync(binaryPath)) fs.unlinkSync(binaryPath);
});

test('settings survive a storage round trip and are normalized', () => {
  installRuntime();
  const api = require('../miniapp/utils/api.js');

  assert.deepEqual(api.readSettings(), { baseUrl: '', token: '' });

  api.writeSettings({ baseUrl: baseUrl + '/', token: '  ' + TOKEN + '  ' });
  const stored = api.readSettings();
  assert.equal(stored.baseUrl, baseUrl, 'trailing slash must be stripped');
  assert.equal(stored.token, TOKEN, 'token must be trimmed');

  api.clearSettings();
  assert.deepEqual(api.readSettings(), { baseUrl: '', token: '' });
});

test('validateBaseURL rejects a bare host and accepts http(s)', () => {
  const api = require('../miniapp/utils/api.js');
  assert.match(api.validateBaseURL('home.example.com'), /https?:\/\//);
  assert.match(api.validateBaseURL(''), /https?:\/\//);
  assert.equal(api.validateBaseURL('https://home.example.com'), '');
  assert.equal(api.validateBaseURL('http://127.0.0.1:8080'), '');
});

test('listDevices returns the demo home', async () => {
  installRuntime();
  const api = require('../miniapp/utils/api.js');
  const client = api.createClient({ baseUrl, token: TOKEN });

  const snapshot = await client.listDevices();
  assert.equal(snapshot.demo, true);
  assert.equal(snapshot.devices.length, 6);

  const light = snapshot.devices.find((d) => d.id === 'demo-light-1');
  assert.ok(light, 'demo-light-1 must be present');
  assert.ok(light.functions.some((f) => f.code === 'switch_led'), 'light must expose switch_led');
  assert.ok(Array.isArray(light.status), 'status must be an array');
});

test('getDevice returns one device with its thing model', async () => {
  installRuntime();
  const api = require('../miniapp/utils/api.js');
  const client = api.createClient({ baseUrl, token: TOKEN });

  const payload = await client.getDevice('demo-light-1');
  assert.equal(payload.device.id, 'demo-light-1');
  const brightness = payload.device.functions.find((f) => f.code === 'bright_value');
  assert.ok(brightness, 'bright_value must be exposed');
  assert.equal(typeof brightness.min, 'number');
  assert.equal(typeof brightness.max, 'number');
});

test('sendCommands writes a data point the server accepts', async () => {
  installRuntime();
  const api = require('../miniapp/utils/api.js');
  const client = api.createClient({ baseUrl, token: TOKEN });

  await client.sendCommands('demo-light-1', [{ code: 'switch_led', value: false }]);
  let payload = await client.getDevice('demo-light-1');
  let state = payload.device.status.find((s) => s.code === 'switch_led');
  assert.equal(state.value, false, 'switch_led should be off after the command');

  await client.sendCommands('demo-light-1', [{ code: 'switch_led', value: true }]);
  payload = await client.getDevice('demo-light-1');
  state = payload.device.status.find((s) => s.code === 'switch_led');
  assert.equal(state.value, true, 'switch_led should be on again');
});

test('a scaled slider value is sent as the shifted integer', async () => {
  installRuntime();
  const api = require('../miniapp/utils/api.js');
  const format = require('../miniapp/utils/format.js');
  const client = api.createClient({ baseUrl, token: TOKEN });

  const payload = await client.getDevice('demo-thermo-1');
  const target = payload.device.functions.find((f) => f.code === 'temp_set');
  assert.ok(target, 'thermostat must expose temp_set');

  // The user drags to 22.5 in display units; the wire value is scale-shifted.
  const wire = format.fromDisplay(22.5, target);
  assert.ok(Number.isInteger(wire), 'wire value must be an integer');

  await client.sendCommands('demo-thermo-1', [{ code: target.code, value: wire }]);
  const after = await client.getDevice('demo-thermo-1');
  const state = after.device.status.find((s) => s.code === target.code);
  assert.equal(state.value, wire);
  assert.equal(format.toDisplay(state.value, target), 22.5);
});

test('a rejected value surfaces the server message as a 400 error', async () => {
  installRuntime();
  const api = require('../miniapp/utils/api.js');
  const client = api.createClient({ baseUrl, token: TOKEN });

  await assert.rejects(
    () => client.sendCommands('demo-light-1', [{ code: 'bright_value', value: 999999 }]),
    (err) => {
      assert.equal(err.coolhomeCode, 'http-400');
      assert.match(err.message, /maximum|максим/i);
      return true;
    }
  );
});

test('an offline device reports the cloud refusal', async () => {
  installRuntime();
  const api = require('../miniapp/utils/api.js');
  const client = api.createClient({ baseUrl, token: TOKEN });

  await assert.rejects(
    () => client.sendCommands('demo-humidifier-1', [{ code: 'switch_1', value: true }]),
    (err) => {
      assert.equal(err.coolhomeCode, 'http-502');
      assert.match(err.message, /offline/i);
      return true;
    }
  );
});

test('a wrong token maps to the unauthorized code the UI acts on', async () => {
  installRuntime();
  const api = require('../miniapp/utils/api.js');
  const client = api.createClient({ baseUrl, token: 'wrong-token' });

  await assert.rejects(
    () => client.listDevices(),
    (err) => {
      assert.equal(err.coolhomeCode, 'unauthorized');
      return true;
    }
  );
});

test('a missing transport fails loudly instead of hanging', async () => {
  delete globalThis.ty;
  // A fresh module instance is not needed: the transport resolves the host at
  // call time, which is the property under test.
  const api = require('../miniapp/utils/api.js');
  const client = api.createClient({ baseUrl, token: TOKEN });

  await assert.rejects(
    () => client.health(),
    (err) => {
      assert.equal(err.coolhomeCode, 'no-transport');
      return true;
    }
  );
  installRuntime();
});

test('health and refresh work through the same client', async () => {
  installRuntime();
  const api = require('../miniapp/utils/api.js');
  const client = api.createClient({ baseUrl, token: TOKEN });

  const health = await client.health();
  assert.equal(health.ok, true);
  assert.equal(health.devices, 6);

  const refreshed = await client.refresh();
  assert.equal(refreshed.devices.length, 6);
});
