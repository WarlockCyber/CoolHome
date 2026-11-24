'use strict';

// Client for the CoolHome REST API, adapted to the Tuya mini-app runtime.
//
// The runtime exposes a WeChat-compatible host API (`ty.request`,
// `ty.setStorageSync`, ...). Published typing packages do not describe the
// network call, so the transport below resolves the host object at call time
// and supports both `ty` and `wx`. When neither is present the request fails
// with an explicit message instead of hanging.

var SETTINGS_KEY = 'coolhome.settings';
var memorySettings = '';

function host() {
  if (typeof ty !== 'undefined' && ty && typeof ty.request === 'function') return ty;
  if (typeof wx !== 'undefined' && wx && typeof wx.request === 'function') return wx;
  return null;
}

function readSettings() {
  var raw = '';
  try {
    var api = host();
    if (api && typeof api.getStorageSync === 'function') {
      raw = api.getStorageSync(SETTINGS_KEY);
    } else {
      raw = memorySettings;
    }
  } catch (e) {
    raw = memorySettings;
  }
  // A runtime that can set but not get returns '' here; fall back to the
  // in-memory mirror so the app does not bounce back to the setup screen.
  if (!raw) raw = memorySettings;
  if (!raw) return { baseUrl: '', token: '' };
  if (typeof raw === 'object') return normalizeSettings(raw);
  try {
    return normalizeSettings(JSON.parse(raw));
  } catch (e) {
    return { baseUrl: '', token: '' };
  }
}

function writeSettings(settings) {
  var value = normalizeSettings(settings);
  var raw = JSON.stringify(value);
  // Mirror to memory first: a runtime that can set but not get (or that throws
  // mid-write) must not leave the app pointing at stale settings.
  memorySettings = raw;
  try {
    var api = host();
    if (api && typeof api.setStorageSync === 'function') {
      api.setStorageSync(SETTINGS_KEY, raw);
    }
  } catch (e) {
    // Memory already holds the value; the next screen still works.
  }
  return value;
}

function clearSettings() {
  memorySettings = '';
  try {
    var api = host();
    if (api && typeof api.removeStorageSync === 'function') api.removeStorageSync(SETTINGS_KEY);
  } catch (e) {
    // Nothing to do: settings are already cleared in memory.
  }
}

// normalizeSettings trims input and strips the trailing slash so paths can be
// joined with a single "/", and drops a token that is whitespace only.
function normalizeSettings(settings) {
  var src = settings || {};
  var baseUrl = String(src.baseUrl || '').trim();
  while (baseUrl.length > 1 && baseUrl.charAt(baseUrl.length - 1) === '/') {
    baseUrl = baseUrl.slice(0, -1);
  }
  return {
    baseUrl: baseUrl,
    token: String(src.token || '').trim(),
  };
}

// validateBaseURL accepts an absolute http(s) URL, because the mini-app runs
// inside a TLS webview and a bare host would silently fail the request.
function validateBaseURL(baseUrl) {
  var value = String(baseUrl || '').trim();
  if (!/^https?:\/\/[^\s/?#]+/i.test(value)) {
    return 'Адрес должен начинаться с http:// или https:// и содержать хост';
  }
  return '';
}

function apiError(message, code) {
  var err = new Error(message);
  err.coolhomeCode = code || 'error';
  return err;
}

// transport performs one request and resolves {statusCode, data}.
function transport(options) {
  return new Promise(function (resolve, reject) {
    var api = host();
    if (!api) {
      reject(apiError('Рантайм не предоставил сетевой API (ty.request или wx.request)', 'no-transport'));
      return;
    }
    api.request({
      url: options.url,
      method: options.method,
      header: options.header,
      data: options.data,
      success: function (res) {
        resolve({ statusCode: res.statusCode, data: res.data });
      },
      fail: function (res) {
        var message = (res && (res.errMsg || res.errmsg)) || 'сеть недоступна';
        reject(apiError('Не удалось связаться с CoolHome: ' + message, 'network'));
      },
    });
  });
}

function createClient(settings) {
  var cfg = normalizeSettings(settings);

  function call(method, path, body) {
    var header = {};
    if (cfg.token) header.Authorization = 'Bearer ' + cfg.token;
    var payload;
    if (body !== undefined) {
      header['Content-Type'] = 'application/json';
      // Serialize here instead of passing an object: a string body is sent
      // verbatim by both ty.request and wx.request, while an object depends on
      // the runtime honoring Content-Type. This keeps the wire format ours.
      payload = JSON.stringify(body);
    }
    return transport({
      url: cfg.baseUrl + path,
      method: method,
      header: header,
      data: payload,
    }).then(function (res) {
      var payload = res.data;
      if (typeof payload === 'string') {
        try {
          payload = JSON.parse(payload);
        } catch (e) {
          payload = null;
        }
      }
      if (res.statusCode === 401) {
        throw apiError('Нужен токен доступа CoolHome', 'unauthorized');
      }
      if (res.statusCode < 200 || res.statusCode >= 300) {
        var message = (payload && payload.error) || 'Ошибка HTTP ' + res.statusCode;
        throw apiError(message, 'http-' + res.statusCode);
      }
      // Every CoolHome endpoint answers with a JSON object. A 2xx carrying
      // anything else means the address points at some other server (a captive
      // portal, an SPA fallback, a proxy): treating that as success would save
      // a wrong address and show an empty home with no explanation.
      if (!payload || typeof payload !== 'object') {
        throw apiError('По адресу отвечает не CoolHome: ожидался JSON', 'not-coolhome');
      }
      return payload;
    });
  }

  return {
    settings: cfg,
    health: function () {
      return call('GET', '/api/health');
    },
    listDevices: function () {
      return call('GET', '/api/devices');
    },
    getDevice: function (id) {
      return call('GET', '/api/devices/' + encodeURIComponent(id));
    },
    refresh: function () {
      return call('POST', '/api/refresh');
    },
    sendCommands: function (id, commands) {
      return call('POST', '/api/devices/' + encodeURIComponent(id) + '/commands', { commands: commands });
    },
  };
}

module.exports = {
  SETTINGS_KEY: SETTINGS_KEY,
  readSettings: readSettings,
  writeSettings: writeSettings,
  clearSettings: clearSettings,
  normalizeSettings: normalizeSettings,
  validateBaseURL: validateBaseURL,
  createClient: createClient,
};
