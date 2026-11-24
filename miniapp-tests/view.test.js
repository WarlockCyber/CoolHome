'use strict';

// Unit tests for the view-model builders, using payloads captured from a real
// CoolHome demo server so the shapes match what the API actually returns.

const test = require('node:test');
const assert = require('node:assert/strict');

const view = require('../miniapp/utils/view.js');

const LIGHT = {
  id: 'demo-light-1',
  name: 'Лампа в гостиной',
  category: 'dj',
  online: true,
  status: [
    { code: 'switch_led', value: true },
    { code: 'bright_value', value: 640 },
    { code: 'work_mode', value: 'white' },
  ],
  functions: [
    { code: 'switch_led', name: 'Свет', type: 'Boolean' },
    { code: 'bright_value', name: 'Яркость', type: 'Integer', min: 10, max: 1000, step: 1 },
    { code: 'work_mode', name: 'Режим', type: 'Enum', options: ['white', 'colour'] },
  ],
  readings: [
    { code: 'switch_led', name: 'Свет', type: 'Boolean' },
    { code: 'bright_value', name: 'Яркость', type: 'Integer' },
    { code: 'work_mode', name: 'Режим', type: 'Enum', options: ['white', 'colour'] },
  ],
};

const THERMOSTAT = {
  id: 'demo-thermo-1',
  name: 'Термостат на кухне',
  category: 'wk',
  online: true,
  status: [
    { code: 'switch', value: true },
    { code: 'temp_set', value: 220 },
    { code: 'temp_current', value: 214 },
  ],
  functions: [
    { code: 'switch', name: 'Питание', type: 'Boolean' },
    { code: 'temp_set', name: 'Целевая температура', type: 'Integer', min: 50, max: 300, step: 5, scale: 1, unit: '°C' },
  ],
  readings: [
    { code: 'switch', name: 'Питание', type: 'Boolean' },
    { code: 'temp_set', name: 'Целевая температура', type: 'Integer', scale: 1, unit: '°C' },
    { code: 'temp_current', name: 'Текущая температура', type: 'Integer', scale: 1, unit: '°C' },
  ],
};

const CURTAIN = {
  id: 'demo-curtain-1',
  name: 'Шторы в кабинете',
  category: 'cl',
  online: true,
  status: [
    { code: 'control', value: 'stop' },
    { code: 'percent_control', value: 40 },
  ],
  functions: [
    { code: 'control', name: 'Управление', type: 'Enum', options: ['open', 'stop', 'close'] },
    { code: 'percent_control', name: 'Открытие', type: 'Integer', min: 0, max: 100, step: 1, unit: '%' },
  ],
  readings: [
    { code: 'control', name: 'Управление', type: 'Enum', options: ['open', 'stop', 'close'] },
    { code: 'percent_control', name: 'Открытие', type: 'Integer', unit: '%' },
  ],
};

test('glyphFor maps common categories and falls back', () => {
  assert.equal(view.glyphFor('dj'), '💡');
  assert.equal(view.glyphFor('wk'), '🌡');
  assert.equal(view.glyphFor('cl'), '🪟');
  assert.equal(view.glyphFor('zz-unknown'), '•');
  assert.equal(view.glyphFor(''), '•');
  assert.equal(view.glyphFor(null), '•');
});

test('buildDeviceRow renders a list card', () => {
  const row = view.buildDeviceRow(LIGHT);
  assert.equal(row.id, 'demo-light-1');
  assert.equal(row.name, 'Лампа в гостиной');
  assert.equal(row.online, true);
  assert.equal(row.glyph, '💡');
  assert.equal(row.controls, 3);
  assert.ok(row.summary.length > 0, 'summary must not be empty');
});

test('unsupportedNote explains each kind of device differently', () => {
  const rf = view.unsupportedNote('rf_switch1');
  assert.match(rf, /RF-выключатель/);
  assert.match(rf, /пульт/, 'an RF switch is driven by a remote');

  const blaster = view.unsupportedNote('wnykq');
  assert.match(blaster, /COOLHOME_IR/, 'the blaster note must say how to get its remotes');

  // A sensor is not controlled by anything: telling the user about a remote
  // would be misleading, which is the defect this text replaces.
  for (const category of ['tdq', 'wk', 'wsdcg', '']) {
    const note = view.unsupportedNote(category);
    assert.ok(!/пульт|remote/i.test(note), category + ' must not mention a remote: ' + note);
    assert.match(note, /не отдаёт/);
  }
});

test('buildDeviceRow flags a device the cloud does not describe', () => {
  const row = view.buildDeviceRow(Object.assign({}, LIGHT, { spec_unsupported: true, functions: [] }));
  assert.equal(row.unsupported, true);
  assert.equal(row.controls, 0);
});

test('buildDeviceRow marks an offline device', () => {
  const row = view.buildDeviceRow(Object.assign({}, LIGHT, { online: false }));
  assert.equal(row.online, false);
});

test('buildDeviceRow falls back to the id for an unnamed device', () => {
  const row = view.buildDeviceRow({ id: 'dev-x', online: true });
  assert.equal(row.name, 'dev-x');
  assert.equal(row.summary, 'dev-x');
});

test('summarize prefers readings and falls back to a switch', () => {
  const thermostat = view.summarize(THERMOSTAT);
  assert.equal(thermostat, '21.4 °C', 'two readings, first is temp_current by priority');

  const plainSwitch = view.summarize({
    id: 'plug',
    status: [{ code: 'switch_1', value: false }],
    readings: [],
  });
  assert.equal(plainSwitch, 'выкл');
});

test('buildReadings pairs values with labels and units', () => {
  const rows = view.buildReadings(THERMOSTAT);
  assert.equal(rows.length, 3);
  assert.deepEqual(rows[0], { code: 'switch', name: 'Питание', value: 'вкл' });
  assert.deepEqual(rows[1], { code: 'temp_set', name: 'Целевая температура', value: '22 °C' });
  assert.deepEqual(rows[2], { code: 'temp_current', name: 'Текущая температура', value: '21.4 °C' });
});

test('buildReadings skips points that have no current value', () => {
  const rows = view.buildReadings({
    id: 'd',
    status: [{ code: 'a', value: 1 }],
    readings: [
      { code: 'a', name: 'A', type: 'Integer' },
      { code: 'b', name: 'B', type: 'Integer' },
    ],
  });
  assert.equal(rows.length, 1);
  assert.equal(rows[0].code, 'a');
});

test('buildControls picks a switch, a slider and a picker for a light', () => {
  const controls = view.buildControls(LIGHT);
  const byCode = {};
  for (const c of controls) byCode[c.code] = c;

  assert.equal(byCode.switch_led.kind, 'switch');
  assert.equal(byCode.switch_led.checked, true);

  assert.equal(byCode.bright_value.kind, 'slider');
  assert.equal(byCode.bright_value.min, 10);
  assert.equal(byCode.bright_value.max, 1000);
  assert.equal(byCode.bright_value.step, 1);
  assert.equal(byCode.bright_value.value, 640);

  assert.equal(byCode.work_mode.kind, 'picker');
  assert.equal(byCode.work_mode.index, 0);
  assert.equal(byCode.work_mode.label, 'белый');
  assert.deepEqual(byCode.work_mode.labels, ['белый', 'цветной']);
});

test('a scaled point becomes a slider in display units', () => {
  const controls = view.buildControls(THERMOSTAT);
  const target = controls.find((c) => c.code === 'temp_set');
  assert.equal(target.kind, 'slider');
  assert.equal(target.min, 5, 'raw min 50 with scale 1 is 5 °C');
  assert.equal(target.max, 30, 'raw max 300 with scale 1 is 30 °C');
  assert.equal(target.step, 0.5, 'raw step 5 with scale 1 is 0.5 °C');
  assert.equal(target.value, 22);
  assert.equal(target.scale, 1);
  assert.equal(target.label, '22 °C');
});

test('a picker opens on the current enum value', () => {
  const controls = view.buildControls(CURTAIN);
  const mode = controls.find((c) => c.code === 'control');
  assert.equal(mode.kind, 'picker');
  assert.equal(mode.index, 1);
  assert.equal(mode.label, 'стоп');
});

test('an infrared key becomes a button', () => {
  const controls = view.buildControls({
    id: 'ir:blast:1',
    online: true,
    status: [],
    functions: [
      { code: 'power', name: 'Питание', type: 'Button' },
      { code: 'vol_up', name: 'Громче', type: 'Button' },
    ],
  });
  assert.equal(controls.length, 2);
  assert.equal(controls[0].kind, 'button');
  assert.equal(controls[0].name, 'Питание');
  assert.equal(controls[0].disabled, false);
  assert.equal(controls[1].code, 'vol_up');
});

test('an infrared remote pressed while offline is disabled', () => {
  const controls = view.buildControls({
    id: 'ir:blast:1',
    online: false,
    status: [],
    functions: [{ code: 'power', name: 'Питание', type: 'Button' }],
  });
  assert.equal(controls[0].kind, 'button');
  assert.equal(controls[0].disabled, true);
});

test('a learned RF key gets a neutral label', () => {
  const controls = view.buildControls({
    id: 'rf-1',
    online: true,
    status: [],
    functions: [{ code: 'one_on', name: 'one ON', type: 'Button' }],
  });
  assert.equal(controls[0].kind, 'button');
  // "one ON" says nothing about whether the physical button toggles, so the
  // label must not promise "включить".
  assert.equal(controls[0].name, 'Кнопка');
});

test('the blaster note matches whether infrared is being served', () => {
  const off = view.unsupportedNote('wnykq', false);
  assert.match(off, /COOLHOME_IR/, 'with IR off the note must say how to enable it');

  const on = view.unsupportedNote('wnykq', true);
  assert.ok(!/COOLHOME_IR/.test(on), 'with IR on the note must not tell the user to enable it again');
  assert.match(on, /отдельными устройствами/);
});

test('buildControls skips points a widget cannot set', () => {
  const controls = view.buildControls({
    id: 'd',
    status: [],
    functions: [
      { code: 'no_range', name: 'No range', type: 'Integer' },
      { code: 'empty_enum', name: 'Empty enum', type: 'Enum', options: [] },
      { code: 'raw_point', name: 'Raw', type: 'Raw' },
      { code: 'ok', name: 'Ok', type: 'Boolean' },
    ],
  });
  assert.equal(controls.length, 1);
  assert.equal(controls[0].code, 'ok');
});

test('buildControls handles a device with no functions at all', () => {
  assert.deepEqual(view.buildControls({ id: 'sensor', status: [] }), []);
  assert.deepEqual(view.buildControls(null), []);
  assert.deepEqual(view.buildReadings(null), []);
});

test('a switch point honours the raw value shapes the cloud sends', () => {
  for (const raw of [true, 'true', 1]) {
    const controls = view.buildControls({
      id: 'd',
      status: [{ code: 'sw', value: raw }],
      functions: [{ code: 'sw', name: 'SW', type: 'Boolean' }],
    });
    assert.equal(controls[0].checked, true, 'raw ' + JSON.stringify(raw) + ' should read as on');
  }
  for (const raw of [false, 'false', 0, undefined]) {
    const controls = view.buildControls({
      id: 'd',
      status: raw === undefined ? [] : [{ code: 'sw', value: raw }],
      functions: [{ code: 'sw', name: 'SW', type: 'Boolean' }],
    });
    assert.equal(controls[0].checked, false, 'raw ' + JSON.stringify(raw) + ' should read as off');
  }
});

test('summarize shows temperature and humidity of a thing-model sensor', () => {
  // The T & H sensor: the legacy API does not describe it, so its readings come
  // from the thing model under humidity_value.
  const sensor = view.summarize({
    id: 'dev-th',
    name: 'T & H Sensor',
    online: true,
    status: [
      { code: 'temp_current', value: 110 },
      { code: 'humidity_value', value: 52 },
    ],
    functions: [],
    readings: [
      { code: 'temp_current', name: 'Температура', type: 'Integer', scale: 1, unit: '℃' },
      { code: 'humidity_value', name: 'Влажность', type: 'Integer', scale: 0, unit: '%' },
    ],
  });
  // The mini-app keeps the cloud's own unit and drops a trailing zero, unlike
  // the web card, which normalises ℃ to °C: what matters here is that the
  // humidity is part of the preview at all.
  assert.equal(sensor, '11 ℃ · 52 %');
});
