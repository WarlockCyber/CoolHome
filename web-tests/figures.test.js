// Which readings become big numbers on a sensor card, and which must therefore
// stay reachable in the settings panel.
//
// The rule is small and easy to get subtly wrong — the first version of the
// temperature-and-humidity card excluded the humidity from the settings panel
// while drawing it nowhere, so a device that reports humidity without a
// temperature lost its value from the interface entirely. These tests pin both
// halves of the rule: what is drawn, and what is skipped only because it is
// drawn.

const test = require('node:test');
const assert = require('node:assert');

const figures = require('../web/static/figures.js');

// A temperature-and-humidity sensor as the thing model describes it: the legacy
// specification does not exist, so the readings arrive under the v2 codes.
const SENSOR = {
  id: 'dev-th',
  readings: [
    { code: 'temp_current', type: 'Integer', scale: 1, unit: '℃' },
    { code: 'humidity_value', type: 'Integer', scale: 0, unit: '%' },
    { code: 'battery_state', type: 'Enum', options: ['low', 'middle', 'high'] },
  ],
  functions: [],
  status: [
    { code: 'temp_current', value: 110 },
    { code: 'humidity_value', value: 52 },
  ],
};

// A thermostat whose set-points share the Celsius unit and whose humidity limits
// share the percent sign with the reading.
const THERMOSTAT = {
  id: 'dev-thermo',
  readings: [
    { code: 'temp_current', type: 'Integer', scale: 1, unit: '℃' },
    { code: 'maxtemp_set', type: 'Integer', scale: 1, unit: '℃' },
    { code: 'maxhum_set', type: 'Integer', scale: 0, unit: '%' },
    { code: 'temp_current_f', type: 'Integer', scale: 1, unit: '℉' },
  ],
  functions: [
    { code: 'maxtemp_set', type: 'Integer', scale: 1, unit: '℃' },
  ],
  status: [{ code: 'temp_current', value: 214 }],
};

test('a temperature-and-humidity sensor draws both and skips both', () => {
  const picked = figures.figures(SENSOR);
  assert.equal(picked.temp.code, 'temp_current');
  assert.equal(picked.humid.code, 'humidity_value');
  assert.deepEqual(picked.codes, ['temp_current', 'humidity_value']);
});

test('humidity without a temperature is drawn nowhere, so it is not skipped', () => {
  // The humidifier in the demo home reports switch_1 and humidity only. Its card
  // is a power card, which has no figures; skipping the humidity there would
  // remove 35 % from the interface.
  const humidifier = {
    id: 'dev-hum',
    readings: [{ code: 'humidity', type: 'Integer', scale: 0, unit: '%' }],
    functions: [{ code: 'switch_1', type: 'Boolean' }],
    status: [{ code: 'humidity', value: 35 }, { code: 'switch_1', value: true }],
  };
  const picked = figures.figures(humidifier);
  assert.equal(picked.temp, null);
  assert.equal(picked.humid, null, 'no figures at all without a temperature');
  assert.deepEqual(picked.codes, [], 'nothing may be excluded from the panel');
});

test('a set-point never becomes a figure, in either unit', () => {
  const picked = figures.figures(THERMOSTAT);
  assert.equal(picked.temp.code, 'temp_current');
  assert.equal(picked.humid, null, 'maxhum_set is a limit, not the measured humidity');
  assert.deepEqual(picked.codes, ['temp_current']);
});

test('the Fahrenheit duplicate is not a second temperature', () => {
  assert.equal(figures.isTemperatureCode('temp_current_f'), false);
  assert.equal(figures.isTemperatureCode('temp_current'), true);
  assert.equal(figures.isTemperatureCode('va_temperature'), true);
});

test('all three spellings of measured humidity are recognised', () => {
  for (const code of ['humidity_value', 'va_humidity', 'humidity']) {
    assert.equal(figures.isHumidityCode(code), true, code);
  }
  assert.equal(figures.isHumidityCode('maxhum_set'), false);
  assert.equal(figures.isHumidityCode('minihum_set'), false);
  assert.equal(figures.isHumidityCode('humidity_set'), false);
});

test('a point declared as a function stays a control, not a figure', () => {
  // Some models list the same code in both lists. Drawing it would put the value
  // on the card twice: once large, once as a slider.
  const both = {
    id: 'dev-both',
    readings: [
      { code: 'temp_current', type: 'Integer', scale: 1, unit: '℃' },
      { code: 'humidity_value', type: 'Integer', scale: 0, unit: '%' },
    ],
    functions: [{ code: 'humidity_value', type: 'Integer' }],
  };
  const picked = figures.figures(both);
  assert.equal(picked.temp.code, 'temp_current');
  assert.equal(picked.humid, null);
  assert.deepEqual(picked.codes, ['temp_current']);
});

test('a sensor with no readings at all has no figures', () => {
  const picked = figures.figures({ id: 'dev-empty', readings: [], functions: [] });
  assert.deepEqual(picked.codes, []);
  assert.equal(picked.temp, null);
  assert.equal(picked.humid, null);
});

test('a missing device is handled, not thrown', () => {
  assert.deepEqual(figures.figures({}).codes, []);
  assert.deepEqual(figures.figures(null).codes, []);
  assert.equal(figures.figurePoints(undefined).temp, null);
});

test('the two figures are never the same reading', () => {
  // A model that somehow lists humidity first must not have it picked as the
  // temperature: the two predicates are separate by design.
  const odd = {
    id: 'dev-odd',
    readings: [{ code: 'humidity_value', type: 'Integer', scale: 0, unit: '%' }],
    functions: [],
  };
  const picked = figures.figures(odd);
  assert.equal(picked.temp, null);
  assert.equal(picked.humid, null);
  assert.deepEqual(picked.codes, []);
});
