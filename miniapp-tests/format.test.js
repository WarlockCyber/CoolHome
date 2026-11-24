'use strict';

// Unit tests for the mini-app's pure helpers. Run with `node --test`.

const test = require('node:test');
const assert = require('node:assert/strict');

const format = require('../miniapp/utils/format.js');

test('toDisplay applies the Tuya scale', () => {
  assert.equal(format.toDisplay(235, { scale: 1 }), 23.5);
  assert.equal(format.toDisplay(2350, { scale: 2 }), 23.5);
  assert.equal(format.toDisplay(235, { scale: 0 }), 235);
  assert.equal(format.toDisplay(235, null), 235);
  assert.equal(format.toDisplay('120', { scale: 1 }), 12);
});

test('toDisplay returns null for missing values', () => {
  assert.equal(format.toDisplay(null, { scale: 1 }), null);
  assert.equal(format.toDisplay(undefined, { scale: 1 }), null);
  assert.equal(format.toDisplay('', { scale: 1 }), null);
  assert.equal(format.toDisplay('abc', { scale: 1 }), null);
});

test('fromDisplay is the inverse of toDisplay', () => {
  assert.equal(format.fromDisplay(23.5, { scale: 1 }), 235);
  assert.equal(format.fromDisplay(23.46, { scale: 1 }), 235);
  assert.equal(format.fromDisplay('7', { scale: 0 }), 7);
  assert.equal(format.fromDisplay('nonsense', { scale: 1 }), null);
});

test('round trip keeps the value the server will accept', () => {
  const points = [{ scale: 0 }, { scale: 1 }, { scale: 2 }, { scale: 3 }];
  const samples = [0, 1, 7, 23.5, 99.9, 123.456];
  for (const point of points) {
    for (const sample of samples) {
      const raw = format.fromDisplay(sample, point);
      assert.ok(Number.isInteger(raw), `fromDisplay(${sample}, scale ${point.scale}) = ${raw} is not an integer`);
      const back = format.toDisplay(raw, point);
      assert.ok(Math.abs(back - sample) <= Math.pow(10, -point.scale) / 2 + 1e-9,
        `round trip ${sample} via scale ${point.scale} gave ${back}`);
    }
  }
});

test('formatValue renders booleans, enums, numbers and units', () => {
  assert.equal(format.formatValue(true, { type: 'Boolean' }), 'вкл');
  assert.equal(format.formatValue(false, { type: 'Boolean' }), 'выкл');
  assert.equal(format.formatValue(true, null), 'вкл');
  assert.equal(format.formatValue('colour', { type: 'Enum' }), 'цветной');
  assert.equal(format.formatValue('unknown_mode', { type: 'Enum' }), 'unknown_mode');
  assert.equal(format.formatValue(235, { type: 'Integer', scale: 1, unit: '°C' }), '23.5 °C');
  assert.equal(format.formatValue(50, { type: 'Integer', scale: 0, unit: '%' }), '50 %');
  assert.equal(format.formatValue(null, { type: 'Integer' }), '—');
});

test('controlKind picks a widget only when the point can carry a value', () => {
  assert.equal(format.controlKind({ type: 'Boolean' }), 'switch');
  assert.equal(format.controlKind({ type: 'Enum', options: ['a', 'b'] }), 'picker');
  assert.equal(format.controlKind({ type: 'Enum', options: [] }), 'none');
  assert.equal(format.controlKind({ type: 'Integer', min: 0, max: 100 }), 'slider');
  assert.equal(format.controlKind({ type: 'Integer' }), 'none');
  assert.equal(format.controlKind({ type: 'String' }), 'none');
  assert.equal(format.controlKind({ type: 'Raw' }), 'none');
  assert.equal(format.controlKind(null), 'none');
});

test('sliderBounds converts the domain to display units', () => {
  const bounds = format.sliderBounds({ type: 'Integer', min: 100, max: 1000, step: 10, scale: 1 });
  assert.equal(bounds.min, 10);
  assert.equal(bounds.max, 100);
  assert.equal(bounds.step, 1);

  // A zero step would freeze the widget, so it is floored at one display unit.
  const flat = format.sliderBounds({ type: 'Integer', min: 0, max: 5, step: 0, scale: 2 });
  assert.equal(flat.step, 1);

  // A missing domain degrades to a single point instead of NaN.
  const missing = format.sliderBounds({ type: 'Integer' });
  assert.equal(missing.min, 0);
  assert.equal(missing.max, 0);
});

test('clamp keeps values inside the declared range', () => {
  assert.equal(format.clamp(150, { min: 0, max: 100 }), 100);
  assert.equal(format.clamp(-5, { min: 0, max: 100 }), 0);
  assert.equal(format.clamp(42, { min: 0, max: 100 }), 42);
  assert.equal(format.clamp(42, {}), 42);
});

test('enumIndex finds the current option and falls back to the first', () => {
  assert.equal(format.enumIndex(['white', 'colour'], 'colour'), 1);
  assert.equal(format.enumIndex(['white', 'colour'], 'missing'), 0);
  assert.equal(format.enumIndex([], 'white'), 0);
  assert.equal(format.enumIndex(null, 'white'), 0);
});

test('formatSyncTime renders a local time or nothing', () => {
  const rendered = format.formatSyncTime('2026-11-24T09:23:37Z');
  assert.match(rendered, /^\d{2}:\d{2}:\d{2}$/);
  assert.equal(format.formatSyncTime(''), '');
  assert.equal(format.formatSyncTime('not a date'), '');
  assert.equal(format.formatSyncTime(null), '');
});

test('isOffline treats only an explicit false as offline', () => {
  assert.equal(format.isOffline({ online: false }), true);
  assert.equal(format.isOffline({ online: true }), false);
  assert.equal(format.isOffline({}), false);
  assert.equal(format.isOffline(null), true);
});
