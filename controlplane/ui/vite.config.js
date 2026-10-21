import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

const apiTarget = process.env.CP_API_PROXY || 'http://localhost:8080';

export default defineConfig({
  plugins: [svelte()],
  server: {
    port: 5173,
    proxy: {
      '/v1': apiTarget,
      '/healthz': apiTarget,
      '/readyz': apiTarget
    }
  },
  preview: {
    port: 4173,
    proxy: {
      '/v1': apiTarget,
      '/healthz': apiTarget,
      '/readyz': apiTarget
    }
  }
});
