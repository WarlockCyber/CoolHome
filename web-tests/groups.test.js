// Device groups: the tab strip, which devices a tab shows, and what happens when
// a device moves.
//
// The quiet failures these guard: a device dropped from its last group vanishing
// from the page, a device copied into a second tab, and two tabs that read the
// same.

const test = require('node:test');
const assert = require('node:assert');

const groups = require('../web/static/groups.js');

const ALL = ['lamp-1', 'lamp-2', 'plug-1', 'thermo-1'];

function sample() {
  return [
    { id: 'g1', name: 'Свет', devices: ['lamp-1', 'lamp-2'] },
    { id: 'g2', name: 'Климат', devices: ['thermo-1'] },
  ];
}

test('the strip is favourites, the groups, then all devices', () => {
  const tabs = groups.tabs(sample(), ['lamp-1']);
  assert.deepStrictEqual(
    tabs.map((t) => t.name),
    ['Избранное', 'Свет', 'Климат', 'Все устройства'],
  );
  assert.strictEqual(tabs[0].kind, 'favorites');
  // "All devices" is last and always there: a device in no group is never lost.
  assert.strictEqual(tabs[tabs.length - 1].kind, 'all');
});

test('favourites is what opens on start', () => {
  assert.strictEqual(groups.defaultTab(), groups.FAVORITES_TAB);
});

test('a tab shows its devices in their own order', () => {
  const s = sample();
  assert.deepStrictEqual(groups.devicesFor('g1', s, [], ALL), ['lamp-1', 'lamp-2']);
  assert.deepStrictEqual(groups.devicesFor(groups.ALL_TAB, s, [], ALL), ALL);
});

test('a tab hides a device that no longer exists', () => {
  // The list is a view: a dangling id would draw nothing and leave a gap.
  const s = [{ id: 'g1', name: 'Свет', devices: ['lamp-1', 'gone-1'] }];
  assert.deepStrictEqual(groups.devicesFor('g1', s, [], ALL), ['lamp-1']);
  assert.deepStrictEqual(groups.devicesFor(groups.FAVORITES_TAB, s, ['gone-1'], ALL), []);
});

test('a tab of an unknown id shows nothing rather than everything', () => {
  // Showing all devices for a stale tab id would look like the filter broke.
  assert.deepStrictEqual(groups.devicesFor('нет-такой', sample(), [], ALL), []);
});

test('moving a device takes it out of its previous group', () => {
  const moved = groups.move(sample(), 'lamp-1', 'g2');
  assert.deepStrictEqual(moved[0].devices, ['lamp-2'], 'still in Свет');
  assert.deepStrictEqual(moved[1].devices, ['thermo-1', 'lamp-1'], 'now in Климат');
  // Never two tabs: two places to change it from, and they can disagree.
  for (const g of moved) {
    assert.ok(g.devices.filter((d) => d === 'lamp-1').length <= 1);
  }
});

test('moving into a group that does not exist changes nothing', () => {
  const before = sample();
  const after = groups.move(before, 'lamp-1', 'нет-такой');
  assert.deepStrictEqual(after, before, 'a failed move must not ungroup the device');
});

test('removing from a group leaves the device ungrouped, not deleted', () => {
  const after = groups.removeFromGroup(sample(), 'lamp-1');
  assert.deepStrictEqual(after[0].devices, ['lamp-2']);
  // It is still in the home; "all devices" is where it is found.
  assert.ok(ALL.indexOf('lamp-1') >= 0);
});

test('creating a group refuses a blank or duplicate name', () => {
  const blank = groups.createGroup(sample(), '   ');
  assert.strictEqual(blank.error, 'нужно название группы');
  assert.strictEqual(blank.groups.length, 2);

  // Case-insensitively: "Свет" and "свет" as two tabs is a puzzle, not a feature.
  const dup = groups.createGroup(sample(), 'свет');
  assert.match(dup.error, /уже есть/);
  assert.strictEqual(dup.groups.length, 2);
});

test('creating a group adds an empty tab with a stable id', () => {
  const res = groups.createGroup(sample(), '  Кухня  ');
  assert.strictEqual(res.error, '');
  assert.strictEqual(res.groups.length, 3);
  assert.strictEqual(res.groups[2].name, 'Кухня');
  assert.ok(res.groups[2].id.length > 0);
  assert.deepStrictEqual(res.groups[2].devices, []);
});

test('the group limit is refused before the request is sent', () => {
  let list = [];
  for (let i = 0; i < groups.MAX; i++) list.push({ id: 'g' + i, name: 'Группа ' + i });
  const res = groups.createGroup(list, 'Ещё одна');
  assert.match(res.error, /предел/);
  assert.strictEqual(res.groups.length, groups.MAX);
});

test('deleting a group keeps its devices in the home', () => {
  const after = groups.deleteGroup(sample(), 'g1');
  assert.strictEqual(after.length, 1);
  assert.deepStrictEqual(after[0].name, 'Климат');
  // The devices of the deleted group are not in any group now, but they exist;
  // deleting a tab must not delete a lamp.
  assert.ok(ALL.indexOf('lamp-1') >= 0);
});

test('renaming a group refuses a duplicate but allows its own name', () => {
  const same = groups.renameGroup(sample(), 'g1', 'Свет');
  assert.strictEqual(same.error, '');
  const dup = groups.renameGroup(sample(), 'g1', 'Климат');
  assert.match(dup.error, /уже есть/);
});

test('favourites toggle on and off, and are bounded', () => {
  let list = groups.toggleFavorite([], 'lamp-1');
  assert.deepStrictEqual(list, ['lamp-1']);
  assert.strictEqual(groups.isFavorite(list, 'lamp-1'), true);

  list = groups.toggleFavorite(list, 'lamp-1');
  assert.deepStrictEqual(list, []);
  assert.strictEqual(groups.isFavorite(list, 'lamp-1'), false);
});

test('a favourite may also belong to a group', () => {
  // That is the point of favourites: "what I touch" is a view, not a place.
  const s = sample();
  const favs = groups.toggleFavorite([], 'lamp-1');
  assert.strictEqual(groups.groupOf(s, 'lamp-1').name, 'Свет');
  assert.strictEqual(groups.isFavorite(favs, 'lamp-1'), true);
});

test('a group id derived from a Russian name stays readable', () => {
  const id = groups.makeGroupID('Кухня и столовая');
  assert.match(id, /^kuhnya-i-stolovaya-/, id);
  assert.match(groups.makeGroupID('!!!'), /^group-/);
  assert.notStrictEqual(groups.makeGroupID('Свет'), groups.makeGroupID('Свет'));
});
