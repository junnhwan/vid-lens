import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'node:path'

export default defineConfig(({ mode }) => {
  const uploadBase = (process.env.VITE_UPLOAD_API_BASE ?? loadEnv(mode, process.cwd()).VITE_UPLOAD_API_BASE)?.trim()
  if (uploadBase) {
    const url = new URL(uploadBase)
    if (url.protocol !== 'https:' || url.username || url.password || url.search || url.hash || url.pathname.replace(/\/+$/, '') !== '/api/v1') {
      throw new Error('VITE_UPLOAD_API_BASE must be an HTTPS URL ending in /api/v1 without credentials, query or fragment')
    }
  }
  return {
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
  }
})
