/** @type {import('next').NextConfig} */
// dev 默认 /api → 本地 :8080；生产用 VIDLENS_API_BASE 指到后端（如 http://127.0.0.1:18083）
const backendUrl = process.env.VIDLENS_API_BASE || 'http://localhost:8080'

const nextConfig = {
  async rewrites() {
    return [
      { source: '/api/:path*', destination: `${backendUrl}/api/:path*` },
    ]
  },
}
module.exports = nextConfig
