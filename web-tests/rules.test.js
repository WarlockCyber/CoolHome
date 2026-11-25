// The rule form: what a rule reads like, what the editor sends, and what stops a
// draft from being saved.
//
// The failures these tests guard are the quiet ones — a value saved as text that
// never matches a numeric reading, a rule with no trigger that never runs, an
// action naming a device nobody picked.

const test = require('node:test');
const assert = require('node:assert');

const rules = require('../web/static/rules.js');

test('a value is parsed into the type the server compares with', () => {
  // "22" as text would compare as text against a numeric reading and never
  // match; the number is what a person means when typing it.
  assert.strictEqual(rules.parseValue('22'), 22);
  assert.strictEqual(rules.parseValue(' 18.5 '), 18.5);
  assert.strictEqual(rules.parseValue('-3'), -3);
  assert.strictEqual(rules.parseValue('true'), true);
  assert.strictEqual(rules.parseValue('false'), false);
  assert.strictEqual(rules.parseValue('low'), 'low');
  assert.strictEqual(rules.parseValue(''), null);
  assert.strictEqual(rules.parseValue(null), null);
  assert.strictEqual(rules.parseValue('22a'), '22a', 'not a number, so text');
});

test('a schedule reads as a sentence', () => {
  assert.strictEqual(rules.scheduleText({ from: '07:30' }), 'каждый день в 07:30');
  assert.strictEqual(
    rules.scheduleText({ from: '22:00', to: '06:00' }),
    'каждый день 22:00–06:00',
  );
  assert.strictEqual(
    rules.scheduleText({ from: '07:30', days: ['mon', 'tue'] }),
    'Пн, Вт в 07:30',
  );
  assert.strictEqual(rules.scheduleText(null), '');
  assert.strictEqual(rules.scheduleText({}), '');
});

test('a rule summarizes as trigger, actions and notes', () => {
  const rule = {
    name: 'Холодно',
    when: [{ device: 'd1', code: 'temp_current', op: '<', value: 180 }],
    at: { from: '22:00', to: '06:00' },
    for: '5m',
    cooldown: '10m',
    mode: 'level',
    actions: [{ kind: 'command', device: 'p1', code: 'switch_1', value: true }],
  };
  const label = (id, code) => (id === 'd1' ? 'Температура' : 'Обогреватель');
  const out = rules.summarize(rule, label);
  assert.match(out.trigger, /каждый день 22:00–06:00/);
  assert.match(out.trigger, /если Температура меньше 180/);
  assert.strictEqual(out.actions, 'Обогреватель → вкл');
  assert.match(out.notes, /держать 5m/);
  assert.match(out.notes, /не чаще 10m/);
  assert.match(out.notes, /повторять/);
});

test('a schedule-only rule has no "если" clause', () => {
  const out = rules.summarize({ at: { from: '07:30' }, actions: [] });
  assert.strictEqual(out.trigger, 'каждый день в 07:30');
  assert.ok(!out.trigger.includes('если'));
});

test('a draft without a trigger is refused', () => {
  // Such a rule would be stored and never run, which is worse than an error.
  const errors = rules.validateDraft({
    name: 'Пустое',
    when: [],
    actions: [{ device: 'd', code: 'c', value: true }],
  });
  assert.ok(errors.some((e) => /расписание/.test(e)), errors.join('; '));
});

test('a draft without actions is refused', () => {
  const errors = rules.validateDraft({
    name: 'Без действий',
    when: [{ device: 'd', code: 'c', op: '<', value: 1 }],
    actions: [],
  });
  assert.ok(errors.some((e) => /действие/.test(e)));
});

test('a draft with an unnamed condition is refused with its number', () => {
  const errors = rules.validateDraft({
    name: 'r',
    when: [
      { device: 'd', code: 'c', op: '<', value: 1 },
      { device: '', code: '', op: '<', value: 1 },
    ],
    actions: [{ device: 'd', code: 'c', value: true }],
  });
  assert.ok(errors.some((e) => /условии 2/.test(e)), errors.join('; '));
});

test('a valid draft reports no errors', () => {
  const errors = rules.validateDraft({
    name: 'r',
    when: [{ device: 'd', code: 'c', op: '<', value: 1 }],
    actions: [{ device: 'd', code: 'c', value: true }],
  });
  assert.deepStrictEqual(errors, []);
});

test('the payload omits empty optional parts', () => {
  const payload = rules.toPayload(
    {
      name: '  Холодно  ',
      when: [{ device: 'd', code: 'c', op: '<', value: 1 }],
      at: null,
      for: '',
      cooldown: '',
      mode: 'edge',
      actions: [{ kind: 'command', device: 'p', code: 's', value: true }],
    },
    'r1',
  );
  assert.strictEqual(payload.id, 'r1');
  assert.strictEqual(payload.name, 'Холодно');
  assert.strictEqual(payload.enabled, true);
  assert.strictEqual(payload.at, undefined, 'an absent schedule must not be sent as null');
  assert.strictEqual(payload.for, undefined);
  assert.strictEqual(payload.cooldown, undefined);
  assert.strictEqual(payload.mode, undefined, 'edge is the default and need not be written');
});

test('the payload keeps a schedule and its days', () => {
  const payload = rules.toPayload(
    {
      name: 'r',
      at: { from: '22:00', to: '06:00', days: ['mon'] },
      actions: [{ kind: 'scene', scene: 's1' }],
    },
    'r1',
  );
  assert.deepStrictEqual(payload.at, { from: '22:00', to: '06:00', days: ['mon'] });
  assert.deepStrictEqual(payload.actions, [{ kind: 'scene', scene: 's1' }]);
  assert.strictEqual(payload.when, undefined);
});

test('an id derived from a Russian name stays readable', () => {
  const id = rules.makeID('Холодно в спальне');
  assert.match(id, /^holodno-v-spalne-/, id);
  assert.ok(id.length <= 45, id);
});

test('an id is never empty, even for a name with no letters', () => {
  assert.match(rules.makeID('!!!'), /^rule-/);
  assert.match(rules.makeID(''), /^rule-/);
  // Two rules with the same name get different ids.
  assert.notStrictEqual(rules.makeID('Свет'), rules.makeID('Свет'));
});

test('a zero hold is not spoken about', () => {
  // The editor writes "0s" when the field is left at zero; "держать 0s" in the
  // list is noise, and the rule without a hold is the normal case.
  const out = rules.summarize({
    at: { from: '07:30' },
    for: '0s',
    cooldown: '0',
    actions: [{ kind: 'command', device: 'p', code: 's', value: true }],
  });
  assert.ok(!out.notes.includes('держать'), out.notes);
  assert.ok(!out.notes.includes('не чаще'), out.notes);

  // A real hold is still shown.
  const withHold = rules.summarize({
    at: { from: '07:30' },
    for: '5m',
    actions: [{ kind: 'command', device: 'p', code: 's', value: true }],
  });
  assert.match(withHold.notes, /держать 5m/);
});

test('isZeroDuration recognises the spellings of "no wait"', () => {
  for (const zero of ['', '0', '0s', '0m', '0h', '00s']) {
    assert.strictEqual(rules.isZeroDuration(zero), true, zero);
  }
  for (const real of ['5m', '1s', '30']) {
    assert.strictEqual(rules.isZeroDuration(real), false, real);
  }
});
