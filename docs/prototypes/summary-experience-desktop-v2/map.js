/* 摘要导图：由当前版本的 blocks / concepts 派生，不另存导图内容 */
(function () {
  const { esc } = window.Core
  const cv = document.createElement('canvas').getContext('2d')
  const FONT = '"PingFang SC","Microsoft YaHei","Hiragino Sans GB",system-ui,sans-serif'
  const width = (text, size, weight) => { cv.font = `${weight} ${size}px ${FONT}`; return cv.measureText(text).width }

  function build(doc, opts = {}) {
    const folded = opts.folded || {}
    const changed = new Set(opts.changed || [])
    const ROW = 34, GAP = 14, PAD = 12
    const rootLabel = doc.mapTitle
    const rootW = width(rootLabel, 15, 700) + 30, rootH = 40
    const chs = doc.blocks.map((b, i) => {
      const label = `${i + 1}  ${b.short || b.title}`
      return { b, i, label, w: width(label, 13.5, 600) + 26, open: !folded[b.id], concepts: b.concepts.map(c => ({ c, w: width(c.label, 13, 500) + 22 })) }
    })
    const chW = Math.max(...chs.map(c => c.w))
    const x0 = 6, x1 = x0 + rootW + 46, x2 = x1 + chW + 52
    let y = PAD
    chs.forEach(c => {
      const n = c.open ? c.concepts.length : 0
      c.h = Math.max(1, n) * ROW
      c.top = y; c.cy = y + c.h / 2
      c.concepts.forEach((k, j) => { k.cy = y + j * ROW + ROW / 2 })
      y += c.h + GAP
    })
    const H = y - GAP + PAD
    const maxCW = Math.max(0, ...chs.filter(c => c.open).flatMap(c => c.concepts.map(k => k.w)))
    const W = Math.max(x1 + chW + 40, x2 + maxCW + 8)
    const ry = H / 2
    const color = i => `var(--map-${(i % 6) + 1})`
    const curve = (xa, ya, xb, yb) => { const m = (xa + xb) / 2; return `M${xa} ${ya} C${m} ${ya}, ${m} ${yb}, ${xb} ${yb}` }
    let links = '', nodes = ''
    chs.forEach(c => {
      links += `<path class="mm-link" d="${curve(x0 + rootW, ry, x1, c.cy)}" style="stroke:${color(c.i)}"/>`
      if (c.open) c.concepts.forEach(k => { links += `<path class="mm-link" d="${curve(x1 + c.w + 22, c.cy, x2, k.cy)}" style="stroke:${color(c.i)};opacity:.4"/>` })
    })
    nodes += `<g class="mm-node" role="button" tabindex="0" data-act="map-top" aria-label="回到概览"><rect class="box" x="${x0}" y="${ry - rootH / 2}" width="${rootW}" height="${rootH}" rx="12" style="fill:var(--acc);stroke:var(--acc)"/><text x="${x0 + rootW / 2}" y="${ry + 5}" text-anchor="middle" font-size="15" font-weight="700" style="fill:var(--acc-ink)">${esc(rootLabel)}</text></g>`
    chs.forEach(c => {
      const ch = changed.has(c.b.id) ? ' changed' : ''
      nodes += `<g class="mm-node${ch}" role="button" tabindex="0" data-act="map-go" data-block="${c.b.id}" aria-label="定位到第 ${c.i + 1} 章：${esc(c.b.title)}"><rect class="box" x="${x1}" y="${c.cy - 16}" width="${c.w}" height="32" rx="9" style="fill:var(--bg-2);stroke:${color(c.i)};stroke-width:1.3"/><text x="${x1 + 13}" y="${c.cy + 4.5}" font-size="13.5" font-weight="600" style="fill:var(--tx-1)">${esc(c.label)}</text></g>`
      const tx = x1 + c.w + 12
      nodes += `<g class="mm-toggle" role="button" tabindex="0" data-act="map-fold" data-block="${c.b.id}" aria-label="${c.open ? '收起' : '展开'}第 ${c.i + 1} 章的概念" aria-expanded="${c.open}"><circle cx="${tx}" cy="${c.cy}" r="8.5" style="stroke:${color(c.i)}"/><text x="${tx}" y="${c.cy + 3.8}" text-anchor="middle" style="fill:${color(c.i)}">${c.open ? '−' : c.concepts.length}</text></g>`
      if (c.open) c.concepts.forEach(k => {
        const kc = k.c.changed || changed.has(k.c.id) ? ' changed' : ''
        nodes += `<g class="mm-node${kc}" role="button" tabindex="0" data-act="map-go" data-block="${c.b.id}" data-para="${k.c.para}" aria-label="定位：${esc(k.c.label)}"><rect class="box" x="${x2}" y="${k.cy - 13}" width="${k.w}" height="26" rx="13" style="fill:transparent;stroke:var(--line-strong)"/><circle cx="${x2 + 11}" cy="${k.cy}" r="2.6" style="fill:${color(c.i)}"/><text x="${x2 + 19}" y="${k.cy + 4.3}" font-size="13" font-weight="500" style="fill:var(--tx-2)">${esc(k.c.label)}</text></g>`
      })
    })
    const n = 1 + chs.length + chs.reduce((s, c) => s + c.concepts.length, 0)
    return { svg: `<svg width="${Math.ceil(W)}" height="${Math.ceil(H)}" viewBox="0 0 ${Math.ceil(W)} ${Math.ceil(H)}" role="group" aria-label="摘要导图">${links}${nodes}</svg>`, count: n }
  }
  window.MindMap = { build }
})()
