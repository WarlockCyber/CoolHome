'use strict';

// View-model builders: they turn a CoolHome device payload into exactly what a
// page renders. Kept apart from the pages so the widget selection and value
// formatting can be tested without a mini-app runtime.

var format = require('./format.js');

// Category prefixes Tuya uses for common devices, mapped to a glyph. Unknown
// categories fall back to a neutral dot rather than an empty cell.
var GLYPHS = [
  ['dj', '💡'],
  ['dd', '🔌'],
  ['cz', '🔌'],
  ['wk', '🌡'],
  ['wsdcg', '🌡'],
  ['mcs', '🌡'],
  ['cl', '🪟'],
  ['clkg', '🪟'],
  ['kj', '🌀'],
  ['fs', '🌀'],
  ['jsq', '💧'],
  ['mal', '💧'],
  ['pir', '👤'],
  ['sj', '🚪'],
  ['ld', '💡'],
];

function glyphFor(category) {
  var code = String(category || '');
  for (var i = 0; i < GLYPHS.length; i++) {
    if (code.indexOf(GLYPHS[i][0]) === 0) return GLYPHS[i][1];
  }
  return '•';
}

// indexStatus flattens the status array into a code -> raw value map.
function indexStatus(device) {
  var byCode = {};
  var list = (device && device.status) || [];
  for (var i = 0; i < list.length; i++) {
    byCode[list[i].code] = list[i].value;
  }
  return byCode;
}

var SUMMARY_CODES = [
  'temp_current',
  'va_temperature',
  'temp_indoor',
  'va_humidity',
  'humidity',
  'battery_percentage',
  'bright_value',
  'percent_control',
];

// summarize picks the most useful readings for a one-line preview of a device.
function summarize(device) {
  var byCode = indexStatus(device);
  var points = {};
  var readings = (device && device.readings) || [];
  for (var i = 0; i < readings.length; i++) {
    points[readings[i].code] = readings[i];
  }

  var parts = [];
  for (var j = 0; j < SUMMARY_CODES.length && parts.length < 2; j++) {
    var code = SUMMARY_CODES[j];
    if (!Object.prototype.hasOwnProperty.call(byCode, code)) continue;
    parts.push(format.formatValue(byCode[code], points[code]));
  }
  if (parts.length) return parts.join(' · ');

  var switches = ['switch_led', 'switch_1', 'switch'];
  for (var k = 0; k < switches.length; k++) {
    if (Object.prototype.hasOwnProperty.call(byCode, switches[k])) {
      return format.formatValue(byCode[switches[k]], { type: 'Boolean' });
    }
  }
  return (device && device.id) || '';
}

// buildReadings pairs each declared status point with its current value, so the
// page shows labelled rows with units instead of raw tuples.
function buildReadings(device) {
  var byCode = indexStatus(device);
  var readings = (device && device.readings) || [];
  var rows = [];
  for (var i = 0; i < readings.length; i++) {
    var point = readings[i];
    if (!Object.prototype.hasOwnProperty.call(byCode, point.code)) continue;
    rows.push({
      code: point.code,
      name: point.name || point.code,
      value: format.formatValue(byCode[point.code], point),
    });
  }
  return rows;
}

// buildControls turns writable points into widgets. Points without a usable
// domain are skipped: a slider with no range would send values the server
// rejects, and a switch is only meaningful for booleans.
function buildControls(device) {
  var byCode = indexStatus(device);
  var functions = (device && device.functions) || [];
  var out = [];
  for (var i = 0; i < functions.length; i++) {
    var point = functions[i];
    var kind = format.controlKind(point);
    if (kind === 'none') continue;

    var raw = byCode[point.code];
    var name = point.name || point.code;
    if (kind === 'button' && format.BUTTON_LABELS[point.code]) {
      name = format.BUTTON_LABELS[point.code];
    }
    var row = {
      code: point.code,
      name: name,
      kind: kind,
      unit: point.unit || '',
      // busy is set by the page while a write is in flight; disabled keeps the
      // widget from accepting input on an offline device or mid-write, which is
      // what stops a second tap from being silently dropped.
      busy: false,
      disabled: device.online === false,
    };

    if (kind === 'switch') {
      row.checked = raw === true || raw === 'true' || raw === 1;
    } else if (kind === 'slider') {
      var bounds = format.sliderBounds(point);
      var current = format.toDisplay(raw, point);
      row.min = bounds.min;
      row.max = bounds.max;
      row.step = bounds.step;
      row.scale = point.scale || 0;
      row.value = current === null ? bounds.min : format.clamp(current, { min: bounds.min, max: bounds.max });
      row.label = format.formatValue(row.value, { scale: 0, unit: point.unit });
    } else if (kind === 'button') {
      // Nothing to read: a key press has no state, so the row is just a label
      // the page turns into a button.
      row.value = true;
    } else if (kind === 'picker') {
      var options = point.options || [];
      var labels = [];
      for (var j = 0; j < options.length; j++) labels.push(format.enumLabel(options[j]));
      row.options = options;
      row.labels = labels;
      row.index = format.enumIndex(options, raw);
      row.label = labels[row.index] || '';
    }
    out.push(row);
  }
  return out;
}

// unsupportedNote explains why a device has no widgets. The reason differs by
// category: an RF switch is driven by a remote, the blaster itself carries only
// remotes, and a sensor is simply not exposed. One generic sentence would be
// wrong for at least one of them.
function unsupportedNote(category, irEnabled) {
  var code = String(category || '');
  if (code === 'wnykq' || code === 'ir' || code === 'infrared') {
    if (irEnabled) {
      return 'ИК-пульт: data-point модели у него нет. Его пульты показаны отдельными устройствами в этом списке.';
    }
    return 'ИК-пульт: data-point модели у него нет. Включите COOLHOME_IR=true и подпишите сервис IR Control в консоли Tuya, чтобы увидеть его пульты отдельными устройствами.';
  }
  if (code.indexOf('rf_switch') === 0) {
    return 'RF-выключатель, привязанный к ИК-пульту: у него нет data-point модели, поэтому Tuya Cloud не отдаёт его функции и состояние. Управление — с физического пульта или в приложении Tuya.';
  }
  return 'Tuya Cloud не отдаёт функции и состояние этого устройства через API — оно доступно только в приложении Tuya.';
}

// buildDeviceRow is the list-page card for one device.
function buildDeviceRow(device) {
  return {
    id: device.id,
    name: device.name || device.id,
    online: device.online !== false,
    glyph: glyphFor(device.category),
    summary: summarize(device),
    controls: ((device && device.functions) || []).length,
    // A device the cloud does not describe (an RF switch behind an IR blaster)
    // has no widgets by design, which the page must explain rather than show
    // as a broken device.
    unsupported: !!device.spec_unsupported,
    // A device the server flags as visible but not controllable.
    note: device.note || '',
  };
}

module.exports = {
  unsupportedNote: unsupportedNote,
  glyphFor: glyphFor,
  indexStatus: indexStatus,
  summarize: summarize,
  buildReadings: buildReadings,
  buildControls: buildControls,
  buildDeviceRow: buildDeviceRow,
};
