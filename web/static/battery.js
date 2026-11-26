// The battery level of a device, for the card's colour.
//
// Tuya reports a battery two ways, and a home has both:
//
//	battery_state      enum: low / middle / high — the cloud's own verdict
//	battery_percentage integer 0..100
//
// Where both are present they agree (10 % with "low", 18 % with "middle" on the
// home this was measured on), and the enum is preferred: it is the device's own
// judgement, made by the firmware that knows the cell, and it needs no threshold
// of ours.
//
// Where only a percentage is present, the thresholds below are a choice, not a
// fact: nothing in the cloud says where "low" starts. They are chosen so that a
// smoke alarm at 28 % is already pointed out, and documented so that they can be
// argued with.
(function (root) {
  'use strict';

  var LEVEL = { low: 'low', middle: 'middle', high: 'high' };

  // Percent bounds used only when the device reports no battery_state.
  var LOW_PERCENT = 15;
  var MIDDLE_PERCENT = 40;

  // STATE_LEVEL maps the cloud's enum onto a level. "lower" appears on some
  // models instead of "low"; anything unknown is not guessed at.
  var STATE_LEVEL = {
    low: LEVEL.low,
    lower: LEVEL.low,
    middle: LEVEL.middle,
    normal: LEVEL.middle,
    medium: LEVEL.middle,
    high: LEVEL.high,
    full: LEVEL.high,
  };

  function numberValue(raw) {
    if (raw === null || raw === undefined || raw === '') return null;
    var n = typeof raw === 'number' ? raw : Number(String(raw).trim());
    return isFinite(n) ? n : null;
  }

  // deviceLevel reads a device's readings and returns {level, percent, source}
  // or null when the device reports no battery at all.
  function deviceLevel(device) {
    // The status is the source: every device on the home this was measured on
    // declares its battery there. Nothing is inferred from the read-out list,
    // because a model that lists the point differently would lose its badge.
    var values = {};
    var list = (device && device.status) || [];
    for (var i = 0; i < list.length; i++) {
      values[list[i].code] = list[i].value;
    }

    var percent = null;
    if ('battery_percentage' in values) {
      percent = numberValue(values.battery_percentage);
      if (percent !== null && (percent < 0 || percent > 100)) percent = null;
    }

    var state = values.battery_state;
    var byState = null;
    if (typeof state === 'string' && state) {
      byState = STATE_LEVEL[state.toLowerCase()] || null;
    }
    if (byState) {
      return { level: byState, percent: percent, source: 'state' };
    }
    if (percent === null) return null;
    var level = LEVEL.high;
    if (percent <= LOW_PERCENT) level = LEVEL.low;
    else if (percent <= MIDDLE_PERCENT) level = LEVEL.middle;
    return { level: level, percent: percent, source: 'percent' };
  }

  // needsAttention reports whether the level is worth colouring the card for.
  // A healthy battery gets no colour: a page where every card is tinted says
  // nothing, and the point of the tint is that it is rare.
  function needsAttention(info) {
    return Boolean(info) && (info.level === LEVEL.low || info.level === LEVEL.middle);
  }

  // label is what the badge says.
  function label(info) {
    if (!info) return '';
    if (info.percent !== null && info.percent !== undefined) return info.percent + ' %';
    if (info.level === LEVEL.low) return 'низкий';
    if (info.level === LEVEL.middle) return 'средний';
    return 'высокий';
  }

  // describe is the sentence for a title attribute: what the card is telling.
  function describe(info) {
    if (!info) return '';
    if (info.level === LEVEL.low) {
      return 'Батарея почти разряжена' + (info.percent !== null ? ' (' + info.percent + ' %)' : '') +
        ' — замените её';
    }
    if (info.level === LEVEL.middle) {
      return 'Батарея разряжается' + (info.percent !== null ? ' (' + info.percent + ' %)' : '');
    }
    return 'Батарея в порядке' + (info.percent !== null ? ' (' + info.percent + ' %)' : '');
  }

  var api = {
    LEVEL: LEVEL,
    LOW_PERCENT: LOW_PERCENT,
    MIDDLE_PERCENT: MIDDLE_PERCENT,
    STATE_LEVEL: STATE_LEVEL,
    deviceLevel: deviceLevel,
    needsAttention: needsAttention,
    label: label,
    describe: describe,
  };

  root.CoolHomeBattery = api;
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
})(typeof window !== 'undefined' ? window : globalThis);
