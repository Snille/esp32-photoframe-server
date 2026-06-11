<template>
  <v-app>
    <v-app-bar color="primary" density="compact">
      <template #prepend>
        <AppLogo :size="32" class="ml-2" />
      </template>
      <v-app-bar-title class="ml-4">ESP32 PhotoFrame Server</v-app-bar-title>
      <template v-slot:append>
        <span
          v-if="serverVersion"
          class="text-caption text-medium-emphasis mr-2"
          title="Server version"
          >{{ serverVersion }}</span
        >
        <v-menu>
          <template #activator="{ props }">
            <v-btn
              icon="mdi-palette"
              variant="text"
              v-bind="props"
              title="Theme"
            ></v-btn>
          </template>
          <v-list density="compact" :selected="[currentTheme]">
            <v-list-item
              v-for="opt in themeOptions"
              :key="opt.value"
              :value="opt.value"
              :active="opt.value === currentTheme"
              @click="applyTheme(opt.value)"
            >
              <v-list-item-title>{{ opt.title }}</v-list-item-title>
            </v-list-item>
          </v-list>
        </v-menu>
        <v-btn
          v-if="authStore.isLoggedIn"
          variant="text"
          @click="authStore.logout"
          prepend-icon="mdi-logout"
        >
          Logout
        </v-btn>
      </template>
    </v-app-bar>

    <v-main>
      <v-container class="py-6" style="max-width: 1200px">
        <div
          v-if="authStore.loading && !authStore.isInitialized"
          class="d-flex justify-center align-center fill-height"
        >
          <v-progress-circular
            indeterminate
            color="primary"
            size="64"
          ></v-progress-circular>
        </div>

        <div v-else>
          <Setup v-if="!authStore.isInitialized" />
          <Login v-else-if="!authStore.isLoggedIn" />
          <Settings v-else />
        </div>
      </v-container>
    </v-main>
  </v-app>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { useTheme } from 'vuetify';
import AppLogo from './components/AppLogo.vue';
import Settings from './components/Settings.vue';
import Login from './components/Login.vue';
import Setup from './components/Setup.vue';
import { useAuthStore } from './stores/auth';
import { getStatus } from './api';
import {
  themeOptions,
  THEME_STORAGE_KEY,
  DEFAULT_THEME,
} from './plugins/vuetify';

const authStore = useAuthStore();
const theme = useTheme();
const currentTheme = ref(DEFAULT_THEME);
const serverVersion = ref('');

const applyTheme = (name: string) => {
  theme.global.name.value = name;
  currentTheme.value = name;
  localStorage.setItem(THEME_STORAGE_KEY, name);
};

onMounted(async () => {
  const saved = localStorage.getItem(THEME_STORAGE_KEY);
  if (saved && themeOptions.some((t) => t.value === saved)) {
    applyTheme(saved);
  }
  // Public endpoint — show the server version (also visible pre-login).
  try {
    const status = await getStatus();
    serverVersion.value = status?.version ?? '';
  } catch {
    serverVersion.value = '';
  }
  await authStore.checkStatus();
});
</script>
