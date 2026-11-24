// Which devices are switched on right now.
//
// The rule is not "any boolean is true". A thermostat reports
// upper_temp_switch and lower_temp_switch permanently true because threshold
// alarms are enabled, and plugs carry child_lock; counting those would list
// four windows as switched on while they do nothing. This file holds the rule
// so it can be tested outside the browser.
(function (root) {
  'use strict';

  // Only these codes switch something on: switch, switch_all, switch_led and
  // the numbered relays switch_1..n. A whitelist rather than "any boolean that
  // starts with switch": products carry settings such as switch_prepayment,
  // switch_alarm_sound and switch_backlight, and a prefix rule would count them
  // as power. Threshold flags (upper_temp_switch) are excluded by the same
  // narrowness, which is what keeps four idle windows out of the panel.
  var POWER_EXACT = ['switch', 'switch_all', 'switch_led'];
  var POWER_NUMBERED = /^switch_\d+$/;

  function isPowerCode(code) {
    var c = String(code);
    if (POWER_EXACT.indexOf(c) >= 0) return true;
    return POWER_NUMBERED.test(c);
  }

  function isOn(value) {
    return value === true || value === 1 || value === '1' || value === 'true';
  }

  // powerCode returns the data point that decides whether a device is on, or
  // null when it is off. It answers with the deciding point either way, so a
  // caller can see which switch it consulted.
  //
  // A master switch wins over the relays under it. A socket thermostat reports
  // switch_all = false with switch_1 = true; the panel used to take any true
  // relay and call the heater on, while the device's own switch_all — the first
  // control in its settings — said off. "At least one relay is true" is right
  // only for a strip with no master: there every gang is independent.
  function powerCode(device) {
    var status = (device && device.status) || [];
    var values = {};
    for (var i = 0; i < status.length; i++) values[status[i].code] = status[i].value;

    var functions = (device && device.functions) || [];
    var relays = null;
    for (var j = 0; j < functions.length; j++) {
      var code = functions[j].code;
      if (!isPowerCode(code)) continue;
      // The master decides on its own: if it is off, the device is off.
      if (code === 'switch_all') return isOn(values[code]) ? code : null;
      if (code === 'switch') return isOn(values[code]) ? code : null;
      if (!relays) relays = [];
      relays.push(code);
    }
    if (!relays) return null;
    // No master: one lit gang is enough to call a strip on.
    for (var k = 0; k < relays.length; k++) {
      if (isOn(values[relays[k]])) return relays[k];
    }
    return null;
  }

  var api = { isPowerCode: isPowerCode, powerCode: powerCode };

  root.CoolHomePower = api;
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
})(typeof window !== 'undefined' ? window : globalThis);
