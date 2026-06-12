<template>
  <div>
    <!-- Search config -->
    <v-row>
      <v-col cols="12" md="4">
        <v-select
          v-model="form.public_art_provider"
          label="Source"
          :items="providerOptions"
          variant="outlined"
          density="compact"
          hint="More public-art sources may be added here in future."
          persistent-hint
        ></v-select>
      </v-col>
      <v-col cols="12" md="5">
        <v-text-field
          v-model="form.public_art_query"
          label="Default search query"
          placeholder="monet, hokusai, landscape..."
          variant="outlined"
          density="compact"
          hint="/image/public_art serves the best-ranked result for this query."
          persistent-hint
        ></v-text-field>
      </v-col>
      <v-col cols="12" md="3">
        <v-select
          v-model="form.public_art_orientation"
          label="Search orientation"
          :items="orientationOptions"
          variant="outlined"
          density="compact"
          hint="Auto = frame orientation."
          persistent-hint
        ></v-select>
      </v-col>
    </v-row>

    <v-row>
      <v-col cols="12" md="6">
        <v-text-field
          v-model.number="form.public_art_min_image_long_edge"
          label="Minimum source long edge"
          type="number"
          variant="outlined"
          density="compact"
          hint="Keep at least 1600 for 13.3 inch readiness."
          persistent-hint
        ></v-text-field>
      </v-col>
      <v-col cols="12" md="6">
        <v-text-field
          v-model.number="form.public_art_preferred_image_long_edge"
          label="Preferred source long edge"
          type="number"
          variant="outlined"
          density="compact"
          hint="Ranking prefers this size or larger."
          persistent-hint
        ></v-text-field>
      </v-col>
    </v-row>

    <v-card variant="tonal" color="surface" class="mb-4 public-art-search-panel">
      <v-card-title
        class="d-flex align-center justify-space-between flex-wrap ga-2"
      >
        <div>
          <div class="text-subtitle-1 font-weight-bold">Search preview</div>
          <div class="text-caption text-medium-emphasis">
            Browse candidates before assigning the public art source to a frame.
          </div>
        </div>
        <div class="d-flex ga-2 flex-wrap">
          <v-btn
            variant="tonal"
            prepend-icon="mdi-close-circle-outline"
            :loading="clearing"
            @click="clearSelection"
          >
            Clear Selection
          </v-btn>
          <v-btn
            color="primary"
            prepend-icon="mdi-magnify"
            :loading="searching"
            @click="search"
          >
            Search Public Art
          </v-btn>
        </div>
      </v-card-title>

      <v-card-text>
        <v-alert type="info" variant="tonal" density="compact" class="mb-4">
          To lock one artwork on the frame: click <strong>Preview &amp; crop</strong>
          on a result, adjust the composition if needed, then click
          <strong>Use this artwork</strong>. While a selection is locked, device
          auto-rotate keeps serving that same artwork. Click
          <strong>Clear Selection</strong> to resume query-based rotation/dedup.
        </v-alert>

        <v-alert
          v-if="searchError"
          type="error"
          variant="tonal"
          density="compact"
          class="mb-4"
        >
          {{ searchError }}
        </v-alert>

        <v-alert
          v-if="!searching && searched && candidates.length === 0 && !searchError"
          type="warning"
          variant="tonal"
          density="compact"
          class="mb-4"
        >
          No artwork candidates found for this query.
        </v-alert>

        <v-row v-if="candidates.length > 0">
          <v-col
            v-for="candidate in candidates"
            :key="candidate.id"
            cols="12"
            sm="6"
            lg="4"
          >
            <v-card variant="outlined" class="h-100 public-art-candidate-card">
              <div class="public-art-thumb-frame bg-grey-lighten-3">
                <img
                  v-if="!thumbnailErrors[candidate.id]"
                  :src="thumbnailUrl(candidate)"
                  :alt="candidate.title || 'Artwork thumbnail'"
                  class="public-art-thumb-img"
                  loading="eager"
                  decoding="async"
                  @error="thumbnailErrors[candidate.id] = true"
                />
                <div
                  v-else
                  class="public-art-thumb-error d-flex align-center justify-center"
                >
                  <v-icon
                    icon="mdi-image-broken-variant"
                    size="48"
                    color="grey-lighten-1"
                  ></v-icon>
                </div>
              </div>
              <v-card-title class="text-subtitle-2 pb-1">
                {{ candidate.title || 'Untitled artwork' }}
              </v-card-title>
              <v-card-subtitle v-if="candidate.artist || candidate.date">
                {{ [candidate.artist, candidate.date].filter(Boolean).join(' · ') }}
              </v-card-subtitle>
              <v-card-text class="pt-2">
                <div class="d-flex flex-wrap ga-2 mb-2">
                  <v-chip size="x-small" color="primary" variant="tonal">
                    {{ candidate.provider.toUpperCase() }}
                  </v-chip>
                  <v-chip
                    v-if="candidate.width || candidate.height"
                    size="x-small"
                    :color="isLargeEnough(candidate) ? 'success' : 'warning'"
                    variant="tonal"
                  >
                    {{ candidate.width || '?' }}×{{ candidate.height || '?' }}
                  </v-chip>
                </div>
                <a
                  v-if="candidate.source_url"
                  :href="candidate.source_url"
                  target="_blank"
                  rel="noopener"
                  class="text-primary text-caption text-decoration-none"
                >
                  Source
                  <v-icon size="x-small" icon="mdi-open-in-new"></v-icon>
                </a>
              </v-card-text>
              <v-card-actions class="px-4 pb-4 pt-0 d-flex flex-wrap ga-2">
                <v-btn
                  size="small"
                  color="primary"
                  variant="outlined"
                  prepend-icon="mdi-crop"
                  :loading="composingId === candidate.id && previewLoading"
                  @click="selectCandidate(candidate)"
                >
                  Preview &amp; crop
                </v-btn>
                <v-btn
                  size="small"
                  color="primary"
                  variant="flat"
                  prepend-icon="mdi-check"
                  :loading="selectingId === candidate.id"
                  :disabled="!!selectingId"
                  @click="useCandidate(candidate)"
                >
                  Use this artwork
                </v-btn>
              </v-card-actions>
            </v-card>
          </v-col>
        </v-row>

        <!-- Compose Panel -->
        <v-expand-transition>
          <div v-if="composingId" class="mt-4">
            <v-divider class="mb-4" />
            <div class="text-subtitle-1 font-weight-bold mb-2">
              Compose: {{ candidates.find((c) => c.id === composingId)?.title }}
            </div>
            <v-row>
              <v-col cols="12" md="5">
                <v-card variant="outlined" class="pa-2" style="background: #f5f5f5">
                  <div
                    v-if="previewLoading"
                    class="d-flex justify-center align-center"
                    style="height: 300px"
                  >
                    <v-progress-circular
                      indeterminate
                      color="primary"
                    ></v-progress-circular>
                  </div>
                  <div
                    v-if="previewUrl && !previewError"
                    ref="cropFrame"
                    class="public-art-crop-frame"
                    @pointerdown="startDrag"
                    @wheel.prevent="zoom"
                  >
                    <img
                      :src="previewUrl"
                      class="public-art-crop-img"
                      draggable="false"
                      alt="Public art crop preview"
                    />
                    <div class="public-art-crop-hint">
                      Drag to reposition · scroll to zoom
                    </div>
                  </div>
                  <v-alert
                    v-if="previewError"
                    type="error"
                    variant="tonal"
                    density="compact"
                  >
                    {{ previewError }}
                  </v-alert>
                </v-card>
              </v-col>
              <v-col cols="12" md="7">
                <v-select
                  v-model="composition.scale_mode"
                  label="Scale mode"
                  :items="[
                    { title: 'Cover (crop to fill)', value: 'cover' },
                    { title: 'Fit (show whole image)', value: 'fit' },
                  ]"
                  variant="outlined"
                  density="compact"
                  class="mb-2"
                  @update:model-value="updatePreview"
                ></v-select>
                <div
                  v-if="composition.scale_mode === 'fit'"
                  class="d-flex align-center flex-wrap ga-2 mb-3"
                >
                  <span class="text-caption text-medium-emphasis">
                    Fill empty area:
                  </span>
                  <v-btn-toggle
                    v-model="composition.background_color"
                    density="compact"
                    mandatory
                    @update:model-value="updatePreview"
                  >
                    <v-btn value="#ffffff" size="small">White</v-btn>
                    <v-btn value="#000000" size="small">Black</v-btn>
                  </v-btn-toggle>
                  <v-text-field
                    v-model="composition.background_color"
                    label="Custom"
                    variant="outlined"
                    density="compact"
                    hide-details
                    style="max-width: 130px"
                    @change="updatePreview"
                  ></v-text-field>
                </div>
                <v-slider
                  v-model="composition.zoom"
                  label="Zoom"
                  :min="0.5"
                  :max="5"
                  :step="0.1"
                  thumb-label
                  class="mb-2"
                  @end="updatePreview"
                >
                  <template #append>
                    <span class="text-caption">{{ composition.zoom.toFixed(1) }}×</span>
                  </template>
                </v-slider>
                <v-row dense>
                  <v-col cols="6">
                    <v-text-field
                      v-model.number="composition.pan_x"
                      label="Pan X"
                      type="number"
                      variant="outlined"
                      density="compact"
                      step="0.05"
                      @change="updatePreview"
                    ></v-text-field>
                  </v-col>
                  <v-col cols="6">
                    <v-text-field
                      v-model.number="composition.pan_y"
                      label="Pan Y"
                      type="number"
                      variant="outlined"
                      density="compact"
                      step="0.05"
                      @change="updatePreview"
                    ></v-text-field>
                  </v-col>
                </v-row>
                <v-divider class="my-3"></v-divider>
                <div class="d-flex align-center flex-wrap ga-2 mb-2">
                  <v-select
                    v-model="pushDeviceId"
                    :items="devices"
                    item-title="name"
                    item-value="id"
                    label="Target device"
                    variant="outlined"
                    density="compact"
                    hide-details
                    style="min-width: 220px"
                  ></v-select>
                  <v-btn
                    color="success"
                    prepend-icon="mdi-send"
                    :loading="pushing"
                    :disabled="!pushDeviceId || pushing"
                    @click="pushComposed"
                  >
                    Push Image
                  </v-btn>
                </div>
                <div class="d-flex ga-2 mt-2">
                  <v-btn
                    variant="tonal"
                    prepend-icon="mdi-close"
                    @click="closeComposePanel"
                  >
                    Cancel
                  </v-btn>
                  <v-btn
                    color="primary"
                    prepend-icon="mdi-check"
                    :loading="!!selectingId"
                    :disabled="!!selectingId"
                    @click="confirmSelection"
                  >
                    Confirm &amp; Save
                  </v-btn>
                </div>
              </v-col>
            </v-row>
          </div>
        </v-expand-transition>
      </v-card-text>
    </v-card>

    <v-btn color="primary" prepend-icon="mdi-content-save" @click="save">
      Save Public Art Settings
    </v-btn>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue';
import {
  searchPublicArt,
  selectPublicArt,
  clearPublicArtSelection,
  pushPublicArtToDevice,
  publicArtThumbnailSrc,
  publicArtPreviewSrc,
} from '../api';

type Candidate = {
  provider: string;
  id: string;
  title?: string;
  artist?: string;
  date?: string;
  image_url: string;
  thumbnail_url?: string;
  source_url?: string;
  width?: number;
  height?: number;
};

type Composition = {
  scale_mode: string;
  zoom: number;
  pan_x: number;
  pan_y: number;
  background_color: string;
};

const props = defineProps<{
  // Global settings form holding the public_art_* fields.
  form: Record<string, any>;
  devices: any[];
  // saveSettingsInternal from the parent; persists public_art_config etc.
  save: () => void | Promise<void>;
}>();

const emit = defineEmits<{
  (e: 'message', text: string, isError?: boolean): void;
}>();

// Only Cleveland Museum of Art is wired up today; the select exists so adding
// more open-access sources later is a drop-in (add an option + a backend provider).
const providerOptions = [
  { title: 'Cleveland Museum of Art', value: 'cma' },
];
const orientationOptions = [
  { title: 'Auto (frame orientation)', value: 'auto' },
  { title: 'Landscape', value: 'landscape' },
  { title: 'Portrait', value: 'portrait' },
  { title: 'Any', value: 'any' },
];

const candidates = ref<Candidate[]>([]);
const thumbnailErrors = reactive<Record<string, boolean>>({});
const searching = ref(false);
const searched = ref(false);
const searchError = ref('');
const selectingId = ref('');
const clearing = ref(false);
const pushing = ref(false);
const pushDeviceId = ref<number | null>(null);
const composingId = ref('');
const previewSourceUrl = ref('');
const previewThumbnailUrl = ref('');
const previewUrl = ref('');
const previewLoading = ref(false);
const previewError = ref('');
const cropFrame = ref<HTMLElement | null>(null);
const composition = reactive<Composition>({
  scale_mode: 'cover',
  zoom: 1.0,
  pan_x: 0,
  pan_y: 0,
  background_color: '#ffffff',
});
const drag = reactive({ active: false, x: 0, y: 0 });
let previewTimer: number | undefined;

const clampPan = (v: number) => Math.max(-0.5, Math.min(0.5, v));
const clampZoom = (v: number) => Math.max(0.5, Math.min(5, v));

const isLargeEnough = (c: Candidate) => {
  const longEdge = Math.max(c.width || 0, c.height || 0);
  return longEdge >= (Number(props.form.public_art_min_image_long_edge) || 1600);
};

const resolveOrientation = () => {
  const o = props.form.public_art_orientation;
  if (o === 'landscape' || o === 'portrait') return o;
  if (o === 'any') return '';
  // auto → frame orientation
  return props.form.Orientation === 'portrait' ? 'portrait' : 'landscape';
};

const previewTarget = () => {
  const orientation = resolveOrientation();
  return orientation === 'portrait'
    ? { width: '300', height: '400' }
    : { width: '400', height: '300' };
};

const configFromForm = () => ({
  provider: props.form.public_art_provider || 'cma',
  query: props.form.public_art_query || 'art',
  orientation: resolveOrientation(),
  min_image_long_edge: Number(props.form.public_art_min_image_long_edge) || 1600,
  preferred_image_long_edge:
    Number(props.form.public_art_preferred_image_long_edge) || 2000,
});

const search = async () => {
  searching.value = true;
  searched.value = true;
  searchError.value = '';
  Object.keys(thumbnailErrors).forEach((k) => delete thumbnailErrors[k]);
  try {
    const data = await searchPublicArt({ ...configFromForm(), limit: 12 });
    candidates.value = Array.isArray(data) ? data : [];
  } catch (e: any) {
    candidates.value = [];
    searchError.value =
      e.response?.data?.error || e.message || 'Failed to search public art';
  } finally {
    searching.value = false;
  }
};

const thumbnailUrl = (c: Candidate) =>
  c.image_url || c.thumbnail_url ? publicArtThumbnailSrc(c) : '';

const openComposePanel = (c: Candidate) => {
  composingId.value = c.id;
  composition.scale_mode = 'cover';
  composition.zoom = 1.0;
  composition.pan_x = 0;
  composition.pan_y = 0;
  composition.background_color = '#ffffff';
  if (!pushDeviceId.value && props.devices.length === 1) {
    pushDeviceId.value = props.devices[0].id;
  }
  previewSourceUrl.value = c.image_url;
  previewThumbnailUrl.value = c.thumbnail_url || '';
  previewUrl.value = thumbnailUrl(c);
};

const closeComposePanel = () => {
  composingId.value = '';
  previewSourceUrl.value = '';
  previewThumbnailUrl.value = '';
  previewUrl.value = '';
  previewError.value = '';
};

const updatePreview = async () => {
  if (!previewSourceUrl.value) return;
  previewLoading.value = true;
  previewError.value = '';
  try {
    const target = previewTarget();
    previewUrl.value = publicArtPreviewSrc({
      candidate_image_url: previewSourceUrl.value,
      candidate_thumbnail_url: previewThumbnailUrl.value,
      scale_mode: composition.scale_mode,
      zoom: String(composition.zoom),
      pan_x: String(composition.pan_x),
      pan_y: String(composition.pan_y),
      background_color: composition.background_color,
      target_width: target.width,
      target_height: target.height,
    });
  } catch (e: any) {
    previewError.value = e.message || 'Failed to load preview';
  } finally {
    previewLoading.value = false;
  }
};

const schedulePreviewUpdate = () => {
  if (previewTimer !== undefined) window.clearTimeout(previewTimer);
  previewTimer = window.setTimeout(() => {
    previewTimer = undefined;
    updatePreview();
  }, 120);
};

const moveCrop = (clientX: number, clientY: number) => {
  if (!drag.active || !cropFrame.value) return;
  const rect = cropFrame.value.getBoundingClientRect();
  const dx = clientX - drag.x;
  const dy = clientY - drag.y;
  drag.x = clientX;
  drag.y = clientY;
  const z = clampZoom(composition.zoom || 1);
  composition.pan_x = clampPan(composition.pan_x - dx / Math.max(rect.width, 1) / z);
  composition.pan_y = clampPan(composition.pan_y - dy / Math.max(rect.height, 1) / z);
  schedulePreviewUpdate();
};

const onPointerMove = (event: PointerEvent) => moveCrop(event.clientX, event.clientY);

const stopDrag = () => {
  if (!drag.active) return;
  drag.active = false;
  window.removeEventListener('pointermove', onPointerMove);
  window.removeEventListener('pointerup', stopDrag);
  window.removeEventListener('pointercancel', stopDrag);
  updatePreview();
};

const startDrag = (event: PointerEvent) => {
  if (!previewUrl.value) return;
  drag.active = true;
  drag.x = event.clientX;
  drag.y = event.clientY;
  window.addEventListener('pointermove', onPointerMove);
  window.addEventListener('pointerup', stopDrag);
  window.addEventListener('pointercancel', stopDrag);
};

const zoom = (event: WheelEvent) => {
  const delta = event.deltaY > 0 ? -0.1 : 0.1;
  composition.zoom = clampZoom(Number((composition.zoom + delta).toFixed(1)));
  schedulePreviewUpdate();
};

const selectCandidate = async (c: Candidate) => {
  openComposePanel(c);
  await updatePreview();
};

const useCandidate = async (c: Candidate) => {
  selectingId.value = c.id;
  try {
    await selectPublicArt(c, {
      scale_mode: 'cover',
      zoom: 1,
      pan_x: 0,
      pan_y: 0,
      background_color: '#ffffff',
    });
    emit(
      'message',
      'Public art selection saved. Frames using /image/public_art will show this artwork.'
    );
  } catch (e: any) {
    emit(
      'message',
      'Failed to select artwork: ' + (e.response?.data?.error || e.message),
      true
    );
  } finally {
    selectingId.value = '';
  }
};

const confirmSelection = async () => {
  if (!composingId.value) return;
  const c = candidates.value.find((x) => x.id === composingId.value);
  if (!c) return;
  selectingId.value = composingId.value;
  try {
    await selectPublicArt(c, { ...composition });
    emit(
      'message',
      'Public art selection saved. Frames using /image/public_art will show this artwork.'
    );
    closeComposePanel();
  } catch (e: any) {
    emit(
      'message',
      'Failed to select artwork: ' + (e.response?.data?.error || e.message),
      true
    );
  } finally {
    selectingId.value = '';
  }
};

const pushComposed = async () => {
  if (!composingId.value || !pushDeviceId.value) return;
  const c = candidates.value.find((x) => x.id === composingId.value);
  if (!c) return;
  pushing.value = true;
  try {
    await pushPublicArtToDevice(pushDeviceId.value, c, { ...composition });
    const dev = props.devices.find((d) => d.id === pushDeviceId.value);
    emit('message', `Public art pushed to ${dev?.name || 'device'}.`);
  } catch (e: any) {
    emit(
      'message',
      'Failed to push artwork: ' + (e.response?.data?.error || e.message),
      true
    );
  } finally {
    pushing.value = false;
  }
};

const clearSelection = async () => {
  clearing.value = true;
  try {
    await clearPublicArtSelection();
    emit(
      'message',
      'Public art selection cleared. Frames will use the default search query again.'
    );
  } catch (e: any) {
    emit(
      'message',
      'Failed to clear artwork selection: ' +
        (e.response?.data?.error || e.message),
      true
    );
  } finally {
    clearing.value = false;
  }
};

const save = () => props.save();
</script>

<style scoped>
.public-art-thumb-frame {
  position: relative;
  width: 100%;
  aspect-ratio: 36 / 19;
  overflow: hidden;
}
.public-art-thumb-img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}
.public-art-thumb-error {
  width: 100%;
  height: 100%;
}
.public-art-crop-frame {
  position: relative;
  width: 100%;
  height: 300px;
  overflow: hidden;
  cursor: grab;
  border-radius: 4px;
  user-select: none;
  touch-action: none;
}
.public-art-crop-frame:active {
  cursor: grabbing;
}
.public-art-crop-img {
  width: 100%;
  height: 100%;
  object-fit: contain;
  display: block;
  pointer-events: none;
}
.public-art-crop-hint {
  position: absolute;
  left: 8px;
  bottom: 8px;
  padding: 2px 8px;
  font-size: 11px;
  color: #fff;
  background: rgba(0, 0, 0, 0.5);
  border-radius: 10px;
  pointer-events: none;
}
.public-art-candidate-card {
  display: flex;
  flex-direction: column;
}
</style>
