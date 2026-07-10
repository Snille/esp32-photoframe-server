import { defineStore } from 'pinia';
import { api } from '../api';

export interface ImmichServer {
  id: number;
  label: string;
  url: string;
  api_key?: string;
  enabled: boolean;
}

export const useImmichStore = defineStore('immich', {
  state: () => ({
    count: 0,
    // albums are AlbumInfo: { id, name, server_id, server_label }
    albums: [] as any[],
    servers: [] as ImmichServer[],
    loading: false,
    error: null as string | null,
  }),
  actions: {
    async fetchServers() {
      try {
        const res = await api.get('/immich/servers');
        this.servers = res.data || [];
      } catch (e: any) {
        console.error('Failed to fetch Immich servers', e);
      }
    },

    async createServer(payload: Partial<ImmichServer>) {
      await api.post('/immich/servers', payload);
      await this.fetchServers();
    },

    async updateServer(id: number, payload: Partial<ImmichServer>) {
      await api.put(`/immich/servers/${id}`, payload);
      await this.fetchServers();
    },

    async deleteServer(id: number) {
      await api.delete(`/immich/servers/${id}`);
      await this.fetchServers();
    },

    // Test a saved server by id.
    async testServer(id: number) {
      const res = await api.post(`/immich/servers/${id}/test`);
      return res.data;
    },

    // Test arbitrary credentials before saving (add/edit form).
    async testCredentials(url: string, apiKey: string) {
      const res = await api.post('/immich/test', { url, api_key: apiKey });
      return res.data;
    },

    async fetchCount() {
      try {
        const res = await api.get('/immich/count');
        this.count = res.data.count || 0;
      } catch (e: any) {
        console.error('Failed to fetch Immich photo count', e);
      }
    },

    async fetchAlbums() {
      this.loading = true;
      this.error = null;
      try {
        const res = await api.get('/immich/albums');
        this.albums = res.data;
      } catch (e: any) {
        this.error = e.response?.data?.error || e.message;
        throw e;
      } finally {
        this.loading = false;
      }
    },

    // Test the default server's credentials. Pass the entered url + key so the
    // check hits exactly what's on the form (the /immich/test endpoint tests the
    // credentials in the body).
    async testConnection(url?: string, apiKey?: string) {
      this.loading = true;
      try {
        const body = url && apiKey ? { url, api_key: apiKey } : {};
        const res = await api.post('/immich/test', body);
        await this.fetchCount();
        await this.fetchServers();
        await this.fetchAlbums().catch(() => {});
        return res.data;
      } catch (e: any) {
        throw e;
      } finally {
        this.loading = false;
      }
    },

    async sync() {
      this.loading = true;
      try {
        await api.post('/immich/sync');
        await this.fetchCount();
      } catch (e: any) {
        throw e;
      } finally {
        this.loading = false;
      }
    },

    async resync() {
      this.loading = true;
      try {
        await api.post('/immich/resync');
        await this.fetchCount();
      } catch (e: any) {
        throw e;
      } finally {
        this.loading = false;
      }
    },
  },
});
