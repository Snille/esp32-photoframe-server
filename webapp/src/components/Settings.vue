```html
<template>
  <div class="pa-4">
    <!-- Gallery Card -->
    <v-card class="mb-6">
      <v-tabs v-model="galleryTab" color="primary">
        <v-tab value="gallery">Gallery</v-tab>
        <v-tab value="immich">Immich</v-tab>
        <v-tab value="google_photos">Google Photos</v-tab>
        <v-tab value="synology_photos">Synology</v-tab>
      </v-tabs>
      <v-card-text>
        <Gallery />
      </v-card-text>
    </v-card>

    <!-- Settings Card -->
    <v-card>
      <v-card-title class="d-flex align-center">
        <v-icon icon="mdi-cog" class="mr-2" />
        Settings
      </v-card-title>

      <div
        v-if="store.loading"
        class="d-flex justify-center align-center pa-10"
      >
        <v-progress-circular
          indeterminate
          color="primary"
        ></v-progress-circular>
      </div>

      <div v-else>
        <v-tabs v-model="activeMainTab" color="primary" grow>
          <v-tab value="devices">Devices</v-tab>
          <v-tab value="datasources">Data Sources</v-tab>
          <v-tab value="security">Security</v-tab>
        </v-tabs>

        <v-window v-model="activeMainTab">
          <!-- Data Sources Tab -->
          <v-window-item value="datasources">
            <v-tabs
              v-model="activeDataSourceTab"
              color="primary"
              density="compact"
              class="mb-4"
            >
              <v-tab value="gallery">Gallery</v-tab>
              <v-tab value="immich">Immich</v-tab>
              <v-tab value="google">Google</v-tab>
              <v-tab value="synology_photos">Synology</v-tab>
              <v-tab value="url">URL Proxy</v-tab>
              <v-tab value="ai_generation">AI Generation</v-tab>
            </v-tabs>

            <v-text-field
              v-model="form.device_image_base_url"
              label="Device-facing server URL (optional)"
              placeholder="Auto-detect — e.g. https://photos.example.com"
              hint="Used to build the Image Endpoint URLs below. Leave blank to auto-detect: http adds the add-on port, https/reverse-proxy URLs are used as-is. Set this (e.g. https://photos.example.com, no port) when running behind a reverse proxy."
              persistent-hint
              clearable
              variant="outlined"
              density="compact"
              prepend-inner-icon="mdi-server-network"
              class="mb-4"
              @blur="saveSettingsInternal()"
              @click:clear="saveSettingsInternal()"
            ></v-text-field>

            <v-window v-model="activeDataSourceTab">
              <!-- URL Proxy -->
              <v-window-item value="url">
                <v-card-text>
                  <v-alert
                    type="info"
                    variant="tonal"
                    class="mb-4"
                    density="compact"
                  >
                    Add external image URLs to be served by the photoframe. You
                    can bind URLs to specific devices or leave them global.
                  </v-alert>

                  <v-text-field
                    :model-value="getImageUrl('url_proxy')"
                    label="Image Endpoint URL (for firmware config)"
                    readonly
                    variant="outlined"
                    density="compact"
                    append-inner-icon="mdi-content-copy"
                    @click:append-inner="
                      copyToClipboard(getImageUrl('url_proxy'))
                    "
                    class="mb-4"
                  ></v-text-field>

                  <div class="d-flex justify-end mb-4">
                    <v-btn
                      color="primary"
                      prepend-icon="mdi-plus"
                      class="mb-4"
                      @click="openAddURLDialog"
                    >
                      Add URL Source
                    </v-btn>
                  </div>

                  <v-table density="comfortable" class="border rounded">
                    <thead>
                      <tr>
                        <th>URL</th>
                        <th>Bound Devices</th>
                        <th class="text-right">Action</th>
                      </tr>
                    </thead>
                    <tbody>
                      <tr v-for="src in urlSources" :key="src.id">
                        <td class="text-truncate" style="max-width: 300px">
                          <a :href="src.url" target="_blank">{{ src.url }}</a>
                        </td>
                        <td>
                          <div v-if="src.device_ids && src.device_ids.length">
                            <v-chip
                              v-for="did in src.device_ids"
                              :key="did"
                              size="x-small"
                              class="mr-1"
                            >
                              {{ getDeviceName(did) }}
                            </v-chip>
                          </div>
                          <span v-else class="text-grey text-caption"
                            >Global</span
                          >
                        </td>
                        <td class="text-right">
                          <v-btn
                            color="primary"
                            variant="text"
                            size="small"
                            icon="mdi-pencil"
                            class="mr-2"
                            @click="openEditURLDialog(src)"
                          ></v-btn>
                          <v-btn
                            color="error"
                            variant="text"
                            size="small"
                            icon="mdi-delete"
                            @click="deleteURLSourceWrapper(src.id)"
                          ></v-btn>
                        </td>
                      </tr>
                      <tr v-if="urlSources.length === 0">
                        <td colspan="4" class="text-center text-grey py-4">
                          No URL sources added.
                        </td>
                      </tr>
                    </tbody>
                  </v-table>
                </v-card-text>
              </v-window-item>

              <!-- Add/Edit URL Dialog -->
              <v-dialog v-model="showAddURLDialog" max-width="500px">
                <v-card>
                  <v-card-title>{{
                    isEditingURL ? 'Edit URL Source' : 'Add URL Source'
                  }}</v-card-title>
                  <v-card-text>
                    <v-form @submit.prevent="saveURLSource">
                      <v-text-field
                        v-model="newURL.url"
                        label="Image URL"
                        placeholder="https://example.com/image.jpg"
                        variant="outlined"
                        class="mb-2"
                        :rules="[(v) => !!v || 'URL is required']"
                      ></v-text-field>

                      <v-select
                        v-model="newURL.device_ids"
                        :items="availableDevices"
                        item-title="name"
                        item-value="id"
                        label="Bind to Devices (Optional)"
                        placeholder="Leave empty for Global"
                        variant="outlined"
                        multiple
                        chips
                        class="mb-4"
                        hint="If selected, only these devices will see this image."
                        persistent-hint
                      ></v-select>
                    </v-form>
                  </v-card-text>
                  <v-card-actions>
                    <v-spacer></v-spacer>
                    <v-btn
                      color="grey"
                      variant="text"
                      @click="showAddURLDialog = false"
                      >Cancel</v-btn
                    >
                    <v-btn color="primary" @click="saveURLSource">Save</v-btn>
                  </v-card-actions>
                </v-card>
              </v-dialog>

              <!-- Google (Photos + Calendar) -->
              <v-window-item value="google">
                <v-card-text>
                  <!-- Shared Google API Credentials -->
                  <h3 class="text-subtitle-1 font-weight-bold mb-3">
                    Google API Credentials
                  </h3>

                  <v-alert
                    type="info"
                    variant="tonal"
                    class="mb-4"
                    density="compact"
                  >
                    <div class="text-body-2">
                      These credentials are shared by Google Photos and Google
                      Calendar. Create a project in
                      <a
                        href="https://console.cloud.google.com/"
                        target="_blank"
                        >Google Cloud Console</a
                      >
                      and add the redirect URI:
                      <br />
                      <code
                        >http://[YOUR_SERVER_IP]:8080/api/auth/google/callback</code
                      >
                    </div>
                  </v-alert>

                  <v-text-field
                    v-model="form.google_client_id"
                    label="Client ID"
                    variant="outlined"
                    class="mb-2"
                  ></v-text-field>

                  <v-text-field
                    v-model="form.google_client_secret"
                    label="Client Secret"
                    type="password"
                    variant="outlined"
                    class="mb-4"
                  ></v-text-field>

                  <v-btn color="grey-darken-1" @click="save" class="mb-2"
                    >Save Credentials</v-btn
                  >

                  <!-- Photos Section -->
                  <v-divider class="my-6"></v-divider>
                  <h3 class="text-subtitle-1 font-weight-bold mb-3">Photos</h3>

                  <div v-if="form.google_connected === 'true'">
                    <v-alert
                      type="success"
                      variant="tonal"
                      class="mb-4"
                      density="compact"
                      icon="mdi-check-circle"
                    >
                      Connected to Google Photos
                    </v-alert>

                    <v-text-field
                      :model-value="getImageUrl('google_photos')"
                      label="Image Endpoint URL (for firmware config)"
                      readonly
                      variant="outlined"
                      density="compact"
                      append-inner-icon="mdi-content-copy"
                      @click:append-inner="
                        copyToClipboard(getImageUrl('google_photos'))
                      "
                    ></v-text-field>

                    <v-btn color="error" variant="text" @click="logoutGoogle">
                      Disconnect Google Photos
                    </v-btn>
                  </div>

                  <div v-else>
                    <v-btn
                      v-if="form.google_client_id && form.google_client_secret"
                      color="primary"
                      @click="connectGoogle"
                    >
                      Authorize Google Photos
                    </v-btn>
                    <v-alert
                      v-else
                      type="warning"
                      variant="tonal"
                      density="compact"
                    >
                      Enter Google API credentials above first.
                    </v-alert>
                  </div>

                  <!-- Calendar Section -->
                  <v-divider class="my-6"></v-divider>
                  <h3 class="text-subtitle-1 font-weight-bold mb-3">
                    Calendar
                  </h3>

                  <div v-if="form.google_calendar_connected === 'true'">
                    <v-alert
                      type="success"
                      variant="tonal"
                      class="mb-4"
                      density="compact"
                      icon="mdi-check-circle"
                    >
                      Google Calendar connected
                    </v-alert>

                    <v-btn
                      color="error"
                      variant="text"
                      @click="logoutGoogleCalendar"
                    >
                      Disconnect Google Calendar
                    </v-btn>
                  </div>

                  <div v-else>
                    <v-alert
                      type="info"
                      variant="tonal"
                      class="mb-4"
                      density="compact"
                    >
                      Connect a Google account for Calendar integration. This
                      can be a different account than Google Photos.
                    </v-alert>

                    <v-btn
                      v-if="form.google_client_id && form.google_client_secret"
                      color="primary"
                      @click="connectGoogleCalendar"
                    >
                      Authorize Google Calendar
                    </v-btn>
                    <v-alert
                      v-else
                      type="warning"
                      variant="tonal"
                      density="compact"
                    >
                      Enter Google API credentials above first.
                    </v-alert>
                  </div>
                </v-card-text>
              </v-window-item>

              <!-- Synology -->
              <v-window-item value="synology_photos">
                <v-card-text>
                  <div v-if="form.synology_sid">
                    <v-alert
                      type="success"
                      variant="tonal"
                      class="mb-4"
                      density="compact"
                      icon="mdi-check-circle"
                    >
                      Connected to Synology Photos ({{
                        form.synology_account
                      }}
                      @ {{ form.synology_url }})
                      <div
                        v-if="synologyStore.count !== null"
                        class="text-caption mt-1"
                      >
                        {{ synologyStore.count }} photo{{
                          synologyStore.count !== 1 ? 's' : ''
                        }}
                        synced
                      </div>
                    </v-alert>

                    <v-text-field
                      :model-value="getImageUrl('synology_photos')"
                      label="Image Endpoint URL (for firmware config)"
                      readonly
                      variant="outlined"
                      density="compact"
                      append-inner-icon="mdi-content-copy"
                      @click:append-inner="
                        copyToClipboard(getImageUrl('synology_photos'))
                      "
                    ></v-text-field>

                    <v-row class="mt-2">
                      <v-col cols="12" sm="8">
                        <v-select
                          v-model="form.synology_album_id"
                          :items="synologyAlbumOptions"
                          item-title="name"
                          item-value="id"
                          label="Sync Album"
                          variant="outlined"
                          density="compact"
                          hint="Select an album to sync photos from"
                          persistent-hint
                          :rules="[(v: any) => !!v || 'Album is required']"
                          @update:model-value="saveSettingsInternal()"
                        ></v-select>
                      </v-col>
                      <v-col cols="12" sm="4">
                        <v-btn
                          block
                          variant="outlined"
                          :loading="synologyStore.loading"
                          @click="loadAlbums"
                          >Refresh Albums</v-btn
                        >
                      </v-col>
                    </v-row>

                    <v-row class="mt-1">
                      <v-col cols="12" md="6">
                        <v-checkbox
                          v-model="form.synology_auto_sync_enabled"
                          label="Auto Sync Album"
                          color="primary"
                          density="compact"
                          hide-details
                          @update:model-value="saveSettingsInternal()"
                        ></v-checkbox>
                      </v-col>
                      <v-col cols="12" md="6">
                        <v-select
                          v-model="form.synology_auto_sync_interval_minutes"
                          :items="autoSyncIntervalOptions"
                          item-title="title"
                          item-value="value"
                          label="Auto Sync Interval"
                          variant="outlined"
                          density="compact"
                          :disabled="!form.synology_auto_sync_enabled"
                          hint="How often to refresh photos from the selected album"
                          persistent-hint
                          @update:model-value="saveSettingsInternal()"
                        ></v-select>
                      </v-col>
                    </v-row>

                    <div class="d-flex flex-wrap ga-2 mt-4">
                      <v-btn
                        color="primary"
                        :loading="synologyStore.loading"
                        @click="syncSynology"
                        >Sync Now</v-btn
                      >
                      <v-btn color="warning" @click="clearSynology"
                        >Clear All Photos</v-btn
                      >
                      <v-btn
                        color="error"
                        variant="text"
                        @click="logoutSynology"
                        >Log Out</v-btn
                      >
                    </div>
                  </div>

                  <div v-else>
                    <v-text-field
                      v-model="form.synology_url"
                      label="NAS URL"
                      placeholder="https://192.168.1.10:5001"
                      variant="outlined"
                      class="mb-2"
                    ></v-text-field>

                    <v-text-field
                      v-model="form.synology_account"
                      label="Account"
                      variant="outlined"
                      class="mb-2"
                    ></v-text-field>

                    <v-text-field
                      v-model="form.synology_password"
                      label="Password"
                      type="password"
                      variant="outlined"
                      class="mb-2"
                    ></v-text-field>

                    <v-checkbox
                      v-model="form.synology_skip_cert"
                      label="Skip Certificate Verification (Insecure)"
                      color="primary"
                      density="compact"
                    ></v-checkbox>

                    <v-text-field
                      v-model="form.synology_otp_code"
                      label="OTP Code (If 2FA enabled)"
                      placeholder="6-digit code"
                      variant="outlined"
                      class="mb-4"
                    ></v-text-field>

                    <v-btn
                      color="primary"
                      :disabled="
                        !form.synology_url ||
                        !form.synology_account ||
                        !form.synology_password
                      "
                      :loading="synologyStore.loading"
                      @click="testSynology"
                    >
                      Connect
                    </v-btn>
                  </div>
                </v-card-text>
              </v-window-item>

              <!-- Immich -->
              <v-window-item value="immich">
                <v-card-text>
                  <div v-if="immichConnected">
                    <v-alert
                      type="success"
                      variant="tonal"
                      class="mb-4"
                      density="compact"
                      icon="mdi-check-circle"
                    >
                      Connected to Immich ({{ form.immich_url }})
                      <div
                        v-if="immichStore.count !== null"
                        class="text-caption mt-1"
                      >
                        {{ immichStore.count }} photo{{
                          immichStore.count !== 1 ? 's' : ''
                        }}
                        synced
                      </div>
                    </v-alert>

                    <v-text-field
                      :model-value="getImageUrl('immich')"
                      label="Image Endpoint URL (for firmware config)"
                      readonly
                      variant="outlined"
                      density="compact"
                      append-inner-icon="mdi-content-copy"
                      @click:append-inner="
                        copyToClipboard(getImageUrl('immich'))
                      "
                    ></v-text-field>

                    <v-row class="mt-2">
                      <v-col cols="12">
                        <v-select
                          v-model="form.immich_source_mode"
                          :items="immichSourceModeOptions"
                          item-title="title"
                          item-value="value"
                          label="Sync Mode"
                          variant="outlined"
                          density="compact"
                          hide-details
                          @update:model-value="saveSettingsInternal()"
                        ></v-select>
                        <v-alert
                          :type="
                            form.immich_source_mode === 'all'
                              ? 'warning'
                              : 'info'
                          "
                          variant="tonal"
                          density="compact"
                          class="mt-2 text-body-2"
                        >
                          {{
                            immichSourceModeHelp[form.immich_source_mode] ||
                            immichSourceModeHelp['album']
                          }}
                        </v-alert>
                      </v-col>
                    </v-row>

                    <v-row
                      v-if="form.immich_source_mode === 'album'"
                      class="mt-2"
                    >
                      <v-col cols="12" sm="8">
                        <v-select
                          v-model="form.immich_album_id"
                          :items="immichAlbumOptions"
                          item-title="name"
                          item-value="id"
                          label="Sync Album"
                          variant="outlined"
                          density="compact"
                          hint="Select an album to sync photos from"
                          persistent-hint
                          :rules="[(v: any) => !!v || 'Album is required']"
                          @update:model-value="saveSettingsInternal()"
                        ></v-select>
                      </v-col>
                      <v-col cols="12" sm="4">
                        <v-btn
                          block
                          variant="outlined"
                          :loading="immichStore.loading"
                          @click="loadImmichAlbums"
                          >Refresh Albums</v-btn
                        >
                      </v-col>
                    </v-row>

                    <v-row class="mt-1">
                      <v-col cols="12" md="6">
                        <v-checkbox
                          v-model="form.immich_auto_sync_enabled"
                          label="Auto Sync Album"
                          color="primary"
                          density="compact"
                          hide-details
                          @update:model-value="saveSettingsInternal()"
                        ></v-checkbox>
                      </v-col>
                      <v-col cols="12" md="6">
                        <v-select
                          v-model="form.immich_auto_sync_interval_minutes"
                          :items="autoSyncIntervalOptions"
                          item-title="title"
                          item-value="value"
                          label="Auto Sync Interval"
                          variant="outlined"
                          density="compact"
                          :disabled="!form.immich_auto_sync_enabled"
                          hint="How often to refresh photos from the selected album"
                          persistent-hint
                          @update:model-value="saveSettingsInternal()"
                        ></v-select>
                      </v-col>
                    </v-row>

                    <div class="d-flex flex-wrap ga-2 mt-4">
                      <v-btn
                        color="primary"
                        :loading="immichStore.loading"
                        @click="syncImmich"
                        >Sync Now</v-btn
                      >
                      <v-btn color="warning" @click="clearImmich"
                        >Clear All Photos</v-btn
                      >
                      <v-btn
                        color="error"
                        variant="text"
                        @click="disconnectImmich"
                        >Disconnect</v-btn
                      >
                    </div>
                  </div>

                  <div v-else>
                    <v-text-field
                      v-model="form.immich_url"
                      label="Immich Server URL"
                      placeholder="http://192.168.1.10:2283"
                      variant="outlined"
                      class="mb-2"
                    ></v-text-field>

                    <v-text-field
                      v-model="form.immich_api_key"
                      label="API Key"
                      type="password"
                      variant="outlined"
                      class="mb-4"
                    ></v-text-field>

                    <v-btn
                      color="primary"
                      :disabled="!form.immich_url || !form.immich_api_key"
                      :loading="immichStore.loading"
                      @click="testImmich"
                    >
                      Connect
                    </v-btn>
                  </div>
                </v-card-text>
              </v-window-item>

              <!-- Gallery -->
              <v-window-item value="gallery">
                <v-card-text>
                  <v-alert
                    type="info"
                    variant="tonal"
                    class="mb-4"
                    density="compact"
                  >
                    Photos in the gallery live on this server. Add them from the
                    Gallery tab above, or send them to the Telegram bot
                    configured below.
                  </v-alert>

                  <v-text-field
                    :model-value="getImageUrl('gallery')"
                    label="Image Endpoint URL (for firmware config)"
                    readonly
                    variant="outlined"
                    density="compact"
                    append-inner-icon="mdi-content-copy"
                    @click:append-inner="
                      copyToClipboard(getImageUrl('gallery'))
                    "
                  ></v-text-field>

                  <v-divider class="my-4"></v-divider>

                  <h3 class="text-subtitle-1 font-weight-bold mb-1">
                    Telegram Bot (Upload)
                  </h3>
                  <div class="text-caption text-grey mb-3">
                    Optional. Configure a Telegram bot to upload photos into the
                    gallery from your phone.
                  </div>

                  <div v-if="form.telegram_bot_token">
                    <v-alert
                      type="success"
                      variant="tonal"
                      class="mb-4"
                      density="compact"
                      icon="mdi-check-circle"
                    >
                      Telegram Bot Configured
                    </v-alert>

                    <v-text-field
                      v-model="form.telegram_bot_token"
                      label="Telegram Bot Token"
                      variant="outlined"
                    ></v-text-field>

                    <v-divider class="my-4"></v-divider>

                    <h3 class="text-subtitle-1 font-weight-bold mb-2">
                      Push to Device
                    </h3>
                    <div class="text-caption text-grey mb-2">
                      When enabled, photos uploaded via the Telegram bot are
                      also pushed to the selected devices immediately. They are
                      added to the gallery either way.
                    </div>

                    <v-checkbox
                      v-model="form.telegram_push_enabled"
                      label="Enable Push to Device"
                      color="primary"
                      hide-details
                      density="compact"
                    ></v-checkbox>

                    <v-expand-transition>
                      <div v-if="form.telegram_push_enabled" class="mt-2">
                        <v-select
                          v-model="form.telegram_target_device_id"
                          :items="availableDevices"
                          item-title="name"
                          item-value="id"
                          label="Target Devices"
                          variant="outlined"
                          density="compact"
                          hint="Select the devices to display photos on"
                          persistent-hint
                          multiple
                          chips
                          closable-chips
                        ></v-select>
                      </div>
                    </v-expand-transition>

                    <v-btn color="primary" class="mt-4" @click="save"
                      >Update Settings</v-btn
                    >
                  </div>

                  <div v-else>
                    <v-text-field
                      v-model="form.telegram_bot_token"
                      label="Telegram Bot Token"
                      placeholder="Enter Bot Token"
                      variant="outlined"
                      hint="Send photos to your bot to add them to the gallery."
                      persistent-hint
                    ></v-text-field>

                    <v-btn color="primary" class="mt-4" @click="save"
                      >Save Token</v-btn
                    >
                  </div>
                </v-card-text>
              </v-window-item>

              <!-- AI Generation -->
              <v-window-item value="ai_generation">
                <v-card-text>
                  <v-alert
                    type="info"
                    variant="tonal"
                    class="mb-4"
                    density="compact"
                  >
                    Generate images using AI (OpenAI or Google Gemini).
                    Configure API keys below, then set the prompt/model
                    per-device in the Edit Device dialog.
                  </v-alert>

                  <v-text-field
                    :model-value="getImageUrl('ai_generation')"
                    label="Image Endpoint URL (for firmware config)"
                    readonly
                    variant="outlined"
                    density="compact"
                    append-inner-icon="mdi-content-copy"
                    @click:append-inner="
                      copyToClipboard(getImageUrl('ai_generation'))
                    "
                    class="mb-4"
                  ></v-text-field>

                  <v-text-field
                    v-model="form.openai_api_key"
                    label="OpenAI API Key"
                    type="password"
                    variant="outlined"
                    class="mb-1"
                    hint="sk-..."
                    persistent-hint
                  ></v-text-field>
                  <div class="text-caption text-grey ml-2 mb-4">
                    Get your API key at
                    <a
                      href="https://platform.openai.com/api-keys"
                      target="_blank"
                      class="text-primary text-decoration-none"
                      >platform.openai.com</a
                    >
                  </div>

                  <v-text-field
                    v-model="form.google_api_key"
                    label="Google Gemini API Key"
                    type="password"
                    variant="outlined"
                    class="mb-1"
                    persistent-hint
                  ></v-text-field>
                  <div class="text-caption text-grey ml-2 mb-4">
                    Get your API key at
                    <a
                      href="https://aistudio.google.com/app/apikey"
                      target="_blank"
                      class="text-primary text-decoration-none"
                      >aistudio.google.com</a
                    >
                  </div>

                  <v-text-field
                    v-model="form.comfyui_host"
                    label="ComfyUI Server (local)"
                    variant="outlined"
                    class="mb-1"
                    placeholder="http://host:8188"
                    hint="Used by the ComfyUI provider. Workflow is read from comfyui_workflow.json in the server data directory."
                    persistent-hint
                  ></v-text-field>
                  <div class="text-caption text-grey ml-2 mb-4">
                    Runs a local ComfyUI workflow (e.g. Z-Image). Select
                    “ComfyUI (local)” as the AI provider per-device.
                  </div>

                  <v-expansion-panels variant="accordion" class="mb-4">
                    <v-expansion-panel>
                      <v-expansion-panel-title>
                        <v-icon size="small" class="mr-2"
                          >mdi-code-json</v-icon
                        >
                        ComfyUI Workflow
                        <v-chip
                          v-if="form.comfyui_workflow && !comfyuiWorkflowValid"
                          color="error"
                          size="x-small"
                          class="ml-2"
                          >invalid JSON</v-chip
                        >
                      </v-expansion-panel-title>
                      <v-expansion-panel-text>
                        <v-alert
                          type="info"
                          variant="tonal"
                          density="compact"
                          class="mb-3"
                        >
                          The prompt is <strong>not</strong> set here — it comes
                          from each device’s <strong>Prompt</strong> field (Edit
                          Device → AI Generation). Image size and seed are also
                          set automatically per generation, so you don’t need to
                          edit them in the JSON. This workflow only defines the
                          model, steps, sampler, etc.
                        </v-alert>
                        <v-textarea
                          v-model="form.comfyui_workflow"
                          label="Workflow (API-format JSON)"
                          variant="outlined"
                          rows="6"
                          auto-grow
                          spellcheck="false"
                          class="mb-1 comfyui-workflow"
                          :error="
                            !!form.comfyui_workflow && !comfyuiWorkflowValid
                          "
                          :hint="
                            !form.comfyui_workflow
                              ? 'Empty → falls back to comfyui_workflow.json on the server.'
                              : comfyuiWorkflowValid
                                ? 'Valid JSON — click Save to store it.'
                                : 'Not valid JSON yet.'
                          "
                          persistent-hint
                        ></v-textarea>
                        <v-file-input
                          label="…or upload a workflow .json"
                          accept="application/json,.json"
                          variant="outlined"
                          density="compact"
                          prepend-icon="mdi-upload"
                          hide-details
                          class="mb-1"
                          @update:model-value="onWorkflowFile"
                        ></v-file-input>
                        <div class="text-caption text-grey ml-2">
                          In ComfyUI enable Dev Mode, then “Save (API Format)”
                          and paste/upload that file here. Stored on the server
                          and used for every generation.
                        </div>
                      </v-expansion-panel-text>
                    </v-expansion-panel>
                  </v-expansion-panels>

                  <v-btn color="primary" @click="save">Save AI Settings</v-btn>
                </v-card-text>
              </v-window-item>
            </v-window>
          </v-window-item>

          <!-- Security Tab -->
          <v-window-item value="security">
            <v-card-text>
              <div class="d-flex justify-space-between align-center mb-4">
                <h3 class="text-h6">Admin Account</h3>
                <v-btn
                  variant="tonal"
                  size="small"
                  @click="showAccountForm = !showAccountForm"
                >
                  {{ showAccountForm ? 'Cancel' : 'Edit Account' }}
                </v-btn>
              </div>

              <v-expand-transition>
                <v-card v-if="showAccountForm" variant="outlined" class="mb-6">
                  <v-card-text>
                    <v-alert
                      type="info"
                      variant="tonal"
                      class="mb-4"
                      density="compact"
                    >
                      Leave new password fields blank if you only want to change
                      the username. Current password is required for any change.
                    </v-alert>
                    <v-text-field
                      v-model="accountForm.newUsername"
                      label="New Username (Optional)"
                      placeholder="Leave empty to keep current"
                      variant="outlined"
                      density="compact"
                      class="mb-2"
                    ></v-text-field>

                    <v-divider class="my-4"></v-divider>

                    <v-text-field
                      v-model="accountForm.newPassword"
                      label="New Password"
                      type="password"
                      variant="outlined"
                      density="compact"
                      class="mb-2"
                    ></v-text-field>
                    <v-text-field
                      v-model="accountForm.confirmPassword"
                      label="Confirm New Password"
                      type="password"
                      variant="outlined"
                      density="compact"
                      class="mb-4"
                    ></v-text-field>

                    <v-divider class="my-4"></v-divider>

                    <v-text-field
                      v-model="accountForm.oldPassword"
                      label="Current Password (Required)"
                      type="password"
                      variant="outlined"
                      density="compact"
                      class="mb-4"
                    ></v-text-field>
                    <v-btn color="primary" @click="updateAccountSettings"
                      >Update Account</v-btn
                    >
                  </v-card-text>
                </v-card>
              </v-expand-transition>

              <v-divider class="mb-6"></v-divider>

              <h3 class="text-h6 mb-4">Active Sessions</h3>
              <v-list density="compact" class="bg-surface rounded border mb-6">
                <v-list-item
                  v-for="session in sessions"
                  :key="session.id"
                  :title="getDeviceFromUA(session.user_agent)"
                  :subtitle="`${session.ip} - Expires: ${new Date(session.expires_at).toLocaleDateString()}`"
                >
                  <template v-slot:append>
                    <div class="d-flex align-center">
                      <v-btn
                        icon="mdi-delete"
                        variant="text"
                        color="error"
                        size="small"
                        @click="revokeSessionHandler(session.id)"
                      ></v-btn>
                    </div>
                  </template>
                </v-list-item>
                <v-list-item v-if="sessions.length === 0">
                  <v-list-item-title class="text-grey text-center"
                    >No active sessions found</v-list-item-title
                  >
                </v-list-item>
              </v-list>

              <v-divider class="mb-6"></v-divider>

              <h3 class="text-h6 mb-4">Device Access Tokens</h3>

              <v-alert
                v-if="generatedToken"
                type="success"
                variant="tonal"
                class="mb-4"
                closable
                @click:close="generatedToken = ''"
              >
                <div class="font-weight-bold mb-1">Token Generated!</div>
                <div class="text-caption mb-2">
                  Copy this token securely. It will not be shown again.
                </div>
                <v-text-field
                  :model-value="generatedToken"
                  readonly
                  variant="outlined"
                  density="compact"
                  hide-details
                  bg-color="white"
                  append-inner-icon="mdi-content-copy"
                  @click:append-inner="copyToken"
                ></v-text-field>
              </v-alert>

              <v-card variant="outlined" class="mb-6">
                <v-card-title class="text-subtitle-1"
                  >Generate New Token</v-card-title
                >
                <v-card-text>
                  <div class="d-flex ga-2 align-center">
                    <v-text-field
                      v-model="newTokenName"
                      label="Token Name (e.g. Living Room Frame)"
                      variant="outlined"
                      density="compact"
                      hide-details
                      class="flex-grow-1"
                    ></v-text-field>
                    <v-select
                      v-model="newTokenDeviceId"
                      :items="[
                        { title: 'None', value: null },
                        ...availableDevices.map((d: any) => ({
                          title: d.name,
                          value: d.id,
                        })),
                      ]"
                      label="Bind to Device"
                      variant="outlined"
                      density="compact"
                      hide-details
                      style="max-width: 220px"
                    ></v-select>
                    <v-btn color="primary" @click="generateToken"
                      >Generate</v-btn
                    >
                  </div>
                </v-card-text>
              </v-card>

              <h4 class="text-subtitle-2 mb-2">Active Tokens</h4>
              <v-table density="comfortable" class="border rounded">
                <thead>
                  <tr>
                    <th>Name</th>
                    <th>Bound Device</th>
                    <th>Created At</th>
                    <th class="text-right">Action</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="token in authStore.tokens" :key="token.id">
                    <td>{{ token.name }}</td>
                    <td>
                      <v-select
                        :model-value="token.device_id"
                        :items="[
                          { title: 'None', value: null },
                          ...availableDevices.map((d: any) => ({
                            title: d.name,
                            value: d.id,
                          })),
                        ]"
                        variant="plain"
                        density="compact"
                        hide-details
                        style="max-width: 180px; font-size: inherit"
                        @update:model-value="
                          (val: any) => updateTokenDevice(token.id, val)
                        "
                      ></v-select>
                    </td>
                    <td>{{ new Date(token.created_at).toLocaleString() }}</td>
                    <td class="text-right">
                      <v-btn
                        color="error"
                        variant="text"
                        size="small"
                        @click="revokeToken(token.id)"
                      >
                        Revoke
                      </v-btn>
                    </td>
                  </tr>
                  <tr v-if="authStore.tokens.length === 0">
                    <td colspan="4" class="text-center text-grey py-4">
                      No active tokens found. Create one above to connect a
                      device.
                    </td>
                  </tr>
                </tbody>
              </v-table>
            </v-card-text>
          </v-window-item>
          <!-- Devices Tab -->
          <v-window-item value="devices">
            <v-card-text>
              <v-alert
                type="info"
                variant="tonal"
                class="mb-4"
                density="compact"
              >
                Manage your ESP32 PhotoFrame devices here. These devices will be
                available for direct push from the Gallery.
              </v-alert>

              <div class="d-flex justify-end mb-4">
                <v-btn
                  color="primary"
                  prepend-icon="mdi-plus"
                  @click="openAddDeviceDialog"
                  :loading="deviceListLoading"
                >
                  Add Device
                </v-btn>
              </div>

              <div
                v-if="deviceListLoading && availableDevices.length === 0"
                class="d-flex justify-center align-center pa-10"
              >
                <v-progress-circular
                  indeterminate
                  color="primary"
                ></v-progress-circular>
              </div>

              <v-table v-else density="comfortable" class="border rounded">
                <thead>
                  <tr>
                    <th>Now</th>
                    <th>Name</th>
                    <th>Model</th>
                    <th>Host</th>
                    <th>Battery</th>
                    <th class="text-right">Action</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="device in availableDevices" :key="device.id">
                    <td>
                      <v-img
                        v-if="device.current_thumb_id"
                        :src="getServedThumbUrl(device.current_thumb_id)"
                        width="80"
                        height="50"
                        cover
                        class="rounded my-1 bg-surface-variant"
                        style="cursor: zoom-in"
                        title="Click to view the full image on the frame"
                        @click="openFullImage(device)"
                      >
                        <template #error>
                          <div
                            class="d-flex align-center justify-center fill-height"
                          >
                            <v-icon color="grey" size="small"
                              >mdi-image-off-outline</v-icon
                            >
                          </div>
                        </template>
                      </v-img>
                      <div
                        v-else
                        class="d-flex align-center justify-center rounded my-1 bg-surface-variant"
                        style="width: 80px; height: 50px"
                        title="No image served yet"
                      >
                        <v-icon color="grey" size="small"
                          >mdi-image-outline</v-icon
                        >
                      </div>
                    </td>
                    <td>{{ device.name }}</td>
                    <td>
                      {{
                        device.board_name || `${device.width}x${device.height}`
                      }}
                    </td>
                    <td>
                      {{ device.host }}
                    </td>
                    <td>
                      <div
                        v-if="(device.battery_percent ?? -1) >= 0"
                        class="d-flex align-center"
                        :title="batteryTitle(device)"
                      >
                        <v-icon
                          size="small"
                          :color="batteryColor(device.battery_percent!)"
                          >{{ batteryIcon(device.battery_percent!) }}</v-icon
                        >
                        <span
                          class="ml-1"
                          :class="
                            device.battery_percent! <= 15
                              ? 'text-error'
                              : 'text-medium-emphasis'
                          "
                          >{{ device.battery_percent }}%</span
                        >
                      </div>
                      <span v-else class="text-grey">—</span>
                    </td>
                    <td class="text-right">
                      <v-btn
                        color="primary"
                        variant="text"
                        size="small"
                        icon="mdi-pencil"
                        title="Edit Device"
                        @click="editDevice(device)"
                      ></v-btn>
                      <v-btn
                        color="error"
                        variant="text"
                        size="small"
                        icon="mdi-delete"
                        title="Delete Device"
                        @click="removeDevice(device.id)"
                      ></v-btn>
                    </td>
                  </tr>
                  <tr v-if="availableDevices.length === 0">
                    <td colspan="6" class="text-center text-grey py-4">
                      No devices added.
                    </td>
                  </tr>
                </tbody>
              </v-table>

              <!-- Full current-image lightbox (click a Devices-list miniature) -->
              <v-dialog v-model="fullImageDialog" width="auto" max-width="96vw">
                <v-card>
                  <v-toolbar density="compact" color="surface">
                    <v-toolbar-title class="text-body-1">{{
                      fullImageTitle
                    }}</v-toolbar-title>
                    <v-spacer></v-spacer>
                    <v-btn
                      icon="mdi-close"
                      variant="text"
                      @click="fullImageDialog = false"
                    ></v-btn>
                  </v-toolbar>
                  <v-img
                    :src="fullImageUrl"
                    max-height="85vh"
                    max-width="96vw"
                    contain
                    class="bg-black"
                  >
                    <template #placeholder>
                      <div
                        class="d-flex align-center justify-center fill-height"
                      >
                        <v-progress-circular
                          indeterminate
                          color="primary"
                        ></v-progress-circular>
                      </div>
                    </template>
                  </v-img>
                </v-card>
              </v-dialog>

              <!-- Edit Device Dialog (tabbed like device webapp) -->
              <v-dialog
                v-model="showEditDeviceDialog"
                width="92vw"
                max-width="1400px"
                scrollable
              >
                <v-card>
                  <v-card-title>{{
                    isAddingDevice
                      ? 'Add Device'
                      : editingDevice.name || 'Edit Device'
                  }}</v-card-title>
                  <v-tabs
                    v-if="!isAddingDevice"
                    v-model="deviceDialogTab"
                    density="compact"
                  >
                    <v-tab value="general">General</v-tab>
                    <v-tab value="autoRotate">Auto Rotate</v-tab>
                    <v-tab value="overlay">Overlay</v-tab>
                    <v-tab value="power">Power</v-tab>
                    <v-tab value="homeAssistant">Home Assistant</v-tab>
                    <v-tab value="processing">Processing</v-tab>
                    <v-tab value="ai">AI Generation</v-tab>
                    <v-tab value="palette">Palette</v-tab>
                  </v-tabs>
                  <v-card-text
                    :style="
                      isAddingDevice
                        ? ''
                        : 'min-height: 300px; max-height: 78vh; overflow-y: auto'
                    "
                  >
                    <!-- Add Device: just host input -->
                    <div v-if="isAddingDevice" class="mt-2">
                      <v-text-field
                        v-model="editingDevice.host"
                        label="Device Host / IP"
                        variant="outlined"
                        hint="e.g., photoframe.local or 192.168.1.100"
                        persistent-hint
                        autofocus
                      ></v-text-field>
                    </div>

                    <!-- Edit Device: full tabbed UI -->
                    <v-tabs-window
                      v-if="!isAddingDevice"
                      v-model="deviceDialogTab"
                    >
                      <!-- General Tab -->
                      <v-tabs-window-item value="general">
                        <v-row class="mt-1">
                          <v-col cols="12" md="6">
                            <v-text-field
                              v-model="editingDevice.name"
                              label="Device Name"
                              variant="outlined"
                              density="compact"
                              hide-details
                            ></v-text-field>
                          </v-col>
                        </v-row>
                        <v-row>
                          <v-col cols="12" md="6">
                            <v-text-field
                              v-model="editingDevice.host"
                              label="Host / IP"
                              variant="outlined"
                              density="compact"
                              hide-details
                            ></v-text-field>
                          </v-col>
                        </v-row>
                        <v-row>
                          <v-col cols="12" md="6">
                            <v-select
                              v-model="deviceConfig.display_rotation_deg"
                              :items="[
                                { title: '0° — native', value: 0 },
                                { title: '90°', value: 90 },
                                { title: '180°', value: 180 },
                                { title: '270°', value: 270 },
                              ]"
                              label="Display Rotation"
                              variant="outlined"
                              density="compact"
                              hint="How the frame is mounted relative to the panel's native orientation. Drives the rendered image and all previews."
                              persistent-hint
                            ></v-select>
                          </v-col>
                          <v-col cols="12" md="6">
                            <v-text-field
                              :model-value="
                                derivedOrientation === 'landscape'
                                  ? 'Landscape'
                                  : 'Portrait'
                              "
                              label="Orientation (derived)"
                              variant="outlined"
                              density="compact"
                              readonly
                              hint="Determined by native panel size + Display Rotation."
                              persistent-hint
                            ></v-text-field>
                          </v-col>
                        </v-row>
                        <v-row>
                          <v-col cols="12" md="6">
                            <v-text-field
                              v-model.number="deviceConfig.timezone_offset"
                              label="Timezone (UTC offset)"
                              type="number"
                              :min="-12"
                              :max="14"
                              :step="0.5"
                              variant="outlined"
                              density="compact"
                              hint="e.g., -8 for PST, +1 for CET, +8 for CST"
                              persistent-hint
                            ></v-text-field>
                          </v-col>
                        </v-row>
                        <v-row>
                          <v-col cols="12" md="6">
                            <v-text-field
                              v-model="deviceConfig.ntp_server"
                              label="NTP Server"
                              variant="outlined"
                              density="compact"
                              hint="e.g., pool.ntp.org"
                              persistent-hint
                            ></v-text-field>
                          </v-col>
                        </v-row>
                      </v-tabs-window-item>

                      <!-- Auto Rotate Tab -->
                      <v-tabs-window-item value="autoRotate">
                        <v-switch
                          v-model="deviceConfig.auto_rotate"
                          label="Enable Auto-Rotate"
                          color="primary"
                          hide-details
                          class="mt-2 mb-2"
                        />
                        <div class="ml-10">
                          <v-select
                            v-model="deviceConfig.rotate_interval"
                            :items="rotateIntervalOptions"
                            label="Rotation Interval"
                            variant="outlined"
                            density="compact"
                            hide-details
                            class="mb-2"
                            :disabled="!deviceConfig.auto_rotate"
                          />
                          <v-checkbox
                            v-model="deviceConfig.auto_rotate_aligned"
                            label="Align rotation to clock boundaries"
                            hide-details
                            class="mb-2"
                            :disabled="!deviceConfig.auto_rotate"
                          />
                          <v-select
                            v-model="deviceConfig.rotation_mode"
                            :items="[
                              { title: 'Local Storage', value: 'storage' },
                              { title: 'URL', value: 'url' },
                            ]"
                            label="Rotation Mode"
                            variant="outlined"
                            density="compact"
                            class="mt-4 mb-2"
                            :disabled="!deviceConfig.auto_rotate"
                          />

                          <!-- URL source config (shown when rotation mode is URL) -->
                          <div v-if="deviceConfig.rotation_mode === 'url'">
                            <v-checkbox
                              v-model="useThisServer"
                              label="Use this server as image source"
                              color="primary"
                              hide-details
                              class="mb-2"
                              :disabled="!deviceConfig.auto_rotate"
                            />

                            <!-- This server: source dropdown -->
                            <v-select
                              v-if="useThisServer"
                              v-model="selectedSource"
                              :items="sourceOptions"
                              label="Image Source"
                              variant="outlined"
                              density="compact"
                              hide-details
                              class="mb-2 ml-8"
                              :disabled="!deviceConfig.auto_rotate"
                            ></v-select>

                            <!-- This server + Immich: per-frame album filter -->
                            <v-select
                              v-if="useThisServer && selectedSource === 'immich'"
                              v-model="immichAlbumIdsArray"
                              :items="deviceImmichAlbumOptions"
                              label="Immich albums (this frame)"
                              placeholder="All Immich photos"
                              persistent-placeholder
                              multiple
                              chips
                              closable-chips
                              variant="outlined"
                              density="compact"
                              class="mb-2 ml-8"
                              :loading="immichStore.loading"
                              :disabled="!deviceConfig.auto_rotate"
                              hint="Leave empty to show all synced Immich photos. Selecting albums limits this frame to those albums (same Immich connection)."
                              persistent-hint
                            ></v-select>

                            <!-- This server: display order -->
                            <v-select
                              v-if="useThisServer"
                              v-model="editingDevice.display_order"
                              :items="displayOrderOptions"
                              label="Display order"
                              variant="outlined"
                              density="compact"
                              hide-details
                              class="mb-1 ml-8"
                              :disabled="!deviceConfig.auto_rotate"
                            ></v-select>
                            <div
                              v-if="
                                useThisServer &&
                                editingDevice.display_order === 'custom'
                              "
                              class="text-caption text-medium-emphasis mb-2 ml-8"
                            >
                              Set the order in the Gallery tab (Reorder).
                            </div>

                            <!-- Custom URL -->
                            <v-text-field
                              v-if="!useThisServer"
                              v-model="deviceConfig.image_url"
                              label="Image URL"
                              variant="outlined"
                              density="compact"
                              hide-details
                              class="mb-2 ml-8"
                              :disabled="!deviceConfig.auto_rotate"
                            />

                            <v-alert
                              v-if="deviceHttpsBlocked"
                              type="warning"
                              variant="tonal"
                              density="compact"
                              class="mb-2 ml-8"
                            >
                              This board has no PSRAM, so it can't fetch over
                              <strong>HTTPS</strong> (the TLS handshake runs out of
                              memory). The image URL resolves to an https:// address —
                              point it at an <strong>http://</strong> endpoint on the
                              local network instead (e.g. set a plain-http "Device-facing
                              server URL" in Settings → General, or use a custom http
                              URL).
                            </v-alert>

                            <v-checkbox
                              v-model="deviceConfig.save_downloaded_images"
                              label="Save downloaded images to Downloads album"
                              color="primary"
                              hide-details
                              class="mb-2"
                              :disabled="!deviceConfig.auto_rotate"
                            />
                          </div>
                        </div>

                        <v-divider class="my-3" />

                        <!-- Sleep Schedule (device config) -->
                        <v-switch
                          v-model="deviceConfig.sleep_schedule_enabled"
                          label="Enable Sleep Schedule"
                          color="primary"
                          hide-details
                          class="mb-2"
                        />
                        <div
                          v-if="deviceConfig.sleep_schedule_enabled"
                          class="ml-10"
                        >
                          <v-row dense>
                            <v-col cols="6">
                              <v-text-field
                                v-model="deviceConfig.sleep_start_time"
                                label="From"
                                type="time"
                                variant="outlined"
                                density="compact"
                                hide-details
                              />
                            </v-col>
                            <v-col cols="6">
                              <v-text-field
                                v-model="deviceConfig.sleep_end_time"
                                label="To"
                                type="time"
                                variant="outlined"
                                density="compact"
                                hide-details
                              />
                            </v-col>
                          </v-row>
                        </div>

                        <v-divider class="my-4" />

                        <!-- Display Settings section header -->
                        <div class="text-body-1 font-weight-medium mb-4">
                          Display Settings
                        </div>
                        <div class="ml-10">
                          <v-row dense>
                            <v-col cols="12" md="6">
                              <v-select
                                v-model="editingDevice.display_mode"
                                :items="[
                                  {
                                    title: 'Cover (fill, may crop)',
                                    value: 'cover',
                                  },
                                  {
                                    title: 'Fit (show entire photo)',
                                    value: 'fit',
                                  },
                                ]"
                                label="Photo Display Mode"
                                variant="outlined"
                                density="compact"
                                hide-details
                              ></v-select>
                            </v-col>
                          </v-row>
                          <v-checkbox
                            v-model="editingDevice.enable_collage"
                            label="Enable Collage Mode"
                            color="primary"
                            hide-details
                            class="mt-2 mb-1"
                          ></v-checkbox>
                        </div>
                      </v-tabs-window-item>

                      <!-- Overlay Tab -->
                      <v-tabs-window-item value="overlay">
                        <!-- Overlay section -->
                        <div class="text-body-1 font-weight-medium mb-4 mt-1">
                          Overlay
                        </div>
                        <div class="ml-10">
                          <div class="d-flex ga-4 flex-wrap">
                            <v-checkbox
                              v-model="editingDevice.show_date"
                              label="Show Date"
                              color="primary"
                              hide-details
                            ></v-checkbox>
                            <v-checkbox
                              v-model="editingDevice.show_photo_date"
                              label="Show Photo Date"
                              color="primary"
                              hide-details
                            ></v-checkbox>
                            <v-checkbox
                              v-model="editingDevice.show_weather"
                              label="Show Weather"
                              color="primary"
                              hide-details
                            ></v-checkbox>
                            <v-checkbox
                              v-model="editingDevice.show_battery"
                              label="Show Battery"
                              color="primary"
                              hide-details
                            ></v-checkbox>
                            <v-checkbox
                              v-model="editingDevice.show_names"
                              label="Show Names"
                              color="primary"
                              hide-details
                            ></v-checkbox>
                            <v-checkbox
                              v-model="editingDevice.show_location"
                              label="Show Location"
                              color="primary"
                              hide-details
                            ></v-checkbox>
                            <v-checkbox
                              v-model="editingDevice.show_description"
                              label="Show Description"
                              color="primary"
                              hide-details
                            ></v-checkbox>
                          </div>

                          <!-- People name options (Immich face metadata) -->
                          <template v-if="editingDevice.show_names">
                            <div class="text-caption text-disabled mt-3 mb-2">
                              Names come from face metadata (Immich). Photos
                              without recognized people show nothing.
                            </div>
                            <v-row dense>
                              <v-col cols="12" sm="6">
                                <v-select
                                  v-model="editingDevice.name_format"
                                  :items="nameFormatOptions"
                                  item-title="label"
                                  item-value="value"
                                  label="Name format"
                                  variant="outlined"
                                  density="compact"
                                  hide-details
                                ></v-select>
                              </v-col>
                              <v-col cols="12" sm="6" class="d-flex align-center">
                                <v-checkbox
                                  v-model="editingDevice.names_show_age"
                                  label="Show age (in parentheses)"
                                  color="primary"
                                  hide-details
                                ></v-checkbox>
                              </v-col>
                            </v-row>
                            <v-slider
                              v-model="editingDevice.names_max_len"
                              :min="8"
                              :max="120"
                              :step="1"
                              label="Names max length"
                              color="primary"
                              hide-details
                              class="mt-4 mr-2"
                            >
                              <template #append>
                                <span
                                  class="text-caption"
                                  style="min-width: 56px"
                                >
                                  {{ editingDevice.names_max_len || 30 }} chars
                                </span>
                              </template>
                            </v-slider>
                          </template>

                          <!-- Location length limit -->
                          <v-slider
                            v-if="editingDevice.show_location"
                            v-model="editingDevice.location_max_len"
                            :min="8"
                            :max="120"
                            :step="1"
                            label="Location max length"
                            color="primary"
                            hide-details
                            class="mt-4 mr-2"
                          >
                            <template #append>
                              <span class="text-caption" style="min-width: 56px">
                                {{ editingDevice.location_max_len || 40 }} chars
                              </span>
                            </template>
                          </v-slider>

                          <!-- Description length limit -->
                          <v-slider
                            v-if="editingDevice.show_description"
                            v-model="editingDevice.description_max_len"
                            :min="8"
                            :max="240"
                            :step="1"
                            label="Description max length"
                            color="primary"
                            hide-details
                            class="mt-4 mr-2"
                          >
                            <template #append>
                              <span class="text-caption" style="min-width: 56px">
                                {{ editingDevice.description_max_len || 80 }}
                                chars
                              </span>
                            </template>
                          </v-slider>
                          <v-select
                            v-if="editingDevice.show_date"
                            v-model="editingDevice.date_format"
                            :items="dateFormatOptions"
                            item-title="label"
                            item-value="value"
                            label="Date Format"
                            variant="outlined"
                            density="compact"
                            hide-details
                            class="mt-3"
                          ></v-select>
                          <v-row
                            v-if="editingDevice.show_weather"
                            dense
                            class="mt-3"
                          >
                            <v-col cols="6">
                              <v-text-field
                                v-model.number="editingDevice.weather_lat"
                                label="Latitude"
                                variant="outlined"
                                density="compact"
                                hide-details
                                type="number"
                              ></v-text-field>
                            </v-col>
                            <v-col cols="6">
                              <v-text-field
                                v-model.number="editingDevice.weather_lon"
                                label="Longitude"
                                variant="outlined"
                                density="compact"
                                hide-details
                                type="number"
                              ></v-text-field>
                            </v-col>
                          </v-row>

                          <!-- Per-element placement -->
                          <template
                            v-if="
                              editingDevice.show_date ||
                              editingDevice.show_photo_date ||
                              editingDevice.show_weather ||
                              editingDevice.show_battery ||
                              editingDevice.show_names ||
                              editingDevice.show_location ||
                              editingDevice.show_description
                            "
                          >
                            <div
                              class="text-caption text-medium-emphasis mt-4 mb-1"
                            >
                              Typeface
                            </div>
                            <div class="text-caption text-disabled mb-2">
                              Applies to every overlay field. The five families
                              were chosen for clarity on e-paper.
                            </div>
                            <v-row dense>
                              <v-col cols="12" sm="6">
                                <v-select
                                  v-model="editingDevice.overlay_font"
                                  :items="fontOptions"
                                  item-title="label"
                                  item-value="value"
                                  label="Font"
                                  variant="outlined"
                                  density="compact"
                                  hide-details
                                ></v-select>
                              </v-col>
                              <v-col cols="12" sm="6">
                                <v-select
                                  v-model="editingDevice.overlay_weight"
                                  :items="fontWeightOptions"
                                  item-title="label"
                                  item-value="value"
                                  label="Weight"
                                  variant="outlined"
                                  density="compact"
                                  hide-details
                                ></v-select>
                              </v-col>
                            </v-row>

                            <div
                              class="text-caption text-medium-emphasis mt-4 mb-1"
                            >
                              Placement
                            </div>
                            <div class="text-caption text-disabled mb-2">
                              Date / Photo Date / Weather positions apply on the
                              Full Photo (overlay) layout. Battery shows on the
                              photo in all layouts.
                            </div>
                            <v-row dense>
                              <v-col
                                v-if="editingDevice.show_date"
                                cols="12"
                                sm="6"
                              >
                                <v-select
                                  v-model="editingDevice.date_position"
                                  :items="positionOptions"
                                  item-title="label"
                                  item-value="value"
                                  label="Date position"
                                  variant="outlined"
                                  density="compact"
                                  hide-details
                                ></v-select>
                              </v-col>
                              <v-col
                                v-if="editingDevice.show_photo_date"
                                cols="12"
                                sm="6"
                              >
                                <v-select
                                  v-model="editingDevice.photo_date_position"
                                  :items="positionOptions"
                                  item-title="label"
                                  item-value="value"
                                  label="Photo Date position"
                                  variant="outlined"
                                  density="compact"
                                  hide-details
                                ></v-select>
                                <v-checkbox
                                  :model-value="!isIconHidden('photo_date')"
                                  @update:model-value="
                                    (v: any) => setIconShown('photo_date', !!v)
                                  "
                                  label="Show icon"
                                  density="compact"
                                  hide-details
                                ></v-checkbox>
                              </v-col>
                              <v-col
                                v-if="editingDevice.show_weather"
                                cols="12"
                                sm="6"
                              >
                                <v-select
                                  v-model="editingDevice.weather_position"
                                  :items="positionOptions"
                                  item-title="label"
                                  item-value="value"
                                  label="Weather position"
                                  variant="outlined"
                                  density="compact"
                                  hide-details
                                ></v-select>
                                <v-checkbox
                                  :model-value="!isIconHidden('weather')"
                                  @update:model-value="
                                    (v: any) => setIconShown('weather', !!v)
                                  "
                                  label="Show icon"
                                  density="compact"
                                  hide-details
                                ></v-checkbox>
                              </v-col>
                              <v-col
                                v-if="editingDevice.show_names"
                                cols="12"
                                sm="6"
                              >
                                <v-select
                                  v-model="editingDevice.names_position"
                                  :items="positionOptions"
                                  item-title="label"
                                  item-value="value"
                                  label="Names position"
                                  variant="outlined"
                                  density="compact"
                                  hide-details
                                ></v-select>
                                <v-checkbox
                                  :model-value="!isIconHidden('names')"
                                  @update:model-value="
                                    (v: any) => setIconShown('names', !!v)
                                  "
                                  label="Show icon"
                                  density="compact"
                                  hide-details
                                ></v-checkbox>
                              </v-col>
                              <v-col
                                v-if="editingDevice.show_location"
                                cols="12"
                                sm="6"
                              >
                                <v-select
                                  v-model="editingDevice.location_position"
                                  :items="positionOptions"
                                  item-title="label"
                                  item-value="value"
                                  label="Location position"
                                  variant="outlined"
                                  density="compact"
                                  hide-details
                                ></v-select>
                                <v-checkbox
                                  :model-value="!isIconHidden('location')"
                                  @update:model-value="
                                    (v: any) => setIconShown('location', !!v)
                                  "
                                  label="Show icon"
                                  density="compact"
                                  hide-details
                                ></v-checkbox>
                              </v-col>
                              <v-col
                                v-if="editingDevice.show_description"
                                cols="12"
                                sm="6"
                              >
                                <v-select
                                  v-model="editingDevice.description_position"
                                  :items="positionOptions"
                                  item-title="label"
                                  item-value="value"
                                  label="Description position"
                                  variant="outlined"
                                  density="compact"
                                  hide-details
                                ></v-select>
                                <v-checkbox
                                  :model-value="!isIconHidden('description')"
                                  @update:model-value="
                                    (v: any) => setIconShown('description', !!v)
                                  "
                                  label="Show icon"
                                  density="compact"
                                  hide-details
                                ></v-checkbox>
                              </v-col>
                              <v-col
                                v-if="editingDevice.show_battery"
                                cols="12"
                                sm="6"
                              >
                                <v-select
                                  v-model="editingDevice.battery_position"
                                  :items="positionOptions"
                                  item-title="label"
                                  item-value="value"
                                  label="Battery position"
                                  variant="outlined"
                                  density="compact"
                                  hide-details
                                ></v-select>
                              </v-col>
                              <v-col
                                v-if="editingDevice.show_battery"
                                cols="12"
                                sm="6"
                              >
                                <v-select
                                  v-model="editingDevice.battery_style"
                                  :items="batteryStyleOptions"
                                  item-title="label"
                                  item-value="value"
                                  label="Battery display"
                                  variant="outlined"
                                  density="compact"
                                  hide-details
                                ></v-select>
                              </v-col>
                              <v-col
                                v-if="editingDevice.show_battery"
                                cols="12"
                                sm="6"
                              >
                                <v-select
                                  v-model.number="editingDevice.battery_rotation"
                                  :items="batteryRotationOptions"
                                  item-title="label"
                                  item-value="value"
                                  label="Battery icon rotation"
                                  variant="outlined"
                                  density="compact"
                                  hide-details
                                ></v-select>
                              </v-col>
                              <v-col
                                v-if="
                                  editingDevice.show_battery &&
                                  editingDevice.battery_style === 'both'
                                "
                                cols="12"
                                sm="6"
                              >
                                <v-select
                                  v-model="editingDevice.battery_text_side"
                                  :items="batteryTextSideOptions"
                                  item-title="label"
                                  item-value="value"
                                  label="Battery text side"
                                  variant="outlined"
                                  density="compact"
                                  hide-details
                                ></v-select>
                              </v-col>
                            </v-row>

                            <v-slider
                              v-if="
                                editingDevice.show_battery &&
                                editingDevice.battery_style !== 'text'
                              "
                              v-model="editingDevice.battery_icon_scale"
                              :min="0.5"
                              :max="2"
                              :step="0.1"
                              label="Battery icon size"
                              color="primary"
                              hide-details
                              class="mt-4 mr-2"
                            >
                              <template #append>
                                <span
                                  class="text-caption"
                                  style="min-width: 42px"
                                >
                                  {{
                                    Math.round(
                                      (editingDevice.battery_icon_scale || 1) *
                                        100
                                    )
                                  }}%
                                </span>
                              </template>
                            </v-slider>

                            <v-slider
                              v-model="editingDevice.overlay_scale"
                              :min="0.5"
                              :max="2"
                              :step="0.1"
                              label="Text size"
                              color="primary"
                              hide-details
                              class="mt-4 mr-2"
                            >
                              <template #append>
                                <span
                                  class="text-caption"
                                  style="min-width: 42px"
                                >
                                  {{
                                    Math.round(
                                      (editingDevice.overlay_scale || 1) * 100
                                    )
                                  }}%
                                </span>
                              </template>
                            </v-slider>

                            <div
                              class="text-caption text-medium-emphasis mt-4 mb-1"
                            >
                              Live preview
                            </div>
                            <div class="overlay-preview" :style="previewBoxStyle">
                              <div
                                v-for="region in previewRegions"
                                :key="region.name"
                                class="op-region"
                                :class="'op-region-' + region.name"
                              >
                                <template
                                  v-for="part in region.parts"
                                  :key="part.key"
                                >
                                  <div
                                    v-if="part.type === 'corners'"
                                    class="op-corner-row"
                                  >
                                    <div
                                      v-for="cell in part.cells"
                                      :key="cell.pos"
                                      class="op-slot"
                                      :class="'op-' + cell.pos"
                                    >
                                      <div
                                        v-for="el in cell.items"
                                        :key="el.key"
                                        class="op-chip"
                                        :class="{ low: el.low }"
                                        :style="previewChipStyle(el)"
                                      >
                                        <span
                                          v-if="el.battery"
                                          class="op-bat"
                                          :style="previewBatStyle(el)"
                                        >
                                          <span
                                            class="op-bat-fill"
                                            :style="{ width: el.pct + '%' }"
                                          ></span>
                                        </span>
                                        <span v-if="el.emoji">{{
                                          el.emoji
                                        }}</span>
                                        <span v-if="el.text">{{ el.text }}</span>
                                      </div>
                                    </div>
                                  </div>
                                  <div v-else class="op-slot op-wide">
                                    <div
                                      v-for="el in part.items"
                                      :key="el.key"
                                      class="op-chip"
                                      :class="{ low: el.low }"
                                      :style="previewChipStyle(el)"
                                    >
                                      <span
                                        v-if="el.battery"
                                        class="op-bat"
                                        :style="previewBatStyle(el)"
                                      >
                                        <span
                                          class="op-bat-fill"
                                          :style="{ width: el.pct + '%' }"
                                        ></span>
                                      </span>
                                      <span v-if="el.emoji">{{ el.emoji }}</span>
                                      <span v-if="el.text">{{ el.text }}</span>
                                    </div>
                                  </div>
                                </template>
                              </div>
                            </div>
                          </template>
                          <v-tooltip
                            :disabled="
                              form.google_calendar_connected === 'true'
                            "
                            location="top"
                            text="Connect Google Calendar in Data Sources first"
                          >
                            <template #activator="{ props }">
                              <div v-bind="props" class="d-inline-flex">
                                <v-checkbox
                                  v-model="editingDevice.show_calendar"
                                  label="Show Google Calendar Events"
                                  color="primary"
                                  hide-details
                                  class="mt-2 mb-1"
                                  :disabled="
                                    form.google_calendar_connected !== 'true'
                                  "
                                ></v-checkbox>
                              </div>
                            </template>
                          </v-tooltip>
                          <v-select
                            v-if="
                              editingDevice.show_calendar &&
                              form.google_calendar_connected === 'true'
                            "
                            v-model="editingDevice.calendar_id"
                            :items="calendars"
                            item-title="summary"
                            item-value="id"
                            label="Select Calendar"
                            variant="outlined"
                            density="compact"
                            class="mt-2"
                            :loading="!calendarLoaded"
                          ></v-select>
                        </div>

                        <v-divider class="my-4" />

                        <!-- Layout section -->
                        <div class="text-body-1 font-weight-medium mb-4">
                          Layout
                        </div>
                        <div class="ml-10">
                          <div class="d-flex flex-wrap ga-3 mb-3">
                            <v-card
                              v-for="opt in filteredLayoutOptions"
                              :key="opt.value"
                              :variant="
                                editingDevice.layout === opt.value
                                  ? 'outlined'
                                  : 'flat'
                              "
                              :color="
                                editingDevice.layout === opt.value
                                  ? 'primary'
                                  : undefined
                              "
                              class="layout-preview-card pa-2 text-center"
                              style="width: 100px; cursor: pointer"
                              @click="editingDevice.layout = opt.value"
                            >
                              <div
                                class="layout-preview mb-1"
                                v-html="
                                  getLayoutPreviewSvg(
                                    opt.value,
                                    deviceConfig.display_orientation ||
                                      editingDevice.orientation ||
                                      'landscape'
                                  )
                                "
                              ></div>
                              <div
                                class="text-caption"
                                style="line-height: 1.2"
                              >
                                {{ opt.title }}
                              </div>
                            </v-card>
                          </div>
                        </div>
                      </v-tabs-window-item>

                      <!-- Power Tab -->
                      <v-tabs-window-item value="power">
                        <!-- Battery drain estimate (derived from the level the
                             frame reports on each image fetch — no external
                             measurement hardware). -->
                        <div class="d-flex align-center mb-1 mt-2">
                          <div class="text-subtitle-2">Battery</div>
                          <v-spacer />
                          <v-btn
                            variant="text"
                            size="x-small"
                            icon="mdi-refresh"
                            title="Refresh estimate"
                            :loading="batteryLoading"
                            @click="loadBatteryEstimate(editingDevice.id)"
                          ></v-btn>
                        </div>
                        <div
                          v-if="!batteryEstimate || !batteryEstimate.has_data"
                          class="text-caption text-medium-emphasis mb-2"
                        >
                          No battery readings yet. The frame reports its level on
                          each image fetch; an estimate appears once a few
                          samples have accumulated.
                        </div>
                        <template v-else>
                          <div
                            class="d-flex align-center mb-2"
                            style="gap: 14px"
                          >
                            <div class="text-h5">
                              {{ batteryEstimate.current_percent }}%
                            </div>
                            <v-chip
                              size="small"
                              :color="batteryTrendColor"
                              variant="tonal"
                              >{{ batteryTrendLabel }}</v-chip
                            >
                            <div class="text-caption text-medium-emphasis">
                              <span v-if="batteryEstimate.current_voltage_mv > 0"
                                >{{
                                  (
                                    batteryEstimate.current_voltage_mv / 1000
                                  ).toFixed(2)
                                }}
                                V ·
                              </span>
                              {{ batteryEstimate.sample_count }} samples
                            </div>
                          </div>
                          <div
                            v-if="batteryEstimate.trend === 'discharging'"
                            class="text-body-2 mb-2"
                          >
                            ~{{ batteryEstimate.drain_per_day.toFixed(1) }} %/day
                            · est.
                            <strong>{{ batteryDaysLabel }}</strong> remaining
                            <span class="text-medium-emphasis">
                              ({{
                                batteryEstimate.basis === 'voltage'
                                  ? 'from voltage'
                                  : 'from %'
                              }})</span
                            >
                          </div>
                          <div
                            v-else-if="batteryEstimate.trend === 'charging'"
                            class="text-body-2 mb-2"
                          >
                            Charging / on USB — level is rising.
                          </div>
                          <div
                            v-else-if="batteryEstimate.trend === 'stable'"
                            class="text-body-2 mb-2"
                          >
                            Level steady — not enough drain yet to estimate
                            runtime.
                          </div>
                          <div v-else class="text-body-2 mb-2">
                            Collecting data — a longer span is needed before a
                            trend can be read.
                          </div>
                          <svg
                            v-if="batterySparkline"
                            viewBox="0 0 100 28"
                            preserveAspectRatio="none"
                            class="battery-spark text-primary"
                          >
                            <polyline
                              :points="batterySparkline"
                              fill="none"
                              stroke="currentColor"
                              stroke-width="1.5"
                              vector-effect="non-scaling-stroke"
                            />
                          </svg>
                        </template>

                        <v-divider class="my-4" />

                        <v-switch
                          v-model="deviceConfig.deep_sleep_enabled"
                          label="Enable Deep Sleep"
                          color="primary"
                          class="mt-2"
                          hide-details
                        />
                        <v-alert
                          type="info"
                          variant="tonal"
                          density="compact"
                          class="mt-4"
                        >
                          <strong>Power Consumption Notice</strong><br />
                          When deep sleep is enabled, the device sleeps between
                          image rotations to save power. WiFi is only active
                          during image fetch.
                        </v-alert>

                        <v-divider class="my-4" />

                        <div class="text-subtitle-2 mb-1">Button</div>
                        <div class="text-caption text-medium-emphasis mb-3">
                          What the wake button does while the frame is awake. A
                          press always wakes the frame from deep sleep first.
                        </div>
                        <v-select
                          v-model="deviceConfig.button_action_short"
                          :items="buttonActionOptions"
                          label="Short press (&lt;2s)"
                          variant="outlined"
                          density="compact"
                          hide-details
                          class="mb-2"
                        />
                        <v-select
                          v-model="deviceConfig.button_action_long"
                          :items="buttonActionOptions"
                          label="Long press (2–5s)"
                          variant="outlined"
                          density="compact"
                          hide-details
                          class="mb-2"
                        />
                        <v-select
                          v-model="deviceConfig.button_action_hold"
                          :items="buttonActionOptions"
                          label="Hold (≥5s)"
                          variant="outlined"
                          density="compact"
                          hide-details
                        />
                      </v-tabs-window-item>

                      <!-- Home Assistant Tab -->
                      <v-tabs-window-item value="homeAssistant">
                        <v-text-field
                          v-model="deviceConfig.ha_url"
                          label="Home Assistant URL"
                          variant="outlined"
                          density="compact"
                          class="mt-2"
                          hint="e.g., http://homeassistant.local:8123"
                          persistent-hint
                          placeholder="http://homeassistant.local:8123"
                        />
                      </v-tabs-window-item>

                      <!-- Processing Tab (matches device webapp ProcessingControls) -->
                      <v-tabs-window-item value="processing">
                        <v-row class="mt-1">
                          <v-col cols="12">
                            <v-card variant="outlined" class="mb-2">
                              <v-card-subtitle class="pt-3"
                                >Processing Preset</v-card-subtitle
                              >
                              <v-card-text>
                                <v-btn-toggle
                                  v-model="processingPreset"
                                  mandatory
                                  color="primary"
                                  variant="outlined"
                                  @update:model-value="applyProcessingPreset"
                                >
                                  <v-btn
                                    v-for="p in processingPresetOptions"
                                    :key="p.value"
                                    :value="p.value"
                                  >
                                    {{ p.title }}
                                  </v-btn>
                                </v-btn-toggle>
                              </v-card-text>
                            </v-card>
                          </v-col>
                        </v-row>
                        <v-row>
                          <v-col cols="12" md="4">
                            <v-select
                              v-model="deviceProcessing.ditherAlgorithm"
                              :items="ditherOptions"
                              item-title="title"
                              item-value="value"
                              label="Dithering Algorithm"
                              variant="outlined"
                              density="compact"
                            />
                          </v-col>
                          <v-col cols="12" md="4">
                            <v-select
                              v-model="deviceProcessing.colorMethod"
                              :items="[
                                { title: 'RGB', value: 'rgb' },
                                { title: 'LAB', value: 'lab' },
                              ]"
                              label="Color Matching"
                              variant="outlined"
                              density="compact"
                            />
                          </v-col>
                        </v-row>

                        <v-row>
                          <v-col cols="12" md="4">
                            <v-slider
                              v-model="deviceProcessing.exposure"
                              :min="0.5"
                              :max="2.0"
                              :step="0.01"
                              label="Exposure"
                              thumb-label
                              color="primary"
                            >
                              <template #append>
                                <span class="text-body-2">{{
                                  deviceProcessing.exposure.toFixed(2)
                                }}</span>
                              </template>
                            </v-slider>
                          </v-col>
                          <v-col cols="12" md="4">
                            <v-slider
                              v-model="deviceProcessing.saturation"
                              :min="0.5"
                              :max="2.0"
                              :step="0.01"
                              label="Saturation"
                              thumb-label
                              color="primary"
                            >
                              <template #append>
                                <span class="text-body-2">{{
                                  deviceProcessing.saturation.toFixed(2)
                                }}</span>
                              </template>
                            </v-slider>
                          </v-col>
                          <v-col cols="12" md="4">
                            <v-checkbox
                              v-model="deviceProcessing.compressDynamicRange"
                              label="Compress Dynamic Range"
                              hint="Map brightness to display's actual white point"
                              persistent-hint
                              color="primary"
                            />
                          </v-col>
                        </v-row>

                        <v-row>
                          <v-col cols="12" md="4">
                            <v-select
                              v-model="deviceProcessing.toneMode"
                              :items="[
                                { title: 'Contrast', value: 'contrast' },
                                { title: 'S-Curve', value: 'scurve' },
                              ]"
                              label="Tone Mapping"
                              variant="outlined"
                              density="compact"
                            />
                          </v-col>
                          <v-col
                            v-if="deviceProcessing.toneMode !== 'scurve'"
                            cols="12"
                            md="4"
                          >
                            <v-slider
                              v-model="deviceProcessing.contrast"
                              :min="0.5"
                              :max="2.0"
                              :step="0.01"
                              label="Contrast"
                              thumb-label
                              color="primary"
                            >
                              <template #append>
                                <span class="text-body-2">{{
                                  deviceProcessing.contrast.toFixed(2)
                                }}</span>
                              </template>
                            </v-slider>
                          </v-col>
                        </v-row>

                        <v-expand-transition>
                          <v-card
                            v-if="deviceProcessing.toneMode === 'scurve'"
                            variant="tonal"
                            class="mt-2"
                          >
                            <v-card-subtitle class="pt-3"
                              >S-Curve Parameters</v-card-subtitle
                            >
                            <v-card-text>
                              <v-row>
                                <v-col cols="12" md="6">
                                  <v-slider
                                    v-model="deviceProcessing.strength"
                                    :min="0"
                                    :max="1"
                                    :step="0.01"
                                    label="Strength"
                                    thumb-label
                                    color="primary"
                                  >
                                    <template #append
                                      ><span class="text-body-2">{{
                                        deviceProcessing.strength.toFixed(2)
                                      }}</span></template
                                    >
                                  </v-slider>
                                </v-col>
                                <v-col cols="12" md="6">
                                  <v-slider
                                    v-model="deviceProcessing.shadowBoost"
                                    :min="0"
                                    :max="1"
                                    :step="0.01"
                                    label="Shadow Boost"
                                    thumb-label
                                    color="primary"
                                  >
                                    <template #append
                                      ><span class="text-body-2">{{
                                        deviceProcessing.shadowBoost.toFixed(2)
                                      }}</span></template
                                    >
                                  </v-slider>
                                </v-col>
                                <v-col cols="12" md="6">
                                  <v-slider
                                    v-model="deviceProcessing.highlightCompress"
                                    :min="0.5"
                                    :max="5"
                                    :step="0.01"
                                    label="Highlight Compress"
                                    thumb-label
                                    color="primary"
                                  >
                                    <template #append
                                      ><span class="text-body-2">{{
                                        deviceProcessing.highlightCompress.toFixed(
                                          2
                                        )
                                      }}</span></template
                                    >
                                  </v-slider>
                                </v-col>
                                <v-col cols="12" md="6">
                                  <v-slider
                                    v-model="deviceProcessing.midpoint"
                                    :min="0.3"
                                    :max="0.7"
                                    :step="0.01"
                                    label="Midpoint"
                                    thumb-label
                                    color="primary"
                                  >
                                    <template #append
                                      ><span class="text-body-2">{{
                                        deviceProcessing.midpoint.toFixed(2)
                                      }}</span></template
                                    >
                                  </v-slider>
                                </v-col>
                              </v-row>
                            </v-card-text>
                          </v-card>
                        </v-expand-transition>
                      </v-tabs-window-item>

                      <!-- AI Generation Tab -->
                      <v-tabs-window-item value="ai">
                        <v-alert
                          type="info"
                          variant="tonal"
                          density="compact"
                          class="mt-2 mb-4"
                        >
                          API keys are stored on the device for client-side AI
                          image generation. Server-side AI provider/model/prompt
                          are used when the image source is set to AI
                          Generation.
                        </v-alert>

                        <v-text-field
                          v-model="deviceConfig.openai_api_key"
                          label="OpenAI API Key"
                          variant="outlined"
                          type="password"
                          hint="sk-..."
                          persistent-hint
                          class="mb-2"
                        />
                        <v-text-field
                          v-model="deviceConfig.google_api_key"
                          label="Google Gemini API Key"
                          variant="outlined"
                          type="password"
                          class="mb-4"
                        />

                        <v-divider class="mb-4" />
                        <div class="text-subtitle-2 mb-2">
                          Server-Side AI Generation
                        </div>

                        <v-select
                          v-model="editingDevice.ai_provider"
                          :items="[
                            { title: 'None', value: '' },
                            { title: 'OpenAI', value: 'openai' },
                            { title: 'Google Gemini', value: 'google' },
                            { title: 'ComfyUI (local)', value: 'comfyui' },
                          ]"
                          label="AI Provider"
                          variant="outlined"
                          density="compact"
                          hide-details
                          class="mb-3"
                        ></v-select>
                        <v-alert
                          v-if="editingDevice.ai_provider === 'comfyui'"
                          type="info"
                          variant="tonal"
                          density="compact"
                          class="mb-3"
                        >
                          Uses the local ComfyUI server and workflow configured
                          under Settings → AI Generation. The model is defined by
                          the workflow file, so no model selection is needed.
                        </v-alert>
                        <v-select
                          v-if="
                            editingDevice.ai_provider &&
                            editingDevice.ai_provider !== 'comfyui'
                          "
                          v-model="editingDevice.ai_model"
                          :items="
                            aiModelOptionsForProvider(editingDevice.ai_provider)
                          "
                          label="Model"
                          variant="outlined"
                          density="compact"
                          hide-details
                          class="mb-3"
                        ></v-select>
                        <v-textarea
                          v-if="editingDevice.ai_provider"
                          v-model="editingDevice.ai_prompt"
                          label="Prompt"
                          variant="outlined"
                          density="compact"
                          rows="3"
                          placeholder="A beautiful landscape painting..."
                          hide-details
                        ></v-textarea>
                      </v-tabs-window-item>

                      <!-- Palette Tab (matches device webapp PaletteCalibration) -->
                      <v-tabs-window-item value="palette">
                        <v-row class="mt-2">
                          <v-col
                            v-for="colorName in paletteColors"
                            :key="colorName"
                            cols="6"
                            md="4"
                            lg="2"
                          >
                            <v-card variant="outlined">
                              <div
                                class="color-swatch"
                                :style="{
                                  backgroundColor: `rgb(${devicePalette[colorName].r}, ${devicePalette[colorName].g}, ${devicePalette[colorName].b})`,
                                }"
                              />
                              <v-card-text class="pa-2">
                                <div
                                  class="text-subtitle-2 text-capitalize mb-2"
                                >
                                  {{ colorName }}
                                </div>
                                <v-text-field
                                  v-model.number="devicePalette[colorName].r"
                                  label="R"
                                  type="number"
                                  :min="0"
                                  :max="255"
                                  density="compact"
                                  variant="outlined"
                                  class="mb-1"
                                />
                                <v-text-field
                                  v-model.number="devicePalette[colorName].g"
                                  label="G"
                                  type="number"
                                  :min="0"
                                  :max="255"
                                  density="compact"
                                  variant="outlined"
                                  class="mb-1"
                                />
                                <v-text-field
                                  v-model.number="devicePalette[colorName].b"
                                  label="B"
                                  type="number"
                                  :min="0"
                                  :max="255"
                                  density="compact"
                                  variant="outlined"
                                />
                              </v-card-text>
                            </v-card>
                          </v-col>
                        </v-row>
                        <v-btn
                          variant="text"
                          color="error"
                          size="small"
                          class="mt-2"
                          @click="
                            Object.assign(devicePalette, {
                              black: { r: 2, g: 2, b: 2 },
                              white: { r: 190, g: 200, b: 200 },
                              yellow: { r: 205, g: 202, b: 0 },
                              red: { r: 135, g: 19, b: 0 },
                              blue: { r: 5, g: 64, b: 158 },
                              green: { r: 39, g: 102, b: 60 },
                            })
                          "
                          >Reset to Defaults</v-btn
                        >
                      </v-tabs-window-item>
                    </v-tabs-window>
                  </v-card-text>
                  <v-card-actions>
                    <v-btn
                      v-if="!isAddingDevice"
                      color="info"
                      variant="text"
                      size="small"
                      :loading="syncingFromDevice"
                      @click="syncFromDevice"
                    >
                      <v-icon start>mdi-sync</v-icon>
                      Sync from Device
                    </v-btn>
                    <v-spacer></v-spacer>
                    <v-btn
                      color="grey"
                      variant="text"
                      @click="showEditDeviceDialog = false"
                      >Cancel</v-btn
                    >
                    <v-btn
                      color="primary"
                      @click="saveDevice"
                      :loading="savingDeviceConfig"
                      :disabled="deviceHttpsBlocked"
                      >{{ isAddingDevice ? 'Add' : 'Save' }}</v-btn
                    >
                  </v-card-actions>
                </v-card>
              </v-dialog>
            </v-card-text>
          </v-window-item>
        </v-window>
      </div>

      <!-- Global Snackbar for Messages -->
      <v-snackbar
        v-model="snackbar.show"
        :color="snackbar.color"
        :timeout="3000"
        location="bottom right"
      >
        {{ snackbar.message }}
        <template v-slot:actions>
          <v-btn variant="text" @click="snackbar.show = false">Close</v-btn>
        </template>
      </v-snackbar>

      <ConfirmDialog ref="confirmDialog" />
    </v-card>
  </div>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, reactive, ref, computed, watch } from 'vue';
import { useSettingsStore } from '../stores/settings';
import { useSynologyStore } from '../stores/synology';
import { useImmichStore } from '../stores/immich';
import { useAuthStore } from '../stores/auth';
import { useGalleryStore } from '../stores/gallery';
import {
  api,
  listDevices,
  addDevice,
  deleteDevice,
  updateDevice,
  refreshDevice,
  type Device,
  createURLSource,
  updateURLSource,
  listURLSources,
  deleteURLSource,
  getDeviceConfig,
  updateDeviceConfig,
  getBatteryEstimate,
  type BatteryEstimate,
  listSources,
  updateAccount,
  listSessions,
  revokeSession,
  listCalendars,
  googleCalendarLogin,
  googleCalendarLogout,
} from '../api';
import Gallery from './Gallery.vue';
import ConfirmDialog from './ConfirmDialog.vue';

const store = useSettingsStore();
const synologyStore = useSynologyStore();
const immichStore = useImmichStore();
const immichConnected = ref(false);
const authStore = useAuthStore();
const galleryStore = useGalleryStore();
const activeMainTab = ref('devices');
const activeDataSourceTab = ref('gallery');
const galleryTab = ref('gallery');
const confirmDialog = ref();

// Image Source Binding State
const useThisServer = ref(true);
const selectedSource = ref('immich');

// HTTPS guard: no-PSRAM boards (e.g. FireBeetle) can't complete a TLS handshake
// alongside the framebuffer, so an https:// image URL never fetches. The device
// reports https_supported=false (refreshed on add/sync); warn + block save when
// the effective image URL would be https on such a board. Missing flag (older
// firmware / remote devices) is treated as capable to avoid false warnings.
const deviceHttpsUnsupported = computed(
  () => editingDevice.https_supported === false
);

const effectiveImageUrl = computed(() => {
  if (deviceConfig.rotation_mode !== 'url') return '';
  if (useThisServer.value) {
    try {
      return getImageUrl(selectedSource.value) || '';
    } catch {
      return '';
    }
  }
  return deviceConfig.image_url || '';
});

const deviceHttpsBlocked = computed(
  () =>
    deviceHttpsUnsupported.value &&
    deviceConfig.rotation_mode === 'url' &&
    /^\s*https:\/\//i.test(effectiveImageUrl.value)
);
// Friendly titles for known sources; anything else is prettified from its name.
const sourceTitles: Record<string, string> = {
  gallery: 'Gallery',
  immich: 'Immich',
  google_photos: 'Google Photos',
  synology_photos: 'Synology Photos',
  url_proxy: 'URL Proxy',
  ai_generation: 'AI Generation',
  fractal: 'Fractal (Mandelbrot zoom)',
  dla: 'DLA (diffusion-limited aggregation)',
};
const titleForSource = (name: string) =>
  sourceTitles[name] ||
  name.replace(/_/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase());
// Populated from the backend registry (GET /api/sources) so new server-side
// sources appear automatically; seeded with the known set as a fallback.
const sourceOptions = ref<{ title: string; value: string }[]>(
  Object.entries(sourceTitles).map(([value, title]) => ({ title, value }))
);
// Per-device photo display order (applies to DB-backed server sources).
const displayOrderOptions = [
  { title: 'Shuffle (each photo once, then reshuffle)', value: 'shuffle' },
  { title: 'Chronological — newest first', value: 'chrono_newest' },
  { title: 'Chronological — oldest first', value: 'chrono_oldest' },
  { title: 'Custom order (set in Gallery)', value: 'custom' },
];

// Per-frame Immich album selection (shown when the source is Immich). The
// album list comes from the global Immich connection, already sorted A–Z by the
// server. Stored on the device as a comma-separated id list.
const deviceImmichAlbumOptions = computed(() =>
  (immichStore.albums || []).map((a: any) => ({
    title: a.assetCount != null ? `${a.albumName} (${a.assetCount})` : a.albumName,
    value: a.id,
  }))
);
const immichAlbumIdsArray = computed<string[]>({
  get() {
    return (editingDevice.immich_album_ids || '')
      .split(',')
      .map((s) => s.trim())
      .filter((s) => s.length > 0);
  },
  set(ids: string[]) {
    editingDevice.immich_album_ids = ids.join(',');
  },
});

// Per-chip icon visibility. overlay_hidden_icons is a CSV of element keys whose
// leading icon is hidden; the checkbox reads as "Show icon" (the inverse).
const isIconHidden = (key: string): boolean =>
  (editingDevice.overlay_hidden_icons || '')
    .split(',')
    .map((s) => s.trim())
    .includes(key);
const setIconShown = (key: string, shown: boolean) => {
  const set = new Set(
    (editingDevice.overlay_hidden_icons || '')
      .split(',')
      .map((s) => s.trim())
      .filter((s) => s.length > 0)
  );
  if (shown) set.delete(key);
  else set.add(key);
  editingDevice.overlay_hidden_icons = Array.from(set).join(',');
};
// Wake-button gesture actions (must match firmware action ids).
const buttonActionOptions = [
  { title: 'Do nothing', value: 'none' },
  { title: 'Next image', value: 'next_image' },
  { title: 'Go to deep sleep', value: 'sleep' },
  { title: 'Toggle deep sleep on/off', value: 'toggle_deep_sleep' },
  { title: 'Show info screen', value: 'info_screen' },
];
const loadSources = async () => {
  try {
    const names = await listSources();
    if (names.length) {
      sourceOptions.value = names.map((n) => ({
        title: titleForSource(n),
        value: n,
      }));
    }
  } catch {
    // Keep the seeded defaults if the request fails.
  }
};

// URL Proxy State
const urlSources = ref<any[]>([]); // Renamed from urlImages
const showAddURLDialog = ref(false);
const isEditingURL = ref(false);
const editingURLId = ref<number | null>(null);
const newURL = reactive({
  url: '',
  device_ids: [] as number[],
});

// URL Proxy Functions
const loadURLSources = async () => {
  try {
    const res = await listURLSources();
    urlSources.value = res;
  } catch (e) {
    console.error('Failed to load URL sources', e);
  }
};

const openAddURLDialog = () => {
  isEditingURL.value = false;
  editingURLId.value = null;
  newURL.url = '';
  newURL.device_ids = [];
  showAddURLDialog.value = true;
};

const openEditURLDialog = (src: any) => {
  isEditingURL.value = true;
  editingURLId.value = src.id;
  newURL.url = src.url;
  // device_ids might come as objects or ids depending on API? API returns list of uints.
  newURL.device_ids = src.device_ids || [];
  showAddURLDialog.value = true;
};

const saveURLSource = async () => {
  if (!newURL.url) {
    showMessage('URL is required', true);
    return;
  }
  try {
    if (isEditingURL.value && editingURLId.value) {
      await updateURLSource(editingURLId.value, newURL.url, newURL.device_ids);
      showMessage('URL source updated');
    } else {
      await createURLSource(newURL.url, newURL.device_ids);
      showMessage('URL source added');
    }
    showAddURLDialog.value = false;
    await loadURLSources();
  } catch (e: any) {
    showMessage(
      'Failed to save URL source: ' + (e.response?.data?.error || e.message),
      true
    );
  }
};

const deleteURLSourceWrapper = async (id: number) => {
  if (!(await confirmDialog.value.open('Delete this URL Source?'))) return;
  try {
    await deleteURLSource(id);
    showMessage('URL source deleted');
    await loadURLSources();
  } catch (e: any) {
    showMessage('Failed to delete URL source', true);
  }
};

// Calendar State
const calendars = ref<any[]>([]);
const calendarConnected = ref(false);
const calendarLoaded = ref(false);

const loadCalendars = async () => {
  if (form.google_calendar_connected !== 'true') {
    calendarLoaded.value = true;
    return;
  }
  try {
    const cals = await listCalendars();
    calendars.value = cals;
    calendarConnected.value = true;
  } catch (e: any) {
    if (e.response?.status === 403) {
      calendarConnected.value = false;
    } else {
      console.error('Failed to load calendars', e);
    }
  } finally {
    calendarLoaded.value = true;
  }
};

// Edit Device State
const showEditDeviceDialog = ref(false);
const editingDevice = reactive<Partial<Device>>({});
const deviceDialogTab = ref('general');
const savingDeviceConfig = ref(false);
const syncingFromDevice = ref(false);

// Device config (synced remotely to device)
const deviceConfig = reactive<Record<string, any>>({
  auto_rotate: false,
  rotate_interval: 3600,
  auto_rotate_aligned: true,
  rotation_mode: 'storage',
  image_url: '',
  save_downloaded_images: true,
  sleep_schedule_enabled: false,
  sleep_start_time: '23:00',
  sleep_end_time: '07:00',
  display_orientation: 'portrait',
  display_rotation_deg: 0,
  timezone_offset: 0,
  ntp_server: 'pool.ntp.org',
  deep_sleep_enabled: true,
  button_action_short: 'next_image',
  button_action_long: 'sleep',
  button_action_hold: 'info_screen',
  ha_url: '',
  openai_api_key: '',
  google_api_key: '',
});

// Device processing settings (synced remotely)
const deviceProcessing = reactive({
  exposure: 1.0,
  saturation: 1.0,
  toneMode: 'contrast',
  contrast: 1.0,
  strength: 0.5,
  shadowBoost: 0.0,
  highlightCompress: 0.0,
  midpoint: 0.5,
  colorMethod: 'rgb',
  ditherAlgorithm: 'floyd-steinberg',
  compressDynamicRange: true,
});

// Processing presets from epaper-image-convert library
import {
  getPresetOptions,
  getPreset,
  getDitherOptions,
} from '@aitjcize/epaper-image-convert';

const processingPreset = ref('custom');
const processingPresetOptions = [
  ...getPresetOptions(),
  { value: 'custom', title: 'Custom' },
];
const processingPresets: Record<string, Record<string, any>> = {};
for (const opt of getPresetOptions()) {
  const p = getPreset(opt.value);
  if (p) processingPresets[opt.value] = p;
}
const ditherOptions = getDitherOptions();

const applyProcessingPreset = (name: string) => {
  const preset = processingPresets[name];
  if (preset) {
    Object.assign(deviceProcessing, preset);
  }
  // 'custom' just keeps current values
};

// Detect current preset on load
// Match preset detection logic from device webapp: only compare keys present in the preset
const presetKeys = [
  'exposure',
  'saturation',
  'toneMode',
  'contrast',
  'strength',
  'shadowBoost',
  'highlightCompress',
  'midpoint',
  'colorMethod',
  'ditherAlgorithm',
  'compressDynamicRange',
];

const detectProcessingPreset = () => {
  for (const [name, preset] of Object.entries(processingPresets)) {
    const matches = presetKeys.every((k) => {
      if (!(k in preset)) return true; // Skip keys not in this preset
      const pv = (preset as any)[k];
      const dv = (deviceProcessing as any)[k];
      // Numeric fields drift through the device's float32 NVS round-trip
      // (e.g. 1.4 comes back as 1.4000000953…), so an exact === check would
      // mislabel a synced-from-device preset as "Custom". Compare with a small
      // epsilon — preset values differ by ≥0.1, far above any drift.
      if (typeof pv === 'number' && typeof dv === 'number') {
        return Math.abs(pv - dv) < 1e-3;
      }
      return pv === dv;
    });
    if (matches) {
      processingPreset.value = name;
      return;
    }
  }
  processingPreset.value = 'custom';
};

// Re-detect preset when processing params change
watch(
  deviceProcessing,
  () => {
    detectProcessingPreset();
  },
  { deep: true }
);

// Device color palette (synced remotely)
const paletteColors = [
  'black',
  'white',
  'yellow',
  'red',
  'blue',
  'green',
] as const;
const devicePalette = reactive<
  Record<string, { r: number; g: number; b: number }>
>({
  black: { r: 2, g: 2, b: 2 },
  white: { r: 190, g: 200, b: 200 },
  yellow: { r: 205, g: 202, b: 0 },
  red: { r: 135, g: 19, b: 0 },
  blue: { r: 5, g: 64, b: 158 },
  green: { r: 39, g: 102, b: 60 },
});

// Auto-update mDNS hostname when device name changes
// Matches firmware's sanitize_hostname: lowercase, non-alnum → hyphen, no leading/trailing/consecutive hyphens
function deviceNameToHostname(name: string): string {
  let result = '';
  let lastWasHyphen = false;
  for (const c of name) {
    if (/[a-zA-Z0-9]/.test(c)) {
      result += c.toLowerCase();
      lastWasHyphen = false;
    } else if (!lastWasHyphen && result.length > 0) {
      result += '-';
      lastWasHyphen = true;
    }
  }
  // Remove trailing hyphen
  if (result.endsWith('-')) result = result.slice(0, -1);
  return result || 'photoframe';
}

watch(
  () => editingDevice.name,
  (newName) => {
    // Only auto-update if current host is an mDNS name
    if (!editingDevice.host?.endsWith('.local')) return;
    if (!newName) return;
    editingDevice.host = deviceNameToHostname(newName) + '.local';
  }
);

// Auto-fill weather coordinates from first device that has them
watch(
  () => editingDevice.show_weather,
  (enabled) => {
    if (!enabled) return;
    // Only fill if lat/lon are empty
    if (editingDevice.weather_lat && editingDevice.weather_lon) return;
    const donor = availableDevices.value.find(
      (d: Device) =>
        d.show_weather &&
        d.weather_lat &&
        d.weather_lon &&
        d.id !== editingDevice.id
    );
    if (donor) {
      editingDevice.weather_lat = donor.weather_lat;
      editingDevice.weather_lon = donor.weather_lon;
    }
  }
);

// Display Rotation is the single source of truth; the landscape/portrait label
// is derived from native panel dims + the chosen rotation (90°/270° swap the
// viewing dimensions). Shown read-only and sent to the firmware config so its
// own WebUI stays consistent.
const derivedOrientation = computed(() => {
  const deg = (((deviceConfig.display_rotation_deg ?? 0) % 360) + 360) % 360;
  const w = editingDevice.width || 800;
  const h = editingDevice.height || 480;
  const [lw, lh] = deg === 90 || deg === 270 ? [h, w] : [w, h];
  return lw >= lh ? 'landscape' : 'portrait';
});

const rotateIntervalOptions = [
  { title: '5 minutes', value: 300 },
  { title: '15 minutes', value: 900 },
  { title: '30 minutes', value: 1800 },
  { title: '1 hour', value: 3600 },
  { title: '2 hours', value: 7200 },
  { title: '4 hours', value: 14400 },
  { title: '6 hours', value: 21600 },
  { title: '12 hours', value: 43200 },
  { title: '24 hours', value: 86400 },
];

const loadDeviceConfig = async (deviceId: number) => {
  try {
    const data = await getDeviceConfig(deviceId);
    const parse = (v: any) =>
      (typeof v === 'string' && v !== '{}' ? JSON.parse(v) : v) || {};

    // Config
    const cfg = parse(data.config);
    // Update device name from device config if available
    if (cfg.device_name) {
      editingDevice.name = cfg.device_name;
    }
    // Surface the frame's own AI prompt (editable on the device, returned by
    // /api/config) so it round-trips on "Sync from Device". The frame's prompt
    // wins at runtime (X-AI-Prompt header), so reflect it here when set; keep
    // the server-side prompt when the frame has none so we never blank it.
    if (typeof cfg.ai_prompt === 'string' && cfg.ai_prompt.trim() !== '') {
      editingDevice.ai_prompt = cfg.ai_prompt;
    }
    Object.assign(deviceConfig, {
      auto_rotate: cfg.auto_rotate ?? false,
      rotate_interval: cfg.rotate_interval ?? 3600,
      auto_rotate_aligned: cfg.auto_rotate_aligned ?? true,
      rotation_mode: cfg.rotation_mode ?? 'storage',
      image_url: cfg.image_url ?? '',
      save_downloaded_images: cfg.save_downloaded_images ?? true,
    });

    // Detect if image_url points to this server
    const imgUrl = cfg.image_url || '';
    let isThisServer = false;
    if (imgUrl.includes('/image/')) {
      try {
        const imgHost = new URL(imgUrl).hostname;
        const serverHost = window.location.hostname;
        isThisServer = imgHost === serverHost;
      } catch {
        isThisServer = false;
      }
    }
    useThisServer.value = isThisServer;
    if (isThisServer) {
      const match = imgUrl.match(/\/image\/([^/?]+)/);
      if (match) {
        selectedSource.value = match[1];
      }
    }

    Object.assign(deviceConfig, {
      sleep_schedule_enabled: cfg.sleep_schedule_enabled ?? false,
      display_orientation:
        cfg.display_orientation ?? deviceConfig.display_orientation,
      display_rotation_deg: cfg.display_rotation_deg ?? 0,
      deep_sleep_enabled: cfg.deep_sleep_enabled ?? true,
      button_action_short: cfg.button_action_short ?? 'next_image',
      button_action_long: cfg.button_action_long ?? 'sleep',
      button_action_hold: cfg.button_action_hold ?? 'info_screen',
      ha_url: cfg.ha_url ?? '',
      ntp_server: cfg.ntp_server ?? 'pool.ntp.org',
      openai_api_key: cfg.openai_api_key ?? '',
      google_api_key: cfg.google_api_key ?? '',
    });
    const startMin = cfg.sleep_schedule_start ?? 1380;
    deviceConfig.sleep_start_time = `${String(Math.floor(startMin / 60)).padStart(2, '0')}:${String(startMin % 60).padStart(2, '0')}`;
    const endMin = cfg.sleep_schedule_end ?? 420;
    deviceConfig.sleep_end_time = `${String(Math.floor(endMin / 60)).padStart(2, '0')}:${String(endMin % 60).padStart(2, '0')}`;

    // Parse POSIX timezone (e.g., "UTC-8" → 8, "UTC+1" → -1, POSIX sign is inverted)
    const tz = cfg.timezone || 'UTC0';
    const tzMatch = tz.match(/UTC([+-]?)(\d+)(?::(\d+))?/);
    if (tzMatch) {
      const sign = tzMatch[1] === '-' ? 1 : -1;
      const hours = parseInt(tzMatch[2]) || 0;
      const minutes = parseInt(tzMatch[3]) || 0;
      deviceConfig.timezone_offset = sign * (hours + minutes / 60);
    } else {
      deviceConfig.timezone_offset = 0;
    }

    // Processing settings
    const proc = parse(data.processing_settings);
    if (Object.keys(proc).length > 0) {
      Object.assign(deviceProcessing, {
        exposure: proc.exposure ?? 1.0,
        saturation: proc.saturation ?? 1.0,
        toneMode: proc.toneMode ?? 'contrast',
        contrast: proc.contrast ?? 1.0,
        strength: proc.strength ?? 0.5,
        shadowBoost: proc.shadowBoost ?? 0.0,
        highlightCompress: proc.highlightCompress ?? 0.0,
        midpoint: proc.midpoint ?? 0.5,
        colorMethod: proc.colorMethod ?? 'rgb',
        ditherAlgorithm: proc.ditherAlgorithm ?? 'floyd-steinberg',
        compressDynamicRange: proc.compressDynamicRange ?? true,
      });
    }
    detectProcessingPreset();

    // Color palette
    const pal = parse(data.color_palette);
    for (const color of paletteColors) {
      if (pal[color]) {
        devicePalette[color] = {
          r: pal[color].r ?? 0,
          g: pal[color].g ?? 0,
          b: pal[color].b ?? 0,
        };
      }
    }
  } catch {
    // No config saved yet, use defaults
  }
};

const syncFromDevice = async () => {
  if (!editingDevice.id) return;
  syncingFromDevice.value = true;
  try {
    await refreshDevice(editingDevice.id);
    await loadDevices();
    // Re-load the updated device into the dialog
    const updated = availableDevices.value.find(
      (d: Device) => d.id === editingDevice.id
    );
    if (updated) Object.assign(editingDevice, updated);
    // Reload device config to reflect synced values
    await loadDeviceConfig(editingDevice.id!);
    showMessage('Settings synced from device');
  } catch (e: any) {
    showMessage(
      'Failed to sync: ' + (e.response?.data?.error || e.message),
      true
    );
  } finally {
    syncingFromDevice.value = false;
  }
};

const allLayoutOptions = [
  {
    title: 'Full Photo + Overlay',
    value: 'photo_overlay',
    orientations: ['portrait', 'landscape'],
  },
  {
    title: 'Photo + Info Strip',
    value: 'photo_info',
    orientations: ['portrait'],
  },
  { title: 'Side Panel', value: 'side_panel', orientations: ['landscape'] },
];

const filteredLayoutOptions = computed(() => {
  const orientation =
    deviceConfig.display_orientation ||
    editingDevice.orientation ||
    'landscape';
  return allLayoutOptions.filter((opt) =>
    opt.orientations.includes(orientation)
  );
});

// Display Rotation is the single source of truth: keep the derived
// landscape/portrait mirror in deviceConfig in sync so the live overlay preview
// and layout filtering react to a rotation change immediately.
watch(
  () => [deviceConfig.display_rotation_deg, editingDevice.width, editingDevice.height],
  () => {
    deviceConfig.display_orientation = derivedOrientation.value;
  }
);

// Auto-select first layout if current layout is not valid for orientation
watch(
  () => deviceConfig.display_orientation,
  () => {
    const valid = filteredLayoutOptions.value.map((o) => o.value);
    if (editingDevice.layout && !valid.includes(editingDevice.layout)) {
      editingDevice.layout = valid[0] || 'photo_overlay';
    }
  }
);

const getLayoutPreviewSvg = (layout: string, orientation: string) => {
  const isPortrait = orientation === 'portrait';
  const w = isPortrait ? 50 : 80;
  const h = isPortrait ? 70 : 50;
  const stroke = '#888';
  const photoFill = '#4a90d9';
  const infoFill = '#333';
  switch (layout) {
    case 'photo_info': {
      const photoH = Math.round(h * 0.6);
      return `<svg width="${w}" height="${h}" viewBox="0 0 ${w} ${h}">
        <rect width="${w}" height="${photoH}" fill="${photoFill}" rx="3"/>
        <rect y="${photoH}" width="${w}" height="${h - photoH}" fill="${infoFill}" rx="3"/>
        <line x1="4" y1="${photoH + 8}" x2="${w * 0.6}" y2="${photoH + 8}" stroke="#aaa" stroke-width="1.5"/>
        <line x1="4" y1="${photoH + 14}" x2="${w * 0.4}" y2="${photoH + 14}" stroke="#666" stroke-width="1"/>
      </svg>`;
    }
    case 'photo_overlay':
      return `<svg width="${w}" height="${h}" viewBox="0 0 ${w} ${h}">
        <rect width="${w}" height="${h}" fill="${photoFill}" rx="3"/>
        <defs><linearGradient id="og" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stop-color="transparent"/>
          <stop offset="100%" stop-color="rgba(0,0,0,0.7)"/>
        </linearGradient></defs>
        <rect y="${h * 0.5}" width="${w}" height="${h * 0.5}" fill="url(#og)" rx="3"/>
        <line x1="6" y1="${h - 12}" x2="${w * 0.55}" y2="${h - 12}" stroke="#fff" stroke-width="1.5" opacity="0.8"/>
        <line x1="6" y1="${h - 6}" x2="${w * 0.35}" y2="${h - 6}" stroke="#fff" stroke-width="1" opacity="0.5"/>
      </svg>`;
    case 'side_panel': {
      const photoW = Math.round(w * 0.65);
      return `<svg width="${w}" height="${h}" viewBox="0 0 ${w} ${h}">
        <rect width="${photoW}" height="${h}" fill="${photoFill}" rx="3"/>
        <rect x="${photoW}" width="${w - photoW}" height="${h}" fill="${infoFill}" rx="3"/>
        <line x1="${photoW + 3}" y1="10" x2="${w - 4}" y2="10" stroke="#aaa" stroke-width="1.5"/>
        <line x1="${photoW + 3}" y1="18" x2="${w - 6}" y2="18" stroke="#666" stroke-width="1"/>
        <line x1="${photoW + 3}" y1="24" x2="${w - 8}" y2="24" stroke="#666" stroke-width="1"/>
      </svg>`;
    }
    default:
      return `<svg width="${w}" height="${h}"><rect width="${w}" height="${h}" fill="${stroke}" rx="3"/></svg>`;
  }
};

const dateFormatOptions = [
  { label: 'Mon, Jan 02 (Default)', value: '' },
  { label: 'Monday, January 02, 2006', value: 'Monday, January 02, 2006' },
  { label: 'DD/MM/YYYY', value: '02/01/2006' },
  { label: 'MM/DD/YYYY', value: '01/02/2006' },
  { label: 'DD.MM.YYYY', value: '02.01.2006' },
  { label: 'DD-MM-YYYY', value: '02-01-2006' },
  { label: 'YYYY-MM-DD', value: '2006-01-02' },
  { label: 'YYYY.MM.DD', value: '2006.01.02' },
];

// formatGoDate renders a date using a Go reference-time layout (the same
// strings dateFormatOptions stores), so the live preview matches what the
// server renderer produces. An empty layout falls back to the renderer default
// "Mon, Jan 02". Tokens are matched longest-first by a left-to-right scan so
// inserted values are never re-interpreted as tokens.
const formatGoDate = (layout: string, d: Date): string => {
  const wdShort = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'][d.getDay()];
  const wdLong = [
    'Sunday',
    'Monday',
    'Tuesday',
    'Wednesday',
    'Thursday',
    'Friday',
    'Saturday',
  ][d.getDay()];
  const moShort = [
    'Jan',
    'Feb',
    'Mar',
    'Apr',
    'May',
    'Jun',
    'Jul',
    'Aug',
    'Sep',
    'Oct',
    'Nov',
    'Dec',
  ][d.getMonth()];
  const moLong = [
    'January',
    'February',
    'March',
    'April',
    'May',
    'June',
    'July',
    'August',
    'September',
    'October',
    'November',
    'December',
  ][d.getMonth()];
  const day2 = String(d.getDate()).padStart(2, '0');
  const month2 = String(d.getMonth() + 1).padStart(2, '0');
  const year4 = String(d.getFullYear());

  if (!layout) return `${wdShort}, ${moShort} ${day2}`;

  const tokens: [string, string][] = [
    ['Monday', wdLong],
    ['January', moLong],
    ['2006', year4],
    ['Mon', wdShort],
    ['Jan', moShort],
    ['02', day2],
    ['01', month2],
  ];
  let out = '';
  let i = 0;
  while (i < layout.length) {
    const match = tokens.find((t) => layout.startsWith(t[0], i));
    if (match) {
      out += match[1];
      i += match[0].length;
    } else {
      out += layout[i];
      i += 1;
    }
  }
  return out;
};

const positionOptions = [
  { label: 'Top Left', value: 'top-left' },
  { label: 'Top Center', value: 'top-center' },
  { label: 'Top Right', value: 'top-right' },
  { label: 'Wide Top (full-width band)', value: 'wide-top' },
  { label: 'Wide Bottom (full-width band)', value: 'wide-bottom' },
  { label: 'Bottom Left', value: 'bottom-left' },
  { label: 'Bottom Center', value: 'bottom-center' },
  { label: 'Bottom Right', value: 'bottom-right' },
];

const batteryStyleOptions = [
  { label: 'Icon + Text', value: 'both' },
  { label: 'Icon only', value: 'icon' },
  { label: 'Text only', value: 'text' },
];

const batteryRotationOptions = [
  { label: 'Normal (0°)', value: 0 },
  { label: 'Rotated 90°', value: 90 },
  { label: 'Upside down (180°)', value: 180 },
  { label: 'Rotated 270°', value: 270 },
];

const batteryTextSideOptions = [
  { label: 'Right of icon', value: 'right' },
  { label: 'Left of icon', value: 'left' },
  { label: 'Above icon', value: 'top' },
  { label: 'Below icon', value: 'bottom' },
];

// Overlay typeface. Keys match the server renderer's overlayFontFamily map; the
// five families are installed in the container and picked for e-paper clarity.
const fontOptions = [
  { label: 'Noto Sans', value: 'noto_sans' },
  { label: 'Inter', value: 'inter' },
  { label: 'DejaVu Sans', value: 'dejavu_sans' },
  { label: 'Liberation Sans', value: 'liberation_sans' },
  { label: 'DejaVu Serif', value: 'dejavu_serif' },
  { label: 'Ole (handwritten)', value: 'ole' },
];

const fontWeightOptions = [
  { label: 'Regular', value: 'regular' },
  { label: 'Medium', value: 'medium' },
  { label: 'Bold', value: 'bold' },
];

// People name rendering formats (keys mirror the backend's validNameFormats).
const nameFormatOptions = [
  { label: 'First Last (Anna Andersson)', value: 'first_last' },
  { label: 'First L. (Anna A.)', value: 'first_initial' },
  { label: 'First (Anna)', value: 'first' },
  { label: 'Last First (Andersson Anna)', value: 'last_first' },
  { label: 'Last F. (Andersson A.)', value: 'last_initial' },
  { label: 'Last (Andersson)', value: 'last' },
];

// sampleNamesText mirrors the backend FormatPeople for the live preview, using
// two sample people so the chosen format, age toggle and length limit are
// visible.
const sampleNamesText = (): string => {
  const sample = [
    { first: 'Anna', last: 'Andersson', age: 34 },
    { first: 'Erik', last: 'Berg', age: 7 },
  ];
  const format = editingDevice.name_format || 'first_last';
  const showAge = !!editingDevice.names_show_age;
  const maxLen = editingDevice.names_max_len || 30;

  const label = (p: { first: string; last: string }) => {
    const li = p.last ? p.last.charAt(0).toUpperCase() + '.' : '';
    const fi = p.first ? p.first.charAt(0).toUpperCase() + '.' : '';
    switch (format) {
      case 'first':
        return p.first;
      case 'first_initial':
        return p.last ? `${p.first} ${li}` : p.first;
      case 'last':
        return p.last || p.first;
      case 'last_first':
        return p.last ? `${p.last} ${p.first}` : p.first;
      case 'last_initial':
        return p.last ? `${p.last} ${fi}` : p.first;
      default:
        return p.last ? `${p.first} ${p.last}` : p.first;
    }
  };

  const parts = sample.map((p) => {
    let s = label(p);
    if (showAge) s += ` (${p.age})`;
    return s;
  });

  let out = '';
  for (let i = 0; i < parts.length; i++) {
    const candidate = i > 0 ? `, ${parts[i]}` : parts[i];
    if (out !== '' && out.length + candidate.length > maxLen) {
      return `${out} +${parts.length - i}`;
    }
    out += candidate;
  }
  return out;
};

// CSS font-family stacks for the live preview (mirrors the renderer). The
// preview is approximate — browsers fall back to a generic family if a face
// isn't installed locally.
const previewFontStacks: Record<string, string> = {
  noto_sans: "'Noto Sans', Arial, sans-serif",
  inter: "'Inter', 'Noto Sans', sans-serif",
  dejavu_sans: "'DejaVu Sans', 'Noto Sans', sans-serif",
  liberation_sans: "'Liberation Sans', Arial, sans-serif",
  dejavu_serif: "'DejaVu Serif', 'Noto Serif', serif",
  ole: "'Ole', cursive",
};

const previewFontWeights: Record<string, number> = {
  regular: 400,
  medium: 500,
  bold: 700,
};

// --- Live overlay preview (mirrors the server renderer's placement rules) ---
interface PreviewEl {
  key: string;
  pos: string;
  kind: string;
  text?: string;
  emoji?: string;
  battery?: boolean;
  batteryRotation?: number;
  batteryTextSide?: string;
  batteryIconScale?: number;
  pct?: number;
  low?: boolean;
}

// Date/photo-date/weather only float on the full-photo (overlay) layout;
// battery shows on the photo in every layout.
const isOverlayLayoutPreview = computed(() => {
  const l = editingDevice.layout || 'photo_overlay';
  return l !== 'photo_info' && l !== 'side_panel';
});

const previewElements = computed<PreviewEl[]>(() => {
  const els: PreviewEl[] = [];
  const ov = isOverlayLayoutPreview.value;
  const now = new Date();
  if (ov && editingDevice.show_date) {
    els.push({
      key: 'date',
      pos: editingDevice.date_position || 'bottom-left',
      kind: 'date',
      text: formatGoDate(editingDevice.date_format || '', now),
    });
  }
  if (ov && editingDevice.show_photo_date) {
    els.push({
      key: 'pdate',
      pos: editingDevice.photo_date_position || 'bottom-left',
      kind: 'photo',
      emoji: isIconHidden('photo_date') ? undefined : '📷',
      // Photo date is always rendered as "Jan 02, 2006" by the server.
      text: formatGoDate('Jan 02, 2006', now),
    });
  }
  if (ov && editingDevice.show_weather) {
    els.push({
      key: 'weather',
      pos: editingDevice.weather_position || 'bottom-right',
      kind: 'weather',
      emoji: isIconHidden('weather') ? undefined : '☀️',
      // Renderer shows temperature AND humidity, e.g. "21.0°C  45%".
      text: '21.0°C  45%',
    });
  }
  if (ov && editingDevice.show_names) {
    els.push({
      key: 'names',
      pos: editingDevice.names_position || 'top-left',
      kind: 'names',
      emoji: isIconHidden('names') ? undefined : '👥',
      text: sampleNamesText(),
    });
  }
  if (ov && editingDevice.show_location) {
    els.push({
      key: 'location',
      pos: editingDevice.location_position || 'bottom-center',
      kind: 'location',
      emoji: isIconHidden('location') ? undefined : '📍',
      text: 'Björnås, Skåne, Sweden',
    });
  }
  if (ov && editingDevice.show_description) {
    const sample = 'A lovely day by the lake with the whole family.';
    const max = editingDevice.description_max_len || 80;
    els.push({
      key: 'description',
      pos: editingDevice.description_position || 'wide-bottom',
      kind: 'description',
      emoji: isIconHidden('description') ? undefined : '📝',
      text:
        sample.length > max ? sample.slice(0, max - 1).trimEnd() + '…' : sample,
    });
  }
  if (editingDevice.show_battery) {
    const style = editingDevice.battery_style || 'both';
    const pct = 76;
    els.push({
      key: 'bat',
      pos: editingDevice.battery_position || 'top-right',
      kind: 'battery',
      battery: style !== 'text',
      batteryRotation: editingDevice.battery_rotation || 0,
      batteryTextSide: editingDevice.battery_text_side || 'right',
      batteryIconScale: editingDevice.battery_icon_scale || 1,
      text: style !== 'icon' ? `${pct}%` : '',
      pct,
      low: pct <= 15,
    });
  }
  return els;
});

// previewRegions mirrors the renderer's floating layout: a top and bottom
// region, each a corner row (left/center/right grid) plus a full-width band.
// Empty parts are dropped, so a wide band collapses into the corner row's place
// when no corner chip is shown.
const previewRegions = computed(() => {
  const at = (pos: string) =>
    previewElements.value.filter((e) => e.pos === pos);
  const topCorners = ['top-left', 'top-center', 'top-right'];
  const botCorners = ['bottom-left', 'bottom-center', 'bottom-right'];

  const topParts: any[] = [];
  if (topCorners.some((p) => at(p).length)) {
    topParts.push({
      type: 'corners',
      key: 'tc',
      cells: topCorners.map((pos) => ({ pos, items: at(pos) })),
    });
  }
  if (at('wide-top').length) {
    topParts.push({ type: 'wide', key: 'wt', items: at('wide-top') });
  }

  const botParts: any[] = [];
  if (at('wide-bottom').length) {
    botParts.push({ type: 'wide', key: 'wb', items: at('wide-bottom') });
  }
  if (botCorners.some((p) => at(p).length)) {
    botParts.push({
      type: 'corners',
      key: 'bc',
      cells: botCorners.map((pos) => ({ pos, items: at(pos) })),
    });
  }

  return [
    { name: 'top', parts: topParts },
    { name: 'bottom', parts: botParts },
  ];
});

const previewBoxStyle = computed(() => {
  const portrait = (editingDevice.orientation || 'landscape') === 'portrait';
  return portrait
    ? { width: '180px', height: '270px' }
    : { width: '270px', height: '180px' };
});

// The preview box is rendered with a fixed 180px short side (270x180 landscape
// / 180x270 portrait). To make the preview match the device 1:1, mirror the
// renderer's --secondary-size formula for the panel, then scale it down by the
// ratio of the preview box to the real panel.
const PREVIEW_SHORT_PX = 180;

const previewPanelMinDim = computed(() => {
  const w = editingDevice.width || 600;
  const h = editingDevice.height || 400;
  const m = Math.min(w, h);
  return m > 0 ? m : 400;
});

// Renderer: baseUnit = 4.8 * (minDim/480)^0.62 ; secondary-size = 4.0 * baseUnit
// (px on the real panel). previewSecondaryPx is that value scaled to the
// preview box, i.e. the px size a --secondary-size element occupies in preview.
const previewSecondaryPx = computed(() => {
  const minDim = previewPanelMinDim.value;
  const baseUnit = 4.8 * Math.pow(minDim / 480, 0.62);
  const secondary = baseUnit * 4.0;
  return secondary * (PREVIEW_SHORT_PX / minDim);
});

const previewFontSize = (_el: PreviewEl) => {
  // All overlay chips share one size in the renderer (--secondary-size * scale).
  return `${previewSecondaryPx.value * (editingDevice.overlay_scale || 1)}px`;
};

// Mirrors the renderer's battery chip rules: text side → flex-direction,
// 90/270 rotation → reserve vertical room (scaled with the icon size) so the
// icon stays inside the chip.
const previewChipStyle = (el: PreviewEl) => {
  const style: Record<string, string> = {
    fontSize: previewFontSize(el),
    fontFamily:
      previewFontStacks[editingDevice.overlay_font || 'noto_sans'] ||
      previewFontStacks.noto_sans,
    fontWeight: String(
      previewFontWeights[editingDevice.overlay_weight || 'medium'] || 500
    ),
  };
  if (el.kind === 'battery') {
    const side = el.batteryTextSide || 'right';
    if (side === 'left') style.flexDirection = 'row-reverse';
    else if (side === 'top') style.flexDirection = 'column-reverse';
    else if (side === 'bottom') style.flexDirection = 'column';
    const rot = el.batteryRotation || 0;
    if (rot === 90 || rot === 270) {
      style.minHeight = `${previewSecondaryPx.value * (el.batteryIconScale || 1) * 2.6}px`;
    }
  }
  return style;
};

// The battery icon is sized off its own base * icon scale, independent of the
// text size, and carries the rotation transform.
const previewBatStyle = (el: PreviewEl) => {
  const style: Record<string, string> = {
    fontSize: `${previewSecondaryPx.value * (el.batteryIconScale || 1)}px`,
  };
  if (el.batteryRotation) {
    style.transform = `rotate(${el.batteryRotation}deg)`;
  }
  return style;
};

const layoutDescriptions: Record<string, string> = {
  photo_info:
    'Photo on top with a dedicated info strip showing date, weather, and calendar events.',
  photo_overlay:
    'Full-screen photo with a semi-transparent overlay showing date, weather, and events.',
  side_panel:
    'Photo with a side panel (landscape) or bottom panel (portrait) showing weather and events.',
};

const aiModelOptionsForProvider = (provider: string | undefined) => {
  if (provider === 'openai') {
    return [
      { title: 'GPT Image 1.5', value: 'gpt-image-1.5' },
      { title: 'GPT Image 1', value: 'gpt-image-1' },
      { title: 'GPT Image 1 Mini', value: 'gpt-image-1-mini' },
    ];
  } else if (provider === 'google') {
    return [
      {
        title: 'Gemini 3.1 Flash Image',
        value: 'gemini-3.1-flash-image-preview',
      },
      { title: 'Gemini 3 Pro Image', value: 'gemini-3-pro-image-preview' },
      { title: 'Gemini 2.5 Flash Image', value: 'gemini-2.5-flash-image' },
    ];
  }
  return [];
};

const getDeviceName = (id: number) => {
  const dev = availableDevices.value.find((d) => d.id === id);
  return dev ? dev.name : `Device ${id}`;
};

watch(activeDataSourceTab, (val) => {
  if (val === 'url') {
    loadURLSources();
  } else if (val === 'google') {
    loadCalendars();
  }
});

// Devices State
const availableDevices = ref<Device[]>([]);
const deviceListLoading = ref(false);

// Load calendars when the edit dialog opens (if not yet loaded)
watch(showEditDeviceDialog, (open) => {
  if (open && !calendarLoaded.value) {
    loadCalendars();
  }
});

// Reset AI model when provider changes
watch(
  () => editingDevice.ai_provider,
  (newProvider, oldProvider) => {
    if (newProvider !== oldProvider && oldProvider !== undefined) {
      // Set default model for the new provider
      if (newProvider === 'openai') {
        editingDevice.ai_model = 'gpt-image-1.5';
      } else if (newProvider === 'google') {
        editingDevice.ai_model = 'gemini-3.1-flash-image-preview';
      } else {
        editingDevice.ai_model = '';
      }
    }
  }
);

const isAddingDevice = ref(false);

const openAddDeviceDialog = () => {
  batteryEstimate.value = null;
  if (!immichStore.albums || immichStore.albums.length === 0) {
    immichStore.fetchAlbums().catch(() => {});
  }
  Object.assign(editingDevice, {
    id: undefined,
    immich_album_ids: '',
    overlay_hidden_icons: '',
    name: '',
    host: '',
    width: 0,
    height: 0,
    orientation: '',
    enable_collage: false,
    show_date: false,
    show_photo_date: false,
    show_weather: false,
    weather_lat: null,
    weather_lon: null,
    ai_provider: '',
    ai_model: '',
    ai_prompt: '',
    layout: 'photo_overlay',
    display_mode: 'cover',
    show_calendar: false,
    calendar_id: '',
    date_format: '',
    show_battery: false,
    display_order: 'shuffle',
    date_position: 'bottom-left',
    photo_date_position: 'bottom-left',
    weather_position: 'bottom-right',
    battery_position: 'top-right',
    battery_style: 'both',
    battery_rotation: 0,
    battery_text_side: 'right',
    battery_icon_scale: 1,
    overlay_scale: 1,
    overlay_font: 'noto_sans',
    overlay_weight: 'medium',
    show_names: false,
    names_position: 'top-left',
    name_format: 'first_last',
    names_show_age: false,
    names_max_len: 30,
    show_location: false,
    location_position: 'bottom-center',
    location_max_len: 40,
    show_description: false,
    description_position: 'wide-bottom',
    description_max_len: 80,
  });
  Object.assign(deviceConfig, {
    auto_rotate: false,
    rotate_interval: 3600,
    auto_rotate_aligned: true,
    rotation_mode: 'storage',
    image_url: '',
    save_downloaded_images: true,
    sleep_schedule_enabled: false,
    sleep_start_time: '23:00',
    sleep_end_time: '07:00',
    display_orientation: 'landscape',
    deep_sleep_enabled: true,
    button_action_short: 'next_image',
    button_action_long: 'sleep',
    button_action_hold: 'info_screen',
  });
  isAddingDevice.value = true;
  deviceDialogTab.value = 'general';
  showEditDeviceDialog.value = true;
};

const editDevice = async (device: Device) => {
  Object.assign(editingDevice, device);
  // Initialize display_orientation from device's orientation
  deviceConfig.display_orientation = device.orientation || 'landscape';
  isAddingDevice.value = false;
  deviceDialogTab.value = 'general';
  showEditDeviceDialog.value = true;
  // Load device remote config + battery drain estimate
  loadBatteryEstimate(device.id);
  // Populate the Immich album picker (best-effort; only matters for Immich frames).
  if (!immichStore.albums || immichStore.albums.length === 0) {
    immichStore.fetchAlbums().catch(() => {});
  }
  await loadDeviceConfig(device.id);
};

const batteryEstimate = ref<BatteryEstimate | null>(null);
const batteryLoading = ref(false);

const loadBatteryEstimate = async (deviceId?: number) => {
  batteryEstimate.value = null;
  if (!deviceId) return;
  batteryLoading.value = true;
  try {
    batteryEstimate.value = await getBatteryEstimate(deviceId);
  } catch {
    batteryEstimate.value = null;
  } finally {
    batteryLoading.value = false;
  }
};

const batteryTrendLabel = computed(() => {
  switch (batteryEstimate.value?.trend) {
    case 'discharging':
      return 'Discharging';
    case 'charging':
      return 'Charging';
    case 'stable':
      return 'Stable';
    default:
      return 'Collecting';
  }
});

const batteryTrendColor = computed(() => {
  switch (batteryEstimate.value?.trend) {
    case 'discharging':
      return 'warning';
    case 'charging':
      return 'success';
    case 'stable':
      return 'info';
    default:
      return 'grey';
  }
});

const batteryDaysLabel = computed(() => {
  const d = batteryEstimate.value?.days_remaining ?? -1;
  if (d < 0) return '—';
  if (d < 1) return `~${Math.round(d * 24)} h`;
  if (d < 14) return `~${d.toFixed(1)} days`;
  return `~${Math.round(d)} days`;
});

// SVG polyline points for the percent sparkline (0..100 x, 0..28 y inverted).
const batterySparkline = computed(() => {
  const pts = batteryEstimate.value?.recent ?? [];
  if (pts.length < 2) return '';
  const t0 = new Date(pts[0].sampled_at).getTime();
  const t1 = new Date(pts[pts.length - 1].sampled_at).getTime();
  const span = t1 - t0 || 1;
  return pts
    .map((p) => {
      const x = ((new Date(p.sampled_at).getTime() - t0) / span) * 100;
      const y = 28 - (Math.max(0, Math.min(100, p.percent)) / 100) * 28;
      return `${x.toFixed(2)},${y.toFixed(2)}`;
    })
    .join(' ');
});

const saveDevice = async () => {
  if (!editingDevice.host) {
    showMessage('Host is required', true);
    return;
  }
  if (editingDevice.show_weather) {
    if (
      editingDevice.weather_lat === null ||
      editingDevice.weather_lat === undefined ||
      isNaN(editingDevice.weather_lat) ||
      editingDevice.weather_lon === null ||
      editingDevice.weather_lon === undefined ||
      isNaN(editingDevice.weather_lon)
    ) {
      showMessage('Latitude and Longitude are required for weather', true);
      return;
    }
  }
  savingDeviceConfig.value = true;
  try {
    if (isAddingDevice.value) {
      const newDevice = await addDevice({
        host: editingDevice.host!,
        enable_collage: editingDevice.enable_collage!,
        show_date: editingDevice.show_date!,
        show_photo_date: editingDevice.show_photo_date || false,
        show_weather: editingDevice.show_weather!,
        weather_lat: editingDevice.weather_lat || 0,
        weather_lon: editingDevice.weather_lon || 0,
        layout: editingDevice.layout || 'photo_overlay',
        display_mode: editingDevice.display_mode || 'cover',
        show_calendar: editingDevice.show_calendar || false,
        calendar_id: editingDevice.calendar_id || '',
        date_format: editingDevice.date_format || '',
        show_battery: editingDevice.show_battery || false,
        display_order: editingDevice.display_order || 'shuffle',
        date_position: editingDevice.date_position || 'bottom-left',
        photo_date_position:
          editingDevice.photo_date_position || 'bottom-left',
        weather_position: editingDevice.weather_position || 'bottom-right',
        battery_position: editingDevice.battery_position || 'top-right',
        battery_style: editingDevice.battery_style || 'both',
        battery_rotation: editingDevice.battery_rotation || 0,
        battery_text_side: editingDevice.battery_text_side || 'right',
        battery_icon_scale: editingDevice.battery_icon_scale ?? 1,
        overlay_scale: editingDevice.overlay_scale ?? 1,
        overlay_font: editingDevice.overlay_font || 'noto_sans',
        overlay_weight: editingDevice.overlay_weight || 'medium',
        show_names: editingDevice.show_names || false,
        names_position: editingDevice.names_position || 'top-left',
        name_format: editingDevice.name_format || 'first_last',
        names_show_age: editingDevice.names_show_age || false,
        names_max_len: editingDevice.names_max_len ?? 30,
        show_location: editingDevice.show_location || false,
        location_position: editingDevice.location_position || 'bottom-center',
        location_max_len: editingDevice.location_max_len ?? 40,
        show_description: editingDevice.show_description || false,
        description_position:
          editingDevice.description_position || 'wide-bottom',
        description_max_len: editingDevice.description_max_len ?? 80,
        immich_album_ids: editingDevice.immich_album_ids || '',
        overlay_hidden_icons: editingDevice.overlay_hidden_icons || '',
      });
      await loadDevices();
      showMessage('Device added. Fetched settings from device.');
      // Re-open in edit mode with fetched config
      const added = availableDevices.value.find(
        (d: Device) => d.id === newDevice.id
      );
      if (added) {
        savingDeviceConfig.value = false;
        await editDevice(added);
        return;
      }
    } else {
      if (!editingDevice.id) return;
      // Save server-side device fields
      await updateDevice(
        editingDevice.id,
        editingDevice.name!,
        editingDevice.host!,
        deviceConfig.display_orientation || editingDevice.orientation!,
        editingDevice.enable_collage!,
        editingDevice.show_date!,
        editingDevice.show_photo_date || false,
        editingDevice.show_weather!,
        editingDevice.weather_lat || 0,
        editingDevice.weather_lon || 0,
        editingDevice.ai_provider || '',
        editingDevice.ai_model || '',
        editingDevice.ai_prompt || '',
        editingDevice.layout || 'photo_overlay',
        editingDevice.display_mode || 'cover',
        editingDevice.show_calendar || false,
        editingDevice.calendar_id || '',
        editingDevice.date_format || '',
        editingDevice.show_battery || false,
        {
          date_position: editingDevice.date_position || 'bottom-left',
          photo_date_position:
            editingDevice.photo_date_position || 'bottom-left',
          weather_position: editingDevice.weather_position || 'bottom-right',
          battery_position: editingDevice.battery_position || 'top-right',
          battery_style: editingDevice.battery_style || 'both',
          battery_rotation: editingDevice.battery_rotation || 0,
          battery_text_side: editingDevice.battery_text_side || 'right',
          battery_icon_scale: editingDevice.battery_icon_scale ?? 1,
          overlay_scale: editingDevice.overlay_scale ?? 1,
          overlay_font: editingDevice.overlay_font || 'noto_sans',
          overlay_weight: editingDevice.overlay_weight || 'medium',
          show_names: editingDevice.show_names || false,
          names_position: editingDevice.names_position || 'top-left',
          name_format: editingDevice.name_format || 'first_last',
          names_show_age: editingDevice.names_show_age || false,
          names_max_len: editingDevice.names_max_len ?? 30,
          show_location: editingDevice.show_location || false,
          location_position: editingDevice.location_position || 'bottom-center',
          location_max_len: editingDevice.location_max_len ?? 40,
          show_description: editingDevice.show_description || false,
          description_position:
            editingDevice.description_position || 'wide-bottom',
          description_max_len: editingDevice.description_max_len ?? 80,
          display_order: editingDevice.display_order || 'shuffle',
          immich_album_ids: editingDevice.immich_album_ids || '',
          overlay_hidden_icons: editingDevice.overlay_hidden_icons || '',
        }
      );

      // Save device remote config (config + processing + palette)
      const [startH, startM] = deviceConfig.sleep_start_time
        .split(':')
        .map(Number);
      const [endH, endM] = deviceConfig.sleep_end_time.split(':').map(Number);

      // Convert UTC offset to POSIX timezone format (sign is inverted)
      const offsetVal = deviceConfig.timezone_offset || 0;
      let timezone = 'UTC0';
      if (offsetVal !== 0) {
        const absOff = Math.abs(offsetVal);
        const h = Math.floor(absOff);
        const m = Math.round((absOff - h) * 60);
        const sign = offsetVal > 0 ? '-' : '+';
        timezone =
          m === 0
            ? `UTC${sign}${h}`
            : `UTC${sign}${h}:${String(m).padStart(2, '0')}`;
      }

      // Compute image URL: use server URL if "use this server" is checked.
      // getImageUrl() targets the direct add-on port, so the URL works when
      // the ESP32 reaches the server straight (ingress port 8123 cannot serve
      // /image/*).
      let imageUrl = deviceConfig.image_url;
      if (useThisServer.value && deviceConfig.rotation_mode === 'url') {
        imageUrl = getImageUrl(selectedSource.value);
      }

      const result = await updateDeviceConfig(editingDevice.id, {
        config: {
          device_name: editingDevice.name,
          auto_rotate: deviceConfig.auto_rotate,
          rotate_interval: deviceConfig.rotate_interval,
          auto_rotate_aligned: deviceConfig.auto_rotate_aligned,
          rotation_mode: deviceConfig.rotation_mode,
          image_url: imageUrl,
          save_downloaded_images: deviceConfig.save_downloaded_images,
          sleep_schedule_enabled: deviceConfig.sleep_schedule_enabled,
          sleep_schedule_start: startH * 60 + startM,
          sleep_schedule_end: endH * 60 + endM,
          display_orientation: derivedOrientation.value,
          display_rotation_deg: deviceConfig.display_rotation_deg,
          timezone: timezone,
          ntp_server: deviceConfig.ntp_server,
          deep_sleep_enabled: deviceConfig.deep_sleep_enabled,
          button_action_short: deviceConfig.button_action_short,
          button_action_long: deviceConfig.button_action_long,
          button_action_hold: deviceConfig.button_action_hold,
          ha_url: deviceConfig.ha_url,
          openai_api_key: deviceConfig.openai_api_key,
          google_api_key: deviceConfig.google_api_key,
        },
        processing_settings: { ...deviceProcessing },
        color_palette: { ...devicePalette },
      });
      if (result.push_result === 'synced') {
        showMessage('Device saved and config pushed to device.');
      } else {
        showMessage(
          'Device saved. Device is offline — config will sync on next image fetch.'
        );
      }
    }
    await loadDevices();
    showEditDeviceDialog.value = false;
  } catch (e: any) {
    showMessage(
      'Failed to save device: ' + (e.response?.data?.error || e.message),
      true
    );
  } finally {
    savingDeviceConfig.value = false;
  }
};

const loadDevices = async () => {
  deviceListLoading.value = true;
  try {
    availableDevices.value = await listDevices();
  } catch (e) {
    console.error('Failed to list devices', e);
  } finally {
    deviceListLoading.value = false;
  }
};

// Silent refresh for the auto-poll (no spinner flicker). The edit dialog uses a
// separate editingDevice copy, so refreshing the list never disrupts editing.
const refreshDevicesSilently = async () => {
  try {
    availableDevices.value = await listDevices();
  } catch (e) {
    /* transient — keep the last good list */
  }
};

// Poll the Devices list so the current-image thumbnail and battery status track
// the frames as they check in. Only while the Devices tab is showing and the
// tab is visible, to avoid needless load.
let deviceRefreshTimer: ReturnType<typeof setInterval> | null = null;
onMounted(() => {
  deviceRefreshTimer = setInterval(() => {
    if (activeMainTab.value === 'devices' && document.visibilityState === 'visible') {
      refreshDevicesSilently();
    }
  }, 30000);
});
onUnmounted(() => {
  if (deviceRefreshTimer) clearInterval(deviceRefreshTimer);
});

// Build the public /served-image-thumbnail/<id> URL for the current-image
// preview. Mirrors getImageUrl's origin/add-on-port logic since the thumbnail
// route lives on the same server (ingress port 8123 can't serve it directly).
const buildServedUrl = (path: string) => {
  const { protocol, hostname, port } = window.location;
  const addonPort = import.meta.env.VITE_ADDON_PORT || '9607';
  if (protocol === 'https:') return `${window.location.origin}/${path}`;
  if (port && port !== '8123' && port !== addonPort) {
    return `${window.location.origin}/${path}`;
  }
  return `${protocol}//${hostname}:${addonPort}/${path}`;
};

const getServedThumbUrl = (thumbId: string) =>
  buildServedUrl(`served-image-thumbnail/${thumbId}`);

// Full-resolution (native panel size) image, served by /served-image-full/:id.
// Opened in a lightbox when the Devices-list miniature is clicked.
const getServedFullUrl = (thumbId: string) =>
  buildServedUrl(`served-image-full/${thumbId}`);

const fullImageDialog = ref(false);
const fullImageUrl = ref('');
const fullImageTitle = ref('');

const openFullImage = (device: Device) => {
  if (!device.current_thumb_id) return;
  fullImageUrl.value = getServedFullUrl(device.current_thumb_id);
  fullImageTitle.value = device.name || 'Current image';
  fullImageDialog.value = true;
};

const batteryColor = (p: number) =>
  p <= 15 ? 'error' : p <= 40 ? 'warning' : 'success';

const batteryIcon = (p: number) => {
  if (p >= 95) return 'mdi-battery';
  if (p < 10) return 'mdi-battery-alert-variant-outline';
  return `mdi-battery-${Math.round(p / 10) * 10}`; // mdi-battery-10 .. -90
};

const batteryTitle = (device: Device) => {
  const days = device.battery_days_remaining ?? -1;
  if (days > 0) {
    return `Battery ${device.battery_percent}% · ~${days.toFixed(1)} days remaining`;
  }
  return `Battery ${device.battery_percent}%`;
};

const removeDevice = async (id: number) => {
  const response = await confirmDialog.value.open(
    'Remove Device',
    'Are you sure you want to remove this device?'
  );

  if (!response) return;

  try {
    await deleteDevice(id);
    await loadDevices();
    showMessage('Device removed');
  } catch (e) {
    showMessage('Failed to remove device', true);
  }
};

watch(galleryTab, (val) => {
  if (val === 'google_photos') {
    galleryStore.setSource('google_photos');
  } else if (val === 'synology_photos') {
    galleryStore.setSource('synology_photos');
  } else if (val === 'immich') {
    galleryStore.setSource('immich');
  } else if (val === 'gallery') {
    galleryStore.setSource('gallery');
  }
});

const snackbar = reactive({
  show: false,
  message: '',
  color: 'success',
});

const form = reactive({
  Orientation: 'landscape',
  DisplayWidth: 800,
  DisplayHeight: 480,
  CollageMode: false,
  show_date: true,
  show_weather: true,
  weather_lat: '',
  weather_lon: '',
  google_connected: 'false',
  google_calendar_connected: 'false',
  google_client_id: '',
  google_client_secret: '',
  synology_sid: '',
  synology_url: '',
  synology_account: '',
  synology_password: '',
  synology_skip_cert: false,
  synology_otp_code: '',
  synology_album_id: '',
  synology_auto_sync_enabled: false,
  synology_auto_sync_interval_minutes: 60,
  albums: [] as any[],
  immich_url: '',
  immich_api_key: '',
  immich_source_mode: 'album',
  immich_album_id: '',
  immich_auto_sync_enabled: false,
  immich_auto_sync_interval_minutes: 60,
  immich_albums: [] as any[],
  telegram_bot_token: '',
  telegram_push_enabled: false,
  telegram_target_device_id: [] as number[],
  openai_api_key: '',
  google_api_key: '',
  comfyui_host: '',
  comfyui_workflow: '',
  device_image_base_url: '',
  device_host: '', // Keep for backward compatibility/display? Or remove. Remove from form, keep in store maybe?
});

const synologyAlbumOptions = computed(() => {
  return form.albums;
});

const immichAlbumOptions = computed(() => {
  return form.immich_albums.map((a: any) => ({ id: a.id, name: a.albumName }));
});

const immichSourceModeOptions = [
  { title: 'Per-device albums', value: 'device_albums' },
  { title: 'One album', value: 'album' },
  { title: 'Favorites only', value: 'favorites' },
  { title: 'Memories (on this day)', value: 'memories' },
  { title: 'Entire library', value: 'all' },
];

// Per-mode explanation shown under the Sync Mode dropdown.
const immichSourceModeHelp: Record<string, string> = {
  device_albums:
    'Only syncs the album(s) each frame selects (Devices → edit → Auto Rotate → "Immich albums"). Nothing is pulled globally — ideal when every frame shows its own album. A frame with no album selected will have no photos.',
  album:
    'Syncs one album as a shared pool. Frames with no album of their own show this album. Frames can still narrow to their own album(s) in the device settings.',
  favorites: 'Syncs the assets you have marked as Favorite in Immich.',
  memories: 'Syncs your "on this day" memories across years.',
  all: '⚠️ Syncs your ENTIRE Immich library — this can be tens of thousands of photos. Only use this on small libraries. For multiple frames with different albums, choose "Per-device albums" instead.',
};

const autoSyncIntervalOptions = [
  { title: 'Every 15 minutes', value: 15 },
  { title: 'Every 30 minutes', value: 30 },
  { title: 'Every 1 hour', value: 60 },
  { title: 'Every 3 hours', value: 180 },
  { title: 'Every 6 hours', value: 360 },
  { title: 'Every 12 hours', value: 720 },
  { title: 'Every 24 hours', value: 1440 },
];

// Helper to show snackbar
const showMessage = (msg: string, isError = false) => {
  snackbar.message = msg;
  snackbar.color = isError ? 'error' : 'success';
  snackbar.show = true;
};

onMounted(async () => {
  loadSessions();
  loadSources();
  await store.fetchSettings();
  Object.assign(form, {
    Orientation: store.settings.orientation || 'landscape',
    DisplayWidth: parseInt(store.settings.display_width || '800'),
    DisplayHeight: parseInt(store.settings.display_height || '480'),
    CollageMode: store.settings.collage_mode === 'true',
    show_date: store.settings.show_date !== 'false',
    show_weather: store.settings.show_weather !== 'false',
    google_client_id: store.settings.google_client_id || '',
    google_client_secret: store.settings.google_client_secret || '',
    google_connected: store.settings.google_connected || 'false',
    google_calendar_connected:
      store.settings.google_calendar_connected || 'false',
    telegram_bot_token: store.settings.telegram_bot_token || '',
    telegram_push_enabled: store.settings.telegram_push_enabled === 'true',
    telegram_target_device_id: store.settings.telegram_target_device_id
      ? store.settings.telegram_target_device_id
          .split(',')
          .filter((id) => id)
          .map((id) => parseInt(id))
      : [],
    weather_lat: store.settings.weather_lat || '',
    weather_lon: store.settings.weather_lon || '',
    synology_url: store.settings.synology_url || '',
    synology_account: store.settings.synology_account || '',
    synology_password: store.settings.synology_password || '',
    synology_skip_cert: store.settings.synology_skip_cert === 'true',
    synology_album_id: store.settings.synology_album_id
      ? parseInt(store.settings.synology_album_id)
      : '',
    synology_auto_sync_enabled:
      store.settings.synology_auto_sync_enabled === 'true',
    synology_auto_sync_interval_minutes: parseInt(
      store.settings.synology_auto_sync_interval_minutes || '60'
    ),
    synology_sid: store.settings.synology_sid || '',
    immich_url: store.settings.immich_url || '',
    immich_api_key: store.settings.immich_api_key || '',
    immich_source_mode: store.settings.immich_source_mode || 'album',
    immich_album_id: store.settings.immich_album_id || '',
    immich_auto_sync_enabled:
      store.settings.immich_auto_sync_enabled === 'true',
    immich_auto_sync_interval_minutes: parseInt(
      store.settings.immich_auto_sync_interval_minutes || '60'
    ),
    openai_api_key: store.settings.openai_api_key || '',
    google_api_key: store.settings.google_api_key || '',
    comfyui_host: store.settings.comfyui_host || '',
    comfyui_workflow: store.settings.comfyui_workflow || '',
    device_image_base_url: store.settings.device_image_base_url || '',
  });

  // Load cached albums if available
  if (store.settings.synology_albums_cache) {
    try {
      form.albums = JSON.parse(store.settings.synology_albums_cache);
    } catch (e) {
      console.error('Failed to parse cached albums', e);
    }
  }

  // Run independent fetches in parallel
  const parallelFetches: Promise<void>[] = [
    authStore.fetchTokens(),
    loadDevices(),
  ];

  // Fetch Synology photo count if connected
  if (form.synology_sid) {
    parallelFetches.push(synologyStore.fetchCount());
  }

  // Fetch Immich photo count and albums if connected
  if (form.immich_url && form.immich_api_key) {
    immichConnected.value = true;
    parallelFetches.push(
      (async () => {
        await immichStore.fetchCount();
        try {
          await immichStore.fetchAlbums();
          form.immich_albums = immichStore.albums;
        } catch (e) {
          // Non-fatal: album names will be shown as UUIDs until user clicks Refresh
        }
      })()
    );
  }

  await Promise.all(parallelFetches);

  // Parse URL params for deep linking (e.g. from OAuth callback)
  const params = new URLSearchParams(window.location.search);
  const tab = params.get('tab');
  const source = params.get('source');

  if (tab) {
    activeMainTab.value = tab;
  }
  if (source) {
    activeDataSourceTab.value = source;
  }

  // Clean up URL if params were present
  if (tab || source) {
    window.history.replaceState({}, '', '/');
  }
});

const saveSettingsInternal = async () => {
  await store.saveSettings({
    orientation: form.Orientation,
    display_width: String(form.DisplayWidth),
    display_height: String(form.DisplayHeight),
    collage_mode: String(form.CollageMode),
    show_date: String(form.show_date),
    show_weather: String(form.show_weather),
    google_client_id: form.google_client_id,
    google_client_secret: form.google_client_secret,
    telegram_bot_token: form.telegram_bot_token,
    telegram_push_enabled: String(form.telegram_push_enabled),
    telegram_target_device_id: Array.isArray(form.telegram_target_device_id)
      ? form.telegram_target_device_id.join(',')
      : form.telegram_target_device_id,
    weather_lat: form.weather_lat,
    weather_lon: form.weather_lon,
    synology_url: form.synology_url,
    synology_account: form.synology_account,
    synology_password: form.synology_password,
    synology_skip_cert: String(form.synology_skip_cert),
    synology_album_id: String(form.synology_album_id),
    synology_auto_sync_enabled: String(form.synology_auto_sync_enabled),
    synology_auto_sync_interval_minutes: String(
      form.synology_auto_sync_interval_minutes
    ),
    immich_url: form.immich_url,
    immich_api_key: form.immich_api_key,
    immich_source_mode: form.immich_source_mode,
    immich_album_id: form.immich_album_id,
    immich_auto_sync_enabled: String(form.immich_auto_sync_enabled),
    immich_auto_sync_interval_minutes: String(
      form.immich_auto_sync_interval_minutes
    ),
    openai_api_key: form.openai_api_key,
    google_api_key: form.google_api_key,
    comfyui_host: form.comfyui_host,
    comfyui_workflow: form.comfyui_workflow,
    device_image_base_url: (form.device_image_base_url || '').trim(),
  });
};

const save = async () => {
  try {
    await saveSettingsInternal();
    showMessage('Settings saved successfully');
  } catch (err: any) {
    showMessage(err.message || 'Failed to save settings', true);
  }
};

const connectGoogle = async () => {
  try {
    await saveSettingsInternal();
    const res = await api.get('/auth/google/login');
    window.location.href = res.data.url;
  } catch (e) {
    showMessage('Failed to connect: ' + e, true);
  }
};

const logoutGoogle = async () => {
  if (
    !(await confirmDialog.value.open(
      'Are you sure you want to disconnect Google Photos?'
    ))
  )
    return;
  try {
    await api.post('/auth/google/logout');
    form.google_connected = 'false';
    showMessage('Disconnected Google Photos.');
    await store.fetchSettings();
  } catch (e) {
    showMessage('Error disconnecting: ' + e, true);
  }
};

const connectGoogleCalendar = async () => {
  try {
    await saveSettingsInternal();
    const res = await googleCalendarLogin();
    window.location.href = res.url;
  } catch (e) {
    showMessage('Failed to connect Google Calendar: ' + e, true);
  }
};

const logoutGoogleCalendar = async () => {
  if (
    !(await confirmDialog.value.open(
      'Are you sure you want to disconnect Google Calendar?'
    ))
  )
    return;
  try {
    await googleCalendarLogout();
    form.google_calendar_connected = 'false';
    calendarConnected.value = false;
    calendars.value = [];
    showMessage('Disconnected Google Calendar.');
    await store.fetchSettings();
  } catch (e) {
    showMessage('Error disconnecting: ' + e, true);
  }
};

const testSynology = async () => {
  await saveSettingsInternal();
  try {
    await synologyStore.testConnection(form.synology_otp_code);
    showMessage('Connection Successful!');
    form.synology_otp_code = '';
    // Store updates settings internally, but we need to update form
    form.synology_sid = store.settings.synology_sid;
  } catch (e: any) {
    const err = e.response?.data?.error || 'Unknown error';
    if (err.includes('code: 403')) {
      showMessage(
        '2FA Required! Please enter OTP code and Test Connection again.',
        true
      );
    } else {
      showMessage('Connection Failed: ' + err, true);
    }
  }
};

const logoutSynology = async () => {
  if (
    !(await confirmDialog.value.open(
      'Are you sure you want to disconnect Synology?'
    ))
  )
    return;
  try {
    await synologyStore.logout();
    form.synology_sid = '';
    showMessage('Logged out from Synology.');
  } catch (e) {
    showMessage('Error logging out: ' + e, true);
  }
};

const loadAlbums = async () => {
  await saveSettingsInternal();
  try {
    await synologyStore.fetchAlbums();
    form.albums = synologyStore.albums;
    showMessage('Albums loaded!');
  } catch (e: any) {
    if (
      e.message === 'Session expired' ||
      (e.response && e.response.status === 401)
    ) {
      showMessage(
        'Session expired or Unauthorized. Please check login/settings.',
        true
      );
    } else {
      showMessage(
        'Failed to load albums: ' + (e.response?.data?.error || e.message),
        true
      );
    }
  }
};

const syncSynology = async () => {
  await saveSettingsInternal();
  try {
    await synologyStore.sync();
    showMessage('Sync started/completed successfully!');
  } catch (e: any) {
    if (e.response && e.response.status === 401) {
      showMessage('Session expired. Please reconnect.', true);
    } else {
      showMessage(
        'Sync Failed: ' + (e.response?.data?.error || 'Unknown error'),
        true
      );
    }
  }
};

const clearSynology = async () => {
  if (
    !(await confirmDialog.value.open(
      'Are you sure you want to clear all Synology photo references? Local files will not be deleted.'
    ))
  )
    return;

  try {
    await api.post('/synology/clear');
    showMessage('All Synology photos cleared from database.');
    await synologyStore.fetchCount();
  } catch (e: any) {
    showMessage(
      'Clear Failed: ' + (e.response?.data?.error || e.message),
      true
    );
  }
};

const testImmich = async () => {
  try {
    await saveSettingsInternal();
    await immichStore.testConnection();
    immichConnected.value = true;
    showMessage('Connection Successful!');
  } catch (e: any) {
    showMessage(
      'Connection Failed: ' + (e.response?.data?.error || e.message),
      true
    );
  }
};

const disconnectImmich = async () => {
  if (
    !(await confirmDialog.value.open(
      'Disconnect Immich? This also removes the photos synced from Immich.'
    ))
  )
    return;
  form.immich_url = '';
  form.immich_api_key = '';
  form.immich_source_mode = 'album';
  form.immich_album_id = '';
  form.immich_albums = [];
  form.immich_auto_sync_enabled = false;
  try {
    await saveSettingsInternal();
    // Drop the synced photo references too, otherwise the Immich albums stay
    // available after disconnecting.
    await api.post('/immich/clear');
    immichConnected.value = false;
    immichStore.count = 0;
    immichStore.albums = [];
    showMessage('Disconnected from Immich.');
  } catch (e: any) {
    showMessage(
      'Failed to disconnect: ' + (e.response?.data?.error || e.message),
      true
    );
  }
};

const loadImmichAlbums = async () => {
  await saveSettingsInternal();
  try {
    await immichStore.fetchAlbums();
    form.immich_albums = immichStore.albums;
    showMessage('Albums loaded!');
  } catch (e: any) {
    showMessage(
      'Failed to load albums: ' + (e.response?.data?.error || e.message),
      true
    );
  }
};

const syncImmich = async () => {
  await saveSettingsInternal();
  try {
    await immichStore.sync();
    showMessage('Sync completed successfully!');
  } catch (e: any) {
    showMessage(
      'Sync Failed: ' + (e.response?.data?.error || 'Unknown error'),
      true
    );
  }
};

const clearImmich = async () => {
  if (
    !(await confirmDialog.value.open(
      'Are you sure you want to clear all Immich photo references?'
    ))
  )
    return;
  try {
    await api.post('/immich/clear');
    showMessage('All Immich photos cleared from database.');
    await immichStore.fetchCount();
  } catch (e: any) {
    showMessage(
      'Clear Failed: ' + (e.response?.data?.error || e.message),
      true
    );
  }
};

// Token Management
const generatedToken = ref('');
const newTokenName = ref('');
const newTokenDeviceId = ref<number | null>(null);

const copyToken = async () => {
  try {
    await navigator.clipboard.writeText(generatedToken.value);
    showMessage('Token copied to clipboard!');
  } catch (e) {
    // Fallback for non-secure contexts could be implemented here given time
    showMessage(
      'Failed to copy token automatically. Please copy manually.',
      true
    );
  }
};

// Password Change
const showAccountForm = ref(false);
const accountForm = reactive({
  oldPassword: '',
  newUsername: '',
  newPassword: '',
  confirmPassword: '',
});

const generateToken = async () => {
  if (!newTokenName.value) {
    showMessage('Please enter a name for the token.', true);
    return;
  }
  try {
    const token = await authStore.generateToken(
      newTokenName.value,
      newTokenDeviceId.value ?? undefined
    );
    generatedToken.value = token;
    newTokenName.value = '';
    newTokenDeviceId.value = null;
    showMessage('Token generated!');
  } catch (e: any) {
    showMessage(
      'Failed to generate token: ' + (e.response?.data?.error || e.message),
      true
    );
  }
};

const updateTokenDevice = async (tokenId: number, deviceId: number | null) => {
  try {
    await authStore.updateTokenDevice(tokenId, deviceId);
    showMessage('Token device binding updated');
  } catch (e: any) {
    showMessage(
      'Failed to update token: ' + (e.response?.data?.error || e.message),
      true
    );
  }
};

const revokeToken = async (id: number) => {
  if (
    !(await confirmDialog.value.open(
      'Revoke this token? Device will lose access.'
    ))
  )
    return;
  try {
    await authStore.revokeToken(id);
    showMessage('Token revoked.');
  } catch (e: any) {
    showMessage('Failed: ' + e.message, true);
  }
};

const updateAccountSettings = async () => {
  if (!accountForm.oldPassword) {
    showMessage('Current password is required.', true);
    return;
  }
  if (!accountForm.newUsername && !accountForm.newPassword) {
    showMessage('Please provide a new username or password.', true);
    return;
  }
  if (accountForm.newPassword) {
    if (accountForm.newPassword !== accountForm.confirmPassword) {
      showMessage('New passwords do not match.', true);
      return;
    }
    if (accountForm.newPassword.length < 6) {
      showMessage('New password must be at least 6 characters.', true);
      return;
    }
  }

  try {
    await updateAccount(
      accountForm.oldPassword,
      accountForm.newUsername,
      accountForm.newPassword
    );
    accountForm.oldPassword = '';
    accountForm.newUsername = '';
    accountForm.newPassword = '';
    accountForm.confirmPassword = '';
    showMessage('Account updated successfully!');
  } catch (e: any) {
    showMessage('Failed: ' + (e.response?.data?.error || e.message), true);
  }
};

// Sessions
const sessions = ref<any[]>([]);

const loadSessions = async () => {
  try {
    sessions.value = await listSessions();
  } catch (e) {
    console.error('Failed to load sessions', e);
  }
};

const revokeSessionHandler = async (id: number) => {
  if (!confirm('Are you sure you want to revoke this session?')) return;
  try {
    await revokeSession(id);
    await loadSessions();
    showMessage('Session revoked');
  } catch (e: any) {
    showMessage('Failed: ' + (e.response?.data?.error || e.message), true);
  }
};

// Get image endpoint URL
// Always use direct add-on port for device access (ESP32 devices access directly, not via ingress)
const comfyuiWorkflowValid = computed(() => {
  if (!form.comfyui_workflow.trim()) return false;
  try {
    JSON.parse(form.comfyui_workflow);
    return true;
  } catch {
    return false;
  }
});

const onWorkflowFile = async (file: File | File[] | null) => {
  const f = Array.isArray(file) ? file[0] : file;
  if (!f) return;
  try {
    form.comfyui_workflow = await f.text();
    showMessage('Workflow loaded — click Save to store it.');
  } catch {
    showMessage('Could not read workflow file', true);
  }
};

const getImageUrl = (source: string) => {
  // 1) Explicit override (e.g. reverse-proxy URL) wins — used verbatim, no port
  //    is appended. Blank = auto-detect.
  const override = (form.device_image_base_url || '').trim();
  if (override) {
    return `${override.replace(/\/+$/, '')}/image/${source}`;
  }

  const { protocol, hostname, port } = window.location;
  // 2) Auto-detect whether the add-on port is needed by looking at the URL the
  //    WebUI is served from:
  //    - https:// almost always means a reverse proxy terminates TLS on its own
  //      port (443/custom) and forwards to us — trust that origin as-is.
  //    - an explicit non-add-on port already in the address bar (other than the
  //      HA ingress port 8123) is likewise a deliberate front-end → keep it.
  //    Otherwise fall back to the direct add-on port the ESP32 reaches.
  const addonPort = import.meta.env.VITE_ADDON_PORT || '9607';
  if (protocol === 'https:') {
    return `${window.location.origin}/image/${source}`;
  }
  if (port && port !== '8123' && port !== addonPort) {
    return `${window.location.origin}/image/${source}`;
  }
  return `${protocol}//${hostname}:${addonPort}/image/${source}`;
};

// Copy to clipboard
const copyToClipboard = async (text: string) => {
  try {
    await navigator.clipboard.writeText(text);
    showMessage('URL copied to clipboard!');
  } catch (e) {
    showMessage('Failed to copy to clipboard', true);
  }
};

const getDeviceFromUA = (ua: string) => {
  if (!ua) return 'Unknown Device';
  if (ua.includes('iPhone')) return 'iPhone';
  if (ua.includes('iPad')) return 'iPad';
  if (ua.includes('Macintosh')) return 'Mac';
  if (ua.includes('Windows')) return 'Windows';
  if (ua.includes('Android')) return 'Android';
  if (ua.includes('Linux')) return 'Linux';
  return 'Other Device';
};
</script>

<style scoped>
.color-swatch {
  height: 60px;
  border-bottom: 1px solid rgba(0, 0, 0, 0.12);
}
.comfyui-workflow :deep(textarea) {
  font-family: 'Roboto Mono', ui-monospace, monospace;
  font-size: 0.8rem;
  line-height: 1.4;
}

/* Live overlay placement preview */
.overlay-preview {
  position: relative;
  border-radius: 8px;
  overflow: hidden;
  background: linear-gradient(135deg, #6a7b8c 0%, #93a3b3 50%, #b9a48c 100%);
  box-shadow: inset 0 0 0 1px rgba(0, 0, 0, 0.2);
}
.overlay-preview .op-region {
  position: absolute;
  left: 6px;
  right: 6px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.overlay-preview .op-region-top {
  top: 6px;
}
.overlay-preview .op-region-bottom {
  bottom: 6px;
}
.overlay-preview .op-corner-row {
  display: grid;
  grid-template-columns: 1fr auto 1fr;
  align-items: start;
  gap: 6px;
}
.overlay-preview .op-region-bottom .op-corner-row {
  align-items: end;
}
.overlay-preview .op-slot {
  display: flex;
  flex-direction: column;
  gap: 3px;
  min-width: 0;
  max-width: 100%;
}
.overlay-preview .op-top-left,
.overlay-preview .op-bottom-left {
  justify-self: start;
  align-items: flex-start;
}
.overlay-preview .op-top-center,
.overlay-preview .op-bottom-center {
  justify-self: center;
  align-items: center;
}
.overlay-preview .op-top-right,
.overlay-preview .op-bottom-right {
  justify-self: end;
  align-items: flex-end;
}
.overlay-preview .op-wide {
  width: 100%;
  align-items: center;
}
.overlay-preview .op-wide .op-chip {
  max-width: 100%;
  justify-content: center;
  text-align: center;
  white-space: normal;
}
.overlay-preview .op-chip {
  display: flex;
  align-items: center;
  /* em-based like the renderer chip so the chip shrinks/grows proportionally
     with the (device-faithful) font size. */
  gap: 0.4em;
  padding: 0.25em 0.55em;
  border-radius: 0.4em;
  background: rgba(0, 0, 0, 0.45);
  color: #fff;
  line-height: 1.15;
  white-space: nowrap;
  font-weight: 600;
}
.overlay-preview .op-chip.low {
  color: #ffd6d6;
}
.overlay-preview .op-bat {
  position: relative;
  display: inline-block;
  width: 1.7em;
  height: 0.9em;
  box-sizing: border-box;
  border: 0.12em solid #fff;
  border-radius: 0.16em;
  padding: 0.1em;
  flex: none;
}
.overlay-preview .op-bat::after {
  content: '';
  position: absolute;
  right: -0.24em;
  top: 50%;
  transform: translateY(-50%);
  width: 0.13em;
  height: 0.42em;
  background: #fff;
  border-radius: 0 0.1em 0.1em 0;
}
.overlay-preview .op-bat-fill {
  display: block;
  height: 100%;
  background: #fff;
  border-radius: 0.04em;
}
.overlay-preview .op-chip.low .op-bat-fill {
  background: #e03b3b;
}
.battery-spark {
  width: 100%;
  height: 40px;
  display: block;
  opacity: 0.85;
}
</style>
