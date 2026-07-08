import axios from 'axios';

export const api = axios.create({
  baseURL: import.meta.env.VITE_API_BASE_URL || 'api',
});

api.interceptors.request.use((config) => {
  const token = localStorage.getItem('token');
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

api.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response && error.response.status === 401) {
      // Ignore Synology endpoints as they use 401 for 2FA challenges
      if (error.config.url && error.config.url.includes('synology/')) {
        return Promise.reject(error);
      }

      // Clear token and redirect to login if 401 received
      // Avoid redirect loop if already on login page
      if (!window.location.pathname.endsWith('/login')) {
        localStorage.removeItem('token');
        window.location.href = 'login';
      }
    }
    return Promise.reject(error);
  }
);

export const getSettings = async () => {
  const response = await api.get('settings');
  return response.data;
};

export const listSources = async (): Promise<string[]> => {
  const response = await api.get('sources');
  return response.data.sources || [];
};

export const updateSettings = async (settings: Record<string, string>) => {
  const response = await api.post('settings', { settings });
  return response.data;
};

export const getStatus = async () => {
  const response = await api.get('status');
  return response.data;
};

export const getGoogleAlbums = async () => {
  const response = await api.get('google/albums');
  return response.data;
};
// Devices
export interface Device {
  id: number;
  name: string;
  host: string;
  width: number;
  height: number;
  orientation: string;
  board_name?: string;
  // Firmware version the frame reports (X-Firmware-Version on each pull);
  // empty until it first checks in. Shown in the Devices list.
  firmware_version?: string;
  // When the frame last checked in (pulled an image), RFC3339; null/absent if
  // never seen. Shown as "Last check-in" in the Devices list.
  last_seen_at?: string | null;
  // Whether the frame checked in within ~2 rotation cycles (server-computed).
  // false = stuck / offline / asleep — detected from absence, so it catches a
  // hung frame that can't report anything itself.
  online?: boolean;
  // The frame's last reset cause (X-Reset-Reason): poweron / deepsleep / sw /
  // task_wdt / int_wdt / wdt / panic / brownout. A crash cause is flagged in the
  // Devices list so a crash loop is visible after the frame recovers.
  last_reset_reason?: string;
  // false on no-PSRAM boards (FireBeetle) that can't do HTTPS; drives the
  // https:// image-URL warning in the device dialog.
  https_supported?: boolean;

  enable_collage: boolean;
  show_date?: boolean;
  show_photo_date?: boolean;
  show_weather?: boolean;
  weather_lat?: number;
  weather_lon?: number;
  ai_provider?: string;
  ai_model?: string;
  ai_prompt?: string;
  layout?: string;
  display_mode?: string;
  show_calendar?: boolean;
  calendar_id?: string;
  date_format?: string;
  show_battery?: boolean;
  display_order?: string;
  date_position?: string;
  photo_date_position?: string;
  weather_position?: string;
  battery_position?: string;
  battery_style?: string;
  battery_rotation?: number;
  battery_text_side?: string;
  battery_icon_scale?: number;
  overlay_scale?: number;
  overlay_font?: string;
  overlay_weight?: string;
  show_names?: boolean;
  names_position?: string;
  name_format?: string;
  names_show_age?: boolean;
  names_max_len?: number;
  show_location?: boolean;
  location_position?: string;
  location_max_len?: number;
  show_description?: boolean;
  description_position?: string;
  description_max_len?: number;
  // Rotation-position overlay chip (where in the rotation the frame is).
  show_rotation?: boolean;
  rotation_position?: string;
  rotation_show_total?: boolean;
  // Comma-separated Immich album IDs this frame is restricted to (empty = all).
  immich_album_ids?: string;
  // Rotation-pool filters: only photos from today's date / only favorites.
  on_this_day?: boolean;
  favorites_only?: boolean;
  // Comma-separated overlay element keys whose icon is hidden (empty = all shown).
  overlay_hidden_icons?: string;
  // Two-tier on-screen low-battery "charge me" warning, rendered into the image.
  low_battery_warn_enabled?: boolean;
  low_battery_warn_percent?: number;
  low_battery_warn_text?: string;
  low_battery_warn_position?: string;
  critical_battery_warn_percent?: number;
  critical_battery_warn_text?: string;
  critical_battery_warn_position?: string;
  // How long this device's activity log is kept before the oldest entries are
  // pruned. unit is one of "days" | "months" | "years". Default 6 months.
  log_retention_value?: number;
  log_retention_unit?: string;
  // How the frame is mounted relative to the panel's native orientation
  // (0/90/180/270). Drives the lightbox's viewing dimensions.
  display_rotation_deg?: number;
  // Id of the most recent served-image thumbnail, served via
  // /served-image-thumbnail/:id — drives the Devices-list current-image preview.
  current_thumb_id?: string;
  // Latest battery estimate, attached by the ListDevices handler.
  battery_percent?: number; // -1 = no data yet
  battery_days_remaining?: number; // -1 = unknown
  battery_trend?: string;
  // Coarse charge state the frame reports each pull (charging | full |
  // on_battery); only boards that can sense USB send it. Drives the "plugged in"
  // indicator instead of a (possibly wrong) percentage while on USB.
  battery_status?: string;
  // Server-inferred "on USB": the latest reading is physically implausible for a
  // running frame (EE02-on-USB ADC garbage), so we show a plugged-in indicator.
  battery_plugged?: boolean;
  // Optional pack capacity in mAh (0/unset = not entered). Not required — the
  // %/day drain estimate already works without it — but when set the server
  // also reports an estimated average discharge current (see below).
  battery_capacity_mah?: number;
  // Estimated average discharge current in mA, derived from drain %/day and
  // battery_capacity_mah. 0 when capacity isn't set or the device isn't
  // currently discharging.
  battery_estimated_current_ma?: number;
  // GPIO the frame is using for an external battery voltage divider, on
  // boards with no built-in battery ADC. Read-only here -- selected on the
  // frame's own local WebGUI, mirrored via X-Battery-ADC-Pin. -1/unset =
  // none configured.
  battery_adc_gpio?: number;
  created_at: string;
  model?: any;
}

export const listDevices = async () => {
  const response = await api.get('devices');
  return response.data;
};

export const addDevice = async (params: {
  host: string;

  enable_collage: boolean;
  show_date: boolean;
  show_photo_date?: boolean;
  show_weather: boolean;
  weather_lat: number;
  weather_lon: number;
  layout?: string;
  display_mode?: string;
  show_calendar?: boolean;
  calendar_id?: string;
  date_format?: string;
  show_battery?: boolean;
  display_order?: string;
  date_position?: string;
  photo_date_position?: string;
  weather_position?: string;
  battery_position?: string;
  battery_style?: string;
  battery_rotation?: number;
  battery_text_side?: string;
  battery_icon_scale?: number;
  overlay_scale?: number;
  overlay_font?: string;
  overlay_weight?: string;
  show_names?: boolean;
  names_position?: string;
  name_format?: string;
  names_show_age?: boolean;
  names_max_len?: number;
  show_location?: boolean;
  location_position?: string;
  location_max_len?: number;
  show_description?: boolean;
  description_position?: string;
  description_max_len?: number;
  show_rotation?: boolean;
  rotation_position?: string;
  rotation_show_total?: boolean;
  immich_album_ids?: string;
  on_this_day?: boolean;
  favorites_only?: boolean;
  overlay_hidden_icons?: string;
  // Two-tier on-screen low-battery "charge me" warning, rendered into the image.
  low_battery_warn_enabled?: boolean;
  low_battery_warn_percent?: number;
  low_battery_warn_text?: string;
  low_battery_warn_position?: string;
  critical_battery_warn_percent?: number;
  critical_battery_warn_text?: string;
  critical_battery_warn_position?: string;
}) => {
  const response = await api.post('devices', params);
  return response.data;
};

// Updates server-owned + shared fields only. Dimensions / board name come
// from refreshDevice(); device-side config (including the shared copy of
// name + orientation) is synced via updateDeviceConfig().
export const updateDevice = async (
  id: number,
  name: string,
  host: string,
  orientation: string,
  enableCollage: boolean,
  showDate: boolean,
  showPhotoDate: boolean,
  showWeather: boolean,
  weatherLat: number,
  weatherLon: number,
  aiProvider?: string,
  aiModel?: string,
  aiPrompt?: string,
  layout?: string,
  displayMode?: string,
  showCalendar?: boolean,
  calendarId?: string,
  dateFormat?: string,
  showBattery?: boolean,
  overlayPositions?: {
    date_position?: string;
    photo_date_position?: string;
    weather_position?: string;
    battery_position?: string;
    battery_style?: string;
    battery_rotation?: number;
    battery_text_side?: string;
    battery_icon_scale?: number;
    overlay_scale?: number;
    overlay_font?: string;
    overlay_weight?: string;
    show_names?: boolean;
    names_position?: string;
    name_format?: string;
    names_show_age?: boolean;
    names_max_len?: number;
    show_location?: boolean;
    location_position?: string;
    location_max_len?: number;
    show_description?: boolean;
    description_position?: string;
    description_max_len?: number;
    show_rotation?: boolean;
    rotation_position?: string;
    rotation_show_total?: boolean;
    display_order?: string;
    immich_album_ids?: string;
    on_this_day?: boolean;
    favorites_only?: boolean;
    overlay_hidden_icons?: string;
    // Two-tier on-screen low-battery "charge me" warning, rendered into the image.
    low_battery_warn_enabled?: boolean;
    low_battery_warn_percent?: number;
    low_battery_warn_text?: string;
    low_battery_warn_position?: string;
    critical_battery_warn_percent?: number;
    critical_battery_warn_text?: string;
    critical_battery_warn_position?: string;
  }
) => {
  const response = await api.put(`/devices/${id}`, {
    name,
    host,
    orientation,
    enable_collage: enableCollage,
    show_date: showDate,
    show_photo_date: showPhotoDate,
    show_weather: showWeather,
    weather_lat: weatherLat,
    weather_lon: weatherLon,
    ai_provider: aiProvider || '',
    ai_model: aiModel || '',
    ai_prompt: aiPrompt || '',
    layout: layout || 'photo_overlay',
    display_mode: displayMode || 'cover',
    show_calendar: showCalendar || false,
    calendar_id: calendarId || '',
    date_format: dateFormat || '',
    show_battery: showBattery || false,
    date_position: overlayPositions?.date_position || 'bottom-left',
    photo_date_position: overlayPositions?.photo_date_position || 'bottom-left',
    weather_position: overlayPositions?.weather_position || 'bottom-right',
    battery_position: overlayPositions?.battery_position || 'top-right',
    battery_style: overlayPositions?.battery_style || 'both',
    battery_rotation: overlayPositions?.battery_rotation ?? 0,
    battery_text_side: overlayPositions?.battery_text_side || 'right',
    battery_icon_scale: overlayPositions?.battery_icon_scale ?? 1,
    overlay_scale: overlayPositions?.overlay_scale ?? 1,
    overlay_font: overlayPositions?.overlay_font || 'noto_sans',
    overlay_weight: overlayPositions?.overlay_weight || 'medium',
    show_names: overlayPositions?.show_names || false,
    names_position: overlayPositions?.names_position || 'top-left',
    name_format: overlayPositions?.name_format || 'first_last',
    names_show_age: overlayPositions?.names_show_age || false,
    names_max_len: overlayPositions?.names_max_len ?? 30,
    show_location: overlayPositions?.show_location || false,
    location_position: overlayPositions?.location_position || 'bottom-center',
    location_max_len: overlayPositions?.location_max_len ?? 40,
    show_description: overlayPositions?.show_description || false,
    description_position:
      overlayPositions?.description_position || 'wide-bottom',
    description_max_len: overlayPositions?.description_max_len ?? 80,
    show_rotation: overlayPositions?.show_rotation || false,
    rotation_position: overlayPositions?.rotation_position || 'bottom-right',
    rotation_show_total: overlayPositions?.rotation_show_total ?? true,
    display_order: overlayPositions?.display_order || 'shuffle',
    immich_album_ids: overlayPositions?.immich_album_ids ?? '',
    on_this_day: overlayPositions?.on_this_day || false,
    favorites_only: overlayPositions?.favorites_only || false,
    overlay_hidden_icons: overlayPositions?.overlay_hidden_icons ?? '',
  });
  return response.data;
};

// Pulls live state (dimensions, board name, config, processing settings,
// palette) from the device. Requires the device to be online.
export interface BatterySample {
  sampled_at: string;
  percent: number;
  voltage_mv: number;
}

export interface BatteryEstimate {
  has_data: boolean;
  current_percent: number;
  current_voltage_mv: number;
  drain_per_day: number;
  days_remaining: number;
  trend: 'discharging' | 'charging' | 'stable' | 'insufficient';
  sample_count: number;
  window_start: string;
  last_sampled_at: string;
  recent: BatterySample[];
  basis: 'voltage' | 'percent';
  // Estimated average discharge current in mA. Only computed when the
  // device's battery_capacity_mah is set and trend === 'discharging'.
  estimated_current_ma?: number;
}

export const getBatteryEstimate = async (
  id: number
): Promise<BatteryEstimate> => {
  const response = await api.get(`/devices/${id}/battery`);
  return response.data;
};

// Battery level since now-value-unit, for the Devices-list history chart
// (day/month/year range picker). Raw samples for short ranges, one point per
// calendar day for longer ones — see the backend's BatteryService.History.
export const getBatteryHistory = async (
  id: number,
  value: number,
  unit: 'days' | 'months' | 'years'
): Promise<BatterySample[]> => {
  const response = await api.get(`/devices/${id}/battery/history`, {
    params: { value, unit },
  });
  return response.data;
};

export const updateDeviceBatteryCapacity = async (
  id: number,
  capacityMah: number
) => {
  const response = await api.put(`/devices/${id}/battery-capacity`, {
    capacity_mah: capacityMah,
  });
  return response.data;
};

export const refreshDevice = async (id: number) => {
  const response = await api.post(`/devices/${id}/refresh`);
  return response.data;
};

export const deleteDevice = async (id: number) => {
  const response = await api.delete(`/devices/${id}`);
  return response.data;
};

// Jump the device's rotation queue by `steps` (positive = forward, negative =
// back). The next ordered pull jumps to that image; no-op for collage / non-
// ordered sources.
export const skipQueue = async (id: number, steps: number) => {
  const response = await api.post(`/devices/${id}/skip`, { steps });
  return response.data;
};

// Ask an awake, OTA-capable frame to check for and install a firmware update.
// Resolves with { updated, message }: updated=false means it's already current.
// Rejects (502) when the frame is unreachable/asleep. Only S3 boards have OTA.
export const triggerOtaUpdate = async (
  id: number
): Promise<{ updated: boolean; message: string }> => {
  const response = await api.post(`/devices/${id}/ota-update`);
  return response.data;
};

// Activity log — every pull attempt the frame makes (success or failure),
// unlike device history which only records a successful serve.
export interface DeviceLogEntry {
  id: number;
  device_id: number;
  timestamp: string;
  success: boolean;
  status_code: number;
  trigger_reason: string;
  source: string;
  image_id: number;
  battery_percent: number;
  voltage_mv: number;
  battery_status: string;
  firmware_version: string;
  reset_reason: string;
  ip: string;
  display_width: number;
  display_height: number;
}

export const listDeviceLogs = async (
  id: number,
  limit = 50,
  offset = 0
): Promise<{ logs: DeviceLogEntry[]; total: number }> => {
  const response = await api.get(`/devices/${id}/logs`, {
    params: { limit, offset },
  });
  return response.data;
};

// How long a device's activity log is kept before the oldest entries are
// pruned. unit is one of "days" | "months" | "years".
export const updateDeviceLogRetention = async (
  id: number,
  value: number,
  unit: string
) => {
  const response = await api.put(`/devices/${id}/log-retention`, {
    value,
    unit,
  });
  return response.data;
};

// Downloads the device's full retained activity log as a CSV file. Fetched
// as a blob (not a plain <a href>) because the download endpoint requires the
// same Bearer-token auth as the rest of the API — a bare link navigation
// wouldn't carry it.
export const downloadDeviceLogs = async (id: number, deviceName: string) => {
  const response = await api.get(`/devices/${id}/logs/download`, {
    responseType: 'blob',
  });
  const url = URL.createObjectURL(response.data);
  const a = document.createElement('a');
  a.href = url;
  a.download = `${deviceName.replace(/\s+/g, '-')}-activity-log.csv`;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
};

export const pushToDevice = async (deviceID: number, imageID: number) => {
  const response = await api.post(`/devices/${deviceID}/push`, {
    image_id: imageID,
  });
  return response.data;
};

export const pushPublicArtToDevice = async (
  deviceID: number,
  candidate: unknown,
  composition: unknown
) => {
  const response = await api.post(`/devices/${deviceID}/push`, {
    public_art: {
      candidate,
      composition,
    },
  });
  return response.data;
};

// ── Public Art ──────────────────────────────────────────────────────────────

export type PublicArtSearchConfig = {
  provider?: string;
  query?: string;
  orientation?: string;
  min_image_long_edge?: number;
  preferred_image_long_edge?: number;
  limit?: number;
};

export const searchPublicArt = async (config: PublicArtSearchConfig) => {
  const response = await api.post('public-art/search', config);
  return response.data;
};

export const selectPublicArt = async (
  candidate: unknown,
  composition: unknown
) => {
  const response = await api.post('public-art/select', {
    candidate,
    composition,
  });
  return response.data;
};

export const clearPublicArtSelection = async () => {
  const response = await api.delete('public-art/select');
  return response.data;
};

const apiBase = () => (api.defaults.baseURL || 'api').replace(/\/$/, '');

// Public (no-auth) image endpoints — used directly as <img> src.
export const publicArtThumbnailSrc = (candidate: {
  image_url?: string;
  thumbnail_url?: string;
}) => {
  const params = new URLSearchParams();
  if (candidate.image_url)
    params.set('candidate_image_url', candidate.image_url);
  if (candidate.thumbnail_url)
    params.set('candidate_thumbnail_url', candidate.thumbnail_url);
  return `${apiBase()}/public-art/thumbnail?${params.toString()}`;
};

export const publicArtPreviewSrc = (params: Record<string, string>) => {
  return `${apiBase()}/public-art/preview?${new URLSearchParams(
    params
  ).toString()}`;
};

export const getMqttStatus = async (): Promise<{
  enabled: boolean;
  connected: boolean;
}> => {
  const response = await api.get('mqtt/status');
  return response.data;
};

export const getDeviceConfig = async (id: number) => {
  const response = await api.get(`/devices/${id}/config`);
  return response.data;
};

export const updateDeviceConfig = async (
  id: number,
  config: Record<string, unknown>
) => {
  const response = await api.put(`/devices/${id}/config`, config);
  return response.data;
};

export const createURLSource = async (url: string, deviceIDs: number[]) => {
  const response = await api.post('gallery/urls', {
    url,
    device_ids: deviceIDs,
  });
  return response.data;
};

export const updateURLSource = async (
  id: number,
  url: string,
  deviceIDs: number[]
) => {
  const response = await api.put(`/gallery/urls/${id}`, {
    url,
    device_ids: deviceIDs,
  });
  return response.data;
};

export const listURLSources = async () => {
  const response = await api.get('gallery/urls');
  return response.data;
};

export const deleteURLSource = async (id: number) => {
  const response = await api.delete(`/gallery/urls/${id}`);
  return response.data;
};

export const listPhotos = async (
  source?: string,
  limit?: number,
  offset?: number,
  sort?: string,
  immichAlbumIds?: string[]
) => {
  const params: any = {};
  if (source) params.source = source;
  if (limit) params.limit = limit;
  if (offset) params.offset = offset;
  if (sort) params.sort = sort;
  if (immichAlbumIds && immichAlbumIds.length > 0)
    params.immich_album_ids = immichAlbumIds.join(',');
  const response = await api.get('gallery/photos', { params });
  return response.data;
};

// Immich albums that currently have synced photos (not every album in the
// whole Immich library) — backs the Gallery's per-album filter chips.
export const listUsedImmichAlbums = async (): Promise<
  { id: string; name: string; count: number }[]
> => {
  const response = await api.get('immich/albums/used');
  return response.data;
};

// Persist a manual photo sequence for 'custom' display order. ids are in
// display order (index 0 shown first).
export const reorderGalleryPhotos = async (ids: number[]) => {
  const response = await api.post('gallery/reorder', { ids });
  return response.data;
};

export const deletePhoto = async (id: number) => {
  const response = await api.delete(`/gallery/photos/${id}`);
  return response.data;
};

export const updateAccount = async (
  oldPassword: string,
  newUsername?: string,
  newPassword?: string
) => {
  const response = await api.post('auth/account', {
    old_password: oldPassword,
    new_username: newUsername,
    new_password: newPassword,
  });
  return response.data;
};

export const listSessions = async () => {
  const response = await api.get('auth/sessions');
  return response.data;
};

export const revokeSession = async (id: number) => {
  const response = await api.delete(`/auth/sessions/${id}`);
  return response.data;
};

// Calendar
export const listCalendars = async () => {
  const response = await api.get('calendar/calendars');
  return response.data;
};

export const googleCalendarLogin = async () => {
  const response = await api.get('auth/google-calendar/login');
  return response.data;
};

export const googleCalendarLogout = async () => {
  const response = await api.post('auth/google-calendar/logout');
  return response.data;
};
