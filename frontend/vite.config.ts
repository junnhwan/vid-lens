import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'node:path'

export default defineConfig({
  plugins: [react()],
  resolve: { alias: { '@': path.resolve(__dirname) } },
  server: {
    proxy: {
      '/api': {
        target: process.env.VIDLENS_API_BASE || 'http://127.0.0.1:8080',
        changeOrigin: true,
        timeout: 960_000,
        proxyTimeout: 960_000,
      },
    },
  },
})
