// Display modes: which of the two screens the page opens in.
//
// The quiet failures these guard: a bookmark that says "полный экран" opening the
// ordinary page, a stored wall mode swallowing the remote editor's ?edit=1, and
// the inline script in index.html drifting away from the module — that one shows
// up as a header flashing on a wall tablet and nowhere else.

const test = require('node:test');
const assert = require('node:assert');
const fs = require('node:fs');
const path = require('node:path');

const view = require('../web/static/view.js');

test('only the two modes are accepted, anything else is "nothing asked"', () => {
  assert.strictEqual(view.normalize('full'), view.FULL);
  assert.strictEqual(view.normalize('edit'), view.EDIT);
  assert.strictEqual(view.normalize(''), '');
  assert.strictEqual(view.normalize(null), '');
  assert.strictEqual(view.normalize(undefined), '');
  // A typo in a bookmark must not become a third state.
  assert.strictEqual(view.normalize('Full'), '');
  assert.strictEqual(view.normalize('fullscreen'), '');
});

test('the address wins over what the device chose last time', () => {
  assert.strictEqual(view.initial(view.FULL, view.EDIT), view.FULL);
  assert.strictEqual(view.initial(view.EDIT, view.FULL), view.EDIT);
  assert.strictEqual(view.initial('', view.FULL), view.FULL);
  // Nothing anywhere: the page as it has always looked.
  assert.strictEqual(view.initial('', ''), view.EDIT);
  assert.strictEqual(view.initial('nonsense', 'nonsense'), view.EDIT);
});

test('?view= asks for a mode and a bare address asks for nothing', () => {
  assert.strictEqual(view.fromQuery(new URLSearchParams('view=full')), view.FULL);
  assert.strictEqual(view.fromQuery(new URLSearchParams('view=edit')), view.EDIT);
  assert.strictEqual(view.fromQuery(new URLSearchParams('tab=scenes')), '');
  assert.strictEqual(view.fromQuery(new URLSearchParams('')), '');
  assert.strictEqual(view.fromQuery(null), '');
});

test('the remote editor bookmark opens the ordinary page, not the wall', () => {
  assert.strictEqual(view.fromQuery(new URLSearchParams('edit=1')), view.EDIT);
  assert.strictEqual(view.fromQuery(new URLSearchParams('edit=1&view=full')), view.EDIT);
  // Through initial as well: the editor must survive a remembered wall mode.
  assert.strictEqual(view.initial(view.fromQuery(new URLSearchParams('edit=1')), view.FULL), view.EDIT);
});

test('the toggle goes to the other mode and never stays put', () => {
  assert.strictEqual(view.next(view.EDIT), view.FULL);
  assert.strictEqual(view.next(view.FULL), view.EDIT);
  assert.strictEqual(view.next(''), view.FULL);
});

test('isFull answers only for the wall', () => {
  assert.strictEqual(view.isFull(view.FULL), true);
  assert.strictEqual(view.isFull(view.EDIT), false);
  assert.strictEqual(view.isFull(undefined), false);
});

test('index.html applies the stored mode before the first paint', () => {
  // The head script cannot import this module: it has to run before any request,
  // and the page would flash its header otherwise. Copies of the key and of the
  // attribute are therefore expected — this is the guard that they match.
  const html = fs.readFileSync(
    path.join(__dirname, '..', 'web', 'templates', 'index.html'),
    'utf8',
  );
  assert.ok(html.includes("'" + view.KEY + "'"), 'the head script reads the stored key');
  assert.ok(html.includes('dataset.view'), 'the head script sets the mode on <html>');
  assert.ok(html.includes('id="view-exit"'), 'the wall keeps a way back');
  assert.ok(!html.includes('id="customize"'), 'the header no longer carries the old editor switch');
});
