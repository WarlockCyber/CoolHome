// Local device names.
//
// The cloud owns the name a device was given in the app, and no API call
// changes it. CoolHome therefore keeps its own name for each device, in the same
// file as the layout, and applies it when devices are served. This module holds
// the two rules that are easy to get wrong:
//
//  1. a name is trimmed and bounded — an empty name means "show the cloud's
//     name again", not "store a blank";
//  2. renaming a device also renames the widgets that were titled after it. A
//     widget's title is frozen at the moment it is added, so without this a
//     wall would keep showing the old name on the remote and the new one on the
//     card beside it.
(function (root) {
  'use strict';

  // MAX matches dashboard.MaxNameLen on the server: a card heading, not a
  // description.
  var MAX = 60;

  // DESC_MAX matches dashboard.MaxDescLen: a sentence about what something does,
  // long enough to name the devices, short enough to sit under a card.
  var DESC_MAX = 280;

  // runeLength counts what the server counts: code points, not UTF-16 units.
  // The two differ for anything outside the basic plane (an emoji is one rune
  // and two units), and a bound checked in the wrong unit lets the page send
  // something the server rejects.
  function runeLength(text) {
    var n = 0;
    for (var i = 0; i < text.length; i++) {
      var code = text.charCodeAt(i);
      if (code >= 0xd800 && code <= 0xdbff && i + 1 < text.length) {
        var next = text.charCodeAt(i + 1);
        if (next >= 0xdc00 && next <= 0xdfff) i++;
      }
      n++;
    }
    return n;
  }

  // cut shortens text to at most `limit` runes without splitting a surrogate
  // pair: a half pair renders as "�", a character nobody typed.
  function cut(text, limit) {
    if (runeLength(text) <= limit) return text;
    var out = '';
    var n = 0;
    for (var i = 0; i < text.length; i++) {
      var code = text.charCodeAt(i);
      var width = 1;
      if (code >= 0xd800 && code <= 0xdbff && i + 1 < text.length) {
        var next = text.charCodeAt(i + 1);
        if (next >= 0xdc00 && next <= 0xdfff) width = 2;
      }
      if (n + 1 > limit) break;
      out += text.substr(i, width);
      i += width - 1;
      n++;
    }
    return out;
  }

  // normalize cleans what the user typed. It returns '' for "no local name".
  function normalize(raw) {
    var text = String(raw == null ? '' : raw).replace(/\s+/g, ' ').trim();
    if (!text) return '';
    return cut(text, MAX).trim();
  }

  // normalizeNote cleans a description. Line breaks are kept — a scene that
  // touches the hall and the kitchen reads better on two lines than in one run-on
  // sentence — but each line is trimmed and empty lines are dropped, so a paste
  // with stray blank lines does not become a tall card.
  function normalizeNote(raw) {
    var text = String(raw == null ? '' : raw).replace(/\r\n?/g, '\n');
    var lines = [];
    var parts = text.split('\n');
    for (var i = 0; i < parts.length; i++) {
      var line = parts[i].replace(/[ \t]+/g, ' ').trim();
      if (line) lines.push(line);
    }
    return cut(lines.join('\n'), DESC_MAX).trim();
  }

  // separator is what joins a device name and a control name in a widget title
  // ("Кофемашина · Таймер"). Renaming has to follow it.
  var SEPARATOR = ' · ';

  // renamedTitles rewrites the widget titles that were derived from a device's
  // name. A title the user typed themselves is left alone: it is not a copy of
  // the device name, and changing it would be an edit nobody asked for.
  function renamedTitles(widgets, deviceId, oldName, newName) {
    var list = Array.isArray(widgets) ? widgets : [];
    var from = String(oldName == null ? '' : oldName).trim();
    var to = String(newName == null ? '' : newName).trim();
    var out = [];
    for (var i = 0; i < list.length; i++) {
      var w = list[i];
      if (!w || w.device_id !== deviceId) {
        out.push(w);
        continue;
      }
      var title = String(w.title == null ? '' : w.title).trim();
      // Titles are only rewritten when the device was called something: with no
      // old name there is nothing to match, and with no new name the widget
      // falls back to the device's own name on its own.
      if (!from || !to) {
        out.push(w);
        continue;
      }
      if (title === from) {
        out.push(Object.assign({}, w, { title: cut(to, MAX) }));
      } else if (title.indexOf(from + SEPARATOR) === 0) {
        // The suffix stays, the name is cut to fit what is left. Truncating the
        // whole string instead would eat the control's name ("Кофемашина ·
        // Тай…"), and the title has to fit the same bound the server enforces
        // or the whole rename is rejected with it.
        var suffix = title.slice(from.length);
        var room = MAX - runeLength(suffix);
        var head = room > 0 ? cut(to, room) : '';
        out.push(Object.assign({}, w, { title: head ? head + suffix : cut(to, MAX) }));
      } else {
        out.push(w);
      }
    }
    return out;
  }

  var api = {
    MAX: MAX,
    DESC_MAX: DESC_MAX,
    normalizeNote: normalizeNote,
    SEPARATOR: SEPARATOR,
    normalize: normalize,
    renamedTitles: renamedTitles,
    runeLength: runeLength,
    cut: cut,
  };

  root.CoolHomeNames = api;
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
})(typeof window !== 'undefined' ? window : globalThis);
