import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';
import { resolve } from 'node:path';

// Single-binary target: the production build is emitted into server/web/dist,
// where the Go binary embeds it via go:embed. In dev, /api is proxied to the Go
// server so the canvas drives the real backend.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { '@': resolve(__dirname, 'src') },
  },
  server: {
    port: 3000,
    proxy: {
      '/api': {
        target: process.env.GO_API_URL ?? 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: resolve(__dirname, '../../server/web/dist'),
    // Keep the committed .gitkeep (which keeps `go:embed dist` compiling) intact
    // across rebuilds; assets are content-hashed so stale chunks are harmless.
    emptyOutDir: false,
  },
});
