/* 映知 VidLens · 摘要体验原型 · 核心状态与模拟引擎（无网络请求） */
(function () {
  const D = window.DEMO
  const $ = (sel, root = document) => root.querySelector(sel)
  const $$ = (sel, root = document) => Array.from(root.querySelectorAll(sel))
  const clone = o => JSON.parse(JSON.stringify(o))
  const esc = s => String(s ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]))
  const fmt = sec => { sec = Math.max(0, Math.floor(sec || 0)); const h = Math.floor(sec / 3600), m = Math.floor(sec % 3600 / 60), s = sec % 60; return (h ? h + ':' + String(m).padStart(2, '0') : String(m).padStart(2, '0')) + ':' + String(s).padStart(2, '0') }
  const ago = ts => { const d = Date.now() - ts; if (d < 60e3) return '刚刚'; if (d < 3600e3) return Math.floor(d / 60e3) + ' 分钟前'; if (d < 864e5) return Math.floor(d / 3600e3) + ' 小时前'; if (d < 30 * 864e5) return Math.floor(d / 864e5) + ' 天前'; return new Date(ts).toLocaleDateString('zh-CN') }
  const clock = ts => new Date(ts).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
  const isMobile = () => matchMedia('(max-width: 960px)').matches
  const uid = p => p + Math.random().toString(36).slice(2, 8)

  /* ---------- Icons (Tabler Icons, MIT) ---------- */
  const ICONS = {
    home: '<path d="M5 12l-2 0l9 -9l9 9l-2 0"/><path d="M5 12v7a2 2 0 0 0 2 2h10a2 2 0 0 0 2 -2v-7"/><path d="M9 21v-6a2 2 0 0 1 2 -2h2a2 2 0 0 1 2 2v6"/>',
    video: '<path d="M15 10l4.55 -2.27a1 1 0 0 1 1.45 .9v6.74a1 1 0 0 1 -1.45 .9l-4.55 -2.29"/><rect x="3" y="6" width="12" height="12" rx="2"/>',
    folder: '<path d="M5 4h4l3 5h7a2 2 0 0 1 2 2v8a2 2 0 0 1 -2 2h-14a2 2 0 0 1 -2 -2v-13a2 2 0 0 1 2 -2z"/>',
    message: '<path d="M8 9h8"/><path d="M8 13h5"/><path d="M12 21a9 9 0 1 0 -9 -9c0 1.6.4 3 1.2 4.3l-1.2 4.7l4.8 -1.2a9 9 0 0 0 4.2 1.2z"/>',
    send: '<path d="M10 14l11 -11"/><path d="M21 3l-6.5 18a.55 .55 0 0 1 -1 0l-3.5 -7l-7 -3.5a.55 .55 0 0 1 0 -1l18 -6.5"/>',
    play: '<path d="M7 5v14l12 -7z" fill="currentColor" stroke="none"/>',
    pause: '<rect x="6" y="5" width="4" height="14" rx="1" fill="currentColor" stroke="none"/><rect x="14" y="5" width="4" height="14" rx="1" fill="currentColor" stroke="none"/>',
    clock: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 3"/>',
    search: '<circle cx="10" cy="10" r="7"/><path d="M21 21l-6 -6"/>',
    upload: '<path d="M4 17v2a2 2 0 0 0 2 2h12a2 2 0 0 0 2 -2v-2"/><path d="M7 9l5 -5l5 5"/><path d="M12 4v12"/>',
    link: '<path d="M9 15l6 -6"/><path d="M11 6l.5 -.5a3.5 3.5 0 0 1 5 5l-.5 .5"/><path d="M13 18l-.5 .5a3.5 3.5 0 0 1 -5 -5l.5 -.5"/>',
    x: '<path d="M18 6l-12 12"/><path d="M6 6l12 12"/>',
    menu: '<path d="M4 6l16 0"/><path d="M4 12l16 0"/><path d="M4 18l16 0"/>',
    check: '<path d="M5 12l5 5l10 -11"/>',
    alert: '<path d="M12 9v4"/><path d="M12 15h.01"/><path d="M10.24 3.957l-8.422 14.06a1.989 1.989 0 0 0 1.7 2.983h16.845a1.989 1.989 0 0 0 1.7 -2.983l-8.423 -14.06a1.989 1.989 0 0 0 -3.4 0z"/>',
    'chev-r': '<path d="M9 6l6 6l-6 6"/>', 'chev-l': '<path d="M15 6l-6 6l6 6"/>', 'chev-d': '<path d="M6 9l6 6l6 -6"/>',
    plus: '<path d="M12 5v14"/><path d="M5 12h14"/>',
    file: '<path d="M14 3v4a1 1 0 0 0 1 1h4"/><path d="M17 21h-10a2 2 0 0 1 -2 -2v-14a2 2 0 0 1 2 -2h7l5 5v11a2 2 0 0 1 -2 2z"/><path d="M9 13l6 0"/><path d="M9 17l6 0"/>',
    refresh: '<path d="M20 11a8.1 8.1 0 0 0 -15.5 -2"/><path d="M4 5v4h4"/><path d="M4 13a8.1 8.1 0 0 0 15.5 2"/><path d="M20 19v-4h-4"/>',
    bulb: '<path d="M9 18h6"/><path d="M10 22h4"/><path d="M12 2a7 7 0 0 1 4 12.7c-.6.5-1 1.4-1 2.3h-6c0-.9-.4-1.8-1-2.3a7 7 0 0 1 4-12.7z"/>',
    layers: '<path d="M12 3l9 4.5l-9 4.5l-9 -4.5l9 -4.5"/><path d="M3 12l9 4.5l9 -4.5"/><path d="M3 16.5l9 4.5l9 -4.5"/>',
    photo: '<rect x="3" y="5" width="18" height="14" rx="2"/><circle cx="9" cy="10" r="1.6"/><path d="M7 19l5.5 -6l3 3.2l2 -2.2l3.5 5"/>',
    target: '<circle cx="12" cy="12" r="9"/><circle cx="12" cy="12" r="5"/><circle cx="12" cy="12" r="1"/>',
    filter: '<path d="M4 4h16l-6 8v6l-4 -2v-4l-6 -8"/>',
    trash: '<path d="M4 7h16"/><path d="M10 11v6"/><path d="M14 11v6"/><path d="M5 7l1 12a2 2 0 0 0 2 2h8a2 2 0 0 0 2 -2l1 -12"/><path d="M9 7V4h6v3"/>',
    download: '<path d="M4 17v2a2 2 0 0 0 2 2h12a2 2 0 0 0 2 -2v-2"/><path d="M7 11l5 5l5 -5"/><path d="M12 4v12"/>',
    info: '<circle cx="12" cy="12" r="9"/><path d="M12 8h.01"/><path d="M11.5 12h.5v4h1"/>',
    pencil: '<path d="M4 20h4l10.5 -10.5a2.1 2.1 0 0 0 -3 -3l-10.5 10.5v4z"/><path d="M13.5 6.5l3 3"/>',
    dots: '<path d="M5 12h.01"/><path d="M12 12h.01"/><path d="M19 12h.01"/>',
    list: '<path d="M9 6h11"/><path d="M9 12h11"/><path d="M9 18h11"/><path d="M5 6h.01"/><path d="M5 12h.01"/><path d="M5 18h.01"/>',
    wand: '<path d="M6 21l15 -15"/><path d="M15 4l1.5 1.5"/><path d="M9 4l.75 .75"/><path d="M4.5 8.5l.75 .75"/><path d="M7 3l.5 .5"/><path d="M18 13l.5 .5"/>',
    sun: '<circle cx="12" cy="12" r="4"/><path d="M3 12h1m8 -9v1m8 8h1m-9 8v1m-6.4 -15.4l.7 .7m12.1 -.7l-.7 .7m0 11.4l.7 .7m-12.1 -.7l-.7 .7"/>',
    moon: '<path d="M12 3c.132 0 .263 0 .393 0a7.5 7.5 0 0 0 7.92 12.446a9 9 0 1 1 -8.313 -12.454z"/>',
    'arrow-r': '<path d="M5 12h14"/><path d="M13 6l6 6l-6 6"/>',
    sparkles: '<path d="M16 18a2 2 0 0 1 2 2a2 2 0 0 1 2 -2a2 2 0 0 1 -2 -2a2 2 0 0 1 -2 2zm0 -12a2 2 0 0 1 2 2a2 2 0 0 1 2 -2a2 2 0 0 1 -2 -2a2 2 0 0 1 -2 2zm-7 12a6 6 0 0 1 6 -6a6 6 0 0 1 -6 -6a6 6 0 0 1 -6 6a6 6 0 0 1 6 6z"/>',
    map: '<rect x="3" y="9" width="6" height="6" rx="1.5"/><rect x="15" y="3" width="6" height="5" rx="1.5"/><rect x="15" y="16" width="6" height="5" rx="1.5"/><path d="M9 12h3m0 -6.5v13m0 -13h3m-3 13h3"/>',
    text: '<path d="M4 6h16"/><path d="M4 12h10"/><path d="M4 18h14"/>',
    grid: '<rect x="4" y="4" width="7" height="7" rx="1.5"/><rect x="13" y="4" width="7" height="7" rx="1.5"/><rect x="4" y="13" width="7" height="7" rx="1.5"/><rect x="13" y="13" width="7" height="7" rx="1.5"/>',
    copy: '<rect x="8" y="8" width="12" height="12" rx="2"/><path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2"/>',
    undo: '<path d="M9 14l-4 -4l4 -4"/><path d="M5 10h11a4 4 0 1 1 0 8h-1"/>',
    history: '<path d="M12 8v4l2 2"/><path d="M3.05 11a9 9 0 1 1 .5 4m-.5 5v-5h5"/>',
    tag: '<circle cx="7.5" cy="7.5" r="1"/><path d="M3 6v5.172a2 2 0 0 0 .586 1.414l7.71 7.71a2.41 2.41 0 0 0 3.408 0l5.592 -5.592a2.41 2.41 0 0 0 0 -3.408l-7.71 -7.71a2 2 0 0 0 -1.414 -.586h-5.172a3 3 0 0 0 -3 3z"/>',
    quote: '<path d="M10 11h-4a1 1 0 0 1 -1 -1v-3a1 1 0 0 1 1 -1h3a1 1 0 0 1 1 1v6c0 2.667 -1.333 4.333 -4 5"/><path d="M19 11h-4a1 1 0 0 1 -1 -1v-3a1 1 0 0 1 1 -1h3a1 1 0 0 1 1 1v6c0 2.667 -1.333 4.333 -4 5"/>',
    panel: '<rect x="4" y="4" width="16" height="16" rx="2"/><path d="M9 4v16"/>',
    external: '<path d="M12 6h-6a2 2 0 0 0 -2 2v10a2 2 0 0 0 2 2h10a2 2 0 0 0 2 -2v-6"/><path d="M11 13l9 -9"/><path d="M15 4h5v5"/>',
    merge: '<circle cx="7" cy="18" r="2"/><circle cx="7" cy="6" r="2"/><circle cx="17" cy="12" r="2"/><path d="M7 8v8"/><path d="M7 8a4 4 0 0 0 4 4h4"/>',
    type: '<path d="M4 20l3 0"/><path d="M14 20l7 0"/><path d="M6.9 15l6.9 0"/><path d="M10.2 6.3l5.8 13.7"/><path d="M5 20l6 -16l2 0l7 16"/>',
    zoom: '<circle cx="10" cy="10" r="7"/><path d="M7 10h6M10 7v6M21 21l-6-6"/>',
    subtitles: '<rect x="3" y="5" width="18" height="14" rx="2"/><path d="M7 15h4M15 15h2M7 11h2M13 11h4"/>',
    book: '<path d="M3 19a9 9 0 0 1 9 0a9 9 0 0 1 9 0"/><path d="M3 6a9 9 0 0 1 9 0a9 9 0 0 1 9 0"/><path d="M3 6v13M12 6v13M21 6v13"/>',
    clipboard: '<path d="M9 5h-2a2 2 0 0 0 -2 2v12a2 2 0 0 0 2 2h10a2 2 0 0 0 2 -2v-12a2 2 0 0 0 -2 -2h-2"/><rect x="9" y="3" width="6" height="4" rx="2"/>',
    flask: '<path d="M9 3h6M10 9h4M10 3v6l-4.5 9a2 2 0 0 0 1.8 3h9.4a2 2 0 0 0 1.8-3L14 9V3"/>',
    maximize: '<path d="M8 3H5a2 2 0 0 0-2 2v3m13-5h3a2 2 0 0 1 2 2v3M3 16v3a2 2 0 0 0 2 2h3m8 0h3a2 2 0 0 0 2-2v-3"/>',
    skip: '<path d="M5 12h14M13 6l6 6l-6 6"/>',
    eye: '<path d="M10 12a2 2 0 1 0 4 0a2 2 0 0 0 -4 0"/><path d="M21 12c-2.4 4-5.4 6-9 6c-3.6 0-6.6-2-9-6c2.4-4 5.4-6 9-6c3.6 0 6.6 2 9 6"/>'
  }
  const sprite = '<svg xmlns="http://www.w3.org/2000/svg" style="display:none">' + Object.entries(ICONS).map(([k, v]) => `<symbol id="i-${k}" viewBox="0 0 24 24">${v}</symbol>`).join('') + '</svg>'
  const ic = (name, size) => `<svg class="ic${size ? ' ic-' + size : ''}" aria-hidden="true"><use href="#i-${name}"/></svg>`
  const brandMark = (s = 30) => `<svg width="${s}" height="${s}" viewBox="0 0 32 32" fill="none" aria-hidden="true"><rect width="32" height="32" rx="9" fill="var(--bg-3)" stroke="var(--acc-line)"/><path d="M16 5.6 L24.9 10.8 V21.2 L16 26.4 L7.1 21.2 V10.8 Z" stroke="var(--acc)" stroke-width="1.55" stroke-linejoin="round"/><path d="M16 9.2 L21.7 12.5 V19.5 L16 22.8 L10.3 19.5 V12.5 Z" stroke="var(--acc)" stroke-width="1.15" stroke-linejoin="round" opacity=".55"/><g stroke="var(--acc)" stroke-width="1.1" opacity=".45"><line x1="16" y1="5.6" x2="16" y2="9.2"/><line x1="24.9" y1="10.8" x2="21.7" y2="12.5"/><line x1="24.9" y1="21.2" x2="21.7" y2="19.5"/><line x1="16" y1="26.4" x2="16" y2="22.8"/><line x1="7.1" y1="21.2" x2="10.3" y2="19.5"/><line x1="7.1" y1="10.8" x2="10.3" y2="12.5"/></g><circle cx="16" cy="16" r="3.05" fill="var(--acc)"/><circle cx="14.6" cy="14.5" r="1" fill="#fff6d8"/></svg>`

  /* ---------- Markdown-lite ---------- */
  const timeBtn = (t, inline) => `<button class="time-btn${inline ? ' inline' : ''}" data-act="seek" data-t="${t}" aria-label="回放 ${fmt(t)}">${ic('play')}${fmt(t)}</button>`
  function inline(s) {
    s = esc(s)
    const codes = []
    s = s.replace(/`([^`]+)`/g, (_, c) => { codes.push(c); return '\u0000' + (codes.length - 1) + '\u0000' })
    s = s.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
    s = s.replace(/\{\{t:(\d+)\}\}/g, (_, t) => timeBtn(+t, true))
    return s.replace(/\u0000(\d+)\u0000/g, (_, i) => `<code>${codes[i]}</code>`)
  }
  function md(src) {
    return String(src).split(/\n{2,}/).map(chunk => {
      const lines = chunk.split('\n').filter(Boolean)
      if (!lines.length) return ''
      if (lines.every(l => /^- /.test(l))) return '<ul>' + lines.map(l => '<li>' + inline(l.slice(2)) + '</li>').join('') + '</ul>'
      if (lines.every(l => /^\d+\. /.test(l))) return '<ol>' + lines.map(l => '<li>' + inline(l.replace(/^\d+\. /, '')) + '</li>').join('') + '</ol>'
      if (lines.every(l => /^> ?/.test(l))) return '<blockquote>' + inline(lines.map(l => l.replace(/^> ?/, '')).join(' ')) + '</blockquote>'
      if (lines.every(l => /^\|/.test(l))) {
        const rows = lines.filter(l => !/^\|\s*-/.test(l)).map(l => l.replace(/^\||\|$/g, '').split('|').map(c => c.trim()))
        return '<table><thead><tr>' + rows[0].map(c => '<th>' + inline(c) + '</th>').join('') + '</tr></thead><tbody>' + rows.slice(1).map(r => '<tr>' + r.map(c => '<td>' + inline(c) + '</td>').join('') + '</tr>').join('') + '</tbody></table>'
      }
      return '<p>' + lines.map(inline).join('<br>') + '</p>'
    }).join('')
  }
  const plain = s => String(s).replace(/\{\{t:\d+\}\}/g, '').replace(/\*\*|`/g, '').replace(/^(- |\d+\. |> ?)/gm, '').replace(/\n+/g, ' ').trim()

  /* ---------- Store ---------- */
  const KEY = 'vidlens-summary-proto-public-activity-v1'
  function seed() {
    const now = Date.now()
    const tags = {}
    Object.entries(D.tags).forEach(([id, t]) => { tags[id] = { id, ...clone(t), protected: t.origin === 'manual' } })
    const videos = D.library.map(o => {
      const v = baseVideo({ id: o.id, title: o.title, kind: o.file ? 'file' : 'bili', up: o.up, bv: o.bv, file: o.file, size: o.size, duration: o.duration, thumb: o.thumb, createdAt: now - o.ago - 1200e3, focus: '' })
      v.updatedAt = now - o.ago
      v.template = o.id
      v.tagLinks = o.tags.map(([id, origin]) => ({ id, origin }))
      if (o.status === 'processing') {
        v.status = 'processing'; v.pipe = { stage: 'organize', organized: 1, startedAt: now - 9000, log: [{ k: 'ok', t: '已确认来源：' + o.bv + ' · P1' }, { k: 'ok', t: '找到平台字幕（中文），无需语音识别' }] }
      } else if (o.status === 'failed') {
        v.status = 'failed'; v.pipe = { stage: 'failed', organized: 3, startedAt: now - o.ago, fail: { stage: 'organize', msg: '模型服务暂时不可用（演示）' }, log: [{ k: 'ok', t: '已确认来源：' + o.bv + ' · P1' }, { k: 'ok', t: '找到平台字幕（中文），无需语音识别' }, { k: 'bad', t: '摘要生成失败：模型服务暂时不可用（演示）' }] }
      } else {
        v.status = 'ready'
        v.versions = [{ id: 'v1', n: 1, by: 'agent', label: 'Agent 生成', at: v.createdAt + 90e3, doc: clone(o.doc) }]
        v.cur = 'v1'
        v.pipe = { stage: 'done', startedAt: v.createdAt, doneAt: v.createdAt + 60e3, log: [{ k: 'ok', t: '找到平台字幕（中文），无需语音识别' }, { k: 'ok', t: '摘要已生成（文字）' }, { k: 'info', t: '以讲解为主，未补充画面' }] }
      }
      return v
    })
    Object.entries(D.main.tagging.create).forEach(([id, t]) => { tags[id] = { id, ...clone(t), protected: false } })
    const pg = baseVideo({ id: 'pg', template: 'pg', title: D.main.doc.title, kind: 'bili', up: D.main.up, bv: D.main.bv, part: 1, duration: D.main.duration, createdAt: now - 8 * 60000 })
    pg.status = 'ready'; pg.updatedAt = now - 8 * 60000
    pg.versions = [{ id: 'v1', n: 1, by: 'agent', label: 'Agent 生成', at: now - 7 * 60000, doc: clone(D.main.doc) }]; pg.cur = 'v1'
    pg.figs = Object.fromEntries(Object.keys(D.main.doc.figures).map(id => [id, 'ok']))
    pg.tagLinks = D.main.tagging.links.map(([id, origin]) => ({ id, origin }))
    pg.pipe = { stage: 'done', startedAt: now - 8 * 60000, doneAt: now - 7 * 60000, log: [{ k: 'ok', t: '找到平台字幕（中文），无需语音识别' }, { k: 'ok', t: '已整理 6 章摘要并补充 6 张画面（演示）' }, { k: 'ok', t: '已按已有标签完成归类' }] }
    videos.unshift(pg)
    return {
      v: 1, theme: 'dark', scenario: 'partial', speed: 'normal', reading: { font: 'sans', size: 'm' },
      tags, mergeSuggestions: clone(D.mergeSuggestions), mergedInto: {}, videos, ui: { askPanel: null, mapOpen: {}, view: {}, logOpen: {}, follow: false }
    }
  }
  function baseVideo(o) {
    return Object.assign({
      status: 'processing', pipe: null, prefs: { mode: 'auto', map: 'auto', visual: true, tags: true, importOnly: false },
      versions: [], cur: null, figs: {}, tagLinks: [], rejected: [], suggestion: null,
      chat: { msgs: [], draft: '', annos: [], mode: 'ask', topic: null }, pendingRegen: null, updatedAt: Date.now()
    }, o)
  }
  let S
  try { S = JSON.parse(localStorage.getItem(KEY)) } catch (e) { S = null }
  if (!S || S.v !== 1 || new URLSearchParams(location.search).has('reset')) S = seed()
  let saveTimer
  const save = () => { clearTimeout(saveTimer); saveTimer = setTimeout(() => { try { localStorage.setItem(KEY, JSON.stringify(S)) } catch (e) { } }, 120) }
  const getV = id => S.videos.find(v => v.id === id)
  const curVer = v => v.versions.find(x => x.id === v.cur)
  const curDoc = v => curVer(v)?.doc || null
  const template = v => v.template === 'pg' ? D.main.doc : D.library.find(o => o.id === v.template)?.doc
  const cuesOf = v => v.template === 'pg' ? D.main.cues : fakeCues(v)
  function fakeCues(v) {
    const d = template(v); if (!d) return []
    const out = [{ t: 0, text: '大家好，今天聊聊' + d.mapTitle + '。' }]
    d.blocks.forEach(b => { out.push({ t: b.start, text: '先说' + b.title + '。' }); out.push({ t: b.start + 20, text: plain(b.paras[0].md).slice(0, 34) + '……' }) })
    return out
  }

  /* ---------- Toast ---------- */
  function toast(text, opts = {}) {
    const box = $('#toasts')
    const el = document.createElement('div')
    el.className = 'toast ' + (opts.kind || '')
    el.setAttribute('role', 'status')
    el.innerHTML = ic(opts.kind === 'info' ? 'info' : opts.kind === 'warn' ? 'alert' : 'check') + `<span class="msg-t">${text}</span>` + (opts.action ? `<button class="tbtn">${esc(opts.action.label)}</button>` : '')
    const close = () => { el.classList.add('out'); setTimeout(() => el.remove(), 200) }
    if (opts.action) el.querySelector('.tbtn').onclick = () => { close(); opts.action.fn() }
    box.appendChild(el)
    while (box.children.length > 3) box.firstChild.remove()
    setTimeout(close, opts.ms || (opts.action ? 6500 : 3200))
  }

  /* ---------- Popover & dialog ---------- */
  let popEl = null, popAnchor = null, popClose = null
  function closePop() { if (popEl) { popEl.remove(); popEl = null; popClose && popClose(); popClose = null; popAnchor?.setAttribute('aria-expanded', 'false'); popAnchor = null } }
  function openPop(anchor, html, opts = {}) {
    if (popEl && popAnchor === anchor) { closePop(); return null }
    closePop()
    const el = document.createElement('div')
    el.className = 'popover ' + (opts.cls || '')
    el.setAttribute('role', 'dialog')
    el.innerHTML = html
    document.body.appendChild(el)
    popEl = el; popAnchor = anchor; popClose = opts.onClose
    anchor.setAttribute('aria-expanded', 'true')
    place()
    function place() {
      const r = anchor.getBoundingClientRect(), w = el.offsetWidth, h = el.offsetHeight
      let left = opts.align === 'left' ? r.left : r.right - w
      left = Math.max(12, Math.min(left, innerWidth - w - 12))
      let top = r.bottom + 8
      if (top + h > innerHeight - 12) top = Math.max(12, r.top - h - 8)
      el.style.left = left + 'px'; el.style.top = top + 'px'
    }
    el._place = place
    requestAnimationFrame(() => (el.querySelector('input,textarea') || el.querySelector('button'))?.focus({ preventScroll: true }))
    return el
  }
  document.addEventListener('pointerdown', e => { if (popEl && !popEl.contains(e.target) && !popAnchor?.contains(e.target)) closePop() }, true)

  let dlg = null
  function openDialog(html, opts = {}) {
    closeDialog(true)
    const veil = document.createElement('div'); veil.className = 'veil'
    const el = document.createElement('div')
    el.className = (opts.drawer ? 'drawer ' : 'dialog ') + (opts.cls || ''); el.setAttribute('role', 'dialog'); el.setAttribute('aria-modal', 'true')
    if (opts.drawer) veil.classList.add('veil-drawer')
    if (opts.width) el.style.setProperty('--dw', opts.width + 'px')
    el.innerHTML = html
    document.body.append(veil, el)
    dlg = { el, veil, onClose: opts.onClose, ret: document.activeElement }
    veil.onclick = () => closeDialog()
    requestAnimationFrame(() => (el.querySelector('[autofocus]') || el.querySelector('textarea,input,button'))?.focus())
    return el
  }
  function closeDialog(instant) {
    if (!dlg) return
    const d = dlg; dlg = null
    d.onClose && d.onClose()
    if (instant) { d.el.remove(); d.veil.remove(); return }
    d.el.classList.add('closing'); d.veil.style.transition = 'opacity .18s'; d.veil.style.opacity = '0'
    setTimeout(() => { d.el.remove(); d.veil.remove() }, 180)
    d.ret?.focus?.({ preventScroll: true })
  }
  const dialogEl = () => dlg?.el

  /* ---------- Simulated player ---------- */
  const P = { vid: null, t: 0, playing: false, rate: 1, timer: null }
  function pLoad(v) { if (P.vid !== v.id) { pPause(); P.vid = v.id; P.t = 0 } }
  function pPlay() { const v = getV(P.vid); if (!v) return; if (P.t >= v.duration - 1) P.t = 0; P.playing = true; clearInterval(P.timer); P.timer = setInterval(() => { P.t += .25 * P.rate; if (P.t >= v.duration) { P.t = v.duration; pPause() } pSync() }, 250); pSync() }
  function pPause() { P.playing = false; clearInterval(P.timer); pSync() }
  function pToggle() { P.playing ? pPause() : pPlay() }
  function pSeek(t, play = true) { const v = getV(P.vid); if (!v) return; P.t = Math.max(0, Math.min(t, v.duration)); play ? pPlay() : pSync() }
  function frameAt(v, t) {
    const doc = curDoc(v) || template(v)
    const block = doc.blocks.find(b => t >= b.start && t < b.end) || (t < doc.blocks[0].start ? null : doc.blocks[doc.blocks.length - 1])
    if (!block) return { kind: 'card', over: '开场', title: doc.mapTitle }
    const figs = block.figures.map(f => ({ id: f.id, ...doc.figures[f.id] })).filter(f => f.t <= t + 2 && v.figs[f.id] === 'ok').sort((a, b) => b.t - a.t)
    if (figs[0]) return { kind: 'img', src: figs[0].src, block }
    return { kind: 'card', over: '第 ' + (doc.blocks.indexOf(block) + 1) + ' 章', title: block.title, block }
  }
  function pSync() {
    const v = getV(P.vid); if (!v) return
    const f = frameAt(v, P.t)
    const cues = cuesOf(v); let cue = ''
    for (const c of cues) { if (c.t <= P.t) cue = c.text; else break }
    if (P.t - (cues.filter(c => c.t <= P.t).pop()?.t ?? -99) > 9) cue = ''
    const pct = v.duration ? P.t / v.duration * 100 : 0
    $$('.player[data-vid="' + v.id + '"]').forEach(el => {
      el.classList.toggle('playing', P.playing)
      const img = el.querySelector('.screen img'), card = el.querySelector('.card-frame')
      if (f.kind === 'img') { if (img.getAttribute('src') !== f.src) img.src = f.src; img.style.opacity = 1; card.hidden = true }
      else { img.style.opacity = 0; card.hidden = false; card.innerHTML = `<div><small>${esc(f.over)}</small><b>${esc(f.title)}</b></div>` }
      el.querySelector('.cue').textContent = cue
      el.querySelector('.time').textContent = fmt(P.t) + ' / ' + fmt(v.duration)
      el.querySelector('.fill').style.width = pct + '%'
      el.querySelector('.knob').style.left = pct + '%'
      const scrub = el.querySelector('.scrub'); scrub.setAttribute('aria-valuenow', Math.floor(P.t)); scrub.setAttribute('aria-valuetext', fmt(P.t))
      const tg = el.querySelector('.controls [data-act="p-toggle"]')
      tg.innerHTML = ic(P.playing ? 'pause' : 'play'); tg.setAttribute('aria-label', P.playing ? '暂停' : '播放')
      el.querySelector('.rate').textContent = P.rate + '×'
    })
    const label = f.block ? `<b>${fmt(P.t)}</b> · ${esc(f.block.title)}` : `<b>${fmt(P.t)}</b> · 开场`
    $$('[data-now="' + v.id + '"]').forEach(el => { el.innerHTML = label })
    $$('[data-mbar-toggle="' + v.id + '"]').forEach(el => { el.innerHTML = ic(P.playing ? 'pause' : 'play'); el.setAttribute('aria-label', P.playing ? '暂停' : '播放') })
    $$('.toc button.item').forEach(el => el.classList.toggle('playing', !!f.block && el.dataset.block === f.block.id))
    window.UI?.onPlayerTick?.(v, f)
  }
  function playerHTML(v, opts = {}) {
    const doc = curDoc(v) || template(v)
    const marks = doc.blocks.map(b => `<span class="mark" style="left:${b.start / v.duration * 100}%"></span>`).join('') +
      Object.entries(doc.figures).filter(([id]) => v.figs[id] === 'ok').map(([, f]) => `<span class="mark fig" style="left:${f.t / v.duration * 100}%"></span>`).join('')
    return `<div class="player" data-vid="${v.id}">
      <div class="screen" data-act="p-toggle" role="button" tabindex="-1" aria-label="播放或暂停">
        <img alt="" src="data:image/gif;base64,R0lGODlhAQABAAAAACw=" style="opacity:0"><div class="card-frame"></div>
        <span class="sim-label">模拟播放器 · 演示画面</span>
        <div class="big-play"><span>${ic('play', 'lg')}</span></div>
        <div class="cue"></div>
      </div>
      <div class="controls">
        <button data-act="p-toggle" aria-label="播放">${ic('play')}</button>
        <div class="scrub" role="slider" tabindex="0" aria-label="播放进度" aria-valuemin="0" aria-valuemax="${v.duration}" aria-valuenow="0" data-scrub="${v.id}">
          <span class="rail-bg"></span>${marks}<span class="fill"></span><span class="knob"></span>
        </div>
        <span class="time mono">00:00 / ${fmt(v.duration)}</span>
        <button class="rate" data-act="p-rate" aria-label="播放速度">1×</button>
      </div>
    </div>`
  }
  function bindScrub(root) {
    $$('[data-scrub]', root).forEach(s => {
      if (s._bound) return; s._bound = true
      const v = () => getV(s.dataset.scrub)
      const at = e => { const r = s.getBoundingClientRect(); return Math.max(0, Math.min(1, (e.clientX - r.left) / r.width)) * v().duration }
      s.addEventListener('pointerdown', e => { s.setPointerCapture(e.pointerId); s._drag = true; P.vid = s.dataset.scrub; pSeek(at(e), P.playing) })
      s.addEventListener('pointermove', e => { if (s._drag) pSeek(at(e), P.playing) })
      s.addEventListener('pointerup', () => { s._drag = false })
      s.addEventListener('keydown', e => {
        const d = { ArrowRight: 5, ArrowLeft: -5, ArrowUp: 30, ArrowDown: -30 }[e.key]
        if (d) { e.preventDefault(); pSeek(P.t + d, P.playing) }
        if (e.key === 'Home') { e.preventDefault(); pSeek(0, P.playing) }
        if (e.key === 'End') { e.preventDefault(); pSeek(v().duration, false) }
        if (e.key === ' ' || e.key === 'k') { e.preventDefault(); pToggle() }
      })
    })
  }

  /* ---------- Tags ---------- */
  const norm = s => String(s).normalize('NFKC').trim().replace(/\s+/g, ' ').toLowerCase()
  function findTag(name) { const n = norm(name); return Object.values(S.tags).find(t => norm(t.name) === n || t.aliases.some(a => norm(a) === n)) }
  const resolveTag = id => { let x = id, i = 0; while (S.mergedInto[x] && i++ < 10) x = S.mergedInto[x]; return S.tags[x] ? x : null }
  function tagCount(id) { return S.videos.filter(v => v.tagLinks.some(l => l.id === id)).length }
  function addTag(v, name) {
    name = name.trim(); if (!name) return null
    if (name.length > 80) { toast('标签名称最多 80 个字', { kind: 'warn' }); return null }
    let t = findTag(name), created = false
    if (!t) { const id = uid('t-'); t = S.tags[id] = { id, name, origin: 'manual', aliases: [], protected: true }; created = true }
    if (v.tagLinks.some(l => l.id === t.id)) { toast('已经有「' + esc(t.name) + '」', { kind: 'info' }); return t }
    if (v.tagLinks.length >= 20) { toast('每个视频最多 20 个标签', { kind: 'warn' }); return null }
    v.tagLinks.push({ id: t.id, origin: 'manual', fresh: true })
    v.rejected = v.rejected.filter(x => x !== t.id)
    t.protected = true
    save()
    toast(created ? '已新建标签「' + esc(t.name) + '」' : (norm(t.name) !== norm(name) ? '「' + esc(name) + '」是「' + esc(t.name) + '」的别名，已使用已有标签' : '已添加「' + esc(t.name) + '」'))
    return t
  }
  function removeTag(v, id) {
    const l = v.tagLinks.find(x => x.id === id); if (!l) return
    v.tagLinks = v.tagLinks.filter(x => x.id !== id)
    if (l.origin === 'auto') { if (!v.rejected.includes(id)) v.rejected.push(id) }
    save()
    return l
  }
  function keepTag(v, id) { const l = v.tagLinks.find(x => x.id === id); if (l) { l.origin = 'manual'; S.tags[id].protected = true; save() } }
  function autoCandidates(v) { return v.template === 'pg' ? D.main.tagging.links : (D.library.find(o => o.id === v.template)?.autoTags || D.library.find(o => o.id === v.template)?.tags.filter(t => t[1] === 'auto') || []) }
  function applyTagging(v, rerun) {
    if (v.template === 'pg') Object.entries(D.main.tagging.create).forEach(([id, t]) => { if (!S.tags[id] && !findTag(t.name)) S.tags[id] = { id, ...clone(t), protected: false } })
    if (v.template === 'vite' || v.template === 'loom') Object.entries(D.extraTags).forEach(([id, t]) => { if (!S.tags[id] && !findTag(t.name)) S.tags[id] = { id, ...clone(t), protected: false } })
    let added = 0, skipped = 0, kept = v.tagLinks.filter(l => l.origin === 'manual').length
    autoCandidates(v).forEach(([cid]) => {
      const id = resolveTag(cid); if (!id) return
      if (v.rejected.includes(id)) { skipped++; return }
      if (v.tagLinks.some(l => l.id === id)) return
      if (v.tagLinks.length >= 20) return
      v.tagLinks.push({ id, origin: 'auto', fresh: true }); added++
    })
    if (v.template === 'pg' && !rerun) v.suggestion = D.main.tagging.suggestion
    save()
    return { added, skipped, kept }
  }
  function renameTag(id, name) {
    name = name.trim(); const t = S.tags[id]
    if (!name || name === t.name) return true
    const other = findTag(name)
    if (other && other.id !== id) { toast('「' + esc(name) + '」已被「' + esc(other.name) + '」使用，可以改用合并', { kind: 'warn' }); return false }
    if (!t.aliases.some(a => norm(a) === norm(t.name))) t.aliases.push(t.name)
    t.aliases = t.aliases.filter(a => norm(a) !== norm(name))
    t.name = name; t.protected = true; save(); return true
  }
  function mergeTags(from, to) {
    const f = S.tags[from], t = S.tags[to]; if (!f || !t) return
    S.videos.forEach(v => {
      const lf = v.tagLinks.find(l => l.id === from); if (!lf) return
      const lt = v.tagLinks.find(l => l.id === to)
      if (lt) { if (lf.origin === 'manual') lt.origin = 'manual' } else v.tagLinks.push({ id: to, origin: lf.origin })
      v.tagLinks = v.tagLinks.filter(l => l.id !== from)
      v.rejected = v.rejected.map(x => x === from ? to : x)
    })
    t.aliases = Array.from(new Set([...t.aliases, f.name, ...f.aliases]))
    delete S.tags[from]; S.mergedInto[from] = to
    S.mergeSuggestions = S.mergeSuggestions.filter(m => m.from !== from && m.to !== from)
    save()
  }
  function deleteTag(id) { S.videos.forEach(v => { v.tagLinks = v.tagLinks.filter(l => l.id !== id); v.rejected = v.rejected.filter(x => x !== id) }); delete S.tags[id]; save() }

  /* ---------- Pipeline simulation ---------- */
  const STAGES = v => [
    { key: 'receive', label: v.kind === 'file' ? '上传文件' : '接收视频' },
    { key: 'source', label: v.kind === 'file' ? '识别语音' : '读取字幕' },
    { key: 'organize', label: '整理内容' },
    { key: 'visual', label: '补充画面' },
    { key: 'tags', label: '归类' }
  ]
  const ORDER = ['receive', 'source', 'organize', 'visual', 'tags', 'done']
  const timers = {}
  const wantsVisual = v => v.prefs.visual && v.prefs.mode !== 'text' && Object.keys(template(v).figures).length > 0
  const factor = v => (S.speed === 'fast' ? .4 : 1) * (v.template === 'vite' ? 3.2 : 1)
  function emit(v, parts) { v.updatedAt = Date.now(); save(); window.UI?.onVideo?.(v, parts) }
  const log = (v, k, t) => v.pipe.log.push({ k, t, at: Date.now() })
  // These are local fixture events. Internal job stages are not UI activity rows.
  function activityBegin(v, key, title, detail = '') {
    const A = window.ActivityView, p = v.pipe
    if (!A || !p) return
    p.activity ||= A.create('summary')
    p.activityIds ||= {}
    const previous = p.activity.steps.find(s => s.id === p.activityIds[key])
    if (previous?.status === 'running') return
    p.activity.status = 'running'
    p.activityIds[key] = A.start(p.activity, title, { detail })
  }
  function activityEnd(v, key, status = 'done', detail) {
    const p = v.pipe
    if (p?.activityIds?.[key]) window.ActivityView.update(p.activity, p.activityIds[key], status, detail)
  }
  function startPipe(v) {
    v.status = 'processing'
    v.pipe = { stage: 'receive', organized: 0, upload: 0, startedAt: Date.now(), log: [] }
    save(); runPipe(v)
  }
  function runPipe(v) {
    clearTimeout(timers[v.id])
    const p = v.pipe, k = factor(v)
    const go = (ms, fn) => { timers[v.id] = setTimeout(() => { if (getV(v.id) !== v || v.pipe !== p) return; fn(); }, ms * k) }
    const doc = template(v)
    if (p.stage === 'receive') {
      activityBegin(v, 'receive', v.kind === 'file' ? '接收本地视频文件' : '确认视频来源与分 P')
      if (v.kind === 'file' && p.upload < 100) return go(320, () => { p.upload = Math.min(100, p.upload + 17 + Math.round(Math.random() * 8)); emit(v, ['progress', 'card']); runPipe(v) })
      return go(900, () => { activityEnd(v, 'receive'); log(v, 'ok', v.kind === 'file' ? '文件已接收：' + v.file + '（演示，未真正上传）' : '已确认来源：' + v.bv + ' · P' + (v.part || 1) + ' · ' + fmt(v.duration)); p.stage = 'source'; runPipe(v); emit(v, ['progress', 'card']) })
    }
    if (p.stage === 'source') {
      activityBegin(v, 'source', v.kind === 'file' ? '识别视频中的语音' : '读取中文平台字幕', v.kind === 'file' ? '本地文件没有平台字幕，使用模拟转写' : '这段演示找到可用字幕，跳过语音识别')
      return go(v.kind === 'file' ? 2600 : 1500, () => {
      activityEnd(v, 'source')
      log(v, 'ok', v.kind === 'file' ? '语音识别完成（本地文件使用语音识别）' : '找到平台字幕（中文），无需语音识别')
      if (v.prefs.importOnly && !p.forceGenerate) { p.stage = 'idle'; v.status = 'idle'; window.ActivityView.finish(p.activity); log(v, 'info', '已按你的选择仅导入，摘要尚未生成'); emit(v, ['progress', 'doc', 'card']); return }
      p.stage = 'organize'; p.organized = 0; runPipe(v); emit(v, ['progress', 'doc', 'card'])
    }) }
    if (p.stage === 'organize') {
      if (p.organized < doc.blocks.length) {
        const n = p.organized, block = doc.blocks[n]
        activityBegin(v, 'chapter:' + n, '整理「' + block.title + '」', v.focus ? '结合你的关注点：' + v.focus : '从带时间的文字中整理本章要点')
        return go(p.retried ? 300 : 560, () => { activityEnd(v, 'chapter:' + n); p.organized++; runPipe(v); emit(v, ['progress', 'doc', 'card']) })
      }
      activityBegin(v, 'publish', '整合概览与关键结论')
      return go(800, () => {
        if (S.scenario === 'fail' && !p.retried && v.template === 'pg') {
          p.stage = 'failed'; v.status = 'failed'; p.fail = { stage: 'organize', msg: '模型服务暂时不可用（演示）' }
          activityEnd(v, 'publish', 'error', p.fail.msg); window.ActivityView.finish(p.activity, 'error')
          log(v, 'bad', '摘要生成失败：模型服务暂时不可用（演示）'); emit(v, ['progress', 'doc', 'card', 'all']); return
        }
        publishText(v)
        activityEnd(v, 'publish')
        p.stage = wantsVisual(v) ? 'visual' : 'tags'
        if (!wantsVisual(v)) log(v, 'info', v.prefs.mode === 'text' || !v.prefs.visual ? '按你的选择生成文字摘要，未补充画面' : '以讲解为主，未补充画面')
        runPipe(v); emit(v, ['progress', 'doc', 'card', 'side', 'tags'])
        window.UI?.onTextReady?.(v)
      })
    }
    if (p.stage === 'visual') {
      const order = doc.blocks.flatMap(b => b.figures.map(f => f.id))
      const next = order.find(id => v.figs[id] === 'pending')
      if (!next) return go(500, () => {
        const ok = order.filter(id => v.figs[id] === 'ok').length, bad = order.filter(id => v.figs[id] === 'failed').length
        log(v, bad ? 'warn' : 'ok', `检查了 ${order.length + 4} 个候选画面，选用 ${ok} 张${bad ? '，' + bad + ' 张暂时没有补充成功' : ''}`)
        if (curVer(v)?.by === 'revise') log(v, 'info', '画面已补充到你修改后的版本，正文保持你的修改')
        p.stage = v.prefs.tags ? 'tags' : 'done'; runPipe(v); emit(v, ['progress', 'card', 'side'])
      })
      const fig = doc.figures[next], block = doc.blocks.find(b => b.figures.some(f => f.id === next))
      activityBegin(v, 'visual:' + next, '查看 ' + fmt(fig.t) + ' 的候选画面', block.title + ' · 检查清晰度与段落相关性')
      v.figs[next] = 'loading'; emit(v, ['fig:' + next, 'progress'])
      return go(1150, () => {
        v.figs[next] = (S.scenario === 'partial' && next === 'f4' && v.template === 'pg') ? 'failed' : 'ok'
        activityEnd(v, 'visual:' + next, v.figs[next] === 'failed' ? 'error' : 'done', v.figs[next] === 'failed' ? '画面读取超时，可稍后单独重试' : '选定截图，附到「' + block.title + '」')
        runPipe(v); emit(v, ['fig:' + next, 'progress', 'side', 'card'])
      })
    }
    if (p.stage === 'tags') {
      if (!v.prefs.tags) { p.stage = 'done'; return runPipe(v) }
      activityBegin(v, 'tags', '复用已有标签并整理分类', '根据视频内容匹配词表，归一明确别名')
      return go(1300, () => {
        applyTagging(v)
        activityEnd(v, 'tags')
        if (v.template === 'pg') D.main.tagging.log.forEach(l => log(v, l.kind === 'skip' ? 'info' : 'ok', l.text))
        else log(v, 'ok', '归类完成：' + v.tagLinks.map(l => S.tags[l.id]?.name).filter(Boolean).join('、'))
        p.stage = 'done'; emit(v, ['progress', 'tags', 'card']); runPipe(v)
      })
    }
    if (p.stage === 'done') {
      p.doneAt = p.doneAt || Date.now()
      v.status = Object.values(v.figs).includes('failed') ? 'partial' : 'ready'
      if (p.activity) window.ActivityView.finish(p.activity)
      emit(v, ['progress', 'card', 'side'])
      window.UI?.onDone?.(v)
    }
  }
  function publishText(v) {
    const doc = clone(template(v))
    doc.focus = v.focus || ''
    if (v.kind === 'file' && v.template === 'pg') doc.source = 'file'
    v.versions = [{ id: 'v1', n: 1, by: 'agent', label: 'Agent 生成', at: Date.now(), doc }]
    v.cur = 'v1'
    v.figs = {}
    doc.blocks.forEach(b => b.figures.forEach(f => { v.figs[f.id] = wantsVisual(v) ? 'pending' : 'skipped' }))
    v.status = 'text_ready'
    if (v.kind === 'file') v.title = doc.title
    log(v, 'ok', '摘要已可阅读：' + doc.blocks.length + ' 个章节')
  }
  function retryPipe(v) {
    if (v.pipe.stage === 'failed') { v.pipe.retried = true; v.pipe.stage = 'organize'; v.pipe.organized = 0; v.pipe.fail = null; v.status = 'processing'; log(v, 'info', '重新生成摘要：复用已保存的视频和字幕'); emit(v, ['progress', 'doc', 'card']); runPipe(v) }
    else if (v.pipe.stage === 'idle') { v.pipe.forceGenerate = true; v.pipe.stage = 'organize'; v.pipe.organized = 0; v.status = 'processing'; emit(v, ['progress', 'doc', 'card']); runPipe(v) }
  }
  function retryFig(v, id) {
    const currentPipe = v.pipe
    activityBegin(v, 'retry:' + id, '重试 ' + fmt((curDoc(v) || template(v)).figures[id].t) + ' 的画面', '复用文字摘要，只重新补充这张图')
    v.figs[id] = 'loading'; emit(v, ['fig:' + id, 'progress'])
    setTimeout(() => {
      if (getV(v.id) !== v || v.pipe !== currentPipe || v.figs[id] !== 'loading') return
      v.figs[id] = 'ok'
      activityEnd(v, 'retry:' + id)
      log(v, 'ok', '重试成功：图 ' + figNo(v, id) + ' 已补充')
      if (v.pipe.stage === 'done') { v.status = Object.values(v.figs).includes('failed') ? 'partial' : 'ready'; window.ActivityView.finish(v.pipe.activity) }
      emit(v, ['fig:' + id, 'progress', 'side', 'card'])
      toast('画面已补充：图 ' + figNo(v, id))
    }, 1300 * (S.speed === 'fast' ? .5 : 1))
  }
  function skipFig(v, id) {
    v.figs[id] = 'skipped'; log(v, 'info', '已跳过图 ' + figNo(v, id) + '，正文不受影响')
    if (v.pipe.stage === 'done') v.status = Object.values(v.figs).includes('failed') ? 'partial' : 'ready'
    emit(v, ['fig:' + id, 'progress', 'side', 'card'])
  }
  function figOrder(doc) { return doc.blocks.flatMap(b => b.figures.map(f => f.id)) }
  function figNo(v, id) { return figOrder(curDoc(v) || template(v)).indexOf(id) + 1 }
  function resumeAll() {
    S.videos.forEach(v => {
      // A browser reload interrupts fixture timers; do not show stale running rows.
      if (v.pipe?.activity?.status === 'running') window.ActivityView.finish(v.pipe.activity, 'cancelled')
      Object.keys(v.figs).forEach(id => { if (v.figs[id] === 'loading') { v.figs[id] = v.pipe?.stage === 'done' ? 'failed' : 'pending'; if (v.pipe?.stage === 'done') v.status = 'partial' } })
      if (v.status === 'processing' || v.status === 'text_ready') runPipe(v)
    })
    save()
  }

  /* ---------- Revisions ---------- */
  function findPara(doc, paraId) {
    if (paraId === 'overview') return { block: null, para: { id: 'overview', md: doc.overview } }
    for (const b of doc.blocks) { const p = b.paras.find(x => x.id === paraId); if (p) return { block: b, para: p } }
    return null
  }
  function stepify(mdText) {
    const head = (mdText.match(/^\*\*[^*]+\*\*：?/) || [''])[0]
    const body = plain(mdText.slice(head.length))
    const parts = body.split(/[。；]/).map(s => s.trim()).filter(s => s.length > 3)
    if (parts.length < 2) return null
    return (head ? head.replace(/：$/, '') + '\n\n' : '') + parts.map((s, i) => (i + 1) + '. ' + s + '。').join('\n')
  }
  function condense(mdText) { const s = plain(mdText).split('。').filter(Boolean); return s.length > 1 ? s.slice(0, Math.max(1, Math.ceil(s.length / 2))).join('。') + '。' : null }
  function planRevision(v, scope, req) {
    const base = curDoc(v), doc = clone(base)
    const kind = /步骤|照着|操作|一步|清单|流程/.test(req) ? 'steps' : /简洁|精简|简短|短一|压缩|精炼/.test(req) ? 'concise' : 'other'
    const R = D.revisions
    let targets
    if (scope.kind === 'doc') targets = kind === 'steps' ? Object.keys(R.steps) : kind === 'concise' ? Object.keys(R.concise) : doc.blocks.map(b => b.paras[0].id)
    else if (scope.kind === 'block') targets = scope.blockId === 'overview' ? ['overview'] : doc.blocks.find(b => b.id === scope.blockId).paras.map(p => p.id)
    else targets = [scope.paraId]
    const changes = [], impact = [], mapChanges = []
    let generic = false
    targets.forEach(pid => {
      const hit = findPara(doc, pid); if (!hit) return
      const before = hit.para.md
      let after = null, concept = null
      if (v.template === 'pg' && kind !== 'other' && R[kind][pid]) { after = R[kind][pid].md; concept = R[kind][pid].concept }
      else if (kind === 'concise') { after = condense(before); generic = true }
      else { after = stepify(before); generic = true }
      if (!after || after === before) return
      if (pid === 'overview') doc.overview = after; else hit.para.md = after
      if (concept && hit.block) { const c = hit.block.concepts.find(x => x.id === concept.id); if (c) { mapChanges.push({ from: c.label, to: concept.label }); c.label = concept.label; c.changed = true } }
      changes.push({ pid, blockId: hit.block?.id || 'overview', blockTitle: hit.block?.title || '概览', chNo: hit.block ? doc.blocks.indexOf(hit.block) + 1 : 0, before, after })
    })
    if (!changes.length) return { empty: true }
    const blocks = new Set(changes.map(c => c.blockId))
    impact.push({ icon: 'text', t: `正文：${changes.length} 段，涉及 ${blocks.size} 个章节` })
    const figs = changes.flatMap(c => { const b = doc.blocks.find(x => x.id === c.blockId); return b ? b.figures.filter(f => f.after === c.pid).map(f => f.id) : [] })
    impact.push({ icon: 'photo', t: figs.length ? `画面：图 ${figs.map(id => figOrder(doc).indexOf(id) + 1).join('、')} 保持在原段之后` : '画面：不受影响' })
    impact.push({ icon: 'map', t: mapChanges.length ? '导图：' + mapChanges.map(m => `「${m.from}」→「${m.to}」`).join('，') : '导图：结构不变' })
    impact.push({ icon: 'clock', t: '回放时间：保持不变' })
    return { doc, changes, impact, generic, kind }
  }
  function applyRevision(v, plan, req) {
    const prev = curVer(v)
    const n = Math.max(...v.versions.map(x => x.n)) + 1
    v.versions = v.versions.filter(x => !x.undone)
    const ver = { id: 'v' + n, n, by: 'revise', label: '按你的要求修改', note: req, at: Date.now(), doc: plan.doc, changed: plan.changes.map(c => c.pid), base: prev.id }
    v.versions.push(ver); v.cur = ver.id
    save()
    return ver
  }
  function undoRevision(v) {
    const ver = curVer(v); if (!ver || ver.by === 'agent' && !ver.base) return null
    const base = v.versions.find(x => x.id === ver.base) || v.versions[v.versions.indexOf(ver) - 1]
    if (!base) return null
    ver.undone = true; v.cur = base.id; save()
    return base
  }
  function regenerate(v) {
    const doc = clone(template(v)); doc.focus = v.focus || ''
    Object.entries(D.regenerated.patch).forEach(([pid, mdText]) => { const h = findPara(doc, pid); if (h) h.para.md = mdText })
    if (curVer(v)?.by === 'revise') { v.pendingRegen = { doc, note: D.regenerated.note, at: Date.now() }; save(); return 'pending' }
    const n = Math.max(...v.versions.map(x => x.n)) + 1
    v.versions.push({ id: 'v' + n, n, by: 'agent', label: 'Agent 新原稿', note: D.regenerated.note, at: Date.now(), doc, base: v.cur, changed: Object.keys(D.regenerated.patch) })
    v.cur = 'v' + n; save(); return 'applied'
  }
  function adoptRegen(v) {
    const r = v.pendingRegen; if (!r) return
    const n = Math.max(...v.versions.map(x => x.n)) + 1
    v.versions.push({ id: 'v' + n, n, by: 'agent', label: '采用 Agent 新原稿', note: r.note, at: Date.now(), doc: r.doc, base: v.cur, changed: Object.keys(D.regenerated.patch) })
    v.cur = 'v' + n; v.pendingRegen = null; save()
  }

  /* ---------- Answers ---------- */
  function answer(v, q, annos) {
    const doc = curDoc(v)
    const chat = v.chat
    if (v.template === 'pg') {
      const follow = !annos.length && (/^(那|这个|它|这|然后|具体|还有|怎么设置|如何设置)/.test(q.trim()) || (q.trim().length <= 8 && chat.topic))
      if (follow && chat.topic) { const a = D.answers.find(x => x.topic === chat.topic); if (a) return { text: a.follow, topic: a.topic, sugg: a.suggestions.filter(s => !/怎么设置/.test(s)), follow: true } }
      const byPara = { c5p2: 'prepared', c5p1: 'maxclient', c5p3: 'waiting', c4p1: 'poolsize', c4p2: 'poolsize', c4p3: 'poolsize', c3p1: 'mode', c3p2: 'mode' }
      const annoTopic = annos.map(x => byPara[x.paraId]).find(Boolean)
      const hit = D.answers.find(a => a.match.test(q)) || D.answers.find(a => a.topic === annoTopic) || D.answers.find(a => annos.some(x => a.match.test(x.quote)))
      if (hit) return { text: hit.text, topic: hit.topic, sugg: hit.suggestions }
    }
    if (annos.length) {
      const a = annos[0], h = findPara(doc, a.paraId) || {}
      const t = h.para?.t || doc.blocks.find(b => b.id === a.blockId)?.start || 0
      return { text: `你选中的内容出自「${a.blockTitle}」。这段的要点是：${plain(h.para?.md || a.quote).split('。').slice(0, 2).join('。')}。{{t:${t}}}\n\n关于「${q.slice(0, 40)}」，视频没有展开更多细节。可以回放上面的片段核对原话，或换一种问法。`, topic: null, sugg: D.suggestions.slice(0, 2) }
    }
    const words = q.replace(/[？?。，,！!]/g, ' ').split(/\s+/).filter(w => w.length > 1)
    const best = doc.blocks.map(b => ({ b, s: words.filter(w => (b.title + plain(b.paras.map(p => p.md).join(''))).includes(w)).length })).sort((x, y) => y.s - x.s)[0]
    return { text: `视频里没有直接回答这个问题。和它最相关的是「${best.b.title}」这一章：${plain(best.b.paras[0].md).split('。')[0]}。{{t:${best.b.start}}}\n\n如果你想了解某一段的细节，可以在左侧摘要中选中那段文字，再继续提问。`, topic: null, sugg: v.template === 'pg' ? D.suggestions : [] }
  }

  window.Core = {
    D, S: () => S, setS: s => { S = s }, seed, save, $, $$, clone, esc, fmt, ago, clock, isMobile, uid, sprite, ic, brandMark, md, inline, plain, timeBtn,
    getV, curVer, curDoc, template, cuesOf, baseVideo, toast, openPop, closePop, openDialog, closeDialog, dialogEl,
    P, pLoad, pPlay, pPause, pToggle, pSeek, pSync, playerHTML, bindScrub,
    norm, findTag, resolveTag, tagCount, addTag, removeTag, keepTag, applyTagging, renameTag, mergeTags, deleteTag,
    STAGES, ORDER, startPipe, runPipe, retryPipe, retryFig, skipFig, resumeAll, figOrder, figNo, wantsVisual, timers,
    findPara, planRevision, applyRevision, undoRevision, regenerate, adoptRegen, answer, KEY
  }
})()
