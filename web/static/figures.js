// Which readings a wall shows as big numbers.
//
// A sensor that measures both temperature and humidity used to show only the
// temperature: the humidity sat behind the settings button, so the card answered
// half the question one walks up to it with. The selection lives here, outside
// the browser, because it has two rules that are easy to break and invisible
// when broken:
//
//  1. match by code, never by unit — set-points ("maxhum_set", "upper_temp")
//     are reported in the same ℃ and % and must not become figures;
//  2. draw exactly what the settings panel excludes and no more — a code the
//     figures do not draw must stay reachable as a row, or the value vanishes
//     from the UI altogether.
(function (root) {
  'use strict';

  // Tuya uses two names for the measured temperature and three for humidity.
  var TEMPERATURE_CODES = ['temp_current', 'va_temperature'];
  var HUMIDITY_CODES = ['humidity_value', 'va_humidity', 'humidity'];

  function isTemperatureCode(code) {
    // "temp_current_f" is the same reading in Fahrenheit: showing it beside the
    // Celsius value would double every sensor on the wall.
    if (String(code).slice(-2) === '_f') return false;
    return TEMPERATURE_CODES.indexOf(String(code)) >= 0;
  }

  function isHumidityCode(code) {
    return HUMIDITY_CODES.indexOf(String(code)) >= 0;
  }

  function isTemperaturePoint(point) {
    return Boolean(point) && isTemperatureCode(point.code);
  }

  function isHumidityPoint(point) {
    return Boolean(point) && isHumidityCode(point.code);
  }

  // readingsOf drops points that are also writable functions. A point declared
  // in both lists is a control, and drawing it as a figure as well would put the
  // same value on the card twice — once large, once as a slider.
  function readingsOf(device) {
    var functions = (device && device.functions) || [];
    var written = {};
    for (var i = 0; i < functions.length; i++) written[functions[i].code] = true;
    var readings = (device && device.readings) || [];
    var out = [];
    for (var j = 0; j < readings.length; j++) {
      if (!written[readings[j].code]) out.push(readings[j]);
    }
    return out;
  }

  function firstMatching(points, predicate) {
    for (var i = 0; i < points.length; i++) {
      if (predicate(points[i])) return points[i];
    }
    return null;
  }

  function figurePoints(device) {
    var readings = readingsOf(device);
    return {
      temp: firstMatching(readings, isTemperaturePoint),
      humid: firstMatching(readings, isHumidityPoint),
    };
  }

  // figures decides what is drawn and what the settings panel must therefore
  // skip, in one place: the two cannot drift apart if the same call produces
  // both answers.
  //
  // No temperature means no figures at all. A device that reports humidity but
  // not temperature has no sensor card, so excluding its humidity from the panel
  // would hide the value completely.
  function figures(device) {
    var picked = figurePoints(device);
    if (!picked.temp) return { temp: null, humid: null, codes: [] };
    var codes = [picked.temp.code];
    if (picked.humid) codes.push(picked.humid.code);
    return { temp: picked.temp, humid: picked.humid, codes: codes };
  }

  var api = {
    TEMPERATURE_CODES: TEMPERATURE_CODES,
    HUMIDITY_CODES: HUMIDITY_CODES,
    isTemperatureCode: isTemperatureCode,
    isHumidityCode: isHumidityCode,
    isTemperaturePoint: isTemperaturePoint,
    isHumidityPoint: isHumidityPoint,
    readingsOf: readingsOf,
    figurePoints: figurePoints,
    figures: figures,
  };

  root.CoolHomeFigures = api;
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
})(typeof window !== 'undefined' ? window : globalThis);
