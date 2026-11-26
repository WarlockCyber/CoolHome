// Battery level as the card shows it: which devices get a coloured card, and
// what the badge says.
//
// Tuya reports a battery two ways — an enum (battery_state: low/middle/high) and
// a percentage — and a real home has both, sometimes on the same device. The
// enum wins when present: it is the device's own judgement and needs no
// threshold of ours. The percentage thresholds are a documented choice, not a
// fact from the cloud.

const test = require('node:test');
const assert = require('node:assert');

const battery = require('../web/static/battery.js');

// The devices below are taken from the real home: "Температура, 1 этаж" reports
// both encodings, "Wireless Switch 2" only a percentage.
function device(status, readings) {
  return { status: status, readings: readings || [] };
}

test('the cloud\'s own verdict wins over the percentage', () => {
  // 10 % and "low" agree here; the enum is what the firmware decided.
  const info = battery.deviceLevel(
    device(
      [{ code: 'battery_state', value: 'low' }, { code: 'battery_percentage', value: 10 }],
      [{ code: 'battery_state' }, { code: 'battery_percentage' }],
    ),
  );
  assert.strictEqual(info.level, 'low');
  assert.strictEqual(info.source, 'state');
  assert.strictEqual(info.percent, 10);
});

test('a percentage alone becomes a level', () => {
  const cases = [
    [5, 'low'],
    [15, 'low'],
    [16, 'middle'],
    [40, 'middle'],
    [41, 'high'],
    [100, 'high'],
  ];
  for (const [percent, want] of cases) {
    const info = battery.deviceLevel(
      device([{ code: 'battery_percentage', value: percent }], [{ code: 'battery_percentage' }]),
    );
    assert.strictEqual(info.level, want, percent + ' %');
    assert.strictEqual(info.source, 'percent');
  }
});

test('a device with no battery reports nothing', () => {
  assert.strictEqual(battery.deviceLevel(device([{ code: 'switch_1', value: true }])), null);
  assert.strictEqual(battery.deviceLevel(device([])), null);
  assert.strictEqual(battery.deviceLevel(null), null);
});

test('an unknown enum value is not guessed at', () => {
  // A model that sends something this code does not know must not paint the
  // card on a guess; with no percentage, there is nothing to say.
  assert.strictEqual(
    battery.deviceLevel(device([{ code: 'battery_state', value: 'charging' }], [{ code: 'battery_state' }])),
    null,
  );
  // With a percentage it falls back to that, which is a fact.
  const info = battery.deviceLevel(
    device(
      [{ code: 'battery_state', value: 'charging' }, { code: 'battery_percentage', value: 80 }],
      [{ code: 'battery_state' }, { code: 'battery_percentage' }],
    ),
  );
  assert.strictEqual(info.level, 'high');
  assert.strictEqual(info.source, 'percent');
});

test('"lower" and "normal" are the spellings other models use', () => {
  assert.strictEqual(battery.STATE_LEVEL.lower, 'low');
  assert.strictEqual(battery.STATE_LEVEL.normal, 'middle');
  assert.strictEqual(battery.STATE_LEVEL.full, 'high');
});

test('a nonsense percentage is ignored, not painted', () => {
  for (const bad of [-5, 130, null, '', 'много', undefined]) {
    const info = battery.deviceLevel(
      device([{ code: 'battery_percentage', value: bad }], [{ code: 'battery_percentage' }]),
    );
    assert.strictEqual(info, null, String(bad));
  }
  // A numeric string is a number: some models send "18".
  const info = battery.deviceLevel(
    device([{ code: 'battery_percentage', value: '18' }], [{ code: 'battery_percentage' }]),
  );
  assert.strictEqual(info.percent, 18);
  assert.strictEqual(info.level, 'middle');
});

// Only a battery that needs something gets colour: a page where every card is
// tinted says nothing, and the tint exists to be rare.
test('only low and middle ask for attention', () => {
  assert.strictEqual(battery.needsAttention({ level: 'low' }), true);
  assert.strictEqual(battery.needsAttention({ level: 'middle' }), true);
  assert.strictEqual(battery.needsAttention({ level: 'high' }), false);
  assert.strictEqual(battery.needsAttention(null), false);
});

test('the badge and the tooltip say what the person needs', () => {
  const low = { level: 'low', percent: 10 };
  assert.strictEqual(battery.label(low), '10 %');
  assert.match(battery.describe(low), /замените/);

  const middle = { level: 'middle', percent: null };
  assert.strictEqual(battery.label(middle), 'средний');
  assert.match(battery.describe(middle), /разряжается/);

  const high = { level: 'high', percent: 100 };
  assert.strictEqual(battery.label(high), '100 %');
  assert.match(battery.describe(high), /в порядке/);
});
