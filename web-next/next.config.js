/** @type {import('next').NextConfig} */
const nextConfig = {
  async rewrites() {
    return [
      // dev 时代理 /api → 后端 :8080；prod 用同源反代或直连
      { source: '/api/:path*', destination: 'http://localhost:8080/api/:path*' },
    ]
  },
}
module.exports = nextConfig
