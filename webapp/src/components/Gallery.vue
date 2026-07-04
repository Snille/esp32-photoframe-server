<template>
  <div>
    <!-- Gallery Content -->
    <div>
      <!-- Header with Stats and Actions -->
      <div class="d-flex justify-space-between align-center mb-4">
        <div>
          <h2 class="text-h6 text-capitalize">
            {{ galleryStore.source.replace('_', ' ') }} Gallery
          </h2>
          <div class="text-caption text-grey">
            {{ galleryStore.totalPhotos }} photo{{
              galleryStore.totalPhotos !== 1 ? 's' : ''
            }}
            total
          </div>
        </div>
        <div class="d-flex gap-2 ga-2">
          <!-- Reorder controls (for 'custom' display order) -->
          <template v-if="reorderMode">
            <v-btn
              variant="text"
              height="40"
              :disabled="reorderSaving"
              @click="cancelReorder"
            >
              Cancel
            </v-btn>
            <v-btn
              color="primary"
              variant="flat"
              height="40"
              :loading="reorderSaving"
              prepend-icon="mdi-content-save"
              @click="saveReorder"
            >
              Save Order
            </v-btn>
          </template>
          <v-btn
            v-else-if="galleryStore.totalPhotos > 1"
            color="primary"
            variant="tonal"
            height="40"
            prepend-icon="mdi-sort"
            @click="enterReorder"
          >
            Reorder
          </v-btn>
          <v-btn
            v-if="!reorderMode && galleryStore.totalPhotos > 0"
            color="error"
            variant="flat"
            height="40"
            prepend-icon="mdi-delete"
            @click="requestDeleteAll"
          >
            Delete All
          </v-btn>
          <v-btn
            v-if="!reorderMode && galleryStore.source === 'gallery'"
            color="primary"
            variant="flat"
            height="40"
            :loading="galleryStore.loading"
            :disabled="galleryStore.loading"
            prepend-icon="mdi-upload"
            @click="triggerUpload"
          >
            Upload Photos
          </v-btn>
          <input
            ref="uploadInput"
            type="file"
            accept="image/*"
            multiple
            class="d-none"
            @change="onFilesSelected"
          />
          <v-btn
            v-if="!reorderMode && galleryStore.source === 'google_photos'"
            color="primary"
            variant="flat"
            height="40"
            :loading="galleryStore.loading"
            :disabled="galleryStore.loading"
            prepend-icon="mdi-google"
            @click="galleryStore.startPicker"
          >
            Add Photos via Google
          </v-btn>
        </div>
      </div>

      <!-- Immich per-album filter: tag-style multi-select. A photo can belong
           to several albums at once, so this is a union filter, not tabs. -->
      <div
        v-if="
          !reorderMode &&
          galleryStore.source === 'immich' &&
          usedAlbums.length > 0
        "
        class="mb-4"
      >
        <div class="text-caption text-medium-emphasis mb-1">
          Filter by album
        </div>
        <v-chip-group
          v-model="selectedAlbumIds"
          multiple
          filter
          @update:model-value="onAlbumFilterChange"
        >
          <v-chip
            v-for="album in usedAlbums"
            :key="album.id"
            :value="album.id"
            variant="tonal"
            size="small"
            >{{ album.name }} ({{ album.count }})</v-chip
          >
        </v-chip-group>
      </div>

      <!-- Notification -->
      <v-alert
        v-if="galleryStore.importMessage"
        :type="
          galleryStore.importMessage.includes('Error') ||
          galleryStore.importMessage.includes('Failed')
            ? 'error'
            : 'success'
        "
        variant="tonal"
        class="mb-4"
        density="compact"
        closable
        @click:close="galleryStore.importMessage = ''"
      >
        {{ galleryStore.importMessage }}
      </v-alert>

      <!-- Loading Spinner -->
      <div
        v-if="galleryStore.loading"
        class="d-flex justify-center align-center pa-10"
      >
        <v-progress-circular
          indeterminate
          color="primary"
        ></v-progress-circular>
      </div>

      <!-- Reorder Grid (custom display order) -->
      <div v-else-if="reorderMode">
        <v-alert
          type="info"
          variant="tonal"
          density="compact"
          class="mb-4"
        >
          Drag photos to set the order they appear on frames using
          <strong>Custom</strong> display order. The first photo shows first.
        </v-alert>
        <div v-if="reorderLoading" class="d-flex justify-center pa-10">
          <v-progress-circular indeterminate color="primary" />
        </div>
        <draggable
          v-else
          v-model="reorderItems"
          item-key="id"
          class="reorder-grid"
        >
          <template #item="{ element, index }">
            <v-card variant="outlined" class="position-relative reorder-card">
              <div class="order-badge">{{ index + 1 }}</div>
              <v-img
                :src="getThumbnailUrl(element.thumbnail_url)"
                aspect-ratio="1"
                contain
                class="bg-surface-variant rounded"
              />
            </v-card>
          </template>
        </draggable>
      </div>

      <!-- Photo Grid -->
      <v-row v-else-if="galleryStore.photos.length > 0">
        <v-col
          v-for="photo in galleryStore.photos"
          :key="photo.id"
          class="v-col-6 v-col-sm-4 v-col-md-3 v-col-lg-custom"
        >
          <v-card
            variant="outlined"
            class="position-relative photo-card"
            @click="openPushDialog(photo.id)"
          >
            <v-img
              :src="getThumbnailUrl(photo.thumbnail_url)"
              :lazy-src="getThumbnailUrl(photo.thumbnail_url)"
              aspect-ratio="1"
              contain
              class="bg-surface-variant rounded"
            >
              <template v-slot:placeholder>
                <div class="d-flex align-center justify-center fill-height">
                  <v-progress-circular
                    color="grey-lighten-4"
                    indeterminate
                  ></v-progress-circular>
                </div>
              </template>
            </v-img>
            <div class="delete-hotspot">
              <v-btn
                icon="mdi-delete"
                size="x-small"
                color="error"
                class="delete-overlay"
                @click.stop="requestDeletePhoto(photo)"
              />
            </div>
            <div
              v-if="photo.immich_albums && photo.immich_albums.length > 0"
              class="album-badge"
              :title="photo.immich_albums.join(', ')"
            >
              <v-icon size="10" class="mr-1"
                >mdi-folder-multiple-image</v-icon
              >{{ photo.immich_albums[0]
              }}<template v-if="photo.immich_albums.length > 1"
                >&nbsp;+{{ photo.immich_albums.length - 1 }}</template
              >
            </div>
          </v-card>
        </v-col>
      </v-row>

      <!-- Delete Image Dialog -->
      <v-dialog v-model="deleteDialog.show" max-width="400">
        <v-card>
          <v-card-title>
            <v-icon icon="mdi-delete" color="error" class="mr-2" />
            Delete Image?
          </v-card-title>
          <v-card-text>
            <div class="mb-3">Are you sure you want to delete this image?</div>
            <div v-if="deleteDialog.photo" class="d-flex justify-center">
              <img
                :src="getThumbnailUrl(deleteDialog.photo.thumbnail_url)"
                alt=""
                class="confirm-thumb"
              />
            </div>
          </v-card-text>
          <v-card-actions>
            <v-spacer />
            <v-btn variant="text" @click="deleteDialog.show = false">
              Cancel
            </v-btn>
            <v-btn
              color="error"
              :loading="deleteDialog.loading"
              @click="confirmDeletePhoto"
            >
              Delete
            </v-btn>
          </v-card-actions>
        </v-card>
      </v-dialog>

      <!-- Delete All Dialog -->
      <v-dialog v-model="deleteAllDialog.show" max-width="400">
        <v-card>
          <v-card-title>
            <v-icon icon="mdi-delete-sweep" color="error" class="mr-2" />
            Delete All Photos?
          </v-card-title>
          <v-card-text>
            Delete all
            <strong>{{ galleryStore.totalPhotos }}</strong> photo{{
              galleryStore.totalPhotos === 1 ? '' : 's'
            }}
            in the
            <strong>{{ galleryStore.source.replace('_', ' ') }}</strong>
            gallery? This cannot be undone.
          </v-card-text>
          <v-card-actions>
            <v-spacer />
            <v-btn variant="text" @click="deleteAllDialog.show = false">
              Cancel
            </v-btn>
            <v-btn
              color="error"
              :loading="deleteAllDialog.loading"
              @click="confirmDeleteAll"
            >
              Delete
            </v-btn>
          </v-card-actions>
        </v-card>
      </v-dialog>

      <!-- Push Dialog -->
      <v-dialog v-model="pushDialog.show" max-width="400">
        <v-card>
          <v-card-title>Push to Device</v-card-title>
          <v-card-text>
            <div v-if="loadingDevices" class="d-flex justify-center pa-4">
              <v-progress-circular indeterminate></v-progress-circular>
            </div>
            <div v-else-if="devices.length === 0">
              No devices found. Please add a device in Settings.
            </div>
            <div v-else>
              <v-radio-group v-model="pushDialog.selectedDevice" hide-details>
                <v-radio
                  v-for="dev in devices"
                  :key="dev.id"
                  :label="`${dev.name} (${dev.host})`"
                  :value="dev.id"
                ></v-radio>
              </v-radio-group>

              <v-checkbox
                v-model="pushDialog.remember"
                label="Remember my choice (this session)"
                density="compact"
                hide-details
                class="mt-2"
              ></v-checkbox>

              <v-alert
                v-if="pushDialog.error"
                type="error"
                variant="tonal"
                density="compact"
                class="mt-4"
                closable
                @click:close="pushDialog.error = ''"
              >
                {{ pushDialog.error }}
              </v-alert>
            </div>
          </v-card-text>
          <v-card-actions>
            <v-spacer></v-spacer>
            <v-btn variant="text" @click="pushDialog.show = false"
              >Cancel</v-btn
            >
            <v-btn
              color="primary"
              :disabled="!pushDialog.selectedDevice"
              :loading="pushDialog.loading"
              @click="confirmPush"
            >
              Push
            </v-btn>
          </v-card-actions>
        </v-card>
      </v-dialog>

      <!-- Pagination Controls -->
      <div
        v-if="!reorderMode && galleryStore.totalPhotos > galleryStore.limit"
        class="d-flex justify-center mt-6"
      >
        <v-pagination
          v-model="galleryStore.page"
          :length="galleryStore.totalPages"
          :total-visible="5"
          rounded="circle"
          @update:model-value="galleryStore.fetchPhotos"
        ></v-pagination>
      </div>

      <!-- Empty State -->
      <div
        v-if="
          !reorderMode &&
          !galleryStore.loading &&
          galleryStore.totalPhotos === 0
        "
        class="text-center py-10"
      >
        <v-icon
          icon="mdi-image-off-outline"
          size="64"
          color="grey-lighten-1"
          class="mb-4"
        ></v-icon>
        <h3 class="text-h6 text-grey-darken-1 mb-2">No photos</h3>
        <p class="text-body-2 text-grey mb-4">
          <span v-if="galleryStore.source === 'google_photos'">
            Get started by adding photos from Google Photos.
          </span>
          <span v-else-if="galleryStore.source === 'gallery'">
            Upload photos here, or send them to your Telegram bot.
          </span>
          <span v-else-if="galleryStore.source === 'synology_photos'">
            Open the <b>Synology</b> tab under Data Sources below and click
            <b>Sync Now</b> to import photos.
          </span>
          <span v-else-if="galleryStore.source === 'immich'">
            Open the <b>Immich</b> tab under Data Sources below and click
            <b>Sync Now</b> to import photos.
          </span>
        </p>
        <v-btn
          v-if="galleryStore.source === 'google_photos'"
          color="primary"
          prepend-icon="mdi-plus"
          @click="galleryStore.startPicker"
        >
          Add Photos
        </v-btn>
        <v-btn
          v-else-if="galleryStore.source === 'gallery'"
          color="primary"
          prepend-icon="mdi-upload"
          @click="triggerUpload"
        >
          Upload Photos
        </v-btn>
      </div>
    </div>
  </div>
</template>

<style scoped>
.photo-card {
  cursor: pointer;
  transition:
    transform 0.2s,
    box-shadow 0.2s;
}

.photo-card:hover {
  transform: translateY(-2px);
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
}

.delete-hotspot {
  position: absolute;
  top: 0;
  right: 0;
  width: 48px;
  height: 48px;
  z-index: 1;
}

.delete-overlay {
  position: absolute;
  top: 4px;
  right: 4px;
  opacity: 0;
  transition: opacity 0.2s;
}

.delete-hotspot:hover .delete-overlay {
  opacity: 1;
}

.album-badge {
  position: absolute;
  bottom: 4px;
  left: 4px;
  z-index: 1;
  max-width: calc(100% - 8px);
  padding: 1px 6px;
  border-radius: 10px;
  background: rgba(0, 0, 0, 0.6);
  color: #fff;
  font-size: 10px;
  line-height: 16px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  display: flex;
  align-items: center;
}

.confirm-thumb {
  max-width: 100%;
  max-height: 60vh;
  display: block;
}

@media (min-width: 1280px) {
  .v-col-lg-custom {
    flex: 0 0 16.6667%;
    max-width: 16.6667%;
  }
}

.reorder-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(110px, 1fr));
  gap: 10px;
}

.reorder-card {
  cursor: grab;
}

.reorder-card:active {
  cursor: grabbing;
}

.order-badge {
  position: absolute;
  top: 4px;
  left: 4px;
  z-index: 1;
  min-width: 22px;
  height: 22px;
  padding: 0 6px;
  border-radius: 11px;
  background: rgba(0, 0, 0, 0.6);
  color: #fff;
  font-size: 12px;
  line-height: 22px;
  text-align: center;
}
</style>

<script setup lang="ts">
import { onMounted, ref, reactive, watch } from 'vue';
import draggable from 'vuedraggable';
import { useAuthStore } from '../stores/auth';
import { useGalleryStore } from '../stores/gallery';
import {
  listDevices,
  listPhotos,
  listUsedImmichAlbums,
  pushToDevice,
  reorderGalleryPhotos,
  type Device,
} from '../api';

const authStore = useAuthStore();
const galleryStore = useGalleryStore();

// --- Immich per-album filter (tag-style, multi-select) ---
// Only albums that actually have synced photos, not every album in the whole
// Immich library — otherwise a large library would clutter this row with
// albums no frame ever selected.
const usedAlbums = ref<{ id: string; name: string; count: number }[]>([]);
const selectedAlbumIds = ref<string[]>([]);

const loadUsedAlbums = async () => {
  try {
    usedAlbums.value = await listUsedImmichAlbums();
  } catch (e) {
    console.error('Failed to load used Immich albums', e);
    usedAlbums.value = [];
  }
};

const onAlbumFilterChange = (ids: string[]) => {
  galleryStore.setImmichAlbumFilter(ids);
};

watch(
  () => galleryStore.source,
  (src) => {
    selectedAlbumIds.value = [];
    if (src === 'immich') {
      loadUsedAlbums();
    } else {
      usedAlbums.value = [];
    }
  }
);

// --- Custom-order drag-and-drop reordering ---
const reorderMode = ref(false);
const reorderLoading = ref(false);
const reorderSaving = ref(false);
const reorderItems = ref<any[]>([]);

const enterReorder = async () => {
  reorderMode.value = true;
  reorderLoading.value = true;
  try {
    // Pull the whole source in its saved custom order so dragging covers every
    // photo (the normal grid is paginated). High cap keeps moderate libraries
    // workable; huge libraries are an edge case for manual curation.
    const res = await listPhotos(galleryStore.source, 1000, 0, 'custom');
    reorderItems.value = res.photos || [];
  } catch (e) {
    galleryStore.importMessage = 'Failed to load photos for reordering';
    reorderMode.value = false;
  } finally {
    reorderLoading.value = false;
  }
};

const cancelReorder = () => {
  reorderMode.value = false;
  reorderItems.value = [];
};

const saveReorder = async () => {
  reorderSaving.value = true;
  try {
    await reorderGalleryPhotos(reorderItems.value.map((p) => p.id));
    galleryStore.importMessage = 'Photo order saved.';
    setTimeout(() => (galleryStore.importMessage = ''), 4000);
    reorderMode.value = false;
    reorderItems.value = [];
    await galleryStore.fetchPhotos();
  } catch (e) {
    galleryStore.importMessage = 'Failed to save photo order';
  } finally {
    reorderSaving.value = false;
  }
};

const uploadInput = ref<HTMLInputElement | null>(null);

const triggerUpload = () => {
  uploadInput.value?.click();
};

const onFilesSelected = async (e: Event) => {
  const target = e.target as HTMLInputElement;
  if (!target.files || target.files.length === 0) return;
  await galleryStore.uploadFiles(target.files);
  target.value = '';
};

const deleteDialog = reactive({
  show: false,
  photo: null as any,
  loading: false,
});

const requestDeletePhoto = (photo: any) => {
  deleteDialog.photo = photo;
  deleteDialog.show = true;
};

const confirmDeletePhoto = async () => {
  if (!deleteDialog.photo) return;
  deleteDialog.loading = true;
  try {
    await galleryStore.deletePhoto(deleteDialog.photo.id);
    deleteDialog.show = false;
    deleteDialog.photo = null;
  } finally {
    deleteDialog.loading = false;
  }
};

const deleteAllDialog = reactive({
  show: false,
  loading: false,
});

const requestDeleteAll = () => {
  deleteAllDialog.show = true;
};

const confirmDeleteAll = async () => {
  deleteAllDialog.loading = true;
  try {
    await galleryStore.deleteAllPhotos();
    deleteAllDialog.show = false;
  } finally {
    deleteAllDialog.loading = false;
  }
};

// Push Dialog State
const devices = ref<Device[]>([]);
const loadingDevices = ref(false);
const pushDialog = reactive({
  show: false,
  imageId: 0,
  selectedDevice: null as number | null,
  remember: false,
  loading: false,
  error: '',
});

// Session memory for device preference
const SESSION_KEY_PREFERRED_DEVICE = 'photoframe_preferred_device';

const openPushDialog = async (imageId: number) => {
  pushDialog.imageId = imageId;
  pushDialog.error = ''; // Clear previous error

  // Check session preference
  const savedId = sessionStorage.getItem(SESSION_KEY_PREFERRED_DEVICE);
  if (savedId) {
    const id = parseInt(savedId);
    if (!isNaN(id)) {
      // Auto-push could go here if implemented
    }
  }

  pushDialog.show = true;
  loadingDevices.value = true;

  try {
    const list = await listDevices();
    devices.value = list;

    // If we have a saved preference and it's in the list, pre-select it
    if (savedId) {
      const found = list.find((d: Device) => d.id === parseInt(savedId));
      if (found) {
        pushDialog.selectedDevice = found.id;
      }
    }

    // If no selection yet and only 1 device, pre-select it
    if (!pushDialog.selectedDevice && list.length === 1) {
      pushDialog.selectedDevice = list[0].id;
    }
  } catch (e) {
    console.error(e);
    pushDialog.error = 'Failed to load devices';
  } finally {
    loadingDevices.value = false;
  }
};

const confirmPush = async () => {
  if (!pushDialog.selectedDevice) return;

  pushDialog.error = ''; // Clear previous error

  if (pushDialog.remember) {
    sessionStorage.setItem(
      SESSION_KEY_PREFERRED_DEVICE,
      String(pushDialog.selectedDevice)
    );
  }

  pushDialog.loading = true;
  try {
    await pushToDevice(pushDialog.selectedDevice, pushDialog.imageId);
    galleryStore.importMessage = 'Image pushed to device successfully';
    pushDialog.show = false;
  } catch (e: any) {
    // Extract error message
    let msg = 'Failed to push image';
    if (e.response && e.response.data && e.response.data.error) {
      msg = e.response.data.error;
    } else if (e.message) {
      msg = e.message;
    }
    pushDialog.error = msg;
    // Keep dialog open to show error
  } finally {
    pushDialog.loading = false;
  }
};

const getThumbnailUrl = (url: string) => {
  const token = authStore.token;
  if (!token) return url;
  // If url already has params, append with &
  const separator = url.includes('?') ? '&' : '?';
  return `${url}${separator}token=${token}`;
};

onMounted(async () => {
  // store.fetchSettings() is called by parent (Settings.vue) or app init.
  // Calling it here triggers a loading state loop if this component is mounted inside Settings.vue
  galleryStore.fetchPhotos();
  if (galleryStore.source === 'immich') {
    loadUsedAlbums();
  }
});
</script>
