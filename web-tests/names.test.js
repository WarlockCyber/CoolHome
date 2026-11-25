// Local device names: trimming, the bound, and which widget titles follow a
// rename.
//
// Renaming is local by design — the cloud keeps its own name — so the failure
// modes are quiet ones: a name that the server rejects, a blank stored where
// "use the cloud's name" was meant, and a remote that goes on showing the old
// title beside a card that shows the new one.

const test = require('node:test');
const assert = require('node:assert');

const names = require('../web/static/names.js');

test('a name is trimmed and inner whitespace collapsed', () => {
  assert.equal(names.normalize('  Кухня  '), 'Кухня');
  assert.equal(names.normalize('Кухня\n  свет'), 'Кухня свет');
  assert.equal(names.normalize(''), '');
  assert.equal(names.normalize(null), '');
  assert.equal(names.normalize(undefined), '');
});

test('a name is bounded at the server limit', () => {
  // The server rejects more than 60 characters with a 400; cutting it here means
  // the user sees the truncation instead of an error after they press save.
  assert.equal(names.MAX, 60);
  const long = 'я'.repeat(80);
  assert.equal(names.normalize(long).length, names.MAX);
  assert.equal(names.normalize('я'.repeat(60)).length, 60);
});

test('a rename rewrites the widget that carries the plain device name', () => {
  const layout = [
    { device_id: 'd1', code: 'switch', title: 'Кофемашина' },
    { device_id: 'd2', code: 'switch', title: 'Лампа' },
  ];
  const out = names.renamedTitles(layout, 'd1', 'Кофемашина', 'Кофе');
  assert.equal(out[0].title, 'Кофе');
  assert.equal(out[1].title, 'Лампа', 'another device is untouched');
});

test('a rename follows the control suffix of a renamed widget', () => {
  const layout = [{ device_id: 'd1', code: 'countdown', title: 'Кофемашина · Таймер' }];
  const out = names.renamedTitles(layout, 'd1', 'Кофемашина', 'Кофе');
  assert.equal(out[0].title, 'Кофе · Таймер');
});

test('a title the user typed is left alone', () => {
  // "Свет на кухне" is not a copy of the device name, so renaming the device
  // must not overwrite a deliberate choice.
  const layout = [{ device_id: 'd1', code: 'switch', title: 'Свет на кухне' }];
  const out = names.renamedTitles(layout, 'd1', 'Кофемашина', 'Кофе');
  assert.equal(out[0].title, 'Свет на кухне');
});

test('a prefix that only looks like a suffix match is not rewritten', () => {
  // "Кофемашина 2" is a different name, not "Кофемашина" plus a control.
  const layout = [{ device_id: 'd1', code: 'switch', title: 'Кофемашина 2' }];
  const out = names.renamedTitles(layout, 'd1', 'Кофемашина', 'Кофе');
  assert.equal(out[0].title, 'Кофемашина 2');
});

test('nothing changes when there is no old name or no new name', () => {
  const layout = [{ device_id: 'd1', code: 'switch', title: 'Кофемашина' }];
  assert.equal(names.renamedTitles(layout, 'd1', '', 'Кофе')[0].title, 'Кофемашина');
  assert.equal(names.renamedTitles(layout, 'd1', 'Кофемашина', '')[0].title, 'Кофемашина');
});

test('the returned layout is a copy, not the caller list', () => {
  const layout = [{ device_id: 'd1', code: 'switch', title: 'Кофемашина' }];
  const out = names.renamedTitles(layout, 'd1', 'Кофемашина', 'Кофе');
  assert.equal(layout[0].title, 'Кофемашина', 'the stored list must not be edited in place');
  assert.notStrictEqual(out, layout);
  assert.notStrictEqual(out[0], layout[0]);
});

test('a missing layout is handled, not thrown', () => {
  assert.deepEqual(names.renamedTitles(null, 'd1', 'a', 'b'), []);
  assert.deepEqual(names.renamedTitles(undefined, 'd1', 'a', 'b'), []);
});

test('a cut never splits a surrogate pair', () => {
  // 😀 is one rune but two UTF-16 units. Cutting the string at 60 units can land
  // between the halves, and the leftover half renders as a replacement
  // character — a name the user never typed.
  const emoji = '😀';
  const text = 'я'.repeat(59) + emoji + 'я';
  const out = names.normalize(text);
  // The bound is in runes, which is what the server counts: an emoji is one
  // rune and two UTF-16 units, so out.length may exceed MAX while the name is
  // still within it.
  assert.ok(names.runeLength(out) <= names.MAX,
    `result is ${names.runeLength(out)} runes, want <= ${names.MAX}`);
  for (let i = 0; i < out.length; i++) {
    const code = out.charCodeAt(i);
    if (code >= 0xd800 && code <= 0xdbff) {
      const next = out.charCodeAt(i + 1);
      assert.ok(next >= 0xdc00 && next <= 0xdfff, 'a high surrogate must have its pair');
      i++;
    }
  }
  assert.ok(!out.includes('\uFFFD'), 'no replacement character');
});

test('the rune count the server checks is never exceeded', () => {
  // The server counts runes; the browser counts UTF-16 units. A rune is at least
  // one unit, so cutting by units can never leave more runes than the limit.
  const mixed = 'я😀a'.repeat(50);
  const out = names.normalize(mixed);
  assert.ok(Array.from(out).length <= names.MAX);
});

test('a renamed title never exceeds the bound the server enforces', () => {
  // The name limit and the title limit are the same 60, so a name at the limit
  // plus " · Таймер" would be rejected — and because the name travels in the
  // same request, the whole rename would be lost with it.
  const longName = 'я'.repeat(60);
  const layout = [{ device_id: 'd1', code: 'x', title: 'Старое · Таймер' }];
  const out = names.renamedTitles(layout, 'd1', 'Старое', longName);
  assert.ok(names.runeLength(out[0].title) <= names.MAX,
    `title is ${names.runeLength(out[0].title)} runes: ${out[0].title}`);
  assert.ok(out[0].title.endsWith(' · Таймер'), 'the control name survives the cut');
});

test('a plain renamed title is cut to the bound too', () => {
  const layout = [{ device_id: 'd1', code: 'x', title: 'Старое' }];
  const out = names.renamedTitles(layout, 'd1', 'Старое', 'я'.repeat(90));
  assert.equal(names.runeLength(out[0].title), names.MAX);
});

test('runeLength counts code points, not UTF-16 units', () => {
  assert.equal(names.runeLength('abc'), 3);
  assert.equal(names.runeLength('яяя'), 3);
  assert.equal(names.runeLength('😀'), 1);
  assert.equal('😀'.length, 2, 'the fixture is only interesting if units differ');
  assert.equal(names.runeLength('а😀б'), 3);
});

test('cut never splits a surrogate pair', () => {
  const text = 'я'.repeat(59) + '😀' + 'я';
  const out = names.cut(text, 60);
  assert.equal(names.runeLength(out), 60);
  assert.ok(!out.includes('\uFFFD'));
});
