// The side panel's device list: adding, removing, reordering, and noticing a
// device that left the home.
//
// Each rule here protects a failure that looks like something else on a wall
// tablet — a card twice, a card that vanishes on reload, a panel that silently
// reorders itself. The server mirrors the same limits (dashboard.MaxPanel), so a
// test here and a test there must agree.

const test = require('node:test');
const assert = require('node:assert');

const panel = require('../web/static/panel.js');

test('an add appends and keeps the order', () => {
  const first = panel.add([], 'thermo-1');
  assert.deepEqual(first.panel, ['thermo-1']);
  assert.equal(first.error, '');

  const second = panel.add(first.panel, 'sensor-1');
  assert.deepEqual(second.panel, ['thermo-1', 'sensor-1']);
});

test('a device is never added twice', () => {
  const once = panel.add(['thermo-1'], 'thermo-1');
  assert.deepEqual(once.panel, ['thermo-1']);
  assert.match(once.error, /уже на панели/);
  assert.equal(panel.has(['thermo-1'], 'thermo-1'), true);
});

test('the limit is refused before the request is sent', () => {
  const full = [];
  for (let i = 0; i < panel.MAX; i++) full.push('dev-' + i);
  const refused = panel.add(full, 'one-more');
  assert.deepEqual(refused.panel, full, 'the list must not grow past the limit');
  assert.match(refused.error, /предел/);

  // The server's limit is the one that decides; a caller may pass a smaller one.
  const small = panel.add(['a'], 'b', 1);
  assert.deepEqual(small.panel, ['a']);
  assert.match(small.error, /предел/);
});

test('an empty id is refused rather than stored', () => {
  for (const bad of ['', '   ', null, undefined]) {
    const res = panel.add(['a'], bad);
    assert.deepEqual(res.panel, ['a'], String(bad));
    assert.notEqual(res.error, '', String(bad));
  }
  assert.equal(panel.add([], '   ').panel.length, 0);
});

test('remove drops exactly one entry and keeps the rest in order', () => {
  const out = panel.remove(['a', 'b', 'c'], 'b');
  assert.deepEqual(out, ['a', 'c']);
  // Removing something absent is not an error; the list simply stays.
  assert.deepEqual(panel.remove(['a'], 'zzz'), ['a']);
});

test('move shifts within bounds and clamps at the edges', () => {
  assert.deepEqual(panel.move(['a', 'b', 'c'], 2, -1), ['a', 'c', 'b']);
  assert.deepEqual(panel.move(['a', 'b', 'c'], 0, 1), ['b', 'a', 'c']);
  assert.deepEqual(panel.move(['a', 'b'], 0, -1), ['a', 'b'], 'first cannot move up');
  assert.deepEqual(panel.move(['a', 'b'], 1, 1), ['a', 'b'], 'last cannot move down');
  assert.deepEqual(panel.move(['a', 'b'], 9, -1), ['a', 'b'], 'a stale index is ignored');
});

test('move returns a new list, so the caller never mutates the stored one by accident', () => {
  const original = ['a', 'b'];
  const moved = panel.move(original, 0, 1);
  assert.deepEqual(original, ['a', 'b']);
  assert.notStrictEqual(moved, original);
});

test('normalize trims, drops duplicates and respects the limit', () => {
  assert.deepEqual(panel.normalize([' a ', 'a', '', 'b']), ['a', 'b']);
  const many = [];
  for (let i = 0; i < panel.MAX + 5; i++) many.push('d' + i);
  assert.equal(panel.normalize(many).length, panel.MAX);
  assert.deepEqual(panel.normalize(null), []);
  assert.deepEqual(panel.normalize('not a list'), []);
});

test('a device that left the home is reported, not dropped', () => {
  // The panel keeps its slot: silently removing it would reorder the column
  // under a hand reaching for a card.
  const devices = [{ id: 'thermo-1' }, { id: 'sensor-1' }];
  assert.deepEqual(panel.missing(['thermo-1', 'gone-1'], devices), ['gone-1']);
  assert.deepEqual(panel.missing(['thermo-1'], devices), []);
});

test('has answers for a device that is in the list after trimming', () => {
  assert.equal(panel.has([' a '], 'a'), true);
  assert.equal(panel.has(['a'], ' a '), true);
  assert.equal(panel.has(['a'], ''), false);
  assert.equal(panel.has(null, 'a'), false);
});

test('the page limit and the server limit are the same number', () => {
  // panel.MAX stops the UI before the request; dashboard.MaxPanel rejects it
  // after. If the two drift apart, one of them is wrong and the user sees
  // "added" followed by a 400. The Go side asserts 12 in
  // internal/dashboard/store_test.go; this is its other half.
  assert.equal(panel.MAX, 12);
});
