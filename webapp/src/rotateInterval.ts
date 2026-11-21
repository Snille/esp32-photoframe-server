// Rotation-interval choices for the device dialog. `rotate_interval` is in
// SECONDS everywhere (frame /api/config, device_config, X-Config-Payload).
//
// The frame's own web UI lets the user type any hours + minutes, so the value
// synced back to the server is often not one of the presets below. A v-select
// whose model value is not in its items shows the bare number with no unit:
// a frame set to 2 minutes (120 s) then read as "120", which looked like
// 120 minutes. rotateIntervalOptions() therefore always includes the current
// value with a proper label.

export const ROTATE_INTERVAL_PRESETS: readonly number[] = [
  300, 900, 1800, 3600, 7200, 14400, 21600, 43200, 86400,
];

const plural = (n: number, unit: string) => `${n} ${unit}${n === 1 ? '' : 's'}`;

// Human label for an interval in seconds, e.g. 120 -> "2 minutes",
// 5400 -> "1 hour 30 minutes", 90 -> "1 minute 30 seconds".
export function formatRotateInterval(seconds: number): string {
  const total = Math.max(0, Math.round(Number(seconds) || 0));
  if (total === 0) return '0 minutes';
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  const parts: string[] = [];
  if (h) parts.push(plural(h, 'hour'));
  if (m) parts.push(plural(m, 'minute'));
  if (s) parts.push(plural(s, 'second'));
  return parts.join(' ');
}

// Preset items plus the current value (when it is a custom one), sorted.
export function rotateIntervalOptions(
  current?: number | null
): { title: string; value: number }[] {
  const values = new Set<number>(ROTATE_INTERVAL_PRESETS);
  const cur = Number(current);
  if (Number.isFinite(cur) && cur > 0) values.add(Math.round(cur));
  return [...values]
    .sort((a, b) => a - b)
    .map((value) => ({ title: formatRotateInterval(value), value }));
}
