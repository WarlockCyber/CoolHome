// Which display mode the page opens in, decided before the first paint.
//
// This is a file rather than an inline <script> because the server sends
// `script-src 'self'`: an inline script is blocked, silently, and the page then
// shows its header for a moment on a wall tablet that was set up to hide it.
// The rule itself lives in view.js; this only reads the address and storage, so
// that the very first paint already has the right mode.
(function () {
  'use strict';
  var mode = 'edit';
  try {
    var params = new URLSearchParams(location.search);
    var asked = params.get('view');
    if (params.get('edit') === '1') {
      // The remote editor's bookmark: it needs the header, whatever the device
      // remembers about the wall.
      mode = 'edit';
    } else if (asked === 'full' || asked === 'edit') {
      mode = asked;
    } else if (localStorage.getItem('coolhome-view') === 'full') {
      mode = 'full';
    }
  } catch (err) {
    // Storage closed: the ordinary page is what an install that never chose a
    // mode shows anyway.
  }
  document.documentElement.dataset.view = mode;
})();
