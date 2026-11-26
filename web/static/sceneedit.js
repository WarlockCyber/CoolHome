// What choosing a device in the scene editor does to the draft.
//
// One rule, in one place, because getting it wrong is invisible: the row is
// repainted right after the choice, and a repaint that reads the draft before
// the choice was written into it silently puts the row back to "— устройство —".
//
// The choice always comes from the control that fired, never from a re-read of
// the DOM: by the time a retry reads the page, the element that held the value
// may already have been replaced.
(function (root) {
  'use strict';

  // applyDeviceChoice records a device on one action row.
  //
  // The function is cleared: it belonged to the previous device, and keeping it
  // would save a pair that cannot exist — a code the new device does not have.
  // The value is kept: a person who typed "true" meant it, and retyping it
  // after every device change is work with nothing to show for it.
  function applyDeviceChoice(actions, index, device) {
    var list = Array.isArray(actions) ? actions : [];
    var action = list[index];
    if (!action) return list;
    action.device = String(device == null ? '' : device);
    action.code = '';
    return list;
  }

  var api = { applyDeviceChoice: applyDeviceChoice };

  root.CoolHomeSceneEdit = api;
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
})(typeof window !== 'undefined' ? window : this);
