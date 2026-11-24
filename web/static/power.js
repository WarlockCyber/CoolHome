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

  function valuesOf(device) {
    var status = (device && device.status) || [];
    var values = {};
    for (var i = 0; i < status.length; i++) values[status[i].code] = status[i].value;
    return values;
  }

  function powerCodes(device) {
    var functions = (device && device.functions) || [];
    var codes = [];
    for (var i = 0; i < functions.length; i++) {
      if (isPowerCode(functions[i].code)) codes.push(functions[i].code);
    }
    return codes;
  }

  // masterOf picks the switch that speaks for the whole device: the master
  // first, then a lamp's own switch, then a lone switch.
  function masterOf(codes) {
    if (codes.indexOf('switch_all') >= 0) return 'switch_all';
    if (codes.indexOf('switch_led') >= 0) return 'switch_led';
    if (codes.indexOf('switch') >= 0) return 'switch';
    return null;
  }

  function relaysOf(codes) {
    var out = [];
    for (var i = 0; i < codes.length; i++) {
      if (/^switch_\d+$/.test(codes[i])) out.push(codes[i]);
    }
    return out;
  }

  // powerControl names the one control that represents the device's power, even
  // while it is off. It returns null when there is none: a strip with several
  // independent gangs has no single switch, and pretending otherwise would hide
  // half of it.
  //
  // A master switch wins over the relays under it. A socket thermostat reports
  // switch_all = false next to switch_1 = true; taking any true relay called the
  // heater on while its own master switch read off, and the card and the
  // settings modal disagreed about the same device.
  function powerControl(device) {
    var codes = powerCodes(device);
    var master = masterOf(codes);
    if (master) return master;
    var relays = relaysOf(codes);
    return relays.length === 1 ? relays[0] : null;
  }

  // powerCode answers with the control a device is on with, or null when it is
  // off. With a master, the master decides; without one, any lit gang counts.
  function powerCode(device) {
    var codes = powerCodes(device);
    var values = valuesOf(device);
    var master = masterOf(codes);
    if (master) return isOn(values[master]) ? master : null;
    var relays = relaysOf(codes);
    for (var i = 0; i < relays.length; i++) {
      if (isOn(values[relays[i]])) return relays[i];
    }
    return null;
  }

  // Controls a wall actually tunes. A device that has none of them is a plug or
  // a plain switch: the only thing worth showing is whether it is on, and
  // everything else — countdown, relay mode, indicator light, child lock —
  // belongs behind a settings button.
  var ADJUSTABLE = [
    'temp_set',
    'temp_set_f',
    'bright_value',
    'bright_value_1',
    'percent_control',
    'colour_data',
    'work_mode',
    'mode',
    'level',
    'speed',
    'fan_speed',
    'humidity_set',
  ];

  // powerCard returns the switches a compact card should show, or null when the
  // device needs its full set of controls on the card.
  //
  // A two-gang switch is listed, not refused: both keys fit on the card as two
  // compact rows, which is exactly what somebody wants to press. Only devices
  // with something to tune — brightness, a set-point, a colour — keep their full
  // card, because that control is the reason to look at them.
  function powerCard(device) {
    var functions = (device && device.functions) || [];
    for (var i = 0; i < functions.length; i++) {
      if (ADJUSTABLE.indexOf(functions[i].code) >= 0) return null;
    }

    var codes = powerCodes(device);
    var master = masterOf(codes);
    if (master) return [master];

    var relays = relaysOf(codes);
    if (!relays.length) return null;
    // The relays come in the order the cloud lists them, which is the order the
    // keys sit on the physical switch.
    return relays;
  }

  var api = {
    isPowerCode: isPowerCode,
    isOn: isOn,
    powerCode: powerCode,
    powerControl: powerControl,
    powerCard: powerCard,
  };

  root.CoolHomePower = api;
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
})(typeof window !== 'undefined' ? window : globalThis);
