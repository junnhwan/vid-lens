import http from 'node:http'
import assert from 'node:assert/strict'
import { createFrontendServer } from '../server.mjs'

async function listen(server) {
  await new Promise((resolve, reject) => server.once('error', reject).listen(0, '127.0.0.1', resolve))
  return server.address().port
}

let release
const upstream = http.createServer((request, response) => {
  if (request.url === '/api/video-probe') {
    assert.equal(request.headers.range, 'bytes=2-4')
    response.writeHead(206, { 'Content-Type': 'video/mp4', 'Content-Range': 'bytes 2-4/8' })
    response.end('234')
    return
  }
  assert.equal(request.method, 'POST')
  assert.equal(request.url, '/api/stream-probe')
  response.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache' })
  response.write('event: answer\ndata: "first"\n\n')
  release = () => response.end('event: done\ndata: {"answer":"firstsecond"}\n\n')
})
let proxy
try {
  const upstreamPort = await listen(upstream)
  proxy = createFrontendServer({ apiBase: `http://127.0.0.1:${upstreamPort}` })
  const proxyPort = await listen(proxy)
  const base = `http://127.0.0.1:${proxyPort}`
  const response = await fetch(`${base}/api/stream-probe`, { method: 'POST', signal: AbortSignal.timeout(5000) })
  assert.equal(response.status, 200)
  const reader = response.body.getReader()
  const first = await reader.read()
  assert.match(new TextDecoder().decode(first.value), /first/)
  assert.equal(first.done, false)
  release()
  while (!(await reader.read()).done) { /* drain terminal event */ }
  const media = await fetch(`${base}/api/video-probe`, { headers: { Range: 'bytes=2-4' } })
  assert.equal(media.status, 206)
  assert.equal(media.headers.get('content-range'), 'bytes 2-4/8')
  assert.equal(await media.text(), '234')
  assert.equal((await fetch(`${base}/dev/product`, { headers: { Accept: 'text/html' } })).status, 404)
  assert.equal((await fetch(`${base}/assets/missing.js`, { headers: { Accept: 'text/html' } })).status, 404)
  const deep = await fetch(`${base}/artifacts/example`, { headers: { Accept: 'text/html' } })
  assert.equal(deep.status, 200)
  assert.match(await deep.text(), /VidLens/)
  console.log('PASS: SSE first delta, media range, deep-link fallback, asset 404, preview 404')
} finally {
  release?.()
  proxy?.closeAllConnections()
  proxy?.close()
  upstream.closeAllConnections()
  upstream.close()
}
