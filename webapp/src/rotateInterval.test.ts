// Run: npm test (node --test, native TypeScript type stripping).
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  formatRotateInterval,
  rotateIntervalOptions,
  ROTATE_INTERVAL_PRESETS,
} from './rotateInterval.ts';

test('formats seconds with units, never a bare number', () => {
  assert.equal(formatRotateInterval(120), '2 minutes');
  assert.equal(formatRotateInterval(60), '1 minute');
  assert.equal(formatRotateInterval(300), '5 minutes');
  assert.equal(formatRotateInterval(3600), '1 hour');
  assert.equal(formatRotateInterval(5400), '1 hour 30 minutes');
  assert.equal(formatRotateInterval(7200), '2 hours');
  assert.equal(formatRotateInterval(90), '1 minute 30 seconds');
});

test('a frame-set 2-minute interval (120 s) gets its own labelled option', () => {
  const opts = rotateIntervalOptions(120);
  const cur = opts.find((o) => o.value === 120);
  assert.ok(cur, '120 s must be selectable');
  assert.equal(cur.title, '2 minutes');
  assert.equal(opts[0].value, 120, 'sorted ascending');
  assert.equal(opts.length, ROTATE_INTERVAL_PRESETS.length + 1);
});

test('a preset value is not duplicated', () => {
  const opts = rotateIntervalOptions(300);
  assert.equal(opts.length, ROTATE_INTERVAL_PRESETS.length);
  assert.equal(opts.find((o) => o.value === 300)?.title, '5 minutes');
});

test('missing or invalid current value adds nothing', () => {
  for (const v of [undefined, null, 0, -5, NaN]) {
    assert.equal(
      rotateIntervalOptions(v as number).length,
      ROTATE_INTERVAL_PRESETS.length
    );
  }
});
