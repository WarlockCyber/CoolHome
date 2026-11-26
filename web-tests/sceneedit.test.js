// The scene editor's one rule: what a device choice does to a draft row.
//
// The failure it guards was reported from a wall tablet: pick a device, let go
// of the mouse, and the row goes back to "— устройство —". The rule has to be
// taken from the control that fired, not from a re-read of a DOM that may have
// been repainted in between.

const test = require('node:test');
const assert = require('node:assert');

const sceneedit = require('../web/static/sceneedit.js');

test('the chosen device is written into the row', () => {
  const actions = [{ device: '', code: '', value: '' }];
  sceneedit.applyDeviceChoice(actions, 0, 'dev-plug');
  assert.strictEqual(actions[0].device, 'dev-plug');
});

test('the function is cleared, because it belonged to the old device', () => {
  const actions = [{ device: 'dev-light', code: 'switch_led', value: 'true' }];
  sceneedit.applyDeviceChoice(actions, 0, 'dev-plug');
  assert.strictEqual(actions[0].code, '', 'a code the new device does not have must not survive');
});

test('the value is kept', () => {
  const actions = [{ device: 'dev-light', code: 'switch_led', value: 'true' }];
  sceneedit.applyDeviceChoice(actions, 0, 'dev-plug');
  assert.strictEqual(actions[0].value, 'true');
});

test('only the row that changed is touched', () => {
  const actions = [
    { device: 'dev-light', code: 'switch_led', value: 'true' },
    { device: 'dev-plug', code: 'switch_1', value: 'true' },
  ];
  sceneedit.applyDeviceChoice(actions, 1, 'dev-thermo');
  assert.strictEqual(actions[0].device, 'dev-light');
  assert.strictEqual(actions[0].code, 'switch_led');
  assert.deepStrictEqual(actions[1], { device: 'dev-thermo', code: '', value: 'true' });
});

test('a row that is not there changes nothing', () => {
  const actions = [{ device: 'dev-light', code: 'switch_led', value: 'true' }];
  sceneedit.applyDeviceChoice(actions, 5, 'dev-plug');
  assert.deepStrictEqual(actions, [{ device: 'dev-light', code: 'switch_led', value: 'true' }]);
  // A draft that has not been built yet must not throw either.
  assert.deepStrictEqual(sceneedit.applyDeviceChoice(null, 0, 'dev-plug'), []);
});

test('an empty choice is a choice: it clears the row', () => {
  const actions = [{ device: 'dev-light', code: 'switch_led', value: 'true' }];
  sceneedit.applyDeviceChoice(actions, 0, '');
  assert.strictEqual(actions[0].device, '');
  assert.strictEqual(actions[0].code, '');
});
