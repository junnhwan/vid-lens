import { createServer as createHttpServer, request as httpRequest } from 'node:http'
import { request as httpsRequest } from 'node:https'
import { createReadStream, existsSync, readFileSync, statSync } from 'node:fs'
import { resolve, sep, extname } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'

const root = resolve(fileURLToPath(new URL('.', import.meta.url)), 'dist')
const runtimeApiBase = resolve(fileURLToPath(new URL('.', import.meta.url)), '.api-base')
const mime = {
  '.html': 'text/html; charset=utf-8', '.js': 'text/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8', '.svg': 'image/svg+xml', '.png': 'image/png',
  '.jpg': 'image/jpeg', '.jpeg': 'image/jpeg', '.webp': 'image/webp',
  '.mp4': 'video/mp4',
  '.ico': 'image/x-icon', '.woff': 'font/woff', '.woff2': 'font/woff2',
  '.json': 'application/json; charset=utf-8', '.map': 'application/json; charset=utf-8',
}
const hopHeaders = new Set(['connection', 'keep-alive', 'proxy-authenticate', 'proxy-authorization', 'te', 'trailer', 'transfer-encoding', 'upgrade'])

function proxyApi(request, response, apiBase) {
  let upstream
  try { upstream = new URL(request.url, apiBase) } catch { response.writeHead(502).end('Invalid API upstream'); return }
  const headers = Object.fromEntries(Object.entries(request.headers).filter(([name]) => !hopHeaders.has(name)))
  headers.host = upstream.host
  headers['accept-encoding'] = 'identity'
  const send = upstream.protocol === 'https:' ? httpsRequest : httpRequest
  const outgoing = send(upstream, { method: request.method, headers, timeout: 960_000 }, incoming => {
    const responseHeaders = Object.fromEntries(Object.entries(incoming.headers).filter(([name]) => !hopHeaders.has(name)))
    response.writeHead(incoming.statusCode || 502, responseHeaders)
    incoming.pipe(response)
  })
  outgoing.on('timeout', () => outgoing.destroy(new Error('API upstream timed out')))
  outgoing.on('error', error => {
    if (!response.headersSent) response.writeHead(502, { 'Content-Type': 'text/plain; charset=utf-8' })
    response.end(`API upstream unavailable: ${error.message}`)
  })
  response.on('close', () => outgoing.destroy())
  request.pipe(outgoing)
}

function serveFile(request, response, filePath) {
  const info = statSync(filePath)
  const isVideo = extname(filePath) === '.mp4'
  const headers = {
    'Content-Type': mime[extname(filePath)] || 'application/octet-stream',
    'Content-Length': info.size,
    'Cache-Control': filePath.includes(`${sep}assets${sep}`) ? 'public, max-age=31536000, immutable' : 'no-cache',
    'X-Content-Type-Options': 'nosniff',
  }
  if (isVideo) headers['Accept-Ranges'] = 'bytes'
  if (isVideo && request.headers.range) {
    const match = /^bytes=(\d*)-(\d*)$/.exec(request.headers.range)
    const first = match?.[1]
    const last = match?.[2]
    const suffix = first === '' && last ? Number(last) : null
    const start = suffix !== null ? Math.max(0, info.size - suffix) : Number(first)
    const end = suffix !== null || last === '' ? info.size - 1 : Math.min(Number(last), info.size - 1)
    if (!match || (!first && !last) || !Number.isSafeInteger(start) || !Number.isSafeInteger(end) || start < 0 || start > end || start >= info.size) {
      response.writeHead(416, { 'Content-Range': `bytes */${info.size}`, 'Accept-Ranges': 'bytes' }).end()
      return
    }
    response.writeHead(206, { ...headers, 'Content-Length': end - start + 1, 'Content-Range': `bytes ${start}-${end}/${info.size}` })
    if (request.method === 'HEAD') response.end()
    else createReadStream(filePath, { start, end }).pipe(response)
    return
  }
  response.writeHead(200, headers)
  if (request.method === 'HEAD') response.end()
  else createReadStream(filePath).pipe(response)
}

export function createFrontendServer({ apiBase = process.env.VIDLENS_API_BASE || (existsSync(runtimeApiBase) ? readFileSync(runtimeApiBase, 'utf8').trim() : 'http://127.0.0.1:8080'), dist = root } = {}) {
  return createHttpServer((request, response) => {
    const path = new URL(request.url, 'http://localhost').pathname
    if (path === '/api' || path.startsWith('/api/')) { proxyApi(request, response, apiBase); return }
    if (['/dev/product', '/dev/motion'].some(route => path === route || path.startsWith(`${route}/`))) { response.writeHead(404).end('Not found'); return }
    if (request.method !== 'GET' && request.method !== 'HEAD') { response.writeHead(405).end('Method not allowed'); return }
    let decoded
    try { decoded = decodeURIComponent(path) } catch { response.writeHead(400).end('Bad path'); return }
    const candidate = resolve(dist, `.${decoded}`)
    if (candidate !== dist && !candidate.startsWith(dist + sep)) { response.writeHead(403).end('Forbidden'); return }
    const file = existsSync(candidate) && statSync(candidate).isFile() ? candidate : null
    if (file) { serveFile(request, response, file); return }
    if (extname(candidate) || !(request.headers.accept || '').includes('text/html')) { response.writeHead(404).end('Not found'); return }
    serveFile(request, response, resolve(dist, 'index.html'))
  })
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  const port = Number(process.env.PORT || 3000)
  createFrontendServer().listen(port, '0.0.0.0', () => console.log(`VidLens frontend listening on ${port}`))
}
