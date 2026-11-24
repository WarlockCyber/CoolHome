'use strict';

// The rule for "this device is switched on" decides what the panel above the
// grid claims. Getting it wrong is not cosmetic: counting threshold settings as
// power made four windows appear switched on while they were idle.

const test = require('node:test');
const assert = require('node:assert/strict');

const power = require('../web/static/power.js');

test('a plain switch is power', () => {
  for (const code of ['switch', 'switch_1', 'switch_2', 'switch_led', 'switch_all']) {
    assert.equal(power.isPowerCode(code), true, code + ' must count as power');
  }
});

test('settings that happen to be boolean are not power', () => {
  for (const code of [
    'upper_temp_switch',
    'lower_temp_switch',
    'cold_delay_switch',
    'switch_prepayment',
    'switch_alarm_sound',
    'overcharge_switch',
    'child_lock',
    'frost',
    'eco',
    'lock',
    'muffling',
  ]) {
    assert.equal(power.isPowerCode(code), false, code + ' must not count as power');
  }
});

test('an unrelated data point is not power', () => {
  for (const code of ['temp_set', 'bright_value', 'countdown_1', 'trigger', 'va_temperature', '']) {
    assert.equal(power.isPowerCode(code), false, code + ' must not count as power');
  }
});

// The real case that broke a naive implementation.
const WINDOW_THERMOSTAT = {
  status: [
    { code: 'switch_all', value: false },
    { code: 'switch_1', value: false },
    { code: 'upper_temp_switch', value: true },
    { code: 'lower_temp_switch', value: true },
    { code: 'cold_delay_switch', value: false },
  ],
  functions: [
    { code: 'switch_all', type: 'Boolean' },
    { code: 'switch_1', type: 'Boolean' },
    { code: 'upper_temp_switch', type: 'Boolean' },
    { code: 'lower_temp_switch', type: 'Boolean' },
    { code: 'cold_delay_switch', type: 'Boolean' },
  ],
};

test('a thermostat with alarms enabled is not "on"', () => {
  assert.equal(power.powerCode(WINDOW_THERMOSTAT), null);
});

test('a plug with child lock on is not "on" while its relay is off', () => {
  const plug = {
    status: [
      { code: 'switch_1', value: false },
      { code: 'child_lock', value: true },
      { code: 'overcharge_switch', value: true },
    ],
    functions: [
      { code: 'switch_1', type: 'Boolean' },
      { code: 'child_lock', type: 'Boolean' },
      { code: 'overcharge_switch', type: 'Boolean' },
    ],
  };
  assert.equal(power.powerCode(plug), null);
});

test('a device with several relays counts if any is on', () => {
  const strip = {
    status: [
      { code: 'switch_1', value: false },
      { code: 'switch_2', value: true },
    ],
    functions: [
      { code: 'switch_1', type: 'Boolean' },
      { code: 'switch_2', type: 'Boolean' },
    ],
  };
  assert.equal(power.powerCode(strip), 'switch_2');
});

test('the raw value shapes the cloud sends are understood', () => {
  for (const on of [true, 1, '1', 'true']) {
    const d = {
      status: [{ code: 'switch', value: on }],
      functions: [{ code: 'switch', type: 'Boolean' }],
    };
    assert.equal(power.powerCode(d), 'switch', JSON.stringify(on) + ' must read as on');
  }
  for (const off of [false, 0, '0', 'false', undefined]) {
    const d = {
      status: off === undefined ? [] : [{ code: 'switch', value: off }],
      functions: [{ code: 'switch', type: 'Boolean' }],
    };
    assert.equal(power.powerCode(d), null, JSON.stringify(off) + ' must read as off');
  }
});

test('a scene has no power state', () => {
  const scene = { status: [], functions: [{ code: 'trigger', type: 'Button' }] };
  assert.equal(power.powerCode(scene), null);
});

test('a device with no functions or no status is not "on"', () => {
  assert.equal(power.powerCode({}), null);
  assert.equal(power.powerCode(null), null);
  assert.equal(power.powerCode({ functions: [{ code: 'switch', type: 'Boolean' }] }), null);
});

// The panel lists only what is reachable: an offline device's last known state
// may be stale, so claiming it is on would be a guess.
test('an offline device is still reported as powered by powerCode', () => {
  // powerCode answers about the data, not about reachability; the panel applies
  // the online filter on top of it. This pins that separation.
  const offline = {
    online: false,
    status: [{ code: 'switch_1', value: true }],
    functions: [{ code: 'switch_1', type: 'Boolean' }],
  };
  assert.equal(power.powerCode(offline), 'switch_1');
});

// The bug that made the panel and the settings modal disagree: a socket
// thermostat reports switch_all = false next to switch_1 = true. Taking any
// true relay called the heater on while its own master switch read off.
const SOCKET_THERMOSTAT = {
  status: [
    { code: 'switch_all', value: false },
    { code: 'switch_1', value: true },
    { code: 'upper_temp_switch', value: true },
    { code: 'lower_temp_switch', value: true },
  ],
  functions: [
    { code: 'switch_all', type: 'Boolean' },
    { code: 'switch_1', type: 'Boolean' },
    { code: 'upper_temp_switch', type: 'Boolean' },
    { code: 'lower_temp_switch', type: 'Boolean' },
  ],
};

test('a master switch that is off decides the device is off', () => {
  assert.equal(power.powerCode(SOCKET_THERMOSTAT), null);
});

test('a master switch that is on decides the device is on', () => {
  const on = {
    status: [
      { code: 'switch_all', value: true },
      { code: 'switch_1', value: false },
    ],
    functions: [
      { code: 'switch_all', type: 'Boolean' },
      { code: 'switch_1', type: 'Boolean' },
    ],
  };
  assert.equal(power.powerCode(on), 'switch_all');
});

// A single 'switch' is the master of a one-gang product, same rule.
test('a lone switch decides on its own', () => {
  const off = {
    status: [
      { code: 'switch', value: false },
      { code: 'switch_1', value: true },
    ],
    functions: [
      { code: 'switch', type: 'Boolean' },
      { code: 'switch_1', type: 'Boolean' },
    ],
  };
  assert.equal(power.powerCode(off), null);
});

// Without a master, independent gangs keep the "any lit" rule.
test('a strip with no master counts any lit gang', () => {
  const strip = {
    status: [
      { code: 'switch_1', value: false },
      { code: 'switch_2', value: true },
    ],
    functions: [
      { code: 'switch_1', type: 'Boolean' },
      { code: 'switch_2', type: 'Boolean' },
    ],
  };
  assert.equal(power.powerCode(strip), 'switch_2');
});
