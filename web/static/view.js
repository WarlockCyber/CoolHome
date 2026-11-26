// Display modes: the ordinary page, and the wall.
//
// The same screen is looked at in two ways. "Режим редактирования" is the page as
// it has always been: header, tabs, refresh, footer. "Полный экран" is what hangs
// on a wall: the device panel and the dashboard, plus one control to get back.
// Nothing else — a corridor tablet should show the house, not the software.
//
// The choice belongs to the device, not to the installation: the phone in a
// pocket and the tablet on the wall want different things, and the server has no
// business remembering either. So it lives in localStorage and is mirrored into
// the address, which is how a bookmark opens the wall directly.
//
// The parsing is here, apart from the DOM, because the failures are quiet: the
// wrong default leaves a wall tablet showing a header it was set up to hide, and
// nobody notices until they are standing in front of it.
(function (root) {
  'use strict';

  var EDIT = 'edit';
  var FULL = 'full';
  // KEY is also read by the inline script in index.html, which has to know the
  // mode before the first paint. The test in web-tests/view.test.js guards the
  // two copies against drifting apart.
  var KEY = 'coolhome-view';

  // normalize answers with one of the two modes or with '' for "nothing was
  // asked". Anything else — a typo in a bookmark, a value from an older
  // version — is treated as no answer rather than as a third mode.
  function normalize(value) {
    if (value === FULL) return FULL;
    if (value === EDIT) return EDIT;
    return '';
  }

  // fromQuery is what the address asks for. "?edit=1" is the remote editor's
  // bookmark and it wins: that link is opened to configure the remote, and a
  // header hidden by a remembered wall mode would hide the editor's own tabs.
  function fromQuery(params) {
    if (!params || typeof params.get !== 'function') return '';
    if (params.get('edit') === '1') return EDIT;
    return normalize(params.get('view'));
  }

  // initial picks the mode the page opens in: the address first (a bookmark is a
  // deliberate act), then what this device chose last time, then the ordinary
  // page — which is what an install that never touched the mode must keep
  // showing.
  function initial(queryView, stored) {
    return normalize(queryView) || normalize(stored) || EDIT;
  }

  // next is the toggle. It is the exit button's job too, so the page never needs
  // to know which mode it came from.
  function next(view) {
    return normalize(view) === FULL ? EDIT : FULL;
  }

  function isFull(view) {
    return normalize(view) === FULL;
  }

  var api = {
    EDIT: EDIT,
    FULL: FULL,
    KEY: KEY,
    normalize: normalize,
    fromQuery: fromQuery,
    initial: initial,
    next: next,
    isFull: isFull,
  };

  root.CoolHomeView = api;
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
})(typeof window !== 'undefined' ? window : globalThis);
