'use strict';

var api = require('../../utils/api.js');
var format = require('../../utils/format.js');
var view = require('../../utils/view.js');

Page({
  data: {
    needsSetup: true,
    baseUrl: '',
    token: '',
    devices: [],
    error: '',
    lastSync: '',
    demo: false,
    loading: false,
    setupError: '',
    showEmpty: false,
  },

  onShow() {
    var settings = api.readSettings();
    if (!settings.baseUrl) {
      this.setData({ needsSetup: true, setupError: '' });
      return;
    }
    this.setData({ needsSetup: false, baseUrl: settings.baseUrl, token: settings.token });
    this.load();
  },

  onPullDownRefresh() {
    this.load().then(function () {
      if (typeof ty !== 'undefined' && ty.stopPullDownRefresh) ty.stopPullDownRefresh();
    });
  },

  load() {
    var self = this;
    var client = api.createClient(api.readSettings());
    this.setData({ loading: true });
    return client
      .listDevices()
      .then(function (snapshot) {
        var devices = (snapshot && snapshot.devices) || [];
        var rows = [];
        for (var i = 0; i < devices.length; i++) rows.push(view.buildDeviceRow(devices[i]));
        self.setData({
          devices: rows,
          error: (snapshot && snapshot.error) || '',
          lastSync: format.formatSyncTime(snapshot && snapshot.last_sync),
          demo: !!(snapshot && snapshot.demo),
          loading: false,
          showEmpty: rows.length === 0 && !(snapshot && snapshot.error),
        });
      })
      .catch(function (err) {
        if (err && err.coolhomeCode === 'unauthorized') {
          self.setData({
            needsSetup: true,
            loading: false,
            setupError: 'Токен не подошёл — проверьте адрес сервера и токен',
          });
          return;
        }
        self.setData({
          error: err && err.message ? err.message : 'Не удалось загрузить устройства',
          loading: false,
          showEmpty: false,
        });
      });
  },

  onBaseUrlInput(e) {
    this.setData({ baseUrl: e.detail.value });
  },

  onTokenInput(e) {
    this.setData({ token: e.detail.value });
  },

  onSubmitSetup() {
    var self = this;
    var baseUrl = String(this.data.baseUrl || '').trim();
    var invalid = api.validateBaseURL(baseUrl);
    if (invalid) {
      this.setData({ setupError: invalid });
      return;
    }
    var candidate = api.normalizeSettings({ baseUrl: baseUrl, token: this.data.token });
    this.setData({ loading: true, setupError: '' });
    var save = function () {
      api.writeSettings(candidate);
      self.setData({
        needsSetup: false,
        baseUrl: candidate.baseUrl,
        token: candidate.token,
        loading: false,
      });
      return self.load();
    };
    // Probe before saving so a typo does not leave the app in a state where
    // every screen fails; the token is optional on a trusted network.
    api
      .createClient(candidate)
      .health()
      .then(save)
      .catch(function (err) {
        var code = (err && err.coolhomeCode) || '';
        // A 5xx means the server answered but its cloud sync is failing — bad
        // Tuya credentials, wrong region, an outage. That is a deployment
        // problem to show on the home screen, not a reason to refuse a correct
        // address and lock the user out of the setup form.
        if (/^http-5/.test(code)) {
          save().then(function () {
            self.setData({ error: (err && err.message) || 'Сервер не синхронизирован' });
          });
          return;
        }
        var message = err && err.message ? err.message : 'Сервер не ответил';
        if (code === 'unauthorized') {
          message = 'Сервер требует токен — введите COOLHOME_AUTH_TOKEN';
        }
        self.setData({ loading: false, setupError: message });
      });
  },

  onEditServer() {
    api.clearSettings();
    this.setData({
      needsSetup: true,
      baseUrl: '',
      token: '',
      setupError: '',
      devices: [],
      error: '',
      showEmpty: false,
    });
  },

  onTapDevice(e) {
    var id = e.currentTarget.dataset.id;
    if (!id) return;
    if (typeof ty !== 'undefined' && ty.navigateTo) {
      ty.navigateTo({ url: '/pages/device/index?id=' + encodeURIComponent(id) });
    }
  },

  onRefreshTap() {
    var self = this;
    var client = api.createClient(api.readSettings());
    this.setData({ loading: true });
    client
      .refresh()
      .then(function () {
        return self.load();
      })
      .catch(function (err) {
        self.setData({
          error: err && err.message ? err.message : 'Обновление не удалось',
          loading: false,
        });
      });
  },
});
