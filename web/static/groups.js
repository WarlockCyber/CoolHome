// Device groups: which tab shows what, and how a device moves between them.
//
// The main screen is a strip of tabs above the grid. The first is always
// "Избранное" — it is what opens on start, because a wall tablet is looked at to
// answer "the things I touch", not "everything I own". A device belongs to at
// most one group; moving it is a move, and it stays reachable on the "all
// devices" tab.
//
// Everything here is a pure function over the stored lists, because the failures
// are quiet: a device dropped from the last group it was in disappears from the
// page, and a duplicate tab looks like a bug in the cloud.
(function (root) {
  'use strict';

  // MAX matches dashboard.MaxGroups: tabs on a wall tablet, not a filing cabinet.
  var MAX = 12;
  // MAX_FAVORITES matches dashboard.MaxFavorites.
  var MAX_FAVORITES = 40;

  // FAVORITES_TAB is the id of the synthetic first tab. It is not a stored
  // group: it is a view of devices that may also live in a group.
  var FAVORITES_TAB = '__favorites';
  var ALL_TAB = '__all';

  function normalizeName(raw) {
    return String(raw == null ? '' : raw).replace(/\s+/g, ' ').trim();
  }

  // tabs builds the strip: favourites, then the groups, then "all devices".
  // "All" is last and always present, so a device in no group is never lost.
  function tabs(groups, favorites) {
    var out = [{ id: FAVORITES_TAB, name: 'Избранное', kind: 'favorites' }];
    var list = Array.isArray(groups) ? groups : [];
    for (var i = 0; i < list.length; i++) {
      out.push({ id: list[i].id, name: list[i].name, kind: 'group' });
    }
    out.push({ id: ALL_TAB, name: 'Все устройства', kind: 'all' });
    return out;
  }

  // defaultTab is what opens on start. Favourites is the answer to "what do I
  // touch", and an empty favourites tab still beats an alphabetical wall of 43
  // cards.
  function defaultTab() {
    return FAVORITES_TAB;
  }

  // groupOf returns the group a device belongs to, or null.
  function groupOf(groups, deviceID) {
    var list = Array.isArray(groups) ? groups : [];
    for (var i = 0; i < list.length; i++) {
      var devices = list[i].devices || [];
      if (devices.indexOf(deviceID) >= 0) return list[i];
    }
    return null;
  }

  // devicesFor returns the device ids shown on a tab, in display order. "All"
  // keeps the natural order of the home; a group keeps its own order; favourites
  // keeps the order they were starred in.
  function devicesFor(tabID, groups, favorites, allDeviceIDs) {
    if (tabID === ALL_TAB) return (allDeviceIDs || []).slice();
    if (tabID === FAVORITES_TAB) {
      // A favourite that no longer exists is not shown: the list is a view, and
      // a dangling id would draw nothing.
      var known = {};
      var all = allDeviceIDs || [];
      for (var k = 0; k < all.length; k++) known[all[k]] = true;
      return (favorites || []).filter(function (id) {
        return known[id];
      });
    }
    var list = Array.isArray(groups) ? groups : [];
    for (var i = 0; i < list.length; i++) {
      if (list[i].id === tabID) {
        var known2 = {};
        var all2 = allDeviceIDs || [];
        for (var j = 0; j < all2.length; j++) known2[all2[j]] = true;
        return (list[i].devices || []).filter(function (id) {
          return known2[id];
        });
      }
    }
    return [];
  }

  // move puts a device into a group, removing it from whichever group held it.
  // The device is never copied: two tabs showing one card would be two places to
  // change it from, and they can disagree after a refresh.
  function move(groups, deviceID, targetGroupID) {
    var id = String(deviceID == null ? '' : deviceID).trim();
    if (!id) return groups;
    var out = [];
    var list = Array.isArray(groups) ? groups : [];
    var placed = false;
    for (var i = 0; i < list.length; i++) {
      var g = list[i];
      var devices = (g.devices || []).filter(function (d) {
        return d !== id;
      });
      if (g.id === targetGroupID) {
        devices.push(id);
        placed = true;
      }
      out.push({ id: g.id, name: g.name, devices: devices });
    }
    if (!placed) {
      // Moving into a group that does not exist is a no-op, not a silent
      // ungrouping: the caller asked for something impossible.
      return groups;
    }
    return out;
  }

  // removeFromGroup takes a device out of every group. It becomes ungrouped, and
  // the "all devices" tab is where it is found again.
  function removeFromGroup(groups, deviceID) {
    var id = String(deviceID == null ? '' : deviceID).trim();
    var list = Array.isArray(groups) ? groups : [];
    return list.map(function (g) {
      return {
        id: g.id,
        name: g.name,
        devices: (g.devices || []).filter(function (d) {
          return d !== id;
        }),
      };
    });
  }

  // createGroup appends a group. It refuses a blank or duplicate name instead of
  // making a tab that reads the same as its neighbour.
  function createGroup(groups, name) {
    var clean = normalizeName(name);
    var list = Array.isArray(groups) ? groups : [];
    if (!clean) return { groups: list, error: 'нужно название группы' };
    if (clean.length > 40) return { groups: list, error: 'название длиннее 40 символов' };
    for (var i = 0; i < list.length; i++) {
      if (String(list[i].name).toLowerCase() === clean.toLowerCase()) {
        return { groups: list, error: 'группа «' + clean + '» уже есть' };
      }
    }
    if (list.length >= MAX) {
      return { groups: list, error: 'групп уже ' + MAX + ' — это предел' };
    }
    return {
      groups: list.concat([{ id: makeGroupID(clean), name: clean, devices: [] }]),
      error: '',
    };
  }

  // deleteGroup removes a tab. Its devices are not deleted — they become
  // ungrouped and stay on "all devices". Losing them would be a data loss
  // nobody asked for.
  function deleteGroup(groups, groupID) {
    var list = Array.isArray(groups) ? groups : [];
    return list.filter(function (g) {
      return g.id !== groupID;
    });
  }

  // renameGroup changes a name, refusing a duplicate.
  function renameGroup(groups, groupID, name) {
    var clean = normalizeName(name);
    var list = Array.isArray(groups) ? groups : [];
    if (!clean) return { groups: list, error: 'нужно название группы' };
    if (clean.length > 40) return { groups: list, error: 'название длиннее 40 символов' };
    for (var i = 0; i < list.length; i++) {
      if (list[i].id !== groupID && String(list[i].name).toLowerCase() === clean.toLowerCase()) {
        return { groups: list, error: 'группа «' + clean + '» уже есть' };
      }
    }
    return {
      groups: list.map(function (g) {
        return g.id === groupID ? { id: g.id, name: clean, devices: g.devices || [] } : g;
      }),
      error: '',
    };
  }

  // toggleFavorite adds or removes a device from favourites.
  function toggleFavorite(favorites, deviceID) {
    var id = String(deviceID == null ? '' : deviceID).trim();
    var list = Array.isArray(favorites) ? favorites.slice() : [];
    if (!id) return list;
    var at = list.indexOf(id);
    if (at >= 0) {
      list.splice(at, 1);
      return list;
    }
    if (list.length >= MAX_FAVORITES) return list;
    list.push(id);
    return list;
  }

  function isFavorite(favorites, deviceID) {
    return (Array.isArray(favorites) ? favorites : []).indexOf(deviceID) >= 0;
  }

  // makeGroupID derives a stable id from a name. Cyrillic is transliterated, the
  // same way rule ids are, so the file stays readable.
  var TRANSLIT = {
    а: 'a', б: 'b', в: 'v', г: 'g', д: 'd', е: 'e', ё: 'e', ж: 'zh', з: 'z',
    и: 'i', й: 'y', к: 'k', л: 'l', м: 'm', н: 'n', о: 'o', п: 'p', р: 'r',
    с: 's', т: 't', у: 'u', ф: 'f', х: 'h', ц: 'c', ч: 'ch', ш: 'sh', щ: 'sch',
    ъ: '', ы: 'y', ь: '', э: 'e', ю: 'yu', я: 'ya',
  };

  var seq = 0;

  function makeGroupID(name) {
    var text = String(name || '').toLowerCase().trim();
    var out = '';
    for (var i = 0; i < text.length; i++) {
      var ch = text[i];
      out += Object.prototype.hasOwnProperty.call(TRANSLIT, ch) ? TRANSLIT[ch] : ch;
    }
    out = out.replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '');
    if (!out) out = 'group';
    seq = (seq + 1) % 1296;
    return out.slice(0, 30) + '-' + Date.now().toString(36).slice(-3) + seq.toString(36).padStart(2, '0');
  }

  var api = {
    MAX: MAX,
    MAX_FAVORITES: MAX_FAVORITES,
    FAVORITES_TAB: FAVORITES_TAB,
    ALL_TAB: ALL_TAB,
    tabs: tabs,
    defaultTab: defaultTab,
    groupOf: groupOf,
    devicesFor: devicesFor,
    move: move,
    removeFromGroup: removeFromGroup,
    createGroup: createGroup,
    deleteGroup: deleteGroup,
    renameGroup: renameGroup,
    toggleFavorite: toggleFavorite,
    isFavorite: isFavorite,
    makeGroupID: makeGroupID,
    normalizeName: normalizeName,
  };

  root.CoolHomeGroups = api;
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
})(typeof window !== 'undefined' ? window : globalThis);
