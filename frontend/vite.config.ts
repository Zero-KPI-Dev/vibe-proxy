import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { fileURLToPath, URL } from 'node:url'

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/admin': 'http://localhost:8080',
      '/healthz': 'http://localhost:8080',
      '/v1': 'http://localhost:8080',
      '/anthropic': 'http://localhost:8080',
    },
  },
  build: {
    outDir: '../internal/runtime/web/dist',
    emptyOutDir: true,
  },
})
