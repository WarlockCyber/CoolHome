// The side panel's list of devices.
//
// It is a list of device ids, nothing more: the card itself is drawn from the
// live device, so a rename in the app shows up without touching the layout.
// The rules are here rather than inline in the page because each of them has a
// failure that is invisible from the outside — a duplicate that makes the same
// card appear twice, an add past the limit that the server then rejects, a
// removal that leaves a gap in the numbering the arrows rely on.
(function (root) {
  'use strict';

  // MAX mirrors dashboard.MaxPanel on the server. The server rejects more, and a
  // request the server will reject should not be sent: the UI stops first and
  // says why.
  var MAX = 12;

  function normalize(panel) {
    var out = [];
    var seen = {};
    var list = Array.isArray(panel) ? panel : [];
    for (var i = 0; i < list.length; i++) {
      var id = String(list[i] == null ? '' : list[i]).trim();
      if (!id || seen[id]) continue;
      if (out.length >= MAX) break;
      seen[id] = true;
      out.push(id);
    }
    return out;
  }

  function has(panel, deviceId) {
    var id = String(deviceId == null ? '' : deviceId).trim();
    if (!id) return false;
    return normalize(panel).indexOf(id) >= 0;
  }

  // add appends a device. It reports why nothing happened instead of silently
  // doing nothing: an add that appears to work and then vanishes on reload is
  // the worst kind of failure on a wall.
  function add(panel, deviceId, max) {
    var list = normalize(panel);
    var id = String(deviceId == null ? '' : deviceId).trim();
    var limit = typeof max === 'number' && max > 0 ? max : MAX;
    if (!id) return { panel: list, error: 'пустой идентификатор устройства' };
    if (list.indexOf(id) >= 0) return { panel: list, error: 'устройство уже на панели' };
    if (list.length >= limit) {
      return { panel: list, error: 'на панели уже ' + limit + ' карточек — это предел' };
    }
    list.push(id);
    return { panel: list, error: '' };
  }

  function remove(panel, deviceId) {
    var id = String(deviceId == null ? '' : deviceId).trim();
    return normalize(panel).filter(function (x) {
      return x !== id;
    });
  }

  // move shifts one entry by delta, clamped. A no-op returns a fresh list so the
  // caller can persist without asking whether anything changed.
  function move(panel, index, delta) {
    var list = normalize(panel);
    if (!Number.isInteger(index) || index < 0 || index >= list.length) return list;
    var to = index + delta;
    if (!Number.isInteger(to) || to < 0 || to >= list.length) return list;
    var moved = list.splice(index, 1)[0];
    list.splice(to, 0, moved);
    return list;
  }

  // missing lists the ids that no longer name a device. The panel keeps their
  // slots so it does not rearrange itself under a hand; the UI draws a
  // placeholder and offers to remove it.
  function missing(panel, devices) {
    var known = {};
    var list = Array.isArray(devices) ? devices : [];
    for (var i = 0; i < list.length; i++) {
      if (list[i] && list[i].id) known[list[i].id] = true;
    }
    return normalize(panel).filter(function (id) {
      return !known[id];
    });
  }

  var api = {
    MAX: MAX,
    normalize: normalize,
    has: has,
    add: add,
    remove: remove,
    move: move,
    missing: missing,
  };

  root.CoolHomePanel = api;
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
})(typeof window !== 'undefined' ? window : globalThis);
