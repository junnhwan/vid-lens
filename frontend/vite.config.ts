import react from "@vitejs/plugin-react"
import { defineConfig } from "vitest/config"

// The running backend checks Origin against its public_origin. The browser
// talks only to this dev server; the proxy presents that origin upstream.
const backend = process.env.VITE_BACKEND_ORIGIN || "http://127.0.0.1:8086"

export default defineConfig({
  plugins: [react()],
  server: {
    host: "127.0.0.1",
    port: 5173,
    proxy: {
      "/api": {
        target: backend,
        changeOrigin: true,
        timeout: 0,
        proxyTimeout: 0,
        configure: (proxy) => {
          proxy.on("proxyReq", (proxyReq) => {
            proxyReq.setHeader("origin", backend)
          })
        },
      },
    },
  },
  preview: { host: "127.0.0.1", port: 4173 },
  test: {
    environment: "node",
    include: ["src/**/*.test.ts"],
  },
})
