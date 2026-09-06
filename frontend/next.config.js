/** @type {import('next').NextConfig} */
// dev 默认 /api → 本地 :8080；生产用 VIDLENS_API_BASE 指到后端（如 http://127.0.0.1:18083）
const backendUrl = process.env.VIDLENS_API_BASE || 'http://localhost:8080'

const nextConfig = {
  // rewrites 代理 /api → 后端;SSE 流式回答经常超过默认 30s,放宽代理超时
  experimental: { proxyTimeout: 180_000 },
  async rewrites() {
    return [
      { source: '/api/:path*', destination: `${backendUrl}/api/:path*` },
    ]
  },
}
module.exports = nextConfig
