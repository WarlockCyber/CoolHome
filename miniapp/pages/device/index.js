'use strict';

var api = require('../../utils/api.js');
var format = require('../../utils/format.js');
var view = require('../../utils/view.js');

// merge copies a plain object; Object.assign is avoided so the bundle does not
// depend on a polyfill the mini-app runtime may not ship.
function merge(source, patch) {
  var out = {};
  var key;
  for (key in source) {
    if (Object.prototype.hasOwnProperty.call(source, key)) out[key] = source[key];
  }
  for (key in patch) {
    if (Object.prototype.hasOwnProperty.call(patch, key)) out[key] = patch[key];
  }
  return out;
}

function findControl(controls, code) {
  for (var i = 0; i < controls.length; i++) {
    if (controls[i].code === code) return controls[i];
  }
  return null;
}

Page({
  data: {
    id: '',
    name: '',
    online: true,
    category: '',
    readings: [],
    controls: [],
    error: '',
    loading: true,
    pending: {},
    showOfflineNote: false,
    showNoData: false,
    showUnsupported: false,
    unsupportedNote: '',
    deviceNote: '',
  },

  onLoad(query) {
    var id = (query && query.id) || '';
    this.setData({ id: id });
    if (!id) {
      this.setData({ error: 'Устройство не выбрано', loading: false });
      return;
    }
    this.load();
  },

  onPullDownRefresh() {
    this.load().then(function () {
      if (typeof ty !== 'undefined' && ty.stopPullDownRefresh) ty.stopPullDownRefresh();
    });
  },

  // load re-reads the device. keepError preserves a message that explains why a
  // write failed: the reload is what repairs the widgets, and it must not erase
  // the reason shown to the user on the way out.
  load(options) {
    var self = this;
    var keepError = !!(options && options.keepError);
    var client = api.createClient(api.readSettings());
    return client
      .getDevice(this.data.id)
      .then(function (payload) {
        var device = (payload && payload.device) || {};
        var readings = view.buildReadings(device);
        var controls = view.buildControls(device);
        var pending = {};
        for (var i = 0; i < controls.length; i++) {
          if (self.data.pending[controls[i].code]) {
            pending[controls[i].code] = true;
            // busy alone is not enough: buildControls derives `disabled` from
            // online-ness only, so a reload during an in-flight write would
            // re-enable the control and the next tap would be swallowed.
            controls[i].busy = true;
            controls[i].disabled = true;
          }
        }
        self.lastGood = { controls: controls, readings: readings };
        self.setData({
          name: device.name || device.id || self.data.id,
          online: device.online !== false,
          category: device.category || '',
          readings: readings,
          controls: controls,
          pending: pending,
          error: keepError ? self.data.error : '',
          loading: false,
          showOfflineNote: device.online === false,
          showUnsupported: !!device.spec_unsupported,
          unsupportedNote: device.note || (device.spec_unsupported ? view.unsupportedNote(device.category, !!payload.ir) : ''),
          deviceNote: device.note || '',
          showNoData: !device.spec_unsupported && readings.length === 0 && controls.length === 0,
        });
        if (typeof ty !== 'undefined' && ty.setNavigationBarTitle) {
          ty.setNavigationBarTitle({ title: device.name || 'Устройство' });
        }
      })
      .catch(function (err) {
        var message = err && err.message ? err.message : 'Не удалось загрузить устройство';
        var patch = {
          error: keepError && self.data.error ? self.data.error + ' · ' + message : message,
          loading: false,
        };
        // The corrective reload failed too, so the optimistic value is still on
        // screen and the server never confirmed it: roll back explicitly.
        if (keepError && self.lastGood) {
          patch.controls = self.lastGood.controls;
          patch.readings = self.lastGood.readings;
          patch.pending = {};
        }
        self.setData(patch);
      });
  },

  // applyOptimistic writes the value the user just chose into the view model so
  // the widget and the data agree while the request is in flight. Without it a
  // failed write leaves a native switch showing a state the device never
  // reached, because the reloaded value would be identical and re-render
  // nothing.
  applyOptimistic(code, patch) {
    var controls = [];
    for (var i = 0; i < this.data.controls.length; i++) {
      var control = this.data.controls[i];
      if (control.code !== code) {
        controls.push(control);
        continue;
      }
      var next = merge(control, patch);
      next.busy = true;
      next.disabled = true;
      controls.push(next);
    }
    this.setData({ controls: controls });
  },

  // send posts one command and reloads, so the page reflects what the cloud
  // accepted rather than the tap. `patch` is the optimistic view-model change;
  // it is applied only after the guards pass, because a refused write must not
  // leave a widget showing a state that was never sent.
  send(code, value, patch) {
    var self = this;
    if (this.data.pending[code]) {
      // Reachable only through a stale binding (a reload re-enabling a control
      // whose write is still in flight). Re-sync instead of dropping the tap
      // silently, so the widget shows what the server actually reports.
      return this.load();
    }
    if (format.isOffline({ online: this.data.online })) {
      this.setData({ error: 'Устройство офлайн — команда не будет доставлена' });
      // The native widget has already moved; re-read so it snaps back.
      return self.load({ keepError: true });
    }

    // Snapshot what the server last confirmed, before the optimistic change,
    // so a refused write whose corrective reload also fails can be undone.
    this.lastGood = { controls: this.data.controls, readings: this.data.readings };
    if (patch) this.applyOptimistic(code, patch);

    var pending = merge(this.data.pending, {});
    pending[code] = true;
    this.setData({ pending: pending, error: '' });

    var client = api.createClient(api.readSettings());
    return client
      .sendCommands(this.data.id, [{ code: code, value: value }])
      .then(function () {
        var cleared = merge(self.data.pending, {});
        delete cleared[code];
        self.setData({ pending: cleared });
        return self.load();
      })
      .catch(function (err) {
        var cleared = merge(self.data.pending, {});
        delete cleared[code];
        self.setData({
          pending: cleared,
          error: err && err.message ? err.message : 'Команда не отправлена',
        });
        // Reload so the UI falls back to the state the server still reports,
        // while keeping the reason the write was refused on screen.
        return self.load({ keepError: true });
      });
  },

  onToggle(e) {
    var code = e.currentTarget.dataset.code;
    var value = !!(e.detail && e.detail.value);
    this.send(code, value, { checked: value });
  },

  onSlide(e) {
    var dataset = e.currentTarget.dataset;
    var display = Number(e.detail && e.detail.value);
    if (isNaN(display)) return;
    var scale = Number(dataset.scale || 0);
    var raw = format.fromDisplay(display, { scale: scale });
    if (raw === null) return;
    this.send(dataset.code, raw, {
      value: display,
      label: format.formatValue(display, { scale: 0, unit: this.unitOf(dataset.code) }),
    });
  },

  // onPress fires an infrared key. There is no state to show optimistically,
  // so the patch is omitted and the control is only marked busy by send().
  onPress(e) {
    this.send(e.currentTarget.dataset.code, true);
  },

  onPick(e) {
    var dataset = e.currentTarget.dataset;
    var index = Number(e.detail && e.detail.value);
    var control = findControl(this.data.controls, dataset.code);
    if (!control || !control.options || !control.options.length) return;
    var option = control.options[index];
    if (option === undefined) return;
    this.send(dataset.code, option, {
      index: index,
      label: control.labels[index] || '',
    });
  },

  unitOf(code) {
    var control = findControl(this.data.controls, code);
    return control ? control.unit : '';
  },

  onRetry() {
    this.setData({ loading: true });
    this.load();
  },
});
