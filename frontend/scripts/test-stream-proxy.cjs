// Integration regression: exercise the real Next rewrite and configuration.
// The upstream is forbidden to finish until its first event reaches fetch.
const http = require('node:http')
const path = require('node:path')
const assert = require('node:assert/strict')

async function listen(server) {
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve))
  return server.address().port
}

async function main() {
  let release
  const upstream = http.createServer((request, response) => {
    response.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache' })
    response.write('event: answer\ndata: "first"\n\n')
    release = () => response.end('event: done\ndata: {"answer":"firstsecond"}\n\n')
  })
  const port = await listen(upstream)
  process.env.VIDLENS_API_BASE = `http://127.0.0.1:${port}`
  const config = require('../next.config.js')
  const app = require('next')({ dev: true, dir: path.resolve(__dirname, '..'), conf: { ...config, distDir: '.next-stream-test' } })
  let proxy
  try {
    await app.prepare()
    proxy = http.createServer(app.getRequestHandler())
    const proxyPort = await listen(proxy)
    const started = Date.now()
    const response = await fetch(`http://127.0.0.1:${proxyPort}/api/stream-probe`, { method: 'POST', signal: AbortSignal.timeout(5000) })
    const reader = response.body.getReader()
    const chunk = await reader.read()
    assert.match(new TextDecoder().decode(chunk.value), /first/)
    console.log(`PASS: first SSE delta arrived before upstream completion (${Date.now() - started}ms)`)
    release()
    while (!(await reader.read()).done) { /* drain terminal event */ }
  } finally {
    release?.()
    proxy?.closeAllConnections()
    proxy?.close()
    upstream.closeAllConnections()
    upstream.close()
    await app.close()
  }
}
main().then(() => process.exit(0), error => { console.error(error.message); process.exit(1) })
