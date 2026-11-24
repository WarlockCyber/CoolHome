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

// Which devices collapse to a power card. A plug's card answers one question;
// a thermostat or a dimmer has something a wall tunes, so it keeps its card.
const PLUG = {
  status: [
    { code: 'switch_1', value: true },
    { code: 'relay_status', value: 'last' },
    { code: 'child_lock', value: false },
  ],
  functions: [
    { code: 'switch_1', type: 'Boolean' },
    { code: 'countdown_1', type: 'Integer' },
    { code: 'relay_status', type: 'Enum' },
    { code: 'light_mode', type: 'Enum' },
    { code: 'child_lock', type: 'Boolean' },
    { code: 'overcharge_switch', type: 'Boolean' },
  ],
};

test('a plug collapses to its power state', () => {
  assert.deepEqual(power.powerCard(PLUG), ['switch_1']);
});

test('a plug that is off still has a card control to show', () => {
  const off = {
    status: [{ code: 'switch_1', value: false }],
    functions: [{ code: 'switch_1', type: 'Boolean' }, { code: 'countdown_1', type: 'Integer' }],
  };
  assert.deepEqual(power.powerCard(off), ['switch_1'], 'the card needs the control even when it is off');
  assert.equal(power.powerCode(off), null, 'but the device is not on');
});

test('a socket thermostat with a master collapses to the master', () => {
  assert.deepEqual(power.powerCard(SOCKET_THERMOSTAT), ['switch_all']);
});

test('a dimmer keeps its own card', () => {
  const dimmer = {
    status: [{ code: 'switch_led', value: true }],
    functions: [
      { code: 'switch_led', type: 'Boolean' },
      { code: 'bright_value', type: 'Integer', min: 10, max: 1000 },
    ],
  };
  assert.equal(power.powerCard(dimmer), null, 'brightness belongs on the card');
});

test('a thermostat keeps its own card', () => {
  const heater = {
    status: [{ code: 'switch', value: true }],
    functions: [
      { code: 'switch', type: 'Boolean' },
      { code: 'temp_set', type: 'Integer', min: 5, max: 30 },
      { code: 'lock', type: 'Boolean' },
    ],
  };
  assert.equal(power.powerCard(heater), null, 'the set-point belongs on the card');
});

// A two-gang switch is a wall switch: both keys belong on the card as compact
// rows, each with its own state. Refusing to collapse it left the card as a
// list of timers and relay modes.
test('a multi-gang switch lists every key on the card', () => {
  const strip = {
    status: [
      { code: 'switch_1', value: true },
      { code: 'switch_2', value: false },
    ],
    functions: [
      { code: 'switch_1', type: 'Boolean' },
      { code: 'switch_2', type: 'Boolean' },
    ],
  };
  assert.deepEqual(power.powerCard(strip), ['switch_1', 'switch_2']);
  assert.equal(power.powerCode(strip), 'switch_1', 'the panel still reports it as on');
});

// The real device: street lighting with two relays and a tail of settings.
test('a two-gang switch with settings still collapses', () => {
  const outdoor = {
    status: [
      { code: 'switch_1', value: false },
      { code: 'switch_2', value: false },
      { code: 'relay_status', value: 'last' },
    ],
    functions: [
      { code: 'switch_1', type: 'Boolean' },
      { code: 'switch_2', type: 'Boolean' },
      { code: 'countdown_1', type: 'Integer' },
      { code: 'countdown_2', type: 'Integer' },
      { code: 'relay_status', type: 'Enum' },
      { code: 'random_time', type: 'String' },
      { code: 'switch_type', type: 'Enum' },
    ],
  };
  assert.deepEqual(power.powerCard(outdoor), ['switch_1', 'switch_2']);
});

// Keys keep the order the cloud lists them: that is the order on the wall.
test('relay order follows the cloud listing', () => {
  const reversed = {
    status: [],
    functions: [
      { code: 'switch_2', type: 'Boolean' },
      { code: 'switch_1', type: 'Boolean' },
    ],
  };
  assert.deepEqual(power.powerCard(reversed), ['switch_2', 'switch_1']);
});

test('a scene or a sensor has no power card', () => {
  assert.equal(power.powerCard({ status: [], functions: [{ code: 'trigger', type: 'Button' }] }), null);
  assert.equal(power.powerCard({ status: [], functions: [] }), null);
  assert.equal(power.powerCard(null), null);
});
