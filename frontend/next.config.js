/** @type {import('next').NextConfig} */
// dev 默认 /api → 本地 :8080；生产用 VIDLENS_API_BASE 指到后端（如 http://127.0.0.1:18083）
const backendUrl = process.env.VIDLENS_API_BASE || 'http://localhost:8080'

const nextConfig = {
  // Next's compression buffers small SSE writes from rewrites until the
  // upstream completes. Keep proxied conversation deltas immediately visible.
  compress: false,
  // dev 与 build 分离构建目录:next dev 固定 NODE_ENV=development,走 .next-dev;
  // next build / next start 固定 production,走 .next(deploy 脚本按 .next 打包)。
  // 这样 dev server 开着时跑 npm run build 不会互相覆盖产物(否则 dev 端会报
  // "Cannot find module './xxx.js'")。
  distDir: process.env.NODE_ENV === 'production' ? '.next' : '.next-dev',
  // rewrites 代理 /api → 后端;SSE 流式回答经常超过默认 30s,放宽代理超时
  experimental: { proxyTimeout: 960_000 },
  async rewrites() {
    return [
      { source: '/api/:path*', destination: `${backendUrl}/api/:path*` },
    ]
  },
}
module.exports = nextConfig
