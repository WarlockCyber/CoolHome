'use strict';

// Pure helpers that turn the CoolHome API payload into something a page can
// render, and turn a UI value back into the integer Tuya expects.
//
// A point looks like
//   {code, name, type, min, max, step, scale, unit, options}
// where `scale` shifts the integer: a raw 235 with scale 1 is 23.5.
// Everything here is side-effect free so it can be tested without a runtime.

var BOOLEAN_LABELS = {
  true: 'вкл',
  false: 'выкл',
};

// A learned RF switch exposes one key; the cloud calls it "one ON", which says
// nothing about what the physical remote's button does (usually a toggle), so
// the label stays neutral.
var BUTTON_LABELS = {
  one_on: 'Кнопка',
};

var ENUM_LABELS = {
  white: 'белый',
  colour: 'цветной',
  color: 'цветной',
  open: 'открыть',
  stop: 'стоп',
  close: 'закрыть',
  on: 'вкл',
  off: 'выкл',
  auto: 'авто',
  manual: 'вручную',
  low: 'низкий',
  middle: 'средний',
  high: 'высокий',
  small: 'слабый',
  large: 'сильный',
  warm: 'тёплый',
  cold: 'холодный',
};

function scaleFactor(point) {
  var scale = point && typeof point.scale === 'number' ? point.scale : 0;
  if (scale <= 0) return 1;
  var factor = 1;
  for (var i = 0; i < scale; i++) factor *= 10;
  return factor;
}

function roundTo(value, decimals) {
  var factor = Math.pow(10, decimals);
  return Math.round(value * factor) / factor;
}

// toDisplay converts a raw data-point value into the number a human edits:
// 235 with scale 1 becomes 23.5.
function toDisplay(raw, point) {
  if (raw === null || raw === undefined || raw === '') return null;
  var num = typeof raw === 'number' ? raw : parseFloat(raw);
  if (isNaN(num)) return null;
  var factor = scaleFactor(point);
  if (factor === 1) return num;
  var decimals = (point && point.scale) || 0;
  return roundTo(num / factor, decimals);
}

// fromDisplay is the inverse of toDisplay: the server and Tuya only accept the
// shifted integer, so 23.5 with scale 1 is sent as 235.
function fromDisplay(value, point) {
  var num = typeof value === 'number' ? value : parseFloat(value);
  if (isNaN(num)) return null;
  var factor = scaleFactor(point);
  return Math.round(num * factor);
}

// clamp keeps a value inside the point's declared domain so a stray tap on a
// slider cannot produce a request the server rejects with 400.
function clamp(value, point) {
  var num = value;
  if (point && typeof point.min === 'number' && num < point.min) num = point.min;
  if (point && typeof point.max === 'number' && num > point.max) num = point.max;
  return num;
}

// formatValue renders a raw value for a read-only row or a control label.
function formatValue(raw, point) {
  if (raw === null || raw === undefined || raw === '') return '—';
  var type = point && point.type ? String(point.type).toLowerCase() : '';
  if (typeof raw === 'boolean') return BOOLEAN_LABELS[raw ? 'true' : 'false'];
  if (type === 'bool' || type === 'boolean') {
    return BOOLEAN_LABELS[raw === true || raw === 'true' || raw === 1 ? 'true' : 'false'];
  }
  if (type === 'enum' && typeof raw === 'string') {
    return ENUM_LABELS[raw] || raw;
  }
  if (type === 'value' || type === 'string') return String(raw);
  var shown = toDisplay(raw, point);
  if (shown === null) return String(raw);
  var text = String(shown);
  var unit = point && point.unit ? String(point.unit) : '';
  return unit ? text + ' ' + unit : text;
}

// controlKind decides which widget a writable point gets. Anything not listed
// falls back to a read-only row rather than a widget that cannot send a value.
function controlKind(point) {
  if (!point) return 'none';
  var type = String(point.type || '').toLowerCase();
  if (type === 'bool' || type === 'boolean') return 'switch';
  if (type === 'enum') {
    return point.options && point.options.length ? 'picker' : 'none';
  }
  if (type === 'value' || type === 'integer') {
    var hasDomain = typeof point.min === 'number' && typeof point.max === 'number';
    return hasDomain ? 'slider' : 'none';
  }
  // An infrared remote key from the code library: a momentary action rendered
  // as a button. A learned RF key is 'passive': the cloud accepts it and never
  // transmits, so it must not become a control.
  if (type === 'button') return 'button';
  return 'none';
}

// sliderBounds returns the slider domain in display units, plus a step of at
// least 1 display unit so the widget never gets a zero step.
function sliderBounds(point) {
  var factor = scaleFactor(point);
  var min = toDisplay(point && point.min, point);
  var max = toDisplay(point && point.max, point);
  if (min === null) min = 0;
  if (max === null || max < min) max = min;
  var step = point && point.step ? point.step / factor : 1;
  if (!(step > 0)) step = 1;
  return { min: min, max: max, step: step };
}

// enumIndex finds the current option so a picker opens on the right row.
function enumIndex(options, raw) {
  if (!options || !options.length) return 0;
  var idx = options.indexOf(raw);
  return idx < 0 ? 0 : idx;
}

// enumLabel localizes one enum option for display in a picker.
function enumLabel(option) {
  return ENUM_LABELS[option] || option;
}

// formatSyncTime renders the snapshot timestamp for the header line.
function formatSyncTime(iso) {
  if (!iso) return '';
  var date = new Date(iso);
  if (isNaN(date.getTime())) return '';
  function pad(n) {
    return n < 10 ? '0' + n : String(n);
  }
  return pad(date.getHours()) + ':' + pad(date.getMinutes()) + ':' + pad(date.getSeconds());
}

// isOffline reports whether a device cannot take commands, so the UI can dim it
// instead of letting the user press a control that will fail with 502.
function isOffline(device) {
  return !device || device.online === false;
}

module.exports = {
  BUTTON_LABELS: BUTTON_LABELS,
  toDisplay: toDisplay,
  fromDisplay: fromDisplay,
  clamp: clamp,
  formatValue: formatValue,
  controlKind: controlKind,
  sliderBounds: sliderBounds,
  enumIndex: enumIndex,
  enumLabel: enumLabel,
  formatSyncTime: formatSyncTime,
  isOffline: isOffline,
};
