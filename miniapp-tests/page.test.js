'use strict';

// Drives the actual page modules: `Page` is stubbed the way the runtime calls
// it, handlers run for real, and every request goes to a live CoolHome server.
// This covers the glue that unit tests on the helpers cannot reach — onLoad,
// widget events, the pending guard and the reload after a write.

const test = require('node:test');
const assert = require('node:assert/strict');
const { execFileSync, spawn } = require('node:child_process');
const fs = require('node:fs');
const net = require('node:net');
const os = require('node:os');
const path = require('node:path');

const REPO = path.resolve(__dirname, '..');
const TOKEN = 'miniapp-page-token';

let server = null;
let baseUrl = '';
let binaryPath = '';
let captured = null;

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
  let last = '';
  while (Date.now() < deadline) {
    try {
      const res = await fetch(url + '/api/health', { headers: { Authorization: 'Bearer ' + TOKEN } });
      if (res.ok) return;
      last = 'status ' + res.status;
    } catch (err) {
      last = String(err.message || err);
    }
    await new Promise((r) => setTimeout(r, 150));
  }
  throw new Error('server did not become healthy: ' + last);
}

function installRuntime(navCalls) {
  const storage = {};
  globalThis.ty = {
    request(options) {
      const method = options.method || 'GET';
      const init = { method, headers: options.header || {} };
      if (options.data !== undefined && method !== 'GET') {
        init.body = typeof options.data === 'string' ? options.data : JSON.stringify(options.data);
      }
      fetch(options.url, init)
        .then(async (res) => {
          const text = await res.text();
          let data = text;
          try {
            data = JSON.parse(text);
          } catch (err) {
            // keep the raw string
          }
          options.success({ statusCode: res.status, data });
        })
        .catch((err) => options.fail && options.fail({ errMsg: 'request:fail ' + (err.message || err) }));
    },
    getStorageSync: (k) => (Object.prototype.hasOwnProperty.call(storage, k) ? storage[k] : ''),
    setStorageSync: (k, v) => {
      storage[k] = v;
    },
    removeStorageSync: (k) => {
      delete storage[k];
    },
    navigateTo: (opts) => navCalls.push(opts.url),
    setNavigationBarTitle: () => {},
    stopPullDownRefresh: () => {},
  };
  globalThis.Page = (config) => {
    captured = config;
  };
  return storage;
}

// installCannedRuntime replaces the network with fixed responses so a page can
// be driven through states the demo server never produces, such as a failed
// cloud sync or a malformed payload.
function installCannedRuntime(responses) {
  const storage = {};
  let index = 0;
  globalThis.ty = {
    request(options) {
      const response = responses[Math.min(index, responses.length - 1)];
      index += 1;
      setTimeout(() => {
        if (response.fail) options.fail({ errMsg: response.fail });
        else options.success({ statusCode: response.statusCode, data: response.data });
      }, 0);
    },
    getStorageSync: (k) => (Object.prototype.hasOwnProperty.call(storage, k) ? storage[k] : ''),
    setStorageSync: (k, v) => {
      storage[k] = v;
    },
    removeStorageSync: (k) => {
      delete storage[k];
    },
    navigateTo: () => {},
    setNavigationBarTitle: () => {},
    stopPullDownRefresh: () => {},
  };
  globalThis.Page = (config) => {
    captured = config;
  };
  return storage;
}

// installGatedRuntime answers immediately except for requests the test chooses
// to hold. That makes "a write is still in flight" a deterministic state
// instead of a race with the event loop.
function installGatedRuntime() {
  const storage = {};
  const gate = { hold: [], release: [], held: 0, calls: {} };
  globalThis.ty = {
    request(options) {
      const url = options.url;
      const send = () => {
        const method = options.method || 'GET';
        const init = { method, headers: options.header || {} };
        if (options.data !== undefined && method !== 'GET') {
          init.body = typeof options.data === 'string' ? options.data : JSON.stringify(options.data);
        }
        fetch(url, init)
          .then(async (res) => {
            const text = await res.text();
            let data = text;
            try {
              data = JSON.parse(text);
            } catch (err) {
              // keep the string
            }
            options.success({ statusCode: res.status, data });
          })
          .catch((err) => options.fail && options.fail({ errMsg: 'request:fail ' + (err.message || err) }));
      };
      gate.calls[url] = (gate.calls[url] || 0) + 1;
      const shouldHold = gate.hold.some((fragment) => url.indexOf(fragment) >= 0);
      if (shouldHold) {
        gate.held += 1;
        gate.release.push(send);
        return;
      }
      send();
    },
    getStorageSync: (k) => (Object.prototype.hasOwnProperty.call(storage, k) ? storage[k] : ''),
    setStorageSync: (k, v) => {
      storage[k] = v;
    },
    removeStorageSync: (k) => {
      delete storage[k];
    },
    navigateTo: () => {},
    setNavigationBarTitle: () => {},
    stopPullDownRefresh: () => {},
  };
  globalThis.Page = (config) => {
    captured = config;
  };
  return { storage, gate };
}

// installScriptedRuntime answers from a fixed script keyed by URL path and can
// hold a request until the test releases it. Tests that assert on an in-flight
// write use this instead of the shared demo server: the demo house is mutable
// and earlier tests would otherwise decide this one's starting state.
function installScriptedRuntime(script) {
  const storage = {};
  const gate = { hold: [], release: [], held: 0, calls: {} };
  globalThis.ty = {
    request(options) {
      // Decode like the server does: the client percent-encodes ids, and a
      // script keyed by a literal id must still match.
      const path = decodeURIComponent(options.url.replace(/^https?:\/\/[^/]+/, ''));
      gate.calls[path] = (gate.calls[path] || 0) + 1;
      const respond = () => {
        const entry = script[path] || script['*'];
        const body = typeof entry === 'function' ? entry(options) : entry;
        setTimeout(() => {
          if (body && body.fail) options.fail({ errMsg: body.fail });
          else options.success({ statusCode: body.statusCode, data: body.data });
        }, 0);
      };
      if (gate.hold.some((fragment) => path.indexOf(fragment) >= 0)) {
        gate.held += 1;
        gate.release.push(respond);
        return;
      }
      respond();
    },
    getStorageSync: (k) => (Object.prototype.hasOwnProperty.call(storage, k) ? storage[k] : ''),
    setStorageSync: (k, v) => {
      storage[k] = v;
    },
    removeStorageSync: (k) => {
      delete storage[k];
    },
    navigateTo: () => {},
    setNavigationBarTitle: () => {},
    stopPullDownRefresh: () => {},
  };
  globalThis.Page = (config) => {
    captured = config;
  };
  return { storage, gate };
}

// loadPage requires a page module fresh and returns an instance with a working
// setData, mirroring what the runtime provides to a page.
function loadPage(relative) {
  const file = require.resolve(path.join(REPO, relative));
  delete require.cache[file];
  captured = null;
  require(file);
  assert.ok(captured, 'page module must call Page()');
  const inst = Object.create(captured);
  inst.data = JSON.parse(JSON.stringify(captured.data));
  inst.setData = function setData(patch) {
    for (const key of Object.keys(patch)) this.data[key] = patch[key];
  };
  return inst;
}

async function until(predicate, label, timeoutMs = 8000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (predicate()) return;
    await new Promise((r) => setTimeout(r, 25));
  }
  throw new Error('timed out waiting for ' + label);
}

// settled reports that the write for a data point finished and the follow-up
// reload landed. `pending` alone is not enough: send() clears it just before
// calling load(), so waiting on it would read the optimistic view model.
function settled(page, code) {
  if (page.data.pending[code] !== undefined) return false;
  const control = page.data.controls.find((c) => c.code === code);
  return !control || control.disabled === false;
}

function event(code, detail, dataset) {
  return {
    currentTarget: { dataset: Object.assign({ code }, dataset || {}) },
    detail: detail,
  };
}

test.before(async () => {
  binaryPath = path.join(os.tmpdir(), 'coolhome-page-test-' + process.pid);
  execFileSync('go', ['build', '-o', binaryPath, './cmd/coolhome'], { cwd: REPO, stdio: 'inherit' });
  const port = await freePort();
  baseUrl = 'http://127.0.0.1:' + port;
  server = spawn(binaryPath, ['-addr', '127.0.0.1:' + String(port)], {
    cwd: REPO,
    env: Object.assign({}, process.env, { COOLHOME_DEMO: 'true', COOLHOME_AUTH_TOKEN: TOKEN }),
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

test('home page asks for setup, then saves and loads the home', async () => {
  const nav = [];
  const storage = installRuntime(nav);
  const api = require('../miniapp/utils/api.js');
  api.clearSettings();

  const home = loadPage('miniapp/pages/home/index.js');
  home.onShow();
  assert.equal(home.data.needsSetup, true, 'a fresh install must ask for the server');

  home.setData({ baseUrl: baseUrl, token: TOKEN });
  home.onSubmitSetup();
  await until(() => home.data.needsSetup === false && home.data.devices.length > 0, 'setup and load');

  assert.equal(home.data.setupError, '');
  assert.equal(home.data.devices.length, 6);
  assert.equal(home.data.demo, true);
  assert.match(home.data.lastSync, /^\d{2}:\d{2}:\d{2}$/);
  assert.deepEqual(Object.keys(storage), ['coolhome.settings'], 'settings must be persisted');

  const light = home.data.devices.find((d) => d.id === 'demo-light-1');
  assert.ok(light, 'the light must be listed');
  assert.equal(light.online, true);
  assert.ok(light.summary.length > 0);
});

test('home page rejects a malformed address without calling the server', () => {
  const nav = [];
  installRuntime(nav);
  const home = loadPage('miniapp/pages/home/index.js');
  home.setData({ baseUrl: 'home.example.com', token: TOKEN });
  home.onSubmitSetup();
  assert.match(home.data.setupError, /https?:\/\//);
  assert.equal(home.data.loading, false);
});

test('home page reports a wrong token as a setup problem', async () => {
  const nav = [];
  installRuntime(nav);
  require('../miniapp/utils/api.js').clearSettings();

  const home = loadPage('miniapp/pages/home/index.js');
  home.setData({ baseUrl: baseUrl, token: 'nope' });
  home.onSubmitSetup();
  await until(() => home.data.setupError.length > 0, 'setup error');
  assert.match(home.data.setupError, /токен/i);
});

test('tapping a device navigates with its id', () => {
  const nav = [];
  installRuntime(nav);
  const home = loadPage('miniapp/pages/home/index.js');
  home.onTapDevice({ currentTarget: { dataset: { id: 'demo-light-1' } } });
  assert.deepEqual(nav, ['/pages/device/index?id=demo-light-1']);
});

test('device page renders controls and readings for a light', async () => {
  const nav = [];
  installRuntime(nav);
  require('../miniapp/utils/api.js').writeSettings({ baseUrl, token: TOKEN });

  const page = loadPage('miniapp/pages/device/index.js');
  page.onLoad({ id: 'demo-light-1' });
  await until(() => page.data.loading === false, 'device load');

  assert.equal(page.data.error, '');
  assert.equal(page.data.name, 'Лампа в гостиной');
  assert.equal(page.data.showOfflineNote, false);
  assert.ok(page.data.readings.length > 0, 'readings must be rendered');

  const kinds = page.data.controls.map((c) => c.kind).sort();
  assert.deepEqual(kinds, ['picker', 'slider', 'switch']);
});

test('toggling a switch writes through and reloads the device', async () => {
  const nav = [];
  installRuntime(nav);
  require('../miniapp/utils/api.js').writeSettings({ baseUrl, token: TOKEN });

  const page = loadPage('miniapp/pages/device/index.js');
  page.onLoad({ id: 'demo-light-1' });
  await until(() => page.data.loading === false, 'device load');

  const sw = page.data.controls.find((c) => c.code === 'switch_led');
  assert.equal(sw.checked, true);

  page.onToggle(event('switch_led', { value: false }));
  // Wait for the state the server accepted, not for the absence of a pending
  // flag: that flag is also absent before the write starts, which would make
  // this assertion race the request.
  await until(() => settled(page, 'switch_led'), 'the switch write to settle');

  assert.equal(page.data.error, '');
  assert.deepEqual(page.data.pending, {}, 'the pending guard must clear');
  const after = page.data.controls.find((c) => c.code === 'switch_led');
  assert.equal(after.checked, false, 'the switch must reflect the accepted write');
});

test('a toggle updates the widget before the request completes', async () => {
  const nav = [];
  installRuntime(nav);
  require('../miniapp/utils/api.js').writeSettings({ baseUrl, token: TOKEN });

  const page = loadPage('miniapp/pages/device/index.js');
  page.onLoad({ id: 'demo-light-1' });
  await until(() => page.data.loading === false, 'device load');

  page.onToggle(event('switch_led', { value: false }));

  // setData is synchronous in this harness, so this observes the state the
  // widget renders while the request is still in flight.
  const optimistic = page.data.controls.find((c) => c.code === 'switch_led');
  assert.equal(optimistic.checked, false, 'the widget must follow the tap immediately');
  assert.equal(optimistic.busy, true, 'the control must show it is busy');
  assert.equal(page.data.pending.switch_led, true, 'a second tap must be ignored');

  await until(() => settled(page, 'switch_led'), 'write to settle');
  const after = page.data.controls.find((c) => c.code === 'switch_led');
  assert.equal(after.disabled, false, 'the control must accept input again after the write');
  assert.equal(after.checked, false);
});

test('a failed write rolls the widget back to the server value', async () => {
  const nav = [];
  installRuntime(nav);
  require('../miniapp/utils/api.js').writeSettings({ baseUrl, token: TOKEN });

  const page = loadPage('miniapp/pages/device/index.js');
  page.onLoad({ id: 'demo-light-1' });
  await until(() => page.data.loading === false, 'device load');

  const before = page.data.controls.find((c) => c.code === 'bright_value');
  assert.equal(before.value, 640);

  // Bypass the slider clamp the way a corrupted payload or a stale thing model
  // would, so the server rejects the write with 400.
  page.applyOptimistic('bright_value', { value: 999999, label: '999999' });
  page.send('bright_value', 999999);

  await until(() => settled(page, 'bright_value') && /maximum|максим/i.test(page.data.error), 'validation error');

  const after = page.data.controls.find((c) => c.code === 'bright_value');
  assert.equal(after.value, 640, 'the widget must fall back to what the server reports');
  assert.deepEqual(page.data.pending, {}, 'the pending guard must clear on failure');
});

test('a slider move sends the scale-shifted integer', async () => {
  const nav = [];
  installRuntime(nav);
  require('../miniapp/utils/api.js').writeSettings({ baseUrl, token: TOKEN });

  const page = loadPage('miniapp/pages/device/index.js');
  page.onLoad({ id: 'demo-thermo-1' });
  await until(() => page.data.loading === false, 'device load');

  const slider = page.data.controls.find((c) => c.code === 'temp_set');
  assert.equal(slider.kind, 'slider');
  assert.equal(slider.value, 22);

  page.onSlide(event('temp_set', { value: 23.5 }, { scale: slider.scale }));
  await until(() => settled(page, 'temp_set'), 'the slider write to settle');

  assert.equal(page.data.error, '');
  assert.deepEqual(page.data.pending, {}, 'the pending guard must clear');
  const after = page.data.controls.find((c) => c.code === 'temp_set');
  assert.equal(after.value, 23.5, 'the slider must land on the value that was sent');
});

test('a picker selection sends the enum option', async () => {
  const nav = [];
  installRuntime(nav);
  require('../miniapp/utils/api.js').writeSettings({ baseUrl, token: TOKEN });

  const page = loadPage('miniapp/pages/device/index.js');
  page.onLoad({ id: 'demo-curtain-1' });
  await until(() => page.data.loading === false, 'device load');

  const picker = page.data.controls.find((c) => c.code === 'control');
  assert.equal(picker.kind, 'picker');

  page.onPick(event('control', { value: 2 })); // "close"
  await until(() => settled(page, 'control'), 'the picker write to settle');

  assert.equal(page.data.error, '');
  const current = page.data.controls.find((c) => c.code === 'control');
  assert.equal(current.options[current.index], 'close');
});

test('home page shows the sync error instead of an empty state', async () => {
  installCannedRuntime([
    {
      statusCode: 200,
      data: { devices: [], error: 'tuya api error 1106: permission deny', demo: false },
    },
  ]);
  require('../miniapp/utils/api.js').writeSettings({ baseUrl: 'https://home.example', token: 't' });

  const home = loadPage('miniapp/pages/home/index.js');
  home.onShow();
  await until(() => home.data.loading === false, 'load');

  assert.match(home.data.error, /1106/, 'the server error must reach the user');
  assert.equal(home.data.showEmpty, false, 'an error is not "no devices"');
  assert.equal(home.data.devices.length, 0);
});

test('home page reports a network failure as a banner', async () => {
  installCannedRuntime([{ fail: 'request:fail timeout' }]);
  require('../miniapp/utils/api.js').writeSettings({ baseUrl: 'https://home.example', token: 't' });

  const home = loadPage('miniapp/pages/home/index.js');
  home.onShow();
  await until(() => home.data.loading === false, 'load');

  assert.match(home.data.error, /timeout|связаться/i);
  assert.equal(home.data.showEmpty, false);
});

test('home page treats a good-but-empty home as empty, not as an error', async () => {
  installCannedRuntime([{ statusCode: 200, data: { devices: [], demo: false } }]);
  require('../miniapp/utils/api.js').writeSettings({ baseUrl: 'https://home.example', token: 't' });

  const home = loadPage('miniapp/pages/home/index.js');
  home.onShow();
  await until(() => home.data.loading === false, 'load');

  assert.equal(home.data.error, '');
  assert.equal(home.data.showEmpty, true);
});

test('device page surfaces a cloud refusal of a write', async () => {
  installCannedRuntime([
    {
      statusCode: 200,
      data: {
        device: {
          id: 'd1',
          name: 'Plug',
          online: true,
          status: [{ code: 'switch_1', value: true }],
          functions: [{ code: 'switch_1', name: 'Питание', type: 'Boolean' }],
          readings: [{ code: 'switch_1', name: 'Питание', type: 'Boolean' }],
        },
      },
    },
    { statusCode: 502, data: { error: 'облако Tuya отклонило команду: timeout' } },
    { statusCode: 200, data: { device: { id: 'd1', name: 'Plug', online: true, status: [{ code: 'switch_1', value: true }], functions: [{ code: 'switch_1', name: 'Питание', type: 'Boolean' }], readings: [] } } },
  ]);
  require('../miniapp/utils/api.js').writeSettings({ baseUrl: 'https://home.example', token: 't' });

  const page = loadPage('miniapp/pages/device/index.js');
  page.onLoad({ id: 'd1' });
  await until(() => page.data.loading === false, 'load');

  page.onToggle(event('switch_1', { value: false }));
  await until(() => page.data.error.length > 0, 'write error');

  assert.match(page.data.error, /отклонило|timeout/i, 'the cloud refusal must be visible');
  const control = page.data.controls.find((c) => c.code === 'switch_1');
  assert.equal(control.checked, true, 'the switch must fall back to the value the server reports');
});

test('a device without a thing model is explained, not shown as a failure', async () => {
  installCannedRuntime([
    {
      statusCode: 200,
      data: {
        device: {
          id: 'rf-1',
          name: 'Внешняя гирлянда',
          category: 'rf_switch1',
          online: true,
          status: [],
          functions: [],
          readings: [],
          spec_unsupported: true,
        },
      },
    },
  ]);
  require('../miniapp/utils/api.js').writeSettings({ baseUrl: 'https://home.example', token: 't' });

  const page = loadPage('miniapp/pages/device/index.js');
  page.onLoad({ id: 'rf-1' });
  await until(() => page.data.loading === false, 'load');

  assert.equal(page.data.error, '', 'an unsupported device is not an error');
  assert.equal(page.data.showUnsupported, true);
  assert.match(page.data.unsupportedNote, /RF-выключатель/, 'the note must match the category');
  assert.equal(page.data.showNoData, false, 'the generic empty message must not double up');
  assert.equal(page.data.controls.length, 0);
});

test('pressing an infrared key sends true and nothing else', async () => {
  const bodies = [];
  installScriptedRuntime({
    '/api/devices/ir:blast:1': {
      statusCode: 200,
      data: {
        device: {
          id: 'ir:blast:1',
          name: 'Телевизор',
          category: 'ir_remote',
          online: true,
          status: [],
          functions: [
            { code: 'power', name: 'Питание', type: 'Button' },
            { code: 'vol_up', name: 'Громче', type: 'Button' },
          ],
          readings: [],
        },
      },
    },
    '/api/devices/ir:blast:1/commands': (options) => {
      bodies.push(options.data);
      return { statusCode: 200, data: {} };
    },
  });
  require('../miniapp/utils/api.js').writeSettings({ baseUrl: 'https://home.example', token: 't' });

  const page = loadPage('miniapp/pages/device/index.js');
  page.onLoad({ id: 'ir:blast:1' });
  await until(() => page.data.loading === false, 'load');

  assert.equal(page.data.controls.length, 2);
  assert.equal(page.data.controls[0].kind, 'button');
  assert.equal(page.data.error, '');

  page.onPress(event('power', {}));
  await until(() => bodies.length === 1, 'the key press to be sent');

  const sent = JSON.parse(bodies[0]);
  assert.deepEqual(sent, { commands: [{ code: 'power', value: true }] });
});

test('a remote with only keys shows no empty message', async () => {
  installCannedRuntime([
    {
      statusCode: 200,
      data: {
        device: {
          id: 'ir:blast:1',
          name: 'Телевизор',
          online: true,
          status: [],
          functions: [{ code: 'power', name: 'Питание', type: 'Button' }],
          readings: [],
        },
      },
    },
  ]);
  require('../miniapp/utils/api.js').writeSettings({ baseUrl: 'https://home.example', token: 't' });

  const page = loadPage('miniapp/pages/device/index.js');
  page.onLoad({ id: 'ir:blast:1' });
  await until(() => page.data.loading === false, 'load');

  assert.equal(page.data.showNoData, false, 'a button-only remote is not empty');
  assert.equal(page.data.readings.length, 0);
});

test('an offline device refuses the command before sending it', async () => {
  const nav = [];
  installRuntime(nav);
  require('../miniapp/utils/api.js').writeSettings({ baseUrl, token: TOKEN });

  const page = loadPage('miniapp/pages/device/index.js');
  page.onLoad({ id: 'demo-humidifier-1' });
  await until(() => page.data.loading === false, 'device load');

  assert.equal(page.data.online, false);
  assert.equal(page.data.showOfflineNote, true);
  assert.equal(page.data.controls[0].disabled, true, 'an offline control must not accept input');

  page.onToggle(event('switch_1', { value: true }));
  assert.match(page.data.error, /офлайн/i);
  assert.deepEqual(page.data.pending, {}, 'nothing may be left pending');

  await until(() => page.data.loading === false, 'reload after refusal');
  const control = page.data.controls.find((c) => c.code === 'switch_1');
  assert.equal(control.checked, false, 'the switch must not show a state that was never sent');
});

test('a correct address is saved even when the server is unsynced', async () => {
  // A 5xx health answer means "server up, cloud failing" — refusing to save
  // would lock the user out of setup exactly when they are configuring.
  installCannedRuntime([
    { statusCode: 503, data: { ok: false, error: 'tuya api error 1106: permission deny' } },
    { statusCode: 200, data: { devices: [], error: 'tuya api error 1106: permission deny', demo: false } },
  ]);
  const storage = installCannedRuntime([
    { statusCode: 503, data: { ok: false, error: 'tuya api error 1106: permission deny' } },
    { statusCode: 200, data: { devices: [], error: 'tuya api error 1106: permission deny', demo: false } },
  ]);
  require('../miniapp/utils/api.js').clearSettings();

  const home = loadPage('miniapp/pages/home/index.js');
  home.setData({ baseUrl: 'https://home.example', token: 'tok' });
  home.onSubmitSetup();
  await until(() => home.data.needsSetup === false, 'settings to be saved');

  assert.equal(home.data.setupError, '', 'a 5xx must not be reported as a setup error');
  assert.ok(storage['coolhome.settings'], 'the address must be persisted');
  await until(() => home.data.loading === false, 'load');
  assert.match(home.data.error, /1106/, 'the deployment problem must still be visible');
});

test('an unreachable address is not saved', async () => {
  const storage = installCannedRuntime([{ fail: 'request:fail timeout' }]);
  require('../miniapp/utils/api.js').clearSettings();

  const home = loadPage('miniapp/pages/home/index.js');
  home.setData({ baseUrl: 'https://nowhere.example', token: 'tok' });
  home.onSubmitSetup();
  await until(() => home.data.setupError.length > 0, 'setup error');

  assert.match(home.data.setupError, /timeout|связаться/i);
  assert.equal(storage['coolhome.settings'], undefined, 'a bad address must not be stored');
});

test('settings survive a runtime whose storage reads back empty', async () => {
  installRuntime([]);
  const api = require('../miniapp/utils/api.js');
  api.clearSettings();

  api.writeSettings({ baseUrl: 'https://home.example', token: 'tok' });
  // A runtime that can set but returns '' on get must not lose the settings.
  const storage = globalThis.ty;
  const readBack = storage.getStorageSync;
  storage.getStorageSync = () => '';

  const settings = api.readSettings();
  assert.equal(settings.baseUrl, 'https://home.example', 'the memory mirror must be consulted');
  assert.equal(settings.token, 'tok');

  storage.getStorageSync = readBack;
  api.clearSettings();
});

test('a reload during a write keeps the busy control disabled', async () => {
  // Scripted, not live: the device starts ON and keeps reporting ON while the
  // command is held, so "the reload rendered" is observable as checked going
  // back to ON. Against the mutable demo house this depended on what earlier
  // tests had done to the lamp, which is how the race slipped through before.
  const onDevice = {
    statusCode: 200,
    data: {
      device: {
        id: 'd1',
        name: 'Lamp',
        online: true,
        status: [{ code: 'switch_1', value: true }],
        functions: [{ code: 'switch_1', name: 'Питание', type: 'Boolean' }],
        readings: [{ code: 'switch_1', name: 'Питание', type: 'Boolean' }],
      },
    },
  };
  const { gate } = installScriptedRuntime({
    '/api/devices/d1': onDevice,
    '/api/devices/d1/commands': { statusCode: 200, data: {} },
  });
  gate.hold = ['/commands'];
  require('../miniapp/utils/api.js').writeSettings({ baseUrl: 'https://home.example', token: 't' });

  const page = loadPage('miniapp/pages/device/index.js');
  page.onLoad({ id: 'd1' });
  await until(() => page.data.loading === false, 'device load');
  assert.equal(page.data.controls[0].checked, true);

  page.onToggle(event('switch_1', { value: false }));
  await until(() => gate.held === 1, 'the command request to be held');

  const inFlight = page.data.controls[0];
  assert.equal(inFlight.checked, false, 'the optimistic value is shown while writing');
  assert.equal(inFlight.disabled, true, 'a control mid-write must reject input');

  page.onPullDownRefresh();
  await until(() => {
    const c = page.data.controls[0];
    return c && c.checked === true && page.data.pending.switch_1 === true;
  }, 'the reload to render the server value');

  const afterRefresh = page.data.controls[0];
  assert.equal(page.data.pending.switch_1, true, 'the write must still be pending');
  assert.equal(afterRefresh.disabled, true, 'a still-pending control must stay disabled across a reload');
  assert.equal(afterRefresh.busy, true);

  // A tap that slips through a stale binding must resync, not vanish.
  const readsBefore = gate.calls['/api/devices/d1'] || 0;
  page.onToggle(event('switch_1', { value: false }));
  await until(() => (gate.calls['/api/devices/d1'] || 0) > readsBefore, 'the swallowed tap to resync');

  gate.release.splice(0).forEach((release) => release());
  await until(() => page.data.pending.switch_1 === undefined && page.data.loading === false, 'write to settle');
});

test('a refused write whose reload fails rolls the widget back', async () => {
  installCannedRuntime([
    {
      statusCode: 200,
      data: {
        device: {
          id: 'd1',
          name: 'Plug',
          online: true,
          status: [{ code: 'switch_1', value: true }],
          functions: [{ code: 'switch_1', name: 'Питание', type: 'Boolean' }],
          readings: [],
        },
      },
    },
    { statusCode: 502, data: { error: 'облако Tuya отклонило команду: offline' } },
    { fail: 'request:fail timeout' },
  ]);
  require('../miniapp/utils/api.js').writeSettings({ baseUrl: 'https://home.example', token: 't' });

  const page = loadPage('miniapp/pages/device/index.js');
  page.onLoad({ id: 'd1' });
  await until(() => page.data.loading === false, 'load');
  assert.equal(page.data.controls[0].checked, true);

  page.onToggle(event('switch_1', { value: false }));
  await until(() => page.data.error.length > 0 && page.data.loading === false, 'write + failed reload');

  const control = page.data.controls[0];
  assert.equal(control.checked, true, 'a value the server never confirmed must not stay on screen');
  assert.match(page.data.error, /отклонило|offline/, 'the refusal reason must survive the reload failure');
  assert.match(page.data.error, /timeout/, 'the reload failure must be reported too');
});

test('every widget in the device template is bound to the disabled flag', () => {
  const fs = require('node:fs');
  const path = require('node:path');
  const tmpl = fs.readFileSync(path.join(REPO, 'miniapp/pages/device/index.tyml'), 'utf8');
  const widgets = tmpl.match(/<(switch|slider|picker)\b[\s\S]*?\/?>/g) || [];
  assert.equal(widgets.length, 3, 'expected a switch, a slider and a picker');
  for (const widget of widgets) {
    const tag = widget.match(/^<(\w+)/)[1];
    assert.match(widget, /disabled="\{\{ctrl\.disabled\}\}"/, tag + ' must bind disabled to ctrl.disabled');
  }
});

test('a server answering 200 with non-JSON is rejected, not saved', async () => {
  const storage = installCannedRuntime([{ statusCode: 200, data: '<html>not coolhome</html>' }]);
  require('../miniapp/utils/api.js').clearSettings();

  const home = loadPage('miniapp/pages/home/index.js');
  home.setData({ baseUrl: 'https://somewhere.example', token: 'tok' });
  home.onSubmitSetup();
  await until(() => home.data.setupError.length > 0, 'setup error');

  assert.match(home.data.setupError, /не CoolHome/i);
  assert.equal(storage['coolhome.settings'], undefined, 'a foreign server must not be persisted');
  assert.equal(home.data.needsSetup, true);
});

test('device page survives an unknown id', async () => {
  const nav = [];
  installRuntime(nav);
  require('../miniapp/utils/api.js').writeSettings({ baseUrl, token: TOKEN });

  const page = loadPage('miniapp/pages/device/index.js');
  page.onLoad({ id: 'no-such-device' });
  await until(() => page.data.loading === false, 'device load');

  assert.match(page.data.error, /не найдено/i);
  assert.equal(page.data.showNoData, false);
});
